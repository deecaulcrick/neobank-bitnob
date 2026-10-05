// Package accounts covers M1: tier-1 onboarding (Bitnob customer + NGN
// virtual account) and crediting deposits made to that account.
package accounts

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deecaulcrick/neobank/backend/internal/bitnob"
	"github.com/deecaulcrick/neobank/backend/internal/ledger"
	"github.com/deecaulcrick/neobank/backend/internal/money"
)

type Service struct {
	Pool    *pgxpool.Pool
	Bitnob  *bitnob.Client
	HashKey string
}

// ValidationError is safe to show to the user as-is.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

var (
	// ErrIdentityInUse means the BVN or email already backs another account,
	// here or at Bitnob.
	ErrIdentityInUse = errors.New("accounts: BVN or email already in use")
	ErrNoAccount     = errors.New("accounts: no virtual account")
	ErrUnavailable   = errors.New("accounts: Bitnob is not configured")
)

type KYCInput struct {
	FirstName   string `json:"first_name"`
	LastName    string `json:"last_name"`
	Email       string `json:"email"`
	DateOfBirth string `json:"date_of_birth"` // YYYY-MM-DD
	BVN         string `json:"bvn"`
}

type VirtualAccount struct {
	AccountNumber string `json:"account_number"`
	AccountName   string `json:"account_name"`
	BankName      string `json:"bank_name"`
}

var bvnPattern = regexp.MustCompile(`^\d{11}$`)

func (in *KYCInput) normalize() error {
	in.FirstName = strings.TrimSpace(in.FirstName)
	in.LastName = strings.TrimSpace(in.LastName)
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.BVN = strings.TrimSpace(in.BVN)

	if in.FirstName == "" || in.LastName == "" {
		return invalid("Enter your first and last name as they appear on your BVN.")
	}
	if addr, err := mail.ParseAddress(in.Email); err != nil || addr.Address != in.Email {
		return invalid("Enter a valid email address.")
	}
	dob, err := time.Parse("2006-01-02", in.DateOfBirth)
	if err != nil {
		return invalid("Enter a valid date of birth.")
	}
	if dob.AddDate(18, 0, 0).After(time.Now()) {
		return invalid("You must be at least 18 to open an account.")
	}
	if !bvnPattern.MatchString(in.BVN) {
		return invalid("Your BVN is 11 digits.")
	}
	return nil
}

// hashBVN is what we keep instead of the BVN: enough to spot the same BVN
// twice, useless without the key. A bare hash of 11 digits could be reversed
// by trying them all.
func (s *Service) hashBVN(bvn string) string {
	mac := hmac.New(sha256.New, []byte(s.HashKey))
	mac.Write([]byte(bvn))
	return hex.EncodeToString(mac.Sum(nil))
}

// splitPhone turns the phone on the Supabase session ("2348012345678") into
// Bitnob's dial code and national number. v1 is Nigeria-only.
func splitPhone(phone string) (dialCode, national string, err error) {
	digits := strings.TrimPrefix(phone, "+")
	if !strings.HasPrefix(digits, "234") || len(digits) < 13 {
		return "", "", invalid("A Nigerian phone number is required.")
	}
	return "+234", digits[3:], nil
}

// Onboard runs tier-1 KYC: it creates the Bitnob customer, issues the NGN
// account number and records both. It is safe to retry: Bitnob keys both
// calls on references derived from the user id.
func (s *Service) Onboard(ctx context.Context, userID uuid.UUID, phone string, in KYCInput) (VirtualAccount, error) {
	if va, err := s.VirtualAccount(ctx, userID); err == nil {
		return va, nil
	} else if !errors.Is(err, ErrNoAccount) {
		return VirtualAccount{}, err
	}
	if !s.Bitnob.Configured() {
		return VirtualAccount{}, ErrUnavailable
	}
	if err := in.normalize(); err != nil {
		return VirtualAccount{}, err
	}
	dialCode, national, err := splitPhone(phone)
	if err != nil {
		return VirtualAccount{}, err
	}

	// Refuse a BVN or email we already hold before involving Bitnob.
	bvnHash := s.hashBVN(in.BVN)
	var taken bool
	err = s.Pool.QueryRow(ctx,
		`select exists (
		   select 1 from kyc_records
		   where id_type = 'bvn' and id_reference = $1 and status = 'approved' and user_id <> $2
		 ) or exists (select 1 from users where email = $3 and id <> $2)`,
		bvnHash, userID, in.Email).Scan(&taken)
	if err != nil {
		return VirtualAccount{}, err
	}
	if taken {
		return VirtualAccount{}, ErrIdentityInUse
	}

	customer, customerRaw, err := s.Bitnob.CreateCustomer(ctx, bitnob.CreateCustomerRequest{
		FirstName:    in.FirstName,
		LastName:     in.LastName,
		Email:        in.Email,
		CustomerType: "individual",
		PhoneNumber:  national,
		DialCode:     dialCode,
		DateOfBirth:  in.DateOfBirth,
		IDType:       "bvn",
		IDNumber:     in.BVN,
		Country:      "NGA",
		Reference:    userID.String(),
	})
	if err != nil {
		return VirtualAccount{}, translateBitnob(err)
	}
	// Bitnob answers a duplicate BVN or email with the existing customer
	// rather than an error, so a different email back means it isn't ours.
	if !strings.EqualFold(customer.Email, in.Email) {
		return VirtualAccount{}, ErrIdentityInUse
	}

	account, accountRaw, err := s.Bitnob.CreateVirtualAccount(ctx, customer.ID, "va-"+userID.String())
	if err != nil {
		return VirtualAccount{}, translateBitnob(err)
	}

	err = pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`update users
			 set first_name = $2, last_name = $3, email = $4, date_of_birth = $5, kyc_tier = greatest(kyc_tier, 1)
			 where id = $1`,
			userID, in.FirstName, in.LastName, in.Email, in.DateOfBirth); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`insert into kyc_records (user_id, tier, id_type, id_reference, status, reviewed_at)
			 values ($1, 1, 'bvn', $2, 'approved', now())
			 on conflict (id_type, id_reference) where status = 'approved' do nothing`,
			userID, bvnHash); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`insert into bitnob_customers (user_id, bitnob_customer_id, raw) values ($1, $2, $3)
			 on conflict (user_id) do nothing`,
			userID, customer.ID, redactBVN(customerRaw, in.BVN)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx,
			`insert into virtual_accounts (user_id, bitnob_account_id, account_number, account_name, bank_name, raw)
			 values ($1, $2, $3, $4, $5, $6)
			 on conflict (user_id) do nothing`,
			userID, account.ID, account.AccountNumber, account.AccountName, account.BankName, accountRaw)
		return err
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		// Lost a race with another user registering the same BVN or email.
		return VirtualAccount{}, ErrIdentityInUse
	}
	if err != nil {
		return VirtualAccount{}, err
	}
	return VirtualAccount{
		AccountNumber: account.AccountNumber,
		AccountName:   account.AccountName,
		BankName:      account.BankName,
	}, nil
}

// redactBVN keeps the raw customer response for debugging without the BVN.
func redactBVN(raw []byte, bvn string) []byte {
	return []byte(strings.ReplaceAll(string(raw), bvn, "[redacted]"))
}

// translateBitnob surfaces Bitnob's validation messages (wrong BVN details,
// under 18, ...) and hides everything else.
func translateBitnob(err error) error {
	var apiErr *bitnob.APIError
	if errors.As(err, &apiErr) && apiErr.Status == 400 {
		if detail := apiErr.Detail(); detail != "" {
			return invalid("%s", detail)
		}
	}
	return err
}

func (s *Service) VirtualAccount(ctx context.Context, userID uuid.UUID) (VirtualAccount, error) {
	var va VirtualAccount
	err := s.Pool.QueryRow(ctx,
		`select account_number, account_name, bank_name from virtual_accounts where user_id = $1`, userID,
	).Scan(&va.AccountNumber, &va.AccountName, &va.BankName)
	if errors.Is(err, pgx.ErrNoRows) {
		return va, ErrNoAccount
	}
	return va, err
}

// Deposit is a settled credit to a virtual account, from a webhook or from
// polling the account's transactions.
type Deposit struct {
	// LedgerTransactionID identifies the deposit in both sources, so a
	// deposit seen twice posts once.
	LedgerTransactionID string
	BitnobAccountID     string
	AmountKobo          int64
	Raw                 []byte
}

// ApplyDeposit credits the account's owner inside tx. A deposit to an account
// we don't recognise goes to suspense for ops instead of being dropped.
func ApplyDeposit(ctx context.Context, tx pgx.Tx, d Deposit) error {
	if d.LedgerTransactionID == "" || d.AmountKobo <= 0 {
		return fmt.Errorf("accounts: malformed deposit %+v", d.LedgerTransactionID)
	}

	var (
		accountID *uuid.UUID
		userID    *uuid.UUID
	)
	err := tx.QueryRow(ctx,
		`select id, user_id from virtual_accounts where bitnob_account_id = $1`, d.BitnobAccountID,
	).Scan(&accountID, &userID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	credit := ledger.Suspense(money.NGN)
	kind := "ngn_deposit_unmatched"
	if userID != nil {
		credit = ledger.UserAccount(*userID, money.NGN)
		kind = "ngn_deposit"
	}
	res, err := ledger.PostTx(ctx, tx, ledger.Entry{
		IdempotencyKey: "deposit:" + d.LedgerTransactionID,
		Kind:           kind,
		Metadata:       map[string]any{"bitnob_account_id": d.BitnobAccountID},
		Lines: []ledger.Line{
			ledger.Debit(ledger.Omnibus(money.NGN), d.AmountKobo),
			ledger.Credit(credit, d.AmountKobo),
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
		`insert into deposits (user_id, virtual_account_id, amount, bitnob_transaction_id, entry_id, raw)
		 values ($1, $2, $3, $4, $5, $6)
		 on conflict (bitnob_transaction_id) do nothing`,
		userID, accountID, d.AmountKobo, d.LedgerTransactionID, res.EntryID, raw)
	return err
}

// SyncDeposits polls Bitnob for one virtual account's transactions and applies
// any settled credit we haven't posted. It reports how many were new.
func (s *Service) SyncDeposits(ctx context.Context, bitnobAccountID string) (int, error) {
	txns, err := s.Bitnob.VirtualAccountTransactions(ctx, bitnobAccountID)
	if err != nil {
		return 0, err
	}
	applied := 0
	for _, t := range txns {
		if t.Type != "credit" || t.Status != "completed" || t.Currency != "NGN" {
			continue
		}
		kobo, err := strconv.ParseInt(t.Amount, 10, 64)
		if err != nil {
			return applied, fmt.Errorf("accounts: deposit %s has amount %q", t.ID, t.Amount)
		}
		var isNew bool
		err = pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
			if err := tx.QueryRow(ctx,
				`select not exists (select 1 from deposits where bitnob_transaction_id = $1)`,
				t.LedgerTransactionID).Scan(&isNew); err != nil || !isNew {
				return err
			}
			return ApplyDeposit(ctx, tx, Deposit{
				LedgerTransactionID: t.LedgerTransactionID,
				BitnobAccountID:     t.VirtualAccountID,
				AmountKobo:          kobo,
			})
		})
		if err != nil {
			return applied, err
		}
		if isNew {
			applied++
		}
	}
	return applied, nil
}

// SyncAllDeposits is the sweeper for missed deposit webhooks.
func (s *Service) SyncAllDeposits(ctx context.Context) error {
	if !s.Bitnob.Configured() {
		return nil
	}
	rows, err := s.Pool.Query(ctx,
		`select bitnob_account_id from virtual_accounts where bitnob_account_id is not null`)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	var firstErr error
	for _, id := range ids {
		if _, err := s.SyncDeposits(ctx, id); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("sync deposits for %s: %w", id, err)
		}
	}
	return firstErr
}

// SimulateDeposit asks the Bitnob sandbox to pay NGN 1,000 into the user's
// account, then pulls it straight into the ledger. Development only.
func (s *Service) SimulateDeposit(ctx context.Context, userID uuid.UUID) error {
	if !s.Bitnob.Configured() {
		return ErrUnavailable
	}
	var bitnobAccountID string
	err := s.Pool.QueryRow(ctx,
		`select bitnob_account_id from virtual_accounts where user_id = $1`, userID).Scan(&bitnobAccountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNoAccount
	}
	if err != nil {
		return err
	}
	if _, err := s.Bitnob.SimulateDeposit(ctx, bitnobAccountID); err != nil {
		return err
	}
	_, err = s.SyncDeposits(ctx, bitnobAccountID)
	return err
}
