package webhooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deecaulcrick/neobank/backend/internal/accounts"
	"github.com/deecaulcrick/neobank/backend/internal/crypto"
	"github.com/deecaulcrick/neobank/backend/internal/payouts"
	"github.com/deecaulcrick/neobank/backend/internal/swaps"
)

// Event is one stored webhook. Handlers dispatch on Event (the event name),
// never on data.status: the two use different vocabularies for payouts.
type Event struct {
	ID      string
	Event   string
	Payload json.RawMessage
}

// Handler applies an event inside tx. Post the journal entry with
// idempotency_key = ev.ID and update the payout/trade/deposit row in the
// same transaction; the processor marks the event processed on commit.
type Handler func(ctx context.Context, tx pgx.Tx, ev Event) error

// ErrNotImplemented keeps an event queued (with backoff) until its handler exists.
var ErrNotImplemented = errors.New("webhooks: handler not implemented")

type Processor struct {
	pool     *pgxpool.Pool
	log      *slog.Logger
	handlers map[string]Handler
}

func NewProcessor(pool *pgxpool.Pool, log *slog.Logger) *Processor {
	p := &Processor{pool: pool, log: log, handlers: map[string]Handler{}}

	// M1
	p.Handle("virtual_account.deposit.success", ngnDeposit)

	// M2
	p.Handle("trade.completed", trade)
	p.Handle("trade.failed", trade)
	// M3
	p.Handle("payouts.initialized", payout)
	p.Handle("payouts.processing", payout)
	p.Handle("payouts.withdrawal.success", payout)
	p.Handle("payouts.withdrawal.expired", payout)
	// M4
	p.Handle("deposit.success", onchain)
	p.Handle("transfer.success", onchain)
	p.Handle("transfer.failed", onchain)
	return p
}

func (p *Processor) Handle(event string, h Handler) { p.handlers[event] = h }

// onchain credits a crypto deposit, or settles or releases a withdrawal.
func onchain(ctx context.Context, tx pgx.Tx, ev Event) error {
	return crypto.ApplyWebhook(ctx, tx, ev.Event, ev.Payload)
}

// payout advances, settles or releases a payout by event name.
func payout(ctx context.Context, tx pgx.Tx, ev Event) error {
	return payouts.ApplyWebhook(ctx, tx, ev.Event, ev.Payload)
}

// trade settles or releases a swap. Like ngnDeposit, it has not yet seen a
// real delivery; the sweeper covers it if the payload shape is off.
func trade(ctx context.Context, tx pgx.Tx, ev Event) error {
	return swaps.ApplyWebhook(ctx, tx, ev.Event, ev.Payload)
}

// ngnDeposit credits a bank transfer into a user's virtual account.
// Payload fields are from Bitnob's docs; the sandbox could not deliver one to
// a local machine, so this path is untested against a real delivery.
func ngnDeposit(ctx context.Context, tx pgx.Tx, ev Event) error {
	var body struct {
		Payload struct {
			Amount              string `json:"amount"` // kobo
			Currency            string `json:"currency"`
			VirtualAccountID    string `json:"virtual_account_id"`
			LedgerTransactionID string `json:"ledger_transaction_id"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(ev.Payload, &body); err != nil {
		return err
	}
	p := body.Payload
	if p.Currency != "NGN" {
		return fmt.Errorf("webhooks: deposit in unexpected currency %q", p.Currency)
	}
	kobo, err := strconv.ParseInt(p.Amount, 10, 64)
	if err != nil {
		return fmt.Errorf("webhooks: deposit amount %q", p.Amount)
	}
	return accounts.ApplyDeposit(ctx, tx, accounts.Deposit{
		LedgerTransactionID: p.LedgerTransactionID,
		BitnobAccountID:     p.VirtualAccountID,
		AmountKobo:          kobo,
		Raw:                 ev.Payload,
	})
}

// Run drains the queue until ctx is cancelled.
func (p *Processor) Run(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		for {
			worked, err := p.processOne(ctx)
			if err != nil && ctx.Err() == nil {
				p.log.Error("webhook processor", "err", err)
			}
			if !worked || err != nil {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// processOne claims one due event with SKIP LOCKED so several workers can run
// side by side, and reports whether there was anything to do.
func (p *Processor) processOne(ctx context.Context) (bool, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	var (
		rowID int64
		ev    Event
	)
	err = tx.QueryRow(ctx,
		`select id, event_id, event, payload from webhook_events
		 where processed_at is null and next_attempt_at <= now()
		 order by id limit 1
		 for update skip locked`,
	).Scan(&rowID, &ev.ID, &ev.Event, &ev.Payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	h, ok := p.handlers[ev.Event]
	if !ok {
		// Unknown event types are kept for ops but must not block the queue.
		p.log.Warn("no handler for webhook event", "event", ev.Event, "event_id", ev.ID)
		_, err := tx.Exec(ctx,
			`update webhook_events set processed_at = now(), last_error = 'no handler' where id = $1`, rowID)
		if err != nil {
			return false, err
		}
		return true, tx.Commit(ctx)
	}

	// The handler runs in a savepoint so its failure doesn't lose our claim.
	handlerErr := pgx.BeginFunc(ctx, tx, func(sp pgx.Tx) error { return h(ctx, sp, ev) })
	if handlerErr != nil {
		if !errors.Is(handlerErr, ErrNotImplemented) {
			p.log.Error("webhook handler failed", "event", ev.Event, "event_id", ev.ID, "err", handlerErr)
		}
		// Exponential backoff from 10s, capped at 1h.
		_, err := tx.Exec(ctx,
			`update webhook_events
			 set attempts = attempts + 1,
			     last_error = $2,
			     next_attempt_at = now() + least(interval '1 hour', interval '10 seconds' * power(2, least(attempts, 10)))
			 where id = $1`, rowID, handlerErr.Error())
		if err != nil {
			return false, err
		}
		return true, tx.Commit(ctx)
	}

	if _, err := tx.Exec(ctx,
		`update webhook_events set processed_at = now(), attempts = attempts + 1, last_error = null where id = $1`,
		rowID); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
