// Package jobs holds the periodic background work run by cmd/worker.
package jobs

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deecaulcrick/neobank/backend/internal/bitnob"
)

type Jobs struct {
	Pool   *pgxpool.Pool
	Bitnob *bitnob.Client
	Log    *slog.Logger
}

// Every runs fn immediately and then on each tick until ctx is cancelled.
func Every(ctx context.Context, d time.Duration, name string, log *slog.Logger, fn func(context.Context) error) {
	ticker := time.NewTicker(d)
	defer ticker.Stop()
	for {
		// Bounded, so one hung call can't stall the job for good.
		runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		err := fn(runCtx)
		cancel()
		if err != nil && ctx.Err() == nil {
			log.Error("job failed", "job", name, "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Reconcile checks the ledger's own invariants. Differences are reported for
// ops, never auto-corrected.
func (j *Jobs) Reconcile(ctx context.Context) error {
	// 1. Every asset nets to zero across all accounts.
	rows, err := j.Pool.Query(ctx,
		`select asset::text, sum(balance)::bigint from ledger_accounts group by asset having sum(balance) <> 0`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var asset string
		var off int64
		if err := rows.Scan(&asset, &off); err != nil {
			rows.Close()
			return err
		}
		j.Log.Error("RECONCILIATION: ledger does not net to zero", "asset", asset, "off_by", off)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// 2. Each cached balance equals the sum of its postings.
	var drifted int
	err = j.Pool.QueryRow(ctx,
		`select count(*) from ledger_accounts a
		 where a.balance <> coalesce((select sum(p.amount) from postings p where p.account_id = a.id), 0)`,
	).Scan(&drifted)
	if err != nil {
		return err
	}
	if drifted > 0 {
		j.Log.Error("RECONCILIATION: cached balances drifted from postings", "accounts", drifted)
	}

	// 3. Per asset, what the ledger says Bitnob holds for us against what
	//    Bitnob reports. Recorded for ops; never corrected automatically.
	if err := j.reconcileBitnob(ctx); err != nil {
		j.Log.Error("RECONCILIATION: could not compare with Bitnob", "err", err)
	}
	// 4. Every Bitnob transaction should map to exactly one ledger record,
	//    and every recent ledger record to a Bitnob transaction.
	if err := j.reconcileTransactions(ctx); err != nil {
		j.Log.Error("RECONCILIATION: could not match transactions", "err", err)
	}
	j.Log.Info("reconciliation finished", "drifted_accounts", drifted)
	return nil
}

// reconcileBitnob writes one reconciliation_reports row per asset. The
// spec's identity is: user + pending + revenue (+ suspense) balances equal
// Bitnob's balance; since the ledger nets to zero that sum is -omnibus.
func (j *Jobs) reconcileBitnob(ctx context.Context) error {
	if !j.Bitnob.Configured() {
		return nil
	}
	raw, err := j.Bitnob.Balances(ctx)
	if err != nil {
		return err
	}
	var body struct {
		Data struct {
			Accounts []struct {
				Currency      string `json:"currency"`
				LedgerBalance string `json:"ledger_balance"`
			} `json:"accounts"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return err
	}
	atBitnob := map[string]int64{}
	for _, a := range body.Data.Accounts {
		if v, err := strconv.ParseInt(a.LedgerBalance, 10, 64); err == nil {
			atBitnob[a.Currency] = v
		}
	}

	rows, err := j.Pool.Query(ctx,
		`select asset::text,
		        -coalesce(sum(balance) filter (where kind = 'omnibus'), 0)::bigint,
		        coalesce(sum(balance) filter (where kind <> 'omnibus'), 0)::bigint
		 from ledger_accounts group by asset order by asset`)
	if err != nil {
		return err
	}
	type line struct {
		asset               string
		ledger, liabilities int64
	}
	lines, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (line, error) {
		var l line
		return l, row.Scan(&l.asset, &l.ledger, &l.liabilities)
	})
	if err != nil {
		return err
	}
	for _, l := range lines {
		var reported *int64
		notes := ""
		if v, ok := atBitnob[l.asset]; ok {
			reported = &v
			if v != l.ledger {
				notes = "mismatch"
				j.Log.Error("RECONCILIATION: ledger and Bitnob disagree",
					"asset", l.asset, "ledger", l.ledger, "bitnob", v, "difference", v-l.ledger)
			}
		} else {
			notes = "Bitnob did not report this asset"
			j.Log.Warn("RECONCILIATION: no Bitnob balance for asset", "asset", l.asset)
		}
		if _, err := j.Pool.Exec(ctx,
			`insert into reconciliation_reports (asset, ledger_amount, bitnob_amount, liabilities, notes)
			 values ($1::text::asset, $2, $3, $4, nullif($5, ''))`,
			l.asset, l.ledger, reported, l.liabilities, notes); err != nil {
			return err
		}
	}
	return nil
}

// reconcileTransactions matches Bitnob's recent transactions against the
// records behind our journal entries and writes one reconciliation_transactions
// row per finding:
//
//	matched           a Bitnob transaction and the one record it belongs to
//	duplicate         a second Bitnob transaction of the same type for a record
//	unmatched_bitnob  money moved at Bitnob that we have no record of
//	unmatched_ledger  a record of ours that Bitnob shows no movement for
//
// It looks at Bitnob's most recent page only, so it is a daily tripwire, not
// a full historical audit. In-app sends never touch Bitnob and are excluded.
func (j *Jobs) reconcileTransactions(ctx context.Context) error {
	if !j.Bitnob.Configured() {
		return nil
	}
	txns, err := j.Bitnob.Transactions(ctx, 200)
	if err != nil {
		return err
	}
	if len(txns) == 0 {
		return nil
	}
	oldest := txns[0].CreatedAt
	for _, t := range txns {
		if t.CreatedAt.Before(oldest) {
			oldest = t.CreatedAt
		}
	}

	// Every identifier each of our records may be known by at Bitnob.
	rows, err := j.Pool.Query(ctx, `
		select kind, id, created_at, key from (
		  select 'deposit' as kind, id, created_at, unnest(array[
		           bitnob_transaction_id, raw->>'reference', raw->'payload'->>'reference',
		           raw->>'id', raw->>'provider_transaction_id', raw->'payload'->>'provider_transaction_id']) as key
		    from deposits
		  union all
		  select 'payout', id, created_at, unnest(array[id::text, reference, bitnob_payout_id])
		    from payouts where status in ('processing', 'success')
		  union all
		  select 'trade', id, created_at, unnest(array[id::text, bitnob_order_id])
		    from trades where status = 'completed'
		  union all
		  select 'crypto_transfer', id, created_at, unnest(array[id::text, bitnob_transaction_id])
		    from crypto_transfers where status <> 'failed'
		) k where key is not null and key <> ''`)
	if err != nil {
		return err
	}
	type record struct {
		kind    string
		id      uuid.UUID
		created time.Time
	}
	byKey := map[string]record{}
	all := map[uuid.UUID]record{}
	var (
		rec record
		key string
	)
	if _, err := pgx.ForEachRow(rows, []any{&rec.kind, &rec.id, &rec.created, &key}, func() error {
		byKey[key] = rec
		all[rec.id] = rec
		return nil
	}); err != nil {
		return err
	}

	ranAt := time.Now()
	report := func(result string, t *bitnob.Transaction, r *record) error {
		var bid, btype, currency, ref, kind *string
		var amount *int64
		var id *uuid.UUID
		if t != nil {
			bid, btype, currency, ref = &t.TransactionID, &t.Type, &t.Currency, &t.Reference
			if v, err := strconv.ParseInt(t.Amount, 10, 64); err == nil {
				amount = &v
			}
		}
		if r != nil {
			kind, id = &r.kind, &r.id
		}
		_, err := j.Pool.Exec(ctx,
			`insert into reconciliation_transactions
			   (ran_at, result, bitnob_transaction_id, bitnob_type, currency, amount, reference, ledger_kind, ledger_id)
			 values ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			ranAt, result, bid, btype, currency, amount, ref, kind, id)
		return err
	}

	seen := map[string]bool{} // ledger record + Bitnob type
	matched := map[uuid.UUID]bool{}
	counts := map[string]int{}
	for i := range txns {
		t := &txns[i]
		var hit *record
		for _, k := range t.Keys() {
			if r, ok := byKey[k]; ok {
				hit = &r
				break
			}
		}
		result := "unmatched_bitnob"
		if hit != nil {
			result = "matched"
			matched[hit.id] = true
			if tag := hit.id.String() + "|" + t.Type; seen[tag] {
				result = "duplicate"
			} else {
				seen[tag] = true
			}
		}
		counts[result]++
		if err := report(result, t, hit); err != nil {
			return err
		}
	}
	// Our records from the window Bitnob's page covers, given ten minutes to
	// show up there.
	for id, r := range all {
		if matched[id] || r.created.Before(oldest) || time.Since(r.created) < 10*time.Minute {
			continue
		}
		counts["unmatched_ledger"]++
		r := r
		if err := report("unmatched_ledger", nil, &r); err != nil {
			return err
		}
	}

	level := slog.LevelInfo
	if counts["unmatched_bitnob"]+counts["unmatched_ledger"]+counts["duplicate"] > 0 {
		level = slog.LevelError
	}
	j.Log.Log(ctx, level, "RECONCILIATION: transactions matched",
		"matched", counts["matched"], "duplicate", counts["duplicate"],
		"unmatched_bitnob", counts["unmatched_bitnob"], "unmatched_ledger", counts["unmatched_ledger"])
	return nil
}
