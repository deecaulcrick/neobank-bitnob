// Package jobs holds the periodic background work run by cmd/worker.
package jobs

import (
	"context"
	"log/slog"
	"time"

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

// Sweep catches missed webhooks: anything in a non-final state older than its
// expected window is polled from Bitnob and pushed through the same posting
// path as the webhook would have used.
func (j *Jobs) Sweep(ctx context.Context) error {
	var payouts, trades int
	err := j.Pool.QueryRow(ctx,
		`select
		   (select count(*) from payouts
		     where status in ('initialized', 'processing') and updated_at < now() - interval '20 minutes'),
		   (select count(*) from trades
		     where status = 'pending' and created_at < now() - interval '5 minutes')`,
	).Scan(&payouts, &trades)
	if err != nil {
		return err
	}
	if payouts+trades > 0 {
		// TODO(M2/M3): poll bitnob.GetPayout / trade status for each and apply.
		j.Log.Warn("stale in-flight records need sweeping", "payouts", payouts, "trades", trades)
	}
	return nil
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

	// 3. TODO(M5): per asset, -balance(omnibus) must equal bitnob.Balances();
	//    every Bitnob transaction ID maps to exactly one journal entry.
	j.Log.Info("reconciliation finished", "drifted_accounts", drifted)
	return nil
}
