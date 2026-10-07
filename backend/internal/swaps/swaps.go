// Package swaps covers M2: quote, then execute, a trade between any two of
// the assets we hold. Every direction is a single Bitnob trade.
package swaps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deecaulcrick/neobank/backend/internal/bitnob"
	"github.com/deecaulcrick/neobank/backend/internal/ledger"
	"github.com/deecaulcrick/neobank/backend/internal/money"
	"github.com/deecaulcrick/neobank/backend/internal/notify"
)

type Service struct {
	Pool   *pgxpool.Pool
	Bitnob *bitnob.Client
	// FeeBps is our margin, in basis points of what the user receives.
	FeeBps int64
	Log    *slog.Logger
}

// ValidationError is safe to show to the user as-is.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

var (
	ErrQuoteNotFound = errors.New("swaps: quote not found")
	ErrQuoteExpired  = errors.New("swaps: quote expired")
	ErrQuoteUsed     = errors.New("swaps: quote already used")
	// ErrUnavailable means Bitnob can't fill this direction right now (not
	// configured, or our pre-funded balance there is short).
	ErrUnavailable = errors.New("swaps: not available right now")
)

// Quote is a locked rate shown on the review screen. Amounts are minor units.
type Quote struct {
	ID         uuid.UUID   `json:"id"`
	FromAsset  money.Asset `json:"from_asset"`
	ToAsset    money.Asset `json:"to_asset"`
	FromAmount int64       `json:"from_amount"`
	// ToAmount is what the user receives, after our fee.
	ToAmount  int64 `json:"to_amount"`
	FeeAmount int64 `json:"fee_amount"` // in ToAsset
	// Rate is ToAsset per one FromAsset, all-in. Display only.
	Rate      string    `json:"rate"`
	ExpiresAt time.Time `json:"expires_at"`
	// EnoughFunds is false when the user's balance can't cover FromAmount.
	EnoughFunds bool `json:"enough_funds"`
}

// Trade is the outcome of executing a quote.
type Trade struct {
	ID         uuid.UUID   `json:"id"`
	Status     string      `json:"status"` // pending | completed | failed
	FromAsset  money.Asset `json:"from_asset"`
	ToAsset    money.Asset `json:"to_asset"`
	FromAmount int64       `json:"from_amount"`
	ToAmount   int64       `json:"to_amount"`
}

// toMinor converts a decimal string in major units to minor units. Precision
// beyond the asset's smallest unit is dropped, or rounded up when roundUp is
// set (used for what the user pays, so we never under-collect).
func toMinor(a money.Asset, decimal string, roundUp bool) (int64, error) {
	r, ok := new(big.Rat).SetString(strings.TrimSpace(decimal))
	if !ok || r.Sign() < 0 {
		return 0, fmt.Errorf("swaps: bad amount %q", decimal)
	}
	r.Mul(r, new(big.Rat).SetInt(pow10(a.Decimals())))
	minor, rem := new(big.Int).QuoRem(r.Num(), r.Denom(), new(big.Int))
	if roundUp && rem.Sign() != 0 {
		minor.Add(minor, big.NewInt(1))
	}
	if !minor.IsInt64() {
		return 0, fmt.Errorf("swaps: amount %q overflows", decimal)
	}
	return minor.Int64(), nil
}

// plain renders minor units the way Bitnob takes quantities: "1000", "0.5".
func plain(a money.Asset, minor int64) string {
	s := money.Format(a, minor)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s
}

// CreateQuote locks a rate between from and to. amount is either what the
// user pays (in from) or, when exactGet is set, exactly what they want to
// receive (in to); the other figure comes from the quote.
func (s *Service) CreateQuote(ctx context.Context, userID uuid.UUID, from, to money.Asset, amount int64, exactGet bool) (Quote, error) {
	if from == to {
		return Quote{}, &ValidationError{"Pick two different currencies."}
	}
	if amount <= 0 {
		return Quote{}, &ValidationError{"Enter an amount."}
	}
	if !s.Bitnob.Configured() {
		return Quote{}, ErrUnavailable
	}

	// Paying a fixed amount is a Bitnob "sell" of from; receiving a fixed
	// amount is a "buy" of to. To leave the user exactly what they asked for
	// after our fee, we buy slightly more and keep the difference.
	req := bitnob.TradingQuoteRequest{BaseCurrency: string(from), QuoteCurrency: string(to), Side: "sell", Quantity: plain(from, amount)}
	var gross int64
	if exactGet {
		gross = (amount*10_000 + (10_000 - s.FeeBps) - 1) / (10_000 - s.FeeBps)
		req = bitnob.TradingQuoteRequest{BaseCurrency: string(to), QuoteCurrency: string(from), Side: "buy", Quantity: plain(to, gross)}
	}
	bq, raw, err := s.Bitnob.CreateTradingQuote(ctx, req)
	if err != nil {
		return Quote{}, translateBitnob(err)
	}
	if bq.Exchange == nil || !strings.EqualFold(bq.Exchange.ReceiveCurrency, string(to)) ||
		!strings.EqualFold(bq.Exchange.SendCurrency, string(from)) {
		return Quote{}, fmt.Errorf("swaps: quote %s has no usable exchange block", bq.ID)
	}
	pays, err := toMinor(from, bq.Exchange.SendQuantity, exactGet)
	if err != nil {
		return Quote{}, err
	}
	received, err := toMinor(to, bq.Exchange.ReceiveQuantity, false)
	if err != nil {
		return Quote{}, err
	}

	var fee, net int64
	if exactGet {
		if received != gross {
			return Quote{}, fmt.Errorf("swaps: quote %s buys %d, asked for %d", bq.ID, received, gross)
		}
		fee, net = gross-amount, amount
	} else {
		if pays != amount {
			return Quote{}, fmt.Errorf("swaps: quote %s is for %d, asked for %d", bq.ID, pays, amount)
		}
		fee = received * s.FeeBps / 10_000
		net = received - fee
	}
	if net <= 0 || pays <= 0 {
		return Quote{}, &ValidationError{"That amount is too small to swap."}
	}

	// All-in rate: what the user gets per unit they give.
	rate := new(big.Rat).SetFrac(
		new(big.Int).Mul(big.NewInt(net), pow10(from.Decimals())),
		new(big.Int).Mul(big.NewInt(pays), pow10(to.Decimals())),
	).FloatString(10)

	q := Quote{
		FromAsset: from, ToAsset: to, FromAmount: pays,
		ToAmount: net, FeeAmount: fee, Rate: rate, ExpiresAt: bq.ExpiresAt,
	}
	err = s.Pool.QueryRow(ctx,
		`insert into quotes (user_id, kind, bitnob_quote_id, from_asset, from_amount, to_currency, to_amount,
		                     rate, fee_asset, fee_amount, expires_at, raw)
		 values ($1, 'swap', $2, $3, $4, $5::text, $6, $7, $5::text::asset, $8, $9, $10)
		 returning id`,
		userID, bq.ID, string(from), pays, string(to), net, rate, fee, bq.ExpiresAt, raw,
	).Scan(&q.ID)
	if err != nil {
		return Quote{}, err
	}

	var available int64
	err = s.Pool.QueryRow(ctx,
		`select coalesce((select balance from ledger_accounts where code = $1), 0)`,
		ledger.UserAccount(userID, from)).Scan(&available)
	if err != nil {
		return Quote{}, err
	}
	q.EnoughFunds = available >= pays
	return q, nil
}

func pow10(n int) *big.Int { return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil) }

// translateBitnob turns Bitnob's refusals into something a user can act on.
func translateBitnob(err error) error {
	var apiErr *bitnob.APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	detail := strings.ToLower(apiErr.Detail())
	switch {
	case strings.Contains(detail, "below configured minimum"), strings.Contains(detail, "too small"):
		return &ValidationError{"That amount is below the minimum for this swap."}
	case strings.Contains(detail, "above configured maximum"):
		return &ValidationError{"That amount is above the maximum for this swap."}
	case strings.Contains(detail, "insufficient balance"):
		// Our pre-funded balance at Bitnob, not the user's.
		return ErrUnavailable
	case strings.Contains(detail, "expired"):
		return ErrQuoteExpired
	}
	return err
}

// storedQuote is the part of Bitnob's quote response needed to place the
// order: it must repeat the quote's own pair, side and quantity.
type storedQuote struct {
	Data struct {
		Quote struct {
			BaseCurrency  string `json:"base_currency"`
			QuoteCurrency string `json:"quote_currency"`
			Side          string `json:"side"`
			Quantity      string `json:"quantity"`
			Price         string `json:"price"`
		} `json:"quote"`
	} `json:"data"`
}

// Execute consumes a quote: it reserves the user's funds, places the order
// with Bitnob and, if it fills at once, settles it. A trade left pending is
// finished later by the webhook or the sweeper.
func (s *Service) Execute(ctx context.Context, userID, quoteID uuid.UUID) (Trade, error) {
	var (
		t             Trade
		bitnobQuoteID string
		raw           []byte
	)
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var (
			expiresAt  time.Time
			consumedAt *time.Time
			toCurrency string
		)
		err := tx.QueryRow(ctx,
			`select bitnob_quote_id, from_asset::text, from_amount, to_currency, to_amount, expires_at, consumed_at, raw
			 from quotes where id = $1 and user_id = $2 and kind = 'swap' for update`,
			quoteID, userID,
		).Scan(&bitnobQuoteID, &t.FromAsset, &t.FromAmount, &toCurrency, &t.ToAmount, &expiresAt, &consumedAt, &raw)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrQuoteNotFound
		}
		if err != nil {
			return err
		}
		t.ToAsset = money.Asset(toCurrency)
		if consumedAt != nil {
			return ErrQuoteUsed
		}
		// Leave Bitnob a couple of seconds to accept the order.
		if time.Until(expiresAt) < 2*time.Second {
			return ErrQuoteExpired
		}
		if _, err := tx.Exec(ctx, `update quotes set consumed_at = now() where id = $1`, quoteID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx,
			`insert into trades (user_id, quote_id) values ($1, $2) returning id`, userID, quoteID,
		).Scan(&t.ID); err != nil {
			return err
		}
		// Reserve the funds so they can't be spent twice while the order is out.
		_, err = ledger.PostTx(ctx, tx, ledger.Entry{
			IdempotencyKey: "swap:" + t.ID.String() + ":hold",
			Kind:           "swap_hold",
			Lines: []ledger.Line{
				ledger.Debit(ledger.UserAccount(userID, t.FromAsset), t.FromAmount),
				ledger.Credit(ledger.PendingAccount(userID, t.FromAsset), t.FromAmount),
			},
		})
		return err
	})
	if err != nil {
		return Trade{}, err
	}
	t.Status = "pending"

	var stored storedQuote
	json.Unmarshal(raw, &stored)
	sq := stored.Data.Quote
	order, orderRaw, err := s.Bitnob.CreateOrder(ctx, bitnob.CreateOrderRequest{
		BaseCurrency:  sq.BaseCurrency,
		QuoteCurrency: sq.QuoteCurrency,
		Side:          strings.ToLower(sq.Side),
		Quantity:      sq.Quantity,
		Price:         sq.Price,
		QuoteID:       bitnobQuoteID,
		Reference:     t.ID.String(),
	})
	var apiErr *bitnob.APIError
	switch {
	case errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 500:
		// Bitnob refused the order outright, so nothing moved there.
		if ferr := s.finish(ctx, t.ID, "failed", "", []byte(apiErr.Body)); ferr != nil {
			return t, ferr
		}
		t.Status = "failed"
		if terr := translateBitnob(err); !errors.As(terr, &apiErr) {
			return t, terr
		}
		return t, &ValidationError{"That swap didn't go through. Nothing was taken from your balance."}
	case err != nil:
		// Timeout or server error: the order may or may not exist. Keep the
		// funds reserved and let the sweeper find out.
		s.Log.Error("swap order outcome unknown", "trade", t.ID, "err", err)
		return t, nil
	}

	switch order.Status {
	case "filled":
		t.Status = "completed"
	case "rejected", "cancelled":
		t.Status = "failed"
	}
	if t.Status == "pending" {
		_, err = s.Pool.Exec(ctx,
			`update trades set bitnob_order_id = $2, raw = $3 where id = $1`, t.ID, order.ID, orderRaw)
		return t, err
	}
	return t, s.finish(ctx, t.ID, t.Status, order.ID, orderRaw)
}

func (s *Service) finish(ctx context.Context, tradeID uuid.UUID, status, orderID string, raw []byte) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		return FinishTx(ctx, tx, tradeID, status, orderID, raw)
	})
}

// FinishTx moves a pending trade to "completed" or "failed" inside tx and
// posts the matching ledger entry. Calling it again is harmless.
//
// Completed (NGN -> USDT, as in the spec's posting table):
//
//	debit  user:NGN:pending, omnibus:USDT
//	credit omnibus:NGN, user:USDT (net), revenue:spread:USDT
//
// Failed: the reserved funds go back to the user.
func FinishTx(ctx context.Context, tx pgx.Tx, tradeID uuid.UUID, status, orderID string, raw []byte) error {
	var (
		userID       uuid.UUID
		current      string
		from, to     money.Asset
		fromAmt, net int64
		fee          int64
	)
	err := tx.QueryRow(ctx,
		`select t.user_id, t.status::text, q.from_asset::text, q.to_currency, q.from_amount, q.to_amount, q.fee_amount
		 from trades t join quotes q on q.id = t.quote_id
		 where t.id = $1 for update of t`, tradeID,
	).Scan(&userID, &current, &from, &to, &fromAmt, &net, &fee)
	if err != nil {
		return err
	}
	if current != "pending" {
		return nil
	}

	entry := ledger.Entry{
		IdempotencyKey: "swap:" + tradeID.String() + ":release",
		Kind:           "swap_release",
		Lines: []ledger.Line{
			ledger.Debit(ledger.PendingAccount(userID, from), fromAmt),
			ledger.Credit(ledger.UserAccount(userID, from), fromAmt),
		},
	}
	if status == "completed" {
		entry = ledger.Entry{
			IdempotencyKey: "swap:" + tradeID.String() + ":settle",
			Kind:           "swap",
			Lines: []ledger.Line{
				ledger.Debit(ledger.PendingAccount(userID, from), fromAmt),
				ledger.Credit(ledger.Omnibus(from), fromAmt),
				ledger.Debit(ledger.Omnibus(to), net+fee),
				ledger.Credit(ledger.UserAccount(userID, to), net),
			},
		}
		if fee > 0 {
			entry.Lines = append(entry.Lines, ledger.Credit(ledger.RevenueSpread(to), fee))
		}
	} else if status != "failed" {
		return fmt.Errorf("swaps: cannot finish trade as %q", status)
	}
	res, err := ledger.PostTx(ctx, tx, entry)
	if err != nil {
		return err
	}
	if raw == nil {
		raw = []byte("null")
	}
	_, err = tx.Exec(ctx,
		`update trades
		 set status = $2::text::trade_status,
		     entry_id = case when $2::text = 'completed' then $3::uuid else entry_id end,
		     bitnob_order_id = coalesce(nullif($4, ''), bitnob_order_id),
		     raw = coalesce(nullif($5::jsonb, 'null'), raw)
		 where id = $1`,
		tradeID, status, res.EntryID, orderID, raw)
	if err != nil {
		return err
	}
	if status == "completed" {
		return notify.Queue(ctx, tx, userID, "Swap complete",
			fmt.Sprintf("You swapped %s for %s.", money.Display(from, fromAmt), money.Display(to, net)))
	}
	return notify.Queue(ctx, tx, userID, "Swap didn't go through",
		money.Display(from, fromAmt)+" is back in your balance.")
}

// tradeEvent is lenient about where Bitnob puts the fields: the docs list
// them without showing a full payload.
type tradeEvent struct {
	Reference string `json:"reference"`
	TradeID   string `json:"tradeId"`
	Data      *struct {
		Reference string `json:"reference"`
		TradeID   string `json:"tradeId"`
	} `json:"data"`
}

// ApplyWebhook handles trade.completed and trade.failed. reference is the
// trade id we sent when placing the order.
func ApplyWebhook(ctx context.Context, tx pgx.Tx, event string, payload []byte) error {
	var ev tradeEvent
	if err := json.Unmarshal(payload, &ev); err != nil {
		return err
	}
	if ev.Data != nil && ev.Reference == "" {
		ev.Reference, ev.TradeID = ev.Data.Reference, ev.Data.TradeID
	}

	var tradeID uuid.UUID
	err := tx.QueryRow(ctx,
		`select id from trades where id::text = $1 or (bitnob_order_id is not null and bitnob_order_id = nullif($2, ''))`,
		ev.Reference, ev.TradeID).Scan(&tradeID)
	if errors.Is(err, pgx.ErrNoRows) {
		// Not one of ours (e.g. a trade placed from the dashboard).
		return nil
	}
	if err != nil {
		return err
	}
	status := "completed"
	if event == "trade.failed" {
		status = "failed"
	}
	return FinishTx(ctx, tx, tradeID, status, ev.TradeID, payload)
}

// Sweep finishes trades whose outcome we never heard: it asks Bitnob for the
// order's status and applies it through the same path as the webhook.
func (s *Service) Sweep(ctx context.Context) error {
	if !s.Bitnob.Configured() {
		return nil
	}
	rows, err := s.Pool.Query(ctx,
		`select id, coalesce(bitnob_order_id, ''), created_at from trades
		 where status = 'pending' and created_at < now() - interval '45 seconds'
		 order by created_at limit 50`)
	if err != nil {
		return err
	}
	type pending struct {
		id      uuid.UUID
		orderID string
		created time.Time
	}
	stale, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (pending, error) {
		var p pending
		return p, row.Scan(&p.id, &p.orderID, &p.created)
	})
	if err != nil || len(stale) == 0 {
		return err
	}

	var listed []bitnob.Order
	for _, p := range stale {
		order := bitnob.Order{ID: p.orderID}
		if p.orderID != "" {
			if order, err = s.Bitnob.GetOrder(ctx, p.orderID); err != nil {
				s.Log.Error("sweep: get order", "trade", p.id, "err", err)
				continue
			}
		} else {
			// The create call never answered; look for our reference.
			if listed == nil {
				if listed, err = s.Bitnob.ListOrders(ctx); err != nil {
					return err
				}
			}
			for _, o := range listed {
				if o.Reference == p.id.String() {
					order = o
				}
			}
			if order.ID == "" {
				// Not listed is not proof it never ran (NGN orders aren't
				// listed), so the funds stay reserved until someone checks.
				if time.Since(p.created) > 10*time.Minute {
					s.Log.Error("NEEDS REVIEW: swap pending with no Bitnob order", "trade", p.id)
				}
				continue
			}
		}

		status := ""
		switch order.Status {
		case "filled":
			status = "completed"
		case "rejected", "cancelled":
			status = "failed"
		default:
			continue
		}
		if err := s.finish(ctx, p.id, status, order.ID, nil); err != nil {
			s.Log.Error("sweep: finish trade", "trade", p.id, "err", err)
		}
	}
	return nil
}
