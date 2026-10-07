// Package jobs holds the periodic background work run by cmd/worker.
package jobs

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"time"

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
		if err := fn(ctx); err != nil && ctx.Err() == nil {
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
	j.Log.Info("reconciliation finished", "drifted_accounts", drifted)
	return nil
}

// reconcileBitnob writes one reconciliation_reports row per asset. The
// spec's identity is: user + pending + revenue (+ suspense) balances equal
// Bitnob's balance; since the ledger nets to zero that sum is -omnibus.
//
// Not covered yet: matching every Bitnob transaction id to exactly one
// journal entry.
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
