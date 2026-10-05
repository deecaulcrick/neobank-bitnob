package ledger

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deecaulcrick/neobank/backend/internal/money"
)

// These tests need a database with the migration applied:
//
//	TEST_DATABASE_URL=postgres://... go test ./internal/ledger
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func newUser(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	if _, err := pool.Exec(ctx, `insert into auth.users (id) values ($1)`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into users (id, phone) values ($1, $2)`, id, "+234"+id.String()[:10]); err != nil {
		t.Fatal(err)
	}
	if err := EnsureUserAccounts(ctx, pool, id); err != nil {
		t.Fatal(err)
	}
	return id
}

func available(t *testing.T, pool *pgxpool.Pool, user uuid.UUID, a money.Asset) int64 {
	t.Helper()
	bals, err := UserBalances(context.Background(), pool, user)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bals {
		if b.Asset == a {
			return b.Available
		}
	}
	return 0
}

func TestDepositIsIdempotent(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	user := newUser(t, pool)

	deposit := Entry{
		IdempotencyKey: "evt_" + uuid.NewString(),
		Kind:           "ngn_deposit",
		Lines: []Line{
			Debit(Omnibus(money.NGN), 500_000),
			Credit(UserAccount(user, money.NGN), 500_000),
		},
	}
	first, err := Post(ctx, pool, deposit)
	if err != nil || !first.Posted {
		t.Fatalf("first post: %+v, %v", first, err)
	}
	second, err := Post(ctx, pool, deposit)
	if err != nil || second.Posted || second.EntryID != first.EntryID {
		t.Fatalf("replay: %+v, %v", second, err)
	}
	if got := available(t, pool, user, money.NGN); got != 500_000 {
		t.Fatalf("balance = %d, want 500000", got)
	}
}

func TestOverdraftIsRejected(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	sender, receiver := newUser(t, pool), newUser(t, pool)

	_, err := Post(ctx, pool, Entry{
		IdempotencyKey: "p2p_" + uuid.NewString(),
		Kind:           "p2p_send",
		Lines: []Line{
			Debit(UserAccount(sender, money.USDT), 1_000_000),
			Credit(UserAccount(receiver, money.USDT), 1_000_000),
		},
	})
	if !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("err = %v, want ErrInsufficientFunds", err)
	}
	if got := available(t, pool, receiver, money.USDT); got != 0 {
		t.Fatalf("receiver balance = %d, want 0", got)
	}
}

func TestUnbalancedIsRejected(t *testing.T) {
	pool := testPool(t)
	user := newUser(t, pool)

	_, err := Post(context.Background(), pool, Entry{
		IdempotencyKey: "bad_" + uuid.NewString(),
		Kind:           "ngn_deposit",
		Lines: []Line{
			Debit(Omnibus(money.NGN), 100),
			Credit(UserAccount(user, money.NGN), 99),
		},
	})
	if !errors.Is(err, ErrUnbalanced) {
		t.Fatalf("err = %v, want ErrUnbalanced", err)
	}
}
