package ledger

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/deecaulcrick/neobank/backend/internal/money"
)

// DBTX is satisfied by both *pgxpool.Pool and pgx.Tx.
type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Account codes, as named in the spec.

func UserAccount(userID uuid.UUID, a money.Asset) string {
	return fmt.Sprintf("user:%s:%s", userID, a)
}

// PendingAccount holds funds reserved by an in-flight payout or withdrawal.
func PendingAccount(userID uuid.UUID, a money.Asset) string {
	return fmt.Sprintf("user:%s:%s:pending", userID, a)
}

// Omnibus mirrors what Bitnob holds for us. It is debit-normal, so its
// balance is negative; -balance is the expected Bitnob balance.
func Omnibus(a money.Asset) string       { return "omnibus:" + string(a) }
func RevenueFees(a money.Asset) string   { return "revenue:fees:" + string(a) }
func RevenueSpread(a money.Asset) string { return "revenue:spread:" + string(a) }

// Suspense takes anything that doesn't match until ops resolves it.
func Suspense(a money.Asset) string { return "suspense:" + string(a) }

// EnsureUserAccounts creates the user's spendable and pending account for
// every asset. Safe to call repeatedly.
func EnsureUserAccounts(ctx context.Context, db DBTX, userID uuid.UUID) error {
	batch := &pgx.Batch{}
	for _, a := range money.Assets {
		batch.Queue(
			`insert into ledger_accounts (code, kind, asset, user_id)
			 values ($1, 'user', $3, $4), ($2, 'user_pending', $3, $4)
			 on conflict (code) do nothing`,
			UserAccount(userID, a), PendingAccount(userID, a), string(a), userID,
		)
	}
	sender, ok := db.(interface {
		SendBatch(context.Context, *pgx.Batch) pgx.BatchResults
	})
	if !ok {
		return fmt.Errorf("ledger: %T cannot send batches", db)
	}
	return sender.SendBatch(ctx, batch).Close()
}

// Balance is a user's holding of one asset, in minor units.
type Balance struct {
	Asset     money.Asset `json:"asset"`
	Available int64       `json:"available"`
	Pending   int64       `json:"pending"`
}

// UserBalances reads the cached balances for every asset, zero-filled.
func UserBalances(ctx context.Context, db DBTX, userID uuid.UUID) ([]Balance, error) {
	rows, err := db.Query(ctx,
		`select asset::text, kind::text, balance from ledger_accounts where user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byAsset := map[money.Asset]*Balance{}
	out := make([]Balance, len(money.Assets))
	for i, a := range money.Assets {
		out[i].Asset = a
		byAsset[a] = &out[i]
	}
	for rows.Next() {
		var asset, kind string
		var bal int64
		if err := rows.Scan(&asset, &kind, &bal); err != nil {
			return nil, err
		}
		b, ok := byAsset[money.Asset(asset)]
		if !ok {
			continue
		}
		if kind == "user_pending" {
			b.Pending = bal
		} else {
			b.Available = bal
		}
	}
	return out, rows.Err()
}
