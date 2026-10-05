// Package ledger is the double-entry ledger: the source of truth for every
// balance a user sees. Bitnob balances are what we reconcile against.
//
// Sign convention: a Line amount > 0 is a credit, < 0 is a debit, and every
// entry sums to zero per asset. See the migration for which accounts are
// credit-normal.
package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrInsufficientFunds means a posting would overdraw a user account.
	ErrInsufficientFunds = errors.New("ledger: insufficient funds")
	ErrUnbalanced        = errors.New("ledger: entry does not sum to zero per asset")
	ErrUnknownAccount    = errors.New("ledger: unknown account")
)

type Line struct {
	Account string // ledger_accounts.code
	Amount  int64  // minor units; credit > 0, debit < 0
}

func Debit(account string, amount int64) Line  { return Line{Account: account, Amount: -amount} }
func Credit(account string, amount int64) Line { return Line{Account: account, Amount: amount} }

// Entry is one business event. IdempotencyKey is the webhook event_id for
// webhook-driven postings, or a caller-supplied key for user actions.
type Entry struct {
	IdempotencyKey string
	Kind           string
	Description    string
	Metadata       map[string]any
	Lines          []Line
}

// Result reports the journal entry and whether this call created it.
// Posted is false when the idempotency key had already been used.
type Result struct {
	EntryID uuid.UUID
	Posted  bool
}

// Post runs PostTx in its own transaction.
func Post(ctx context.Context, pool *pgxpool.Pool, e Entry) (Result, error) {
	var res Result
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var err error
		res, err = PostTx(ctx, tx, e)
		return err
	})
	return res, translate(err)
}

// PostTx writes the entry, its postings and the cached balances inside the
// caller's transaction, so the payout/trade/deposit row can be updated
// atomically with the money movement.
func PostTx(ctx context.Context, tx pgx.Tx, e Entry) (Result, error) {
	if e.IdempotencyKey == "" || e.Kind == "" {
		return Result{}, errors.New("ledger: idempotency key and kind are required")
	}
	if len(e.Lines) < 2 {
		return Result{}, errors.New("ledger: an entry needs at least two lines")
	}
	for _, l := range e.Lines {
		if l.Amount == 0 {
			return Result{}, fmt.Errorf("ledger: zero amount on %s", l.Account)
		}
	}

	meta := []byte("{}")
	if e.Metadata != nil {
		var err error
		if meta, err = json.Marshal(e.Metadata); err != nil {
			return Result{}, fmt.Errorf("ledger: metadata: %w", err)
		}
	}

	var res Result
	err := tx.QueryRow(ctx,
		`insert into journal_entries (idempotency_key, kind, description, metadata)
		 values ($1, $2, nullif($3, ''), $4)
		 on conflict (idempotency_key) do nothing
		 returning id`,
		e.IdempotencyKey, e.Kind, e.Description, meta,
	).Scan(&res.EntryID)
	if errors.Is(err, pgx.ErrNoRows) {
		// Already posted: report the original entry and change nothing.
		err = tx.QueryRow(ctx,
			`select id from journal_entries where idempotency_key = $1`, e.IdempotencyKey,
		).Scan(&res.EntryID)
		return res, err
	}
	if err != nil {
		return Result{}, err
	}

	// Lock the accounts in a stable order so concurrent entries can't deadlock.
	codes := make([]string, 0, len(e.Lines))
	seen := map[string]bool{}
	for _, l := range e.Lines {
		if !seen[l.Account] {
			seen[l.Account] = true
			codes = append(codes, l.Account)
		}
	}
	type account struct {
		id    uuid.UUID
		asset string
	}
	accounts := map[string]account{}
	rows, err := tx.Query(ctx,
		`select code, id, asset::text from ledger_accounts
		 where code = any($1) order by id for update`, codes)
	if err != nil {
		return Result{}, err
	}
	for rows.Next() {
		var code string
		var a account
		if err := rows.Scan(&code, &a.id, &a.asset); err != nil {
			rows.Close()
			return Result{}, err
		}
		accounts[code] = a
	}
	if err := rows.Err(); err != nil {
		return Result{}, err
	}

	sums := map[string]int64{}
	deltas := map[uuid.UUID]int64{}
	batch := &pgx.Batch{}
	for _, l := range e.Lines {
		a, ok := accounts[l.Account]
		if !ok {
			return Result{}, fmt.Errorf("%w: %s", ErrUnknownAccount, l.Account)
		}
		sums[a.asset] += l.Amount
		deltas[a.id] += l.Amount
		batch.Queue(
			`insert into postings (entry_id, account_id, asset, amount) values ($1, $2, $3, $4)`,
			res.EntryID, a.id, a.asset, l.Amount,
		)
	}
	// The database enforces this too (deferred trigger); failing here gives
	// a clearer error before anything is written.
	for asset, sum := range sums {
		if sum != 0 {
			return Result{}, fmt.Errorf("%w: %s off by %d", ErrUnbalanced, asset, sum)
		}
	}
	for id, delta := range deltas {
		if delta != 0 {
			batch.Queue(`update ledger_accounts set balance = balance + $2 where id = $1`, id, delta)
		}
	}
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return Result{}, translate(err)
	}

	res.Posted = true
	return res, nil
}

// translate maps the balance >= 0 check on user accounts to ErrInsufficientFunds.
func translate(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23514" && pgErr.TableName == "ledger_accounts" {
		return ErrInsufficientFunds
	}
	return err
}
