// Package crypto covers M4: receiving USDT, USDC and BTC on-chain at a
// per-user address, and sending them out to an external address.
package crypto

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	Log    *slog.Logger
	// Check, when set, enforces the user's limits before funds are held.
	Check func(ctx context.Context, userID uuid.UUID, asset money.Asset, amount int64) error

	mu       sync.Mutex
	chains   []bitnob.Chain
	chainsAt time.Time
}

// ValidationError is safe to show to the user as-is.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

var ErrUnavailable = errors.New("crypto: not available right now")

// Network is one chain an asset can move on.
type Network struct {
	Network string `json:"network"`
	Label   string `json:"label"`
	// Fee is what we charge to send on this network, in the asset's minor units.
	Fee           int64 `json:"fee"`
	MinWithdrawal int64 `json:"min_withdrawal"`
}

// Cheapest first, so the default suits small amounts. Tron leads for USDT
// where Bitnob offers it.
var preference = []string{"tron", "polygon", "solana", "base", "optimism", "avalanche", "bsc", "ethereum", "bitcoin"}

var labels = map[string]string{
	"tron": "Tron (TRC-20)", "polygon": "Polygon", "solana": "Solana", "base": "Base",
	"optimism": "Optimism", "avalanche": "Avalanche", "bsc": "BNB Chain", "ethereum": "Ethereum (ERC-20)",
	"bitcoin": "Bitcoin",
}

// feeFor is our flat charge per withdrawal. Bitnob has no fee-estimate
// endpoint and takes its own fee on top of the amount (1 USDC on the one
// sandbox withdrawal we made), so these are placeholders sized to cover
// that until real per-network costs are known.
func feeFor(a money.Asset, network string) int64 {
	if a == money.BTC {
		return 5_000 // sats
	}
	if network == "ethereum" {
		return 5_000_000 // $5.00
	}
	return 1_500_000 // $1.50
}

// minFor mirrors Bitnob's minimum (the sandbox refuses under $1).
func minFor(a money.Asset) int64 {
	if a == money.BTC {
		return 2_000 // sats
	}
	return 1_000_000
}

func (s *Service) supportedChains(ctx context.Context) ([]bitnob.Chain, error) {
	if !s.Bitnob.Configured() {
		return nil, ErrUnavailable
	}
	s.mu.Lock()
	chains, at := s.chains, s.chainsAt
	s.mu.Unlock()
	if chains != nil && time.Since(at) < 10*time.Minute {
		return chains, nil
	}
	fresh, err := s.Bitnob.SupportedChains(ctx)
	if err != nil {
		if chains != nil {
			return chains, nil
		}
		return nil, err
	}
	s.mu.Lock()
	s.chains, s.chainsAt = fresh, time.Now()
	s.mu.Unlock()
	return fresh, nil
}

// Networks lists where asset can be received and sent, read live from Bitnob.
func (s *Service) Networks(ctx context.Context, asset money.Asset) ([]Network, error) {
	if asset == money.NGN {
		return nil, &ValidationError{"Naira doesn't move on-chain."}
	}
	chains, err := s.supportedChains(ctx)
	if err != nil {
		return nil, err
	}
	out := []Network{}
	for _, c := range chains {
		// Stellar shares one address between all users and tells them apart
		// by memo, which the app doesn't handle yet.
		if c.Chain == "stellar" {
			continue
		}
		carries := asset == money.BTC && c.Chain == "bitcoin"
		for _, coin := range c.Stablecoins {
			carries = carries || coin.Symbol == string(asset)
		}
		if !carries {
			continue
		}
		label := labels[c.Chain]
		if label == "" {
			label = strings.ToUpper(c.Chain[:1]) + c.Chain[1:]
		}
		out = append(out, Network{Network: c.Chain, Label: label, Fee: feeFor(asset, c.Chain), MinWithdrawal: minFor(asset)})
	}
	rank := func(n string) int {
		for i, p := range preference {
			if p == n {
				return i
			}
		}
		return len(preference)
	}
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i].Network) < rank(out[j].Network) })
	return out, nil
}

func (s *Service) network(ctx context.Context, asset money.Asset, name string) (Network, error) {
	networks, err := s.Networks(ctx, asset)
	if err != nil {
		return Network{}, err
	}
	for _, n := range networks {
		if n.Network == name {
			return n, nil
		}
	}
	return Network{}, &ValidationError{fmt.Sprintf("%s isn't available on that network.", asset)}
}

// Address returns the user's deposit address on a network, creating it at
// Bitnob the first time. The same address takes every asset on that network.
func (s *Service) Address(ctx context.Context, userID uuid.UUID, asset money.Asset, network string) (string, error) {
	if _, err := s.network(ctx, asset, network); err != nil {
		return "", err
	}
	const find = `select address from crypto_addresses where user_id = $1 and network = $2`
	var address string
	err := s.Pool.QueryRow(ctx, find, userID, network).Scan(&address)
	if err == nil || !errors.Is(err, pgx.ErrNoRows) {
		return address, err
	}

	created, err := s.Bitnob.GenerateAddress(ctx, network, "user "+userID.String()[:8], "addr-"+userID.String()+"-"+network)
	if err != nil {
		var apiErr *bitnob.APIError
		if errors.As(err, &apiErr) {
			s.Log.Error("generate address", "network", network, "err", err)
			return "", ErrUnavailable
		}
		return "", err
	}
	if _, err := s.Pool.Exec(ctx,
		`insert into crypto_addresses (user_id, network, address, bitnob_address_id)
		 values ($1, $2, $3, $4) on conflict (user_id, network) do nothing`,
		userID, network, created.Address, created.ID); err != nil {
		return "", err
	}
	// Re-read: a concurrent request may have stored a different address first.
	err = s.Pool.QueryRow(ctx, find, userID, network).Scan(&address)
	return address, err
}

// Deposit is crypto arriving at one of our addresses. Amount is in the
// asset's smallest unit.
type Deposit struct {
	Reference string // Bitnob's id for the deposit; our idempotency key
	Network   string
	Address   string
	Currency  string
	Amount    string
	Hash      string
	Raw       []byte
}

// ApplyDeposit credits the address's owner inside tx. Money sent to an
// address we don't recognise goes to suspense for ops.
func ApplyDeposit(ctx context.Context, tx pgx.Tx, d Deposit) error {
	asset, err := money.ParseAsset(d.Currency)
	if err != nil || asset == money.NGN {
		return fmt.Errorf("crypto: deposit %s in unsupported currency %q", d.Reference, d.Currency)
	}
	amount, err := strconv.ParseInt(d.Amount, 10, 64)
	if err != nil || amount <= 0 || d.Reference == "" {
		return fmt.Errorf("crypto: malformed deposit %q amount %q", d.Reference, d.Amount)
	}

	var userID *uuid.UUID
	err = tx.QueryRow(ctx,
		`select user_id from crypto_addresses where network = $1 and lower(address) = lower($2)`,
		d.Network, d.Address).Scan(&userID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	credit, kind := ledger.Suspense(asset), "crypto_deposit_unmatched"
	if userID != nil {
		credit, kind = ledger.UserAccount(*userID, asset), "crypto_deposit"
	}
	res, err := ledger.PostTx(ctx, tx, ledger.Entry{
		IdempotencyKey: "crypto_deposit:" + d.Reference,
		Kind:           kind,
		Metadata:       map[string]any{"network": d.Network, "hash": d.Hash},
		Lines: []ledger.Line{
			ledger.Debit(ledger.Omnibus(asset), amount),
			ledger.Credit(credit, amount),
		},
	})
	if err != nil || !res.Posted {
		return err
	}
	raw := d.Raw
	if raw == nil {
		raw = []byte("{}")
	}
	_, err = tx.Exec(ctx,
		`insert into crypto_transfers (user_id, direction, asset, network, address, amount, tx_hash,
		                               bitnob_transaction_id, status, entry_id, raw)
		 values ($1, 'deposit', $2::text::asset, $3, $4, $5, nullif($6, ''), $7, 'success', $8, $9)
		 on conflict (bitnob_transaction_id) do nothing`,
		userID, string(asset), d.Network, d.Address, amount, d.Hash, d.Reference, res.EntryID, raw)
	if err != nil || userID == nil {
		return err
	}
	return notify.Queue(ctx, tx, *userID, "Crypto received", money.Display(asset, amount)+" arrived in your balance.")
}

type WithdrawInput struct {
	Asset   money.Asset
	Network string
	Address string
	Amount  int64
	// IdempotencyKey makes a retried request return the first withdrawal.
	IdempotencyKey string
}

// Preview is what the user confirms: the amount that arrives, our fee, and
// the total leaving their balance.
type Preview struct {
	Asset       money.Asset `json:"asset"`
	Network     string      `json:"network"`
	Address     string      `json:"address"`
	Amount      int64       `json:"amount"`
	Fee         int64       `json:"fee"`
	Total       int64       `json:"total"`
	EnoughFunds bool        `json:"enough_funds"`
}

// Preview validates a withdrawal and prices it, without moving anything.
func (s *Service) Preview(ctx context.Context, userID uuid.UUID, in WithdrawInput) (Preview, error) {
	in.Address = strings.TrimSpace(in.Address)
	n, err := s.network(ctx, in.Asset, in.Network)
	if err != nil {
		return Preview{}, err
	}
	if in.Amount < n.MinWithdrawal {
		min := strings.TrimRight(strings.TrimRight(money.Format(in.Asset, n.MinWithdrawal), "0"), ".")
		return Preview{}, &ValidationError{fmt.Sprintf("The minimum is %s %s.", min, in.Asset)}
	}
	if in.Address == "" {
		return Preview{}, &ValidationError{"Enter an address."}
	}
	valid, err := s.Bitnob.ValidateAddress(ctx, in.Network, in.Address)
	if err != nil {
		return Preview{}, err
	}
	if !valid {
		return Preview{}, &ValidationError{fmt.Sprintf("That isn't a valid %s address.", n.Label)}
	}

	p := Preview{Asset: in.Asset, Network: in.Network, Address: in.Address, Amount: in.Amount, Fee: n.Fee, Total: in.Amount + n.Fee}
	var available int64
	if err := s.Pool.QueryRow(ctx,
		`select coalesce((select balance from ledger_accounts where code = $1), 0)`,
		ledger.UserAccount(userID, in.Asset)).Scan(&available); err != nil {
		return Preview{}, err
	}
	p.EnoughFunds = available >= p.Total
	return p, nil
}

// Transfer is a withdrawal as the app sees it: "pending" until the chain
// confirms, then "success" or "failed".
type Transfer struct {
	ID      uuid.UUID   `json:"id"`
	Status  string      `json:"status"`
	Asset   money.Asset `json:"asset"`
	Network string      `json:"network"`
	Address string      `json:"address"`
	Amount  int64       `json:"amount"`
	Fee     int64       `json:"fee"`
}

// Withdraw reserves amount + fee, then asks Bitnob to send. The reserve is
// settled or released when Bitnob reports the outcome.
func (s *Service) Withdraw(ctx context.Context, userID uuid.UUID, in WithdrawInput) (Transfer, error) {
	if in.IdempotencyKey == "" {
		return Transfer{}, &ValidationError{"idempotency_key is required"}
	}
	p, err := s.Preview(ctx, userID, in)
	if err != nil {
		return Transfer{}, err
	}
	if s.Check != nil {
		if err := s.Check(ctx, userID, p.Asset, p.Total); err != nil {
			return Transfer{}, err
		}
	}
	// TODO: travel-rule data above thresholds (spec: "Compliance, risk and limits").

	// The id is derived from the user's key, so a retry lands on the same row.
	t := Transfer{
		ID:     uuid.NewSHA1(userID, []byte("withdrawal:"+in.IdempotencyKey)),
		Status: "pending", Asset: p.Asset, Network: p.Network, Address: p.Address, Amount: p.Amount, Fee: p.Fee,
	}
	var existing bool
	err = pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`insert into crypto_transfers (id, user_id, direction, asset, network, address, amount, service_fee)
			 values ($1, $2, 'withdrawal', $3::text::asset, $4, $5, $6, $7)
			 on conflict (id) do nothing`,
			t.ID, userID, string(t.Asset), t.Network, t.Address, t.Amount, t.Fee)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			existing = true
			return tx.QueryRow(ctx, `select status::text from crypto_transfers where id = $1`, t.ID).Scan(&t.Status)
		}
		_, err = ledger.PostTx(ctx, tx, ledger.Entry{
			IdempotencyKey: "withdrawal:" + t.ID.String() + ":hold",
			Kind:           "withdrawal_hold",
			Lines: []ledger.Line{
				ledger.Debit(ledger.UserAccount(userID, t.Asset), p.Total),
				ledger.Credit(ledger.PendingAccount(userID, t.Asset), p.Total),
			},
		})
		return err
	})
	if err != nil || existing {
		return t, err
	}

	w, raw, err := s.Bitnob.CreateWithdrawal(ctx, bitnob.WithdrawalRequest{
		ToAddress: t.Address,
		Amount:    strconv.FormatInt(t.Amount, 10),
		Currency:  string(t.Asset),
		Chain:     t.Network,
		Reference: t.ID.String(),
	})
	var apiErr *bitnob.APIError
	switch {
	case bitnob.IsDuplicate(err):
		// Bitnob already has this reference; its webhook will tell us how it ended.
		return t, nil
	case errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 500:
		if ferr := s.finish(ctx, t.ID, "failed", 0, "", apiErr.Detail(), []byte(apiErr.Body)); ferr != nil {
			return t, ferr
		}
		t.Status = "failed"
		if strings.Contains(strings.ToLower(apiErr.Detail()), "insufficient") {
			return t, ErrUnavailable // our pre-funded balance at Bitnob, not the user's
		}
		return t, &ValidationError{"That withdrawal didn't go through. Nothing was taken from your balance."}
	case err != nil:
		s.Log.Error("withdrawal outcome unknown", "transfer", t.ID, "err", err)
		return t, nil
	}

	if _, err := s.Pool.Exec(ctx,
		`update crypto_transfers set bitnob_transaction_id = $2, raw = $3 where id = $1`,
		t.ID, w.TransactionID, raw); err != nil {
		return t, err
	}
	switch w.Status {
	case "completed":
		t.Status = "success"
		return t, s.finish(ctx, t.ID, "success", 0, "", "", nil)
	case "failed":
		t.Status = "failed"
		return t, s.finish(ctx, t.ID, "failed", 0, "", "Bitnob could not send it.", nil)
	}
	return t, nil
}

func (s *Service) finish(ctx context.Context, id uuid.UUID, status string, bitnobFee int64, hash, reason string, raw []byte) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		return FinishTx(ctx, tx, id, status, bitnobFee, hash, reason, raw)
	})
}

// FinishTx settles or releases a pending withdrawal inside tx. Calling it
// again is harmless.
//
//	success: debit user:asset:pending (amount + our fee)
//	         credit omnibus:asset (amount + Bitnob's fee), revenue:fees:asset (our fee - Bitnob's fee)
//	failed:  debit user:asset:pending; credit user:asset
//
// bitnobFee is what Bitnob took on top, in the asset's minor units. If it
// exceeds our fee the revenue line is a debit: a loss, recorded as one.
func FinishTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, status string, bitnobFee int64, hash, reason string, raw []byte) error {
	var (
		userID  *uuid.UUID
		current string
		asset   money.Asset
		amount  int64
		fee     int64
	)
	err := tx.QueryRow(ctx,
		`select user_id, status::text, asset::text, amount, service_fee
		 from crypto_transfers where id = $1 and direction = 'withdrawal' for update`, id,
	).Scan(&userID, &current, &asset, &amount, &fee)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if current != "pending" || userID == nil {
		return nil
	}

	total := amount + fee
	var entry ledger.Entry
	switch status {
	case "success":
		entry = ledger.Entry{
			IdempotencyKey: "withdrawal:" + id.String() + ":settle",
			Kind:           "withdrawal",
			Lines: []ledger.Line{
				ledger.Debit(ledger.PendingAccount(*userID, asset), total),
				ledger.Credit(ledger.Omnibus(asset), amount+bitnobFee),
			},
		}
		if margin := fee - bitnobFee; margin != 0 {
			entry.Lines = append(entry.Lines, ledger.Credit(ledger.RevenueFees(asset), margin))
		}
	case "failed":
		entry = ledger.Entry{
			IdempotencyKey: "withdrawal:" + id.String() + ":release",
			Kind:           "withdrawal_failed",
			Lines: []ledger.Line{
				ledger.Debit(ledger.PendingAccount(*userID, asset), total),
				ledger.Credit(ledger.UserAccount(*userID, asset), total),
			},
		}
	default:
		return fmt.Errorf("crypto: cannot finish withdrawal as %q", status)
	}
	res, err := ledger.PostTx(ctx, tx, entry)
	if err != nil {
		return err
	}
	if raw == nil {
		raw = []byte("null")
	}
	_, err = tx.Exec(ctx,
		`update crypto_transfers
		 set status = $2::text::crypto_transfer_status, entry_id = $3, network_fee = $4,
		     tx_hash = coalesce(nullif($5, ''), tx_hash), failure_reason = nullif($6, ''),
		     raw = coalesce(nullif($7::jsonb, 'null'), raw)
		 where id = $1`,
		id, status, res.EntryID, bitnobFee, hash, reason, raw)
	if err != nil {
		return err
	}
	if status == "success" {
		return notify.Queue(ctx, tx, *userID, "Crypto sent", money.Display(asset, amount)+" was confirmed on the network.")
	}
	return notify.Queue(ctx, tx, *userID, "Crypto send returned",
		money.Display(asset, total)+" is back in your balance.")
}

// event is the body of deposit.success, transfer.success and transfer.failed.
type event struct {
	Data struct {
		Fee       string `json:"fee"`
		Hash      string `json:"hash"`
		Chain     string `json:"chain"`
		Amount    string `json:"amount"`
		Address   string `json:"address"`
		Currency  string `json:"currency"`
		Reference string `json:"reference"`
	} `json:"data"`
}

// ApplyWebhook handles the three crypto events.
func ApplyWebhook(ctx context.Context, tx pgx.Tx, name string, payload []byte) error {
	var ev event
	if err := json.Unmarshal(payload, &ev); err != nil {
		return err
	}
	d := ev.Data
	if name == "deposit.success" {
		return ApplyDeposit(ctx, tx, Deposit{
			Reference: d.Reference, Network: d.Chain, Address: d.Address,
			Currency: d.Currency, Amount: d.Amount, Hash: d.Hash, Raw: payload,
		})
	}
	// Withdrawals carry the reference we sent: our transfer id.
	id, err := uuid.Parse(d.Reference)
	if err != nil {
		return nil // not one of ours
	}
	if name == "transfer.failed" {
		return FinishTx(ctx, tx, id, "failed", 0, d.Hash, "The network could not complete it.", payload)
	}
	bitnobFee, _ := strconv.ParseInt(d.Fee, 10, 64)
	return FinishTx(ctx, tx, id, "success", bitnobFee, d.Hash, "", payload)
}

// Sweep reads Bitnob's recent transactions to pick up crypto deposits whose
// webhook never arrived. Withdrawals have no status endpoint, so a pending
// one that is getting old is flagged for a person to check.
func (s *Service) Sweep(ctx context.Context) error {
	if !s.Bitnob.Configured() {
		return nil
	}
	txns, err := s.Bitnob.Transactions(ctx, 50)
	if err != nil {
		return err
	}
	for _, t := range txns {
		if t.Type != "DEPOSIT_CONFIRMED" || t.State != "SETTLED" || t.Metadata.Address == "" {
			continue
		}
		if a, err := money.ParseAsset(t.Currency); err != nil || a == money.NGN {
			continue
		}
		err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
			return ApplyDeposit(ctx, tx, Deposit{
				Reference: t.Reference, Network: t.Metadata.Chain, Address: t.Metadata.Address,
				Currency: t.Currency, Amount: t.Amount, Hash: t.Metadata.TxHash,
			})
		})
		if err != nil {
			s.Log.Error("sweep: crypto deposit", "reference", t.Reference, "err", err)
		}
	}

	var stuck int
	if err := s.Pool.QueryRow(ctx,
		`select count(*) from crypto_transfers
		 where direction = 'withdrawal' and status = 'pending' and created_at < now() - interval '1 hour'`,
	).Scan(&stuck); err != nil {
		return err
	}
	if stuck > 0 {
		s.Log.Error("NEEDS REVIEW: withdrawals pending for over an hour", "count", stuck)
	}
	return nil
}
