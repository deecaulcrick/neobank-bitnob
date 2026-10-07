// Package limits enforces how much a user may move out per day: a daily and
// a single-transaction cap, a cap on the number of sends, and a lower cap on
// money leaving the app while an account is new.
//
// These are our own risk controls, not Bitnob tiers (Bitnob has none). The
// amounts are placeholders and every one is configurable.
package limits

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deecaulcrick/neobank/backend/internal/money"
	"github.com/deecaulcrick/neobank/backend/internal/prices"
)

// Kind is what sort of outflow is being checked.
type Kind string

const (
	Transfer Kind = "transfer" // to another user, in-app
	Payout   Kind = "payout"   // to a bank or mobile wallet
	Crypto   Kind = "crypto"   // to an external address
)

// Config holds every threshold, in kobo.
type Config struct {
	Daily    int64 // total outflow per day
	Single   int64 // largest single outflow
	MaxSends int   // outflows per day, any size
	// For the first NewAccountHold after first funding, payouts and crypto
	// sends together are capped at NewAccountDaily per day.
	NewAccountHold  time.Duration
	NewAccountDaily int64
}

// Error is a refusal that is safe to show to the user.
type Error struct{ Message string }

func (e *Error) Error() string { return e.Message }

type Service struct {
	Pool   *pgxpool.Pool
	Prices *prices.Service
	Cfg    Config
}

// Usage is where a user stands today. All amounts are kobo.
type Usage struct {
	DailyLimit  int64 `json:"daily_limit"`
	SingleLimit int64 `json:"single_limit"`
	UsedToday   int64 `json:"used_today"`
	Remaining   int64 `json:"remaining"`
	SendsToday  int   `json:"sends_today"`
	MaxSends    int   `json:"max_sends"`
	// Set while the new-account cap applies.
	NewAccountUntil *time.Time `json:"new_account_until"`
	NewAccountDaily int64      `json:"new_account_daily_limit"`
	// What has left the app (payouts and crypto) today.
	usedExternal int64
}

// inNaira values an amount in kobo. Naira needs no rate; anything else uses
// the indicative price, which is good enough for a limit.
func (s *Service) inNaira(ctx context.Context, asset money.Asset, minor int64) (int64, error) {
	if asset == money.NGN {
		return minor, nil
	}
	rates, err := s.Prices.Rates(ctx)
	if err != nil {
		return 0, &Error{"We can't check your limits right now. Try again in a moment."}
	}
	major := float64(minor)
	for i := 0; i < asset.Decimals(); i++ {
		major /= 10
	}
	return int64(major * rates.NGN[asset] * 100), nil
}

// Usage adds up today's outflows. The day runs midnight to midnight in Lagos.
func (s *Service) Usage(ctx context.Context, userID uuid.UUID) (Usage, error) {
	u := Usage{
		DailyLimit: s.Cfg.Daily, SingleLimit: s.Cfg.Single,
		MaxSends: s.Cfg.MaxSends, NewAccountDaily: s.Cfg.NewAccountDaily,
	}
	var firstFunded *time.Time
	var created time.Time
	err := s.Pool.QueryRow(ctx,
		`select u.created_at, (
		   select min(at) from (
		     select min(created_at) as at from deposits where user_id = u.id
		     union all select min(created_at) from crypto_transfers where user_id = u.id and direction = 'deposit'
		     union all select min(created_at) from p2p_transfers where receiver_id = u.id
		   ) f)
		 from users u where u.id = $1`, userID).Scan(&created, &firstFunded)
	if err != nil {
		return u, err
	}
	// An account that has never been funded is as new as it gets.
	since := created
	if firstFunded != nil {
		since = *firstFunded
	}
	if until := since.Add(s.Cfg.NewAccountHold); time.Now().Before(until) {
		u.NewAccountUntil = &until
	}

	rows, err := s.Pool.Query(ctx,
		`with today as (
		   select date_trunc('day', now() at time zone 'Africa/Lagos') at time zone 'Africa/Lagos' as start
		 )
		 select 'transfer', asset::text, amount from p2p_transfers, today
		  where sender_id = $1 and created_at >= start
		 union all
		 select 'payout', q.from_asset::text, q.from_amount
		   from payouts p join quotes q on q.id = p.quote_id, today
		  where p.user_id = $1 and p.created_at >= start and p.status not in ('failed', 'expired')
		 union all
		 select 'crypto', asset::text, amount + service_fee from crypto_transfers, today
		  where user_id = $1 and direction = 'withdrawal' and created_at >= start and status <> 'failed'`, userID)
	if err != nil {
		return u, err
	}
	type out struct {
		kind, asset string
		amount      int64
	}
	outs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (out, error) {
		var o out
		return o, row.Scan(&o.kind, &o.asset, &o.amount)
	})
	if err != nil {
		return u, err
	}
	for _, o := range outs {
		v, err := s.inNaira(ctx, money.Asset(o.asset), o.amount)
		if err != nil {
			return u, err
		}
		u.UsedToday += v
		u.SendsToday++
		if o.kind != string(Transfer) {
			u.usedExternal += v
		}
	}
	if u.Remaining = u.DailyLimit - u.UsedToday; u.Remaining < 0 {
		u.Remaining = 0
	}
	return u, nil
}

// Check returns an *Error if this outflow would break a limit, nil if it may
// go ahead. amount is everything leaving the user's balance, fees included.
func (s *Service) Check(ctx context.Context, userID uuid.UUID, kind Kind, asset money.Asset, amount int64) error {
	value, err := s.inNaira(ctx, asset, amount)
	if err != nil {
		return err
	}
	u, err := s.Usage(ctx, userID)
	if err != nil {
		return err
	}
	naira := func(kobo int64) string { return money.Display(money.NGN, kobo) }

	switch {
	case u.SendsToday >= u.MaxSends:
		return &Error{fmt.Sprintf("You've reached today's limit of %d sends. Try again tomorrow.", u.MaxSends)}
	case value > u.SingleLimit:
		return &Error{fmt.Sprintf("The most you can send at once is %s.", naira(u.SingleLimit))}
	case u.UsedToday+value > u.DailyLimit:
		return &Error{fmt.Sprintf("That's over your daily limit of %s. You have %s left today.", naira(u.DailyLimit), naira(u.Remaining))}
	case kind != Transfer && u.NewAccountUntil != nil && u.usedExternal+value > u.NewAccountDaily:
		left := u.NewAccountDaily - u.usedExternal
		if left < 0 {
			left = 0
		}
		return &Error{fmt.Sprintf(
			"New accounts can send up to %s a day outside the app until %s. You have %s left today.",
			naira(u.NewAccountDaily), u.NewAccountUntil.Format("2 Jan"), naira(left))}
	}
	return nil
}
