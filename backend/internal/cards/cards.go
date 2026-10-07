// Package cards issues and runs a user's virtual dollar card. The card is
// loaded from the user's USDC balance; its own balance sits at the issuer
// and is read live from Bitnob.
package cards

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
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

// Fees are in micro-dollars. Fee is what the user pays; Cost is what Bitnob
// takes, so Fee - Cost is ours.
type Fees struct {
	CreationFee, CreationCost int64
	FundFee, FundCost         int64
}

type Service struct {
	Pool   *pgxpool.Pool
	Bitnob *bitnob.Client
	Fees   Fees
	// HashKey is the BVN hash key, to check the BVN given for card KYC is the
	// one the account was opened with.
	HashKey string
	Log     *slog.Logger
}

// ValidationError is safe to show to the user as-is.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

var (
	ErrNoCard      = errors.New("cards: no card")
	ErrUnavailable = errors.New("cards: not available right now")
)

// The card's money is dollars held as USDC.
const asset = money.USDC

// View is everything the card screen needs.
type View struct {
	// KYC is "", "pending", "approved" or "rejected".
	KYC  string `json:"kyc_status"`
	Card *Card  `json:"card"`
	// What it costs to issue and to load a card, in micro-dollars.
	CreationFee int64 `json:"creation_fee"`
	FundFee     int64 `json:"fund_fee"`
}

type Card struct {
	ID     uuid.UUID `json:"id"`
	Status string    `json:"status"` // pending | active | frozen
	Brand  string    `json:"brand"`
	Last4  string    `json:"last4"`
	Name   string    `json:"name"`
	// Balance is read live; nil if Bitnob couldn't be reached.
	Balance *int64 `json:"balance"`
}

type row struct {
	id       uuid.UUID
	bitnobID *string
	status   string
	brand    *string
	last4    *string
	name     string
}

func (s *Service) liveCard(ctx context.Context, userID uuid.UUID) (row, error) {
	var r row
	err := s.Pool.QueryRow(ctx,
		`select id, bitnob_card_id, status, brand, last4, name from cards
		 where user_id = $1 and status in ('pending', 'active', 'frozen')`, userID,
	).Scan(&r.id, &r.bitnobID, &r.status, &r.brand, &r.last4, &r.name)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNoCard
	}
	return r, err
}

func (s *Service) Get(ctx context.Context, userID uuid.UUID) (View, error) {
	v := View{CreationFee: s.Fees.CreationFee, FundFee: s.Fees.FundFee}
	var kyc *string
	if err := s.Pool.QueryRow(ctx, `select card_kyc_status from users where id = $1`, userID).Scan(&kyc); err != nil {
		return v, err
	}
	if kyc != nil {
		v.KYC = *kyc
	}
	r, err := s.liveCard(ctx, userID)
	if errors.Is(err, ErrNoCard) {
		return v, nil
	}
	if err != nil {
		return v, err
	}
	c := &Card{ID: r.id, Status: r.status, Name: r.name}
	if r.brand != nil {
		c.Brand = *r.brand
	}
	if r.last4 != nil {
		c.Last4 = *r.last4
	}
	if r.bitnobID != nil && s.Bitnob.Configured() {
		if live, err := s.Bitnob.GetCard(ctx, *r.bitnobID); err == nil {
			if bal, err := strconv.ParseInt(live.BalanceAmount, 10, 64); err == nil {
				c.Balance = &bal
			}
			// The last four digits only exist once the card has been issued.
			if c.Last4 == "" && live.LastFour != "" {
				c.Last4, c.Brand = live.LastFour, live.CardBrand
				s.Pool.Exec(ctx, `update cards set last4 = $2, brand = nullif($3, '') where id = $1`, r.id, live.LastFour, live.CardBrand)
			}
		}
	}
	v.Card = c
	return v, nil
}

// KYCInput is what card KYC needs beyond the account's own details.
type KYCInput struct {
	BVN              string `json:"bvn"`
	Line1            string `json:"line1"`
	City             string `json:"city"`
	State            string `json:"state"`
	PostalCode       string `json:"postal_code"`
	Occupation       string `json:"occupation"`
	EmploymentStatus string `json:"employment_status"`
	AccountPurpose   string `json:"account_purpose"`
	AnnualSalary     string `json:"annual_salary"`
	MonthlyVolume    string `json:"expected_monthly_volume"`
	AcceptTerms      bool   `json:"accept_terms"`
}

var (
	bvnPattern = regexp.MustCompile(`^\d{11}$`)
	wholeUnits = regexp.MustCompile(`^\d{1,12}$`)
	snakeCase  = regexp.MustCompile(`^[a-z][a-z_]{1,40}$`)
)

func (s *Service) hashBVN(bvn string) string {
	mac := hmac.New(sha256.New, []byte(s.HashKey))
	mac.Write([]byte(bvn))
	return hex.EncodeToString(mac.Sum(nil))
}

// SubmitKYC runs Bitnob's card KYC for the user. It returns the resulting
// status: "approved", or "pending" while Bitnob reviews.
func (s *Service) SubmitKYC(ctx context.Context, userID uuid.UUID, in KYCInput) (string, error) {
	if !s.Bitnob.Configured() {
		return "", ErrUnavailable
	}
	for _, f := range []*string{&in.BVN, &in.Line1, &in.City, &in.State, &in.PostalCode} {
		*f = strings.TrimSpace(*f)
	}
	switch {
	case !in.AcceptTerms:
		return "", &ValidationError{"You need to accept the card terms."}
	case !bvnPattern.MatchString(in.BVN):
		return "", &ValidationError{"Your BVN is 11 digits."}
	case in.Line1 == "" || in.City == "" || in.State == "" || in.PostalCode == "":
		return "", &ValidationError{"Enter your full home address."}
	case !snakeCase.MatchString(in.Occupation) || !snakeCase.MatchString(in.EmploymentStatus) || !snakeCase.MatchString(in.AccountPurpose):
		return "", &ValidationError{"Tell us your work and what the card is for."}
	case !wholeUnits.MatchString(in.AnnualSalary) || !wholeUnits.MatchString(in.MonthlyVolume):
		return "", &ValidationError{"Pick your income and expected monthly spend."}
	}

	var (
		first, last, email *string
		dob                *time.Time
		phone              string
		sameBVN            bool
	)
	err := s.Pool.QueryRow(ctx,
		`select first_name, last_name, email::text, date_of_birth, phone,
		        exists (select 1 from kyc_records k
		                 where k.user_id = u.id and k.id_type = 'bvn' and k.status = 'approved' and k.id_reference = $2)
		 from users u where u.id = $1`, userID, s.hashBVN(in.BVN),
	).Scan(&first, &last, &email, &dob, &phone, &sameBVN)
	if err != nil {
		return "", err
	}
	if first == nil || last == nil || email == nil || dob == nil {
		return "", &ValidationError{"Finish setting up your account first."}
	}
	if !sameBVN {
		return "", &ValidationError{"That isn't the BVN this account was opened with."}
	}
	digits := strings.TrimPrefix(phone, "+")
	if !strings.HasPrefix(digits, "234") || len(digits) < 13 {
		return "", &ValidationError{"A Nigerian phone number is required."}
	}

	var req bitnob.CardKYCRequest
	c := &req.Customer
	c.CustomerType, c.FirstName, c.LastName, c.Email = "individual", *first, *last, *email
	c.DialCode, c.PhoneNumber, c.DateOfBirth, c.Country = "+234", digits[3:], dob.Format("2006-01-02"), "NGA"
	c.Line1, c.City, c.State, c.PostalCode = in.Line1, in.City, in.State, in.PostalCode
	c.IDType, c.IDNumber = "bvn", in.BVN
	req.IDType, req.IDNumber = "bvn", in.BVN
	req.Occupation, req.EmploymentStatus, req.AccountPurpose = in.Occupation, in.EmploymentStatus, in.AccountPurpose
	req.AnnualSalary, req.ExpectedMonthlyVolume, req.TermsAccepted = in.AnnualSalary, in.MonthlyVolume, true

	res, err := s.Bitnob.CardKYC(ctx, req)
	if err != nil {
		return "", s.translate(err)
	}
	status := "pending"
	switch strings.ToLower(res.NormalizedStatus) {
	case "approved":
		status = "approved"
	case "rejected", "failed", "declined":
		status = "rejected"
	}
	_, err = s.Pool.Exec(ctx,
		`update users set card_customer_id = $2, card_kyc_status = $3 where id = $1`, userID, res.CustomerID, status)
	return status, err
}

func (s *Service) translate(err error) error {
	var apiErr *bitnob.APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	detail := apiErr.Detail()
	switch {
	case strings.Contains(strings.ToLower(detail), "insufficient company balance"):
		// Our USDC float at Bitnob is short, not the user's balance.
		s.Log.Error("card operation blocked: company USDC balance too low at Bitnob", "detail", detail)
		return ErrUnavailable
	case apiErr.Status == 400 && detail != "":
		return &ValidationError{detail}
	case apiErr.Status == 429:
		return &ValidationError{"Give it a minute and try again."}
	}
	return err
}

// Transfer is a movement between the user's USDC balance and their card.
type Transfer struct {
	ID     uuid.UUID `json:"id"`
	Kind   string    `json:"kind"`   // create | fund | withdraw
	Status string    `json:"status"` // pending | success | failed
	Amount int64     `json:"amount"`
	Fee    int64     `json:"fee"`
}

// begin records a transfer and, for money leaving the balance, reserves it.
// The id is derived from the user's key so a retry lands on the same row.
func (s *Service) begin(ctx context.Context, userID, cardID uuid.UUID, kind, key string, amount, fee, cost int64) (Transfer, bool, error) {
	t := Transfer{
		ID: uuid.NewSHA1(userID, []byte("card:"+kind+":"+key)), Kind: kind, Status: "pending", Amount: amount, Fee: fee,
	}
	var existing bool
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`insert into card_transfers (id, user_id, card_id, kind, amount, fee, cost)
			 values ($1, $2, $3, $4, $5, $6, $7) on conflict (id) do nothing`,
			t.ID, userID, cardID, kind, amount, fee, cost)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			existing = true
			return tx.QueryRow(ctx, `select status from card_transfers where id = $1`, t.ID).Scan(&t.Status)
		}
		if kind == "withdraw" {
			return nil // nothing to reserve: the money is on the card
		}
		hold, err := ledger.PostTx(ctx, tx, ledger.Entry{
			IdempotencyKey: "card:" + t.ID.String() + ":hold",
			Kind:           "card_hold",
			Lines: []ledger.Line{
				ledger.Debit(ledger.UserAccount(userID, asset), amount+fee),
				ledger.Credit(ledger.PendingAccount(userID, asset), amount+fee),
			},
		})
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `update card_transfers set hold_entry_id = $2 where id = $1`, t.ID, hold.EntryID)
		return err
	})
	return t, existing, err
}

// refused handles Bitnob's answer to a card request: a definite refusal
// undoes the transfer; anything uncertain is left for the sweeper.
func (s *Service) refused(ctx context.Context, t *Transfer, err error) error {
	var apiErr *bitnob.APIError
	if errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 500 && !bitnob.IsDuplicate(err) {
		if ferr := s.finish(ctx, t.ID, "failed", apiErr.Detail(), []byte(apiErr.Body)); ferr != nil {
			return ferr
		}
		t.Status = "failed"
		if terr := s.translate(err); !errors.As(terr, &apiErr) {
			return terr
		}
		return &ValidationError{"That didn't go through. Nothing was taken from your balance."}
	}
	s.Log.Error("card request outcome unknown", "transfer", t.ID, "err", err)
	return nil
}

// Create issues the user's card, loaded with amount micro-dollars from their
// USDC balance. The card arrives pending and turns active when Bitnob
// confirms.
func (s *Service) Create(ctx context.Context, userID uuid.UUID, amount int64, key string) (Transfer, error) {
	if !s.Bitnob.Configured() {
		return Transfer{}, ErrUnavailable
	}
	if key == "" {
		return Transfer{}, &ValidationError{"idempotency_key is required"}
	}
	if amount < 1_000_000 {
		return Transfer{}, &ValidationError{"Load at least $1 onto your card."}
	}
	var (
		customer, kyc *string
		first, last   *string
	)
	if err := s.Pool.QueryRow(ctx,
		`select card_customer_id, card_kyc_status, first_name, last_name from users where id = $1`, userID,
	).Scan(&customer, &kyc, &first, &last); err != nil {
		return Transfer{}, err
	}
	if customer == nil || kyc == nil || *kyc != "approved" || first == nil || last == nil {
		return Transfer{}, &ValidationError{"Finish card verification first."}
	}
	name := *first + " " + *last

	var cardID uuid.UUID
	err := s.Pool.QueryRow(ctx,
		`insert into cards (user_id, name) values ($1, $2) returning id`, userID, name).Scan(&cardID)
	if err != nil {
		if strings.Contains(err.Error(), "cards_one_live_idx") {
			return Transfer{}, &ValidationError{"You already have a card."}
		}
		return Transfer{}, err
	}
	t, existing, err := s.begin(ctx, userID, cardID, "create", key, amount, s.Fees.CreationFee, s.Fees.CreationCost)
	if err != nil || existing {
		// Either a retry of the same request or no funds: this card row is spare.
		s.Pool.Exec(ctx, `update cards set status = 'failed' where id = $1`, cardID)
		return t, err
	}

	card, raw, err := s.Bitnob.CreateCard(ctx, *customer, name, t.ID.String(), amount)
	if err != nil {
		return t, s.refused(ctx, &t, err)
	}
	if _, err := s.Pool.Exec(ctx,
		`update cards set bitnob_card_id = $2, brand = nullif($3, ''), last4 = nullif($4, '') where id = $1`,
		cardID, card.ID, card.CardBrand, card.LastFour); err != nil {
		return t, err
	}
	s.Pool.Exec(ctx, `update card_transfers set raw = $2 where id = $1`, t.ID, raw)
	if card.CreatedStatus == "completed" || card.Status == "active" {
		t.Status = "success"
		return t, s.finish(ctx, t.ID, "success", "", nil)
	}
	if card.CreatedStatus == "failed" {
		t.Status = "failed"
		return t, s.finish(ctx, t.ID, "failed", "The card could not be issued.", nil)
	}
	return t, nil
}

// Move loads ("fund") or unloads ("withdraw") the card.
func (s *Service) Move(ctx context.Context, userID uuid.UUID, kind string, amount int64, key string) (Transfer, error) {
	if !s.Bitnob.Configured() {
		return Transfer{}, ErrUnavailable
	}
	if key == "" {
		return Transfer{}, &ValidationError{"idempotency_key is required"}
	}
	if amount < 1_000_000 {
		return Transfer{}, &ValidationError{"The minimum is $1."}
	}
	card, err := s.liveCard(ctx, userID)
	if err != nil {
		return Transfer{}, err
	}
	if card.status != "active" || card.bitnobID == nil {
		return Transfer{}, &ValidationError{"Your card isn't active. Unlock it first."}
	}
	fee, cost := s.Fees.FundFee, s.Fees.FundCost
	if kind == "withdraw" {
		fee, cost = 0, 0
	}
	t, existing, err := s.begin(ctx, userID, card.id, kind, key, amount, fee, cost)
	if err != nil || existing {
		return t, err
	}
	raw, err := s.Bitnob.MoveCardBalance(ctx, *card.bitnobID, kind, t.ID.String(), amount)
	if err != nil {
		return t, s.refused(ctx, &t, err)
	}
	s.Pool.Exec(ctx, `update card_transfers set raw = $2 where id = $1`, t.ID, raw)
	return t, nil
}

func (s *Service) finish(ctx context.Context, id uuid.UUID, status, reason string, raw []byte) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		return FinishTx(ctx, tx, id, status, reason, raw)
	})
}

// FinishTx settles or undoes a pending card transfer inside tx. Calling it
// again is harmless.
//
//	create/fund success: debit user:USDC:pending (amount + fee)
//	                     credit omnibus:USDC (amount + cost), revenue:fees:USDC (fee - cost)
//	create/fund failed:  debit user:USDC:pending; credit user:USDC
//	withdraw success:    debit omnibus:USDC; credit user:USDC
//	withdraw failed:     nothing moved
func FinishTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, status, reason string, raw []byte) error {
	var (
		userID, cardID    uuid.UUID
		kind, current     string
		amount, fee, cost int64
	)
	err := tx.QueryRow(ctx,
		`select user_id, card_id, kind, status, amount, fee, cost from card_transfers where id = $1 for update`, id,
	).Scan(&userID, &cardID, &kind, &current, &amount, &fee, &cost)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if current != "pending" {
		return nil
	}
	if status != "success" && status != "failed" {
		return fmt.Errorf("cards: cannot finish transfer as %q", status)
	}

	var entry *ledger.Entry
	title, body := "", ""
	dollars := usd(amount)
	switch {
	case kind == "withdraw" && status == "success":
		entry = &ledger.Entry{Kind: "card_withdraw", Lines: []ledger.Line{
			ledger.Debit(ledger.Omnibus(asset), amount),
			ledger.Credit(ledger.UserAccount(userID, asset), amount),
		}}
		title, body = "Money back from your card", dollars+" is in your USDC balance."
	case kind == "withdraw":
		title, body = "Card withdrawal didn't go through", "Your card balance is unchanged."
	case status == "success":
		entry = &ledger.Entry{Kind: "card_" + kind, Lines: []ledger.Line{
			ledger.Debit(ledger.PendingAccount(userID, asset), amount+fee),
			ledger.Credit(ledger.Omnibus(asset), amount+cost),
		}}
		if margin := fee - cost; margin != 0 {
			entry.Lines = append(entry.Lines, ledger.Credit(ledger.RevenueFees(asset), margin))
		}
		title, body = "Card loaded", dollars+" was added to your card."
		if kind == "create" {
			title, body = "Your card is ready", "It's loaded with "+dollars+"."
		}
	default:
		entry = &ledger.Entry{Kind: "card_" + kind + "_failed", Lines: []ledger.Line{
			ledger.Debit(ledger.PendingAccount(userID, asset), amount+fee),
			ledger.Credit(ledger.UserAccount(userID, asset), amount+fee),
		}}
		title, body = "Card load didn't go through", usd(amount+fee)+" is back in your balance."
		if kind == "create" {
			title = "We couldn't issue your card"
		}
	}

	var entryID *uuid.UUID
	if entry != nil {
		entry.IdempotencyKey = "card:" + id.String() + ":" + status
		res, err := ledger.PostTx(ctx, tx, *entry)
		if err != nil {
			return err
		}
		entryID = &res.EntryID
	}
	if raw == nil {
		raw = []byte("null")
	}
	if _, err := tx.Exec(ctx,
		`update card_transfers
		 set status = $2, settle_entry_id = $3, failure_reason = nullif($4, ''),
		     raw = coalesce(nullif($5::jsonb, 'null'), raw)
		 where id = $1`, id, status, entryID, reason, raw); err != nil {
		return err
	}
	if kind == "create" {
		next := "active"
		if status == "failed" {
			next = "failed"
		}
		if _, err := tx.Exec(ctx, `update cards set status = $2 where id = $1 and status = 'pending'`, cardID, next); err != nil {
			return err
		}
	}
	return notify.Queue(ctx, tx, userID, title, body)
}

// usd renders micro-dollars as "$12.50".
func usd(micro int64) string {
	return fmt.Sprintf("$%d.%02d", micro/1_000_000, micro%1_000_000/10_000)
}

// Secrets are shown once on screen and never stored or logged.
type Secrets struct {
	Number      string `json:"number"`
	CVV         string `json:"cvv"`
	ExpiryMonth string `json:"expiry_month"`
	ExpiryYear  string `json:"expiry_year"`
	Name        string `json:"name"`
	Billing     string `json:"billing_address"`
}

// Reveal fetches the full card number, security code and billing address.
func (s *Service) Reveal(ctx context.Context, userID uuid.UUID) (Secrets, error) {
	card, err := s.liveCard(ctx, userID)
	if err != nil {
		return Secrets{}, err
	}
	if card.bitnobID == nil || card.status == "pending" {
		return Secrets{}, &ValidationError{"Your card is still being made."}
	}
	d, err := s.Bitnob.CardSecrets(ctx, *card.bitnobID)
	if err != nil {
		return Secrets{}, s.translate(err)
	}
	out := Secrets{Number: d.CardNumber, CVV: d.CVV, ExpiryMonth: d.ExpiryMonth, ExpiryYear: d.ExpiryYear, Name: d.Name}
	if live, err := s.Bitnob.GetCard(ctx, *card.bitnobID); err == nil {
		a := live.BillingAddress
		var parts []string
		for _, p := range []string{a.Line1, a.City, a.State, a.PostalCode, a.Country} {
			if p != "" {
				parts = append(parts, p)
			}
		}
		out.Billing = strings.Join(parts, ", ")
	}
	return out, nil
}

// SetLocked freezes or unfreezes the card. A locked card declines everything.
func (s *Service) SetLocked(ctx context.Context, userID uuid.UUID, locked bool) (string, error) {
	card, err := s.liveCard(ctx, userID)
	if err != nil {
		return "", err
	}
	if card.bitnobID == nil || card.status == "pending" {
		return "", &ValidationError{"Your card is still being made."}
	}
	status := "active"
	if locked {
		status = "frozen"
	}
	if err := s.Bitnob.SetCardStatus(ctx, *card.bitnobID, status); err != nil {
		return "", s.translate(err)
	}
	_, err = s.Pool.Exec(ctx, `update cards set status = $2 where id = $1`, card.id, status)
	return status, err
}

// Statement is one line of card activity, in micro-dollars.
type Statement struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Status      string `json:"status"`
	Description string `json:"description"`
	Amount      int64  `json:"amount"`
	CreatedAt   string `json:"created_at"`
}

func (s *Service) Transactions(ctx context.Context, userID uuid.UUID) ([]Statement, error) {
	card, err := s.liveCard(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := []Statement{}
	if card.bitnobID == nil {
		return out, nil
	}
	txns, err := s.Bitnob.CardTransactions(ctx, *card.bitnobID)
	if err != nil {
		return nil, s.translate(err)
	}
	for _, t := range txns {
		amount, _ := strconv.ParseInt(t.Amount, 10, 64)
		out = append(out, Statement{ID: t.ID, Type: t.Type, Status: t.Status, Description: t.Description, Amount: amount, CreatedAt: t.CreatedAt})
	}
	return out, nil
}

// event covers the card webhooks we act on. Field names differ by event, so
// each is read wherever it appears.
type event struct {
	Data struct {
		ID           string `json:"id"`
		CardID       string `json:"cardId"`
		Reference    string `json:"reference"`
		CustomerID   string `json:"customerId"`
		DisplayAmt   any    `json:"displayAmount"`
		MerchantName string `json:"merchantName"`
		Reason       string `json:"reason"`
	} `json:"data"`
}

// ApplyWebhook handles virtualcard.* events: issuing, loading and unloading
// outcomes, KYC results, and purchases (which only notify).
func ApplyWebhook(ctx context.Context, tx pgx.Tx, name string, payload []byte) error {
	var ev event
	if err := json.Unmarshal(payload, &ev); err != nil {
		return err
	}
	d := ev.Data

	switch name {
	case "virtualcard.user.kyc.complete", "virtualcard.user.kyc.failed":
		status := "approved"
		if strings.HasSuffix(name, "failed") {
			status = "rejected"
		}
		_, err := tx.Exec(ctx,
			`update users set card_kyc_status = $2 where card_customer_id = nullif($1, '')`, d.CustomerID, status)
		return err

	case "virtualcard.created.completed", "virtualcard.created.failed",
		"virtualcard.topup.completed", "virtualcard.topup.failed",
		"virtualcard.withdrawal.completed", "virtualcard.withdrawal.failed":
		// The reference is the transfer id we sent.
		id, err := uuid.Parse(d.Reference)
		if err != nil {
			return nil // not one of ours
		}
		status := "success"
		if strings.HasSuffix(name, "failed") {
			status = "failed"
		}
		if name == "virtualcard.created.completed" && d.ID != "" {
			if _, err := tx.Exec(ctx,
				`update cards set bitnob_card_id = coalesce(bitnob_card_id, $2)
				 where id = (select card_id from card_transfers where id = $1)`, id, d.ID); err != nil {
				return err
			}
		}
		return FinishTx(ctx, tx, id, status, d.Reason, payload)

	case "virtualcard.transaction.debit", "virtualcard.transaction.authorization", "virtualcard.transaction.declined":
		var userID uuid.UUID
		err := tx.QueryRow(ctx, `select user_id from cards where bitnob_card_id = nullif($1, '')`, d.CardID).Scan(&userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		where := d.MerchantName
		if where == "" {
			where = "a merchant"
		}
		amount := ""
		if d.DisplayAmt != nil {
			amount = fmt.Sprintf("$%v ", d.DisplayAmt)
		}
		if name == "virtualcard.transaction.declined" {
			return notify.Queue(ctx, tx, userID, "Card declined", fmt.Sprintf("%sat %s was declined.", amount, where))
		}
		return notify.Queue(ctx, tx, userID, "Card payment", fmt.Sprintf("%sat %s.", amount, where))
	}
	return nil
}

// Sweep finishes card transfers whose webhook never arrived, by asking
// Bitnob for the card's state or statement.
func (s *Service) Sweep(ctx context.Context) error {
	if !s.Bitnob.Configured() {
		return nil
	}
	rows, err := s.Pool.Query(ctx,
		`select t.id, t.kind, c.bitnob_card_id from card_transfers t join cards c on c.id = t.card_id
		 where t.status = 'pending' and t.created_at < now() - interval '1 minute' and c.bitnob_card_id is not null
		 order by t.created_at limit 50`)
	if err != nil {
		return err
	}
	type open struct {
		id   uuid.UUID
		kind string
		card string
	}
	waiting, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (open, error) {
		var o open
		return o, row.Scan(&o.id, &o.kind, &o.card)
	})
	if err != nil {
		return err
	}
	for _, o := range waiting {
		status := ""
		if o.kind == "create" {
			card, err := s.Bitnob.GetCard(ctx, o.card)
			if err != nil {
				s.Log.Error("sweep: get card", "transfer", o.id, "err", err)
				continue
			}
			switch {
			case card.CreatedStatus == "completed" || card.Status == "active":
				status = "success"
			case card.CreatedStatus == "failed":
				status = "failed"
			}
		} else {
			txns, err := s.Bitnob.CardTransactions(ctx, o.card)
			if err != nil {
				s.Log.Error("sweep: card transactions", "transfer", o.id, "err", err)
				continue
			}
			for _, t := range txns {
				if !strings.HasPrefix(t.Reference, o.id.String()) {
					continue
				}
				switch t.Status {
				case "completed":
					status = "success"
				case "failed":
					status = "failed"
				}
			}
		}
		if status == "" {
			continue
		}
		if err := s.finish(ctx, o.id, status, "", nil); err != nil {
			s.Log.Error("sweep: finish card transfer", "transfer", o.id, "err", err)
		}
	}
	return nil
}
