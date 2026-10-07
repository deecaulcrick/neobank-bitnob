// Package activity is the user's history: one feed over deposits, in-app
// sends, swaps, payouts and crypto transfers, plus the detail of each.
package activity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deecaulcrick/neobank/backend/internal/money"
)

var ErrNotFound = errors.New("activity: not found")

// Item is one row in the feed. Amount is signed: positive is money in.
// Status is done, pending or failed.
type Item struct {
	ID     string `json:"id"`   // "<kind>:<uuid>", opaque to the app
	Kind   string `json:"kind"` // deposit | transfer_in | transfer_out | swap | payout | crypto_in | crypto_out
	Status string `json:"status"`
	Asset  string `json:"asset"`
	Amount int64  `json:"amount"`
	// For swaps and payouts: what was received, in OtherCurrency. Swaps use
	// the asset's minor units; payouts use hundredths.
	OtherCurrency *string   `json:"other_currency"`
	OtherAmount   *int64    `json:"other_amount"`
	Title         string    `json:"title"`
	CreatedAt     time.Time `json:"created_at"`
}

// feed is every movement for user $1, one shape. Pending and failed map from
// each table's own vocabulary.
const feed = `
  select 'deposit' as kind, d.id, 'done' as status, 'NGN' as asset, d.amount,
         null::text as other_currency, null::bigint as other_amount,
         coalesce(nullif(d.sender_name, ''), 'Bank transfer') as title, d.created_at
    from deposits d where d.user_id = $1
  union all
  select 'transfer_out', t.id, 'done', t.asset::text, -t.amount, null, null, '@' || u.tag, t.created_at
    from p2p_transfers t join users u on u.id = t.receiver_id where t.sender_id = $1
  union all
  select 'transfer_in', t.id, 'done', t.asset::text, t.amount, null, null, '@' || u.tag, t.created_at
    from p2p_transfers t join users u on u.id = t.sender_id where t.receiver_id = $1
  union all
  select 'swap', t.id,
         case t.status when 'completed' then 'done' when 'failed' then 'failed' else 'pending' end,
         q.from_asset::text, -q.from_amount, q.to_currency, q.to_amount,
         q.from_asset::text || ' to ' || q.to_currency, t.created_at
    from trades t join quotes q on q.id = t.quote_id where t.user_id = $1
  union all
  select 'payout', p.id,
         case p.status when 'success' then 'done' when 'failed' then 'failed' when 'expired' then 'failed' else 'pending' end,
         q.from_asset::text, -q.from_amount, q.to_currency, q.to_amount, b.display_name, p.created_at
    from payouts p join quotes q on q.id = p.quote_id join beneficiaries b on b.id = p.beneficiary_id
   where p.user_id = $1
  union all
  select case c.direction when 'deposit' then 'crypto_in' else 'crypto_out' end, c.id,
         case c.status when 'success' then 'done' when 'failed' then 'failed' else 'pending' end,
         c.asset::text,
         case c.direction when 'deposit' then c.amount else -(c.amount + c.service_fee) end,
         null, null, c.network, c.created_at
    from crypto_transfers c where c.user_id = $1`

type Filter struct {
	Asset  string    // "" for all
	Kinds  []string  // empty for all
	Before time.Time // zero for newest
	Limit  int
}

type Service struct {
	Pool *pgxpool.Pool
}

// List returns the newest items first. Pass the last item's CreatedAt as
// Before to page.
func (s *Service) List(ctx context.Context, userID uuid.UUID, f Filter) ([]Item, error) {
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 30
	}
	if f.Before.IsZero() {
		f.Before = time.Now().Add(time.Hour)
	}
	if f.Kinds == nil {
		f.Kinds = []string{} // a nil slice would reach Postgres as NULL
	}
	rows, err := s.Pool.Query(ctx,
		`select kind, id, status, asset, amount, other_currency, other_amount, title, created_at
		 from (`+feed+`) a
		 where ($2 = '' or asset = $2 or other_currency = $2)
		   and (cardinality($3::text[]) = 0 or kind = any($3))
		   and created_at < $4
		 order by created_at desc limit $5`,
		userID, strings.ToUpper(f.Asset), f.Kinds, f.Before, f.Limit)
	if err != nil {
		return nil, err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Item, error) {
		var it Item
		var id uuid.UUID
		err := row.Scan(&it.Kind, &id, &it.Status, &it.Asset, &it.Amount, &it.OtherCurrency, &it.OtherAmount, &it.Title, &it.CreatedAt)
		it.ID = it.Kind + ":" + id.String()
		return it, err
	})
	if items == nil {
		items = []Item{}
	}
	return items, err
}

// Step is one point on a transaction's timeline. At is nil until it happens.
type Step struct {
	Label string     `json:"label"`
	At    *time.Time `json:"at"`
}

type Row struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Detail is one movement in full: the feed item, its timeline and the
// figures behind it.
type Detail struct {
	Item
	Timeline  []Step `json:"timeline"`
	Rows      []Row  `json:"rows"`
	Reference string `json:"reference"`
}

func amount(asset string, minor int64) string {
	a := money.Asset(asset)
	if !a.Valid() {
		return fmt.Sprintf("%s %s", money.Format(money.NGN, minor), asset) // payout currencies: hundredths
	}
	s := money.Format(a, minor)
	if a == money.BTC || a == money.USDT || a == money.USDC {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s + " " + asset
}

// Get returns one item by the id List gave out. It only finds the caller's own.
func (s *Service) Get(ctx context.Context, userID uuid.UUID, itemID string) (Detail, error) {
	kind, rawID, ok := strings.Cut(itemID, ":")
	id, err := uuid.Parse(rawID)
	if !ok || err != nil {
		return Detail{}, ErrNotFound
	}

	var d Detail
	err = s.Pool.QueryRow(ctx,
		`select kind, status, asset, amount, other_currency, other_amount, title, created_at
		 from (`+feed+`) a where id = $2 and kind = $3`, userID, id, kind,
	).Scan(&d.Kind, &d.Status, &d.Asset, &d.Amount, &d.OtherCurrency, &d.OtherAmount, &d.Title, &d.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Detail{}, ErrNotFound
	}
	if err != nil {
		return Detail{}, err
	}
	d.ID, d.Reference = itemID, id.String()
	created := d.CreatedAt
	done := func(at *time.Time) *time.Time {
		if d.Status == "done" {
			return at
		}
		return nil
	}

	switch kind {
	case "deposit":
		d.Timeline = []Step{{"Received", &created}}
		d.Rows = []Row{{"Added", amount(d.Asset, d.Amount)}, {"Via", "Bank transfer to your account number"}}

	case "transfer_in", "transfer_out":
		label, who := "Sent", "To"
		if kind == "transfer_in" {
			label, who = "Received", "From"
		}
		var note *string
		if err := s.Pool.QueryRow(ctx, `select note from p2p_transfers where id = $1`, id).Scan(&note); err != nil {
			return Detail{}, err
		}
		d.Timeline = []Step{{label, &created}}
		d.Rows = []Row{{who, d.Title}, {"Amount", amount(d.Asset, abs(d.Amount))}, {"Fee", "Free"}}
		if note != nil {
			d.Rows = append(d.Rows, Row{"Note", *note})
		}

	case "swap":
		var (
			updated time.Time
			fee     int64
			rate    string
		)
		if err := s.Pool.QueryRow(ctx,
			`select t.updated_at, q.fee_amount, trim(trailing '0' from q.rate::text)
			 from trades t join quotes q on q.id = t.quote_id where t.id = $1`, id,
		).Scan(&updated, &fee, &rate); err != nil {
			return Detail{}, err
		}
		d.Timeline = []Step{{"Started", &created}, {"Completed", done(&updated)}}
		if d.Status == "failed" {
			d.Timeline[1] = Step{"Didn't go through. Returned to your balance", &updated}
		}
		d.Rows = []Row{
			{"Paid", amount(d.Asset, abs(d.Amount))},
			{"Received", amount(*d.OtherCurrency, *d.OtherAmount)},
			{"Rate", fmt.Sprintf("1 %s = %s %s", d.Asset, strings.TrimRight(rate, "."), *d.OtherCurrency)},
			{"Fee (included)", amount(*d.OtherCurrency, fee)},
		}

	case "payout":
		var (
			updated       time.Time
			status        string
			fee           int64
			country, rail string
			reason        *string
		)
		if err := s.Pool.QueryRow(ctx,
			`select p.updated_at, p.status::text, q.fee_amount, b.country, b.rail, p.failure_reason
			 from payouts p join quotes q on q.id = p.quote_id join beneficiaries b on b.id = p.beneficiary_id
			 where p.id = $1`, id,
		).Scan(&updated, &status, &fee, &country, &rail, &reason); err != nil {
			return Detail{}, err
		}
		d.Timeline = []Step{{"Started", &created}, {"Sending", &created}, {"Delivered", done(&updated)}}
		if d.Status == "failed" {
			label := "Didn't arrive. Returned to your balance"
			if status == "expired" {
				label = "Expired. Returned to your balance"
			}
			d.Timeline[2] = Step{label, &updated}
		}
		d.Rows = []Row{
			{"To", d.Title},
			{"They get", amount(*d.OtherCurrency, *d.OtherAmount)},
			{"You paid", amount(d.Asset, abs(d.Amount))},
			{"Fee (included)", amount(d.Asset, fee)},
			{"Sent by", strings.ToUpper(rail[:1]) + strings.ReplaceAll(rail[1:], "_", " ") + ", " + country},
		}

	case "crypto_in", "crypto_out":
		var (
			updated       time.Time
			address       string
			hash          *string
			amt, fee      int64
			failureReason *string
		)
		if err := s.Pool.QueryRow(ctx,
			`select updated_at, address, tx_hash, amount, service_fee, failure_reason from crypto_transfers where id = $1`, id,
		).Scan(&updated, &address, &hash, &amt, &fee, &failureReason); err != nil {
			return Detail{}, err
		}
		if kind == "crypto_in" {
			d.Timeline = []Step{{"Received", &created}}
			d.Rows = []Row{{"Added", amount(d.Asset, amt)}, {"Network", d.Title}, {"To your address", address}}
		} else {
			d.Timeline = []Step{{"Started", &created}, {"Confirmed on the network", done(&updated)}}
			if d.Status == "failed" {
				d.Timeline[1] = Step{"Didn't go through. Returned to your balance", &updated}
			}
			d.Rows = []Row{
				{"Sent", amount(d.Asset, amt)}, {"Fee", amount(d.Asset, fee)},
				{"Network", d.Title}, {"To", address},
			}
		}
		if hash != nil && *hash != "" {
			d.Rows = append(d.Rows, Row{"Transaction hash", *hash})
		}
	}
	return d, nil
}

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// Person is another user as shown when choosing who to pay: a tag and a
// first name, nothing more.
type Person struct {
	Tag       string  `json:"tag"`
	FirstName *string `json:"first_name"`
}

// People finds users to pay. With an empty query it returns who the caller
// has sent to most recently; otherwise tags starting with the query.
func (s *Service) People(ctx context.Context, userID uuid.UUID, query string) ([]Person, error) {
	query = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(query), "@"))
	var (
		rows pgx.Rows
		err  error
	)
	if query == "" {
		rows, err = s.Pool.Query(ctx,
			`select u.tag::text, u.first_name
			 from (select receiver_id, max(created_at) as last from p2p_transfers where sender_id = $1 group by receiver_id) r
			 join users u on u.id = r.receiver_id
			 order by r.last desc limit 8`, userID)
	} else {
		// Escape LIKE wildcards so "_" in a tag matches itself.
		pattern := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query) + "%"
		rows, err = s.Pool.Query(ctx,
			`select tag::text, first_name from users
			 where tag::text like $2 and id <> $1 and kyc_tier >= 1
			 order by tag limit 8`, userID, pattern)
	}
	if err != nil {
		return nil, err
	}
	people, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Person, error) {
		var p Person
		return p, row.Scan(&p.Tag, &p.FirstName)
	})
	if people == nil {
		people = []Person{}
	}
	return people, err
}
