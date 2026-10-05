// Package payouts covers M3: sending money out to a bank account or mobile
// wallet. Each payout is quote, initialize, finalize at Bitnob; the user's
// funds sit in their pending account until the rail confirms.
package payouts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deecaulcrick/neobank/backend/internal/bitnob"
	"github.com/deecaulcrick/neobank/backend/internal/ledger"
	"github.com/deecaulcrick/neobank/backend/internal/money"
)

type Service struct {
	Pool   *pgxpool.Pool
	Bitnob *bitnob.Client
	// FeeBps is our fee, in basis points of what Bitnob takes for the payout.
	FeeBps int64
	Log    *slog.Logger

	mu        sync.Mutex
	countries cached
	details   map[string]cached
}

type cached struct {
	body json.RawMessage
	at   time.Time
}

// ValidationError is safe to show to the user as-is.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

var (
	ErrQuoteNotFound  = errors.New("payouts: quote not found")
	ErrQuoteExpired   = errors.New("payouts: quote expired")
	ErrQuoteUsed      = errors.New("payouts: quote already used")
	ErrPayoutNotFound = errors.New("payouts: payout not found")
	ErrUnavailable    = errors.New("payouts: not available right now")
)

// Rails whose beneficiary form is a flat list of text and select fields.
// SWIFT, wire, ACH and SEPA need nested sender and beneficiary blocks and are
// not offered yet.
var supportedRails = map[string]bool{"bank": true, "mobile_money": true, "paybill": true, "paytill": true}

const cacheFor = 10 * time.Minute

type Corridor struct {
	Currency string   `json:"currency"`
	Rails    []string `json:"rails"`
}

type Country struct {
	Code      string     `json:"code"`
	Name      string     `json:"name"`
	Flag      string     `json:"flag"`
	Corridors []Corridor `json:"corridors"`
}

// Countries is the live corridor list, trimmed to the rails we can serve.
func (s *Service) Countries(ctx context.Context) ([]Country, error) {
	raw, err := s.cachedFetch(ctx, "", func() (json.RawMessage, error) { return s.Bitnob.SupportedCountries(ctx) })
	if err != nil {
		return nil, err
	}
	var body struct {
		Data struct {
			Countries []struct {
				Code      string `json:"code"`
				Name      string `json:"name"`
				Flag      string `json:"flag"`
				Corridors []struct {
					Currency         string   `json:"currency"`
					DestinationTypes []string `json:"destination_types"`
				} `json:"corridors"`
			} `json:"countries"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	out := []Country{}
	for _, c := range body.Data.Countries {
		country := Country{Code: c.Code, Name: c.Name, Flag: c.Flag}
		for _, k := range c.Corridors {
			var rails []string
			for _, r := range k.DestinationTypes {
				if supportedRails[r] {
					rails = append(rails, r)
				}
			}
			if len(rails) > 0 {
				country.Corridors = append(country.Corridors, Corridor{Currency: k.Currency, Rails: rails})
			}
		}
		if len(country.Corridors) > 0 {
			out = append(out, country)
		}
	}
	return out, nil
}

var countryCode = regexp.MustCompile(`^[A-Z]{2}$`)

// CountryDetails passes through Bitnob's per-rail form definition (fields,
// banks, limits) for the rails we serve. The app renders it as given.
func (s *Service) CountryDetails(ctx context.Context, code string) (json.RawMessage, error) {
	code = strings.ToUpper(code)
	if !countryCode.MatchString(code) {
		return nil, &ValidationError{"Unknown country."}
	}
	raw, err := s.cachedFetch(ctx, code, func() (json.RawMessage, error) { return s.Bitnob.CountryDetails(ctx, code) })
	if err != nil {
		return nil, err
	}
	var body struct {
		Data struct {
			Code             string                     `json:"code"`
			Name             string                     `json:"name"`
			Flag             string                     `json:"flag"`
			DestinationTypes map[string]json.RawMessage `json:"destination_types"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	for rail := range body.Data.DestinationTypes {
		if !supportedRails[rail] {
			delete(body.Data.DestinationTypes, rail)
		}
	}
	return json.Marshal(map[string]any{
		"code": body.Data.Code, "name": body.Data.Name, "flag": body.Data.Flag,
		"rails": body.Data.DestinationTypes,
	})
}

func (s *Service) cachedFetch(ctx context.Context, key string, fetch func() (json.RawMessage, error)) (json.RawMessage, error) {
	if !s.Bitnob.Configured() {
		return nil, ErrUnavailable
	}
	s.mu.Lock()
	hit := s.countries
	if key != "" {
		hit = s.details[key]
	}
	s.mu.Unlock()
	if hit.body != nil && time.Since(hit.at) < cacheFor {
		return hit.body, nil
	}
	body, err := fetch()
	if err != nil {
		if hit.body != nil {
			return hit.body, nil // stale beats unavailable for a directory
		}
		return nil, translateBitnob(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if key == "" {
		s.countries = cached{body, time.Now()}
	} else {
		if s.details == nil {
			s.details = map[string]cached{}
		}
		s.details[key] = cached{body, time.Now()}
	}
	return body, nil
}

// LookupAccount resolves the name on the destination account, where the rail
// supports it (Nigerian banks, Ghanaian mobile money).
func (s *Service) LookupAccount(ctx context.Context, country, rail, provider, account string) (string, error) {
	if !s.Bitnob.Configured() {
		return "", ErrUnavailable
	}
	q := url.Values{"country": {strings.ToUpper(country)}, "account_number": {account}, "bank_code": {provider}}
	if rail == "mobile_money" {
		q.Set("type", "mobile_money")
		q.Set("bank_code", strings.ToUpper(provider))
	}
	raw, err := s.Bitnob.AccountLookup(ctx, q)
	if err != nil {
		return "", translateBitnob(err)
	}
	var body struct {
		Data struct {
			AccountName string `json:"account_name"`
			IsVerified  bool   `json:"is_verified"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return "", err
	}
	if !body.Data.IsVerified || body.Data.AccountName == "" {
		return "", &ValidationError{"We couldn't find that account. Check the details."}
	}
	return body.Data.AccountName, nil
}

// Quote is a locked payout rate. FromAmount and FeeAmount are minor units of
// FromAsset; ToAmount is hundredths of ToCurrency.
type Quote struct {
	ID        uuid.UUID   `json:"id"`
	FromAsset money.Asset `json:"from_asset"`
	// FromAmount is everything the user pays, our fee included.
	FromAmount int64  `json:"from_amount"`
	FeeAmount  int64  `json:"fee_amount"`
	Country    string `json:"country"`
	ToCurrency string `json:"to_currency"`
	// ToAmount is what the beneficiary receives.
	ToAmount    int64     `json:"to_amount"`
	Rate        string    `json:"rate"` // ToCurrency per one FromAsset, all-in
	ExpiresAt   time.Time `json:"expires_at"`
	EnoughFunds bool      `json:"enough_funds"`
}

type QuoteInput struct {
	Country    string
	ToCurrency string
	FromAsset  money.Asset
	// Exactly one of these is set: what the user sends (minor units of
	// FromAsset, before our fee) or what the beneficiary should receive
	// (decimal string in ToCurrency).
	Amount           int64
	SettlementAmount string
}

var (
	currencyCode = regexp.MustCompile(`^[A-Z]{3}$`)
	decimal      = regexp.MustCompile(`^\d+(\.\d{1,2})?$`)
)

func (s *Service) CreateQuote(ctx context.Context, userID uuid.UUID, in QuoteInput) (Quote, error) {
	in.Country, in.ToCurrency = strings.ToUpper(in.Country), strings.ToUpper(in.ToCurrency)
	if !countryCode.MatchString(in.Country) || !currencyCode.MatchString(in.ToCurrency) {
		return Quote{}, &ValidationError{"Pick a destination."}
	}
	if (in.Amount > 0) == (in.SettlementAmount != "") {
		return Quote{}, &ValidationError{"Enter an amount."}
	}
	if in.SettlementAmount != "" && !decimal.MatchString(in.SettlementAmount) {
		return Quote{}, &ValidationError{"Enter a valid amount."}
	}
	if !s.Bitnob.Configured() {
		return Quote{}, ErrUnavailable
	}

	id := uuid.New()
	req := bitnob.PayoutQuoteRequest{
		Country: in.Country, FromAsset: string(in.FromAsset), ToCurrency: in.ToCurrency,
		Source: "offchain", Reference: id.String(), SettlementAmount: in.SettlementAmount,
	}
	if in.Amount > 0 {
		req.Amount = plain(in.FromAsset, in.Amount)
	}
	bp, raw, err := s.Bitnob.CreatePayoutQuote(ctx, req)
	if err != nil {
		return Quote{}, translateBitnob(err)
	}

	// Round what Bitnob takes up to our smallest unit, so we never under-collect.
	cost, err := toMinor(bp.TotalAmount, in.FromAsset.Decimals(), true)
	if err != nil {
		return Quote{}, err
	}
	settles, err := toMinor(bp.SettlementAmount, 2, false)
	if err != nil {
		return Quote{}, err
	}
	if cost <= 0 || settles <= 0 {
		return Quote{}, &ValidationError{"That amount is too small to send."}
	}
	fee := cost * s.FeeBps / 10_000

	q := Quote{
		ID: id, FromAsset: in.FromAsset, FromAmount: cost + fee, FeeAmount: fee,
		Country: in.Country, ToCurrency: in.ToCurrency, ToAmount: settles, ExpiresAt: bp.ExpiresAt,
	}
	q.Rate = new(big.Rat).SetFrac(
		new(big.Int).Mul(big.NewInt(settles), pow10(in.FromAsset.Decimals())),
		new(big.Int).Mul(big.NewInt(q.FromAmount), pow10(2)),
	).FloatString(10)

	_, err = s.Pool.Exec(ctx,
		`insert into quotes (id, user_id, kind, bitnob_quote_id, from_asset, from_amount, to_currency, to_amount,
		                     rate, fee_asset, fee_amount, expires_at, raw)
		 values ($1, $2, 'payout', $3, $4::text::asset, $5, $6, $7, $8, $4::text::asset, $9, $10, $11)`,
		id, userID, bp.QuoteID, string(in.FromAsset), q.FromAmount, in.ToCurrency, settles, q.Rate, fee, bp.ExpiresAt, raw)
	if err != nil {
		return Quote{}, err
	}

	var available int64
	if err := s.Pool.QueryRow(ctx,
		`select coalesce((select balance from ledger_accounts where code = $1), 0)`,
		ledger.UserAccount(userID, in.FromAsset)).Scan(&available); err != nil {
		return Quote{}, err
	}
	q.EnoughFunds = available >= q.FromAmount
	return q, nil
}

func pow10(n int) *big.Int { return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil) }

// toMinor converts a decimal string to minor units at the given precision,
// rounding any excess precision up or down as asked.
func toMinor(dec string, decimals int, roundUp bool) (int64, error) {
	r, ok := new(big.Rat).SetString(strings.TrimSpace(dec))
	if !ok || r.Sign() < 0 {
		return 0, fmt.Errorf("payouts: bad amount %q", dec)
	}
	r.Mul(r, new(big.Rat).SetInt(pow10(decimals)))
	minor, rem := new(big.Int).QuoRem(r.Num(), r.Denom(), new(big.Int))
	if roundUp && rem.Sign() != 0 {
		minor.Add(minor, big.NewInt(1))
	}
	if !minor.IsInt64() {
		return 0, fmt.Errorf("payouts: amount %q overflows", dec)
	}
	return minor.Int64(), nil
}

func plain(a money.Asset, minor int64) string {
	s := money.Format(a, minor)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s
}

func translateBitnob(err error) error {
	var apiErr *bitnob.APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	detail := apiErr.Detail()
	lower := strings.ToLower(detail)
	switch {
	case strings.Contains(lower, "insufficient"):
		return ErrUnavailable // our pre-funded balance at Bitnob, not the user's
	case strings.Contains(lower, "expired"):
		return ErrQuoteExpired
	case apiErr.Status == 400 && detail != "":
		// Limits and beneficiary validation read well enough to show.
		return &ValidationError{detail}
	}
	return err
}

// Beneficiary is who gets paid. Fields holds the rail's field keys exactly as
// Country Details names them (bank_code, account_number, network, ...).
type Beneficiary struct {
	Rail        string            `json:"rail"`
	AccountName string            `json:"account_name"`
	Fields      map[string]string `json:"fields"`
}

type SendInput struct {
	QuoteID       uuid.UUID   `json:"quote_id"`
	Beneficiary   Beneficiary `json:"beneficiary"`
	PaymentReason string      `json:"payment_reason"`
}

// Payout is what the app shows. Status is one of processing, success,
// expired, failed ("processing" until the rail confirms, never "sent" early).
type Payout struct {
	ID          uuid.UUID   `json:"id"`
	Status      string      `json:"status"`
	FromAsset   money.Asset `json:"from_asset"`
	FromAmount  int64       `json:"from_amount"`
	ToCurrency  string      `json:"to_currency"`
	ToAmount    int64       `json:"to_amount"`
	Beneficiary string      `json:"beneficiary_name"`
	CreatedAt   time.Time   `json:"created_at"`
}

var reasons = map[string]bool{"family_support": true, "education": true, "salary": true, "vendor_payment": true}

// Send consumes a quote: it reserves the user's funds, then initializes and
// finalizes the payout at Bitnob. The payout is finished later by a webhook
// or the sweeper.
func (s *Service) Send(ctx context.Context, userID uuid.UUID, in SendInput) (Payout, error) {
	b := in.Beneficiary
	b.AccountName = strings.TrimSpace(b.AccountName)
	if !supportedRails[b.Rail] || b.AccountName == "" || len(b.Fields) == 0 {
		return Payout{}, &ValidationError{"Add the recipient's details."}
	}
	if !reasons[in.PaymentReason] {
		in.PaymentReason = "family_support"
	}
	// TODO: sanctions and name screening on the beneficiary before sending
	// (spec: "Fraud and risk controls"), plus velocity limits and new-account holds.

	var (
		p             = Payout{ID: uuid.New(), Beneficiary: b.AccountName, CreatedAt: time.Now()}
		bitnobQuoteID string
		bitnobID      string
		country       string
	)
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var (
			expiresAt  time.Time
			consumedAt *time.Time
		)
		err := tx.QueryRow(ctx,
			`select bitnob_quote_id, from_asset::text, from_amount, to_currency, to_amount, expires_at, consumed_at,
			        raw->'data'->'payout'->>'id', raw->'data'->'payout'->>'country'
			 from quotes where id = $1 and user_id = $2 and kind = 'payout' for update`,
			in.QuoteID, userID,
		).Scan(&bitnobQuoteID, &p.FromAsset, &p.FromAmount, &p.ToCurrency, &p.ToAmount, &expiresAt, &consumedAt,
			&bitnobID, &country)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrQuoteNotFound
		}
		if err != nil {
			return err
		}
		if consumedAt != nil {
			return ErrQuoteUsed
		}
		if time.Until(expiresAt) < 5*time.Second {
			return ErrQuoteExpired
		}
		if _, err := tx.Exec(ctx, `update quotes set consumed_at = now() where id = $1`, in.QuoteID); err != nil {
			return err
		}

		details, _ := json.Marshal(b.Fields)
		var beneficiaryID uuid.UUID
		if err := tx.QueryRow(ctx,
			`insert into beneficiaries (user_id, country, currency, rail, display_name, details)
			 values ($1, $2, $3, $4, $5, $6) returning id`,
			userID, country, p.ToCurrency, b.Rail, b.AccountName, details,
		).Scan(&beneficiaryID); err != nil {
			return err
		}

		// Payout initiated: user:asset -> user:asset:pending.
		hold, err := ledger.PostTx(ctx, tx, ledger.Entry{
			IdempotencyKey: "payout:" + p.ID.String() + ":hold",
			Kind:           "payout_hold",
			Lines: []ledger.Line{
				ledger.Debit(ledger.UserAccount(userID, p.FromAsset), p.FromAmount),
				ledger.Credit(ledger.PendingAccount(userID, p.FromAsset), p.FromAmount),
			},
		})
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx,
			`insert into payouts (id, user_id, quote_id, beneficiary_id, bitnob_payout_id, reference, status,
			                      hold_entry_id, payment_reason)
			 values ($1::uuid, $2, $3, $4, $5, $1::uuid::text, 'initialized', $6, $7)`,
			p.ID, userID, in.QuoteID, beneficiaryID, bitnobID, hold.EntryID, in.PaymentReason)
		return err
	})
	if err != nil {
		return Payout{}, err
	}
	p.Status = "processing"

	beneficiary := map[string]any{"destination_type": b.Rail, "account_name": b.AccountName}
	for k, v := range b.Fields {
		beneficiary[k] = v
	}
	beneficiary["country"] = country

	// A definite refusal at either step means nothing left our balance, so
	// the hold is released. Anything uncertain is left for the sweeper.
	step := func(bp bitnob.Payout, stepRaw json.RawMessage, err error) (done bool, _ error) {
		var apiErr *bitnob.APIError
		if errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 500 {
			reason := apiErr.Detail()
			if ferr := s.finish(ctx, p.ID, "failed", reason, []byte(apiErr.Body)); ferr != nil {
				return true, ferr
			}
			p.Status = "failed"
			if terr := translateBitnob(err); !errors.As(terr, &apiErr) {
				return true, terr
			}
			return true, &ValidationError{"That payout didn't go through. Nothing was taken from your balance."}
		}
		if err != nil {
			s.Log.Error("payout step outcome unknown", "payout", p.ID, "err", err)
			return true, nil
		}
		if status := ours(bp.Status); status != "" && status != "processing" {
			p.Status = status
			return true, s.finish(ctx, p.ID, status, "", stepRaw)
		}
		return false, nil
	}

	if done, err := step(s.Bitnob.InitializePayout(ctx, bitnobQuoteID, p.ID.String(), in.PaymentReason, beneficiary)); done {
		return p, err
	}
	if done, err := step(s.Bitnob.FinalizePayout(ctx, bitnobQuoteID)); done {
		return p, err
	}
	_, err = s.Pool.Exec(ctx, `update payouts set status = 'processing' where id = $1 and status = 'initialized'`, p.ID)
	return p, err
}

// ours maps Bitnob's payout record status to our final states. Empty means
// "no change yet".
func ours(bitnobStatus string) string {
	switch strings.ToUpper(bitnobStatus) {
	case "SUCCESS", "COMPLETED":
		return "success"
	case "EXPIRED":
		return "expired"
	case "FAILED", "CANCELLED", "REJECTED":
		return "failed"
	case "PENDING", "PROCESSING":
		return "processing"
	}
	return ""
}

func (s *Service) finish(ctx context.Context, id uuid.UUID, status, reason string, raw []byte) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		return FinishTx(ctx, tx, id, status, reason, raw)
	})
}

// FinishTx moves an in-flight payout to success, expired or failed inside tx
// and posts the matching entry. Calling it again is harmless.
//
//	success:        debit user:asset:pending; credit omnibus:asset, revenue:fees:asset
//	expired/failed: debit user:asset:pending; credit user:asset
func FinishTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, status, reason string, raw []byte) error {
	var (
		userID  uuid.UUID
		current string
		asset   money.Asset
		total   int64
		fee     int64
	)
	err := tx.QueryRow(ctx,
		`select p.user_id, p.status::text, q.from_asset::text, q.from_amount, q.fee_amount
		 from payouts p join quotes q on q.id = p.quote_id
		 where p.id = $1 for update of p`, id,
	).Scan(&userID, &current, &asset, &total, &fee)
	if err != nil {
		return err
	}
	if current != "initialized" && current != "processing" {
		return nil
	}

	var entry ledger.Entry
	switch status {
	case "success":
		entry = ledger.Entry{
			IdempotencyKey: "payout:" + id.String() + ":settle",
			Kind:           "payout_success",
			Lines: []ledger.Line{
				ledger.Debit(ledger.PendingAccount(userID, asset), total),
				ledger.Credit(ledger.Omnibus(asset), total-fee),
			},
		}
		if fee > 0 {
			entry.Lines = append(entry.Lines, ledger.Credit(ledger.RevenueFees(asset), fee))
		}
	case "expired", "failed":
		entry = ledger.Entry{
			IdempotencyKey: "payout:" + id.String() + ":release",
			Kind:           "payout_" + status,
			Lines: []ledger.Line{
				ledger.Debit(ledger.PendingAccount(userID, asset), total),
				ledger.Credit(ledger.UserAccount(userID, asset), total),
			},
		}
	default:
		return fmt.Errorf("payouts: cannot finish payout as %q", status)
	}
	res, err := ledger.PostTx(ctx, tx, entry)
	if err != nil {
		return err
	}
	if raw == nil {
		raw = []byte("null")
	}
	_, err = tx.Exec(ctx,
		`update payouts
		 set status = $2::text::payout_status, settle_entry_id = $3,
		     failure_reason = nullif($4, ''), raw = coalesce(nullif($5::jsonb, 'null'), raw)
		 where id = $1`,
		id, status, res.EntryID, reason, raw)
	return err
}

// ApplyWebhook handles the payouts.* events. It keys off the event name, not
// data.status, which uses a different vocabulary. reference is our payout id.
func ApplyWebhook(ctx context.Context, tx pgx.Tx, event string, payload []byte) error {
	var body struct {
		Data struct {
			ID        string `json:"id"`
			Reference string `json:"reference"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return err
	}
	var id uuid.UUID
	err := tx.QueryRow(ctx,
		`select id from payouts where reference = $1 or bitnob_payout_id = nullif($2, '')`,
		body.Data.Reference, body.Data.ID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // not one of ours
	}
	if err != nil {
		return err
	}
	switch event {
	case "payouts.withdrawal.success":
		return FinishTx(ctx, tx, id, "success", "", payload)
	case "payouts.withdrawal.expired":
		return FinishTx(ctx, tx, id, "expired", "The payout expired before it completed.", payload)
	case "payouts.processing":
		_, err = tx.Exec(ctx, `update payouts set status = 'processing' where id = $1 and status = 'initialized'`, id)
	}
	// payouts.initialized needs no action: we already hold the funds.
	return err
}

// Sweep polls Bitnob for payouts we are still waiting on and applies the
// result through the same path as the webhooks.
func (s *Service) Sweep(ctx context.Context) error {
	if !s.Bitnob.Configured() {
		return nil
	}
	rows, err := s.Pool.Query(ctx,
		`select id, bitnob_payout_id from payouts
		 where status in ('initialized', 'processing') and bitnob_payout_id is not null
		   and updated_at < now() - interval '30 seconds'
		 order by created_at limit 50`)
	if err != nil {
		return err
	}
	type open struct {
		id       uuid.UUID
		bitnobID string
	}
	waiting, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (open, error) {
		var o open
		return o, row.Scan(&o.id, &o.bitnobID)
	})
	if err != nil {
		return err
	}
	for _, o := range waiting {
		bp, raw, err := s.Bitnob.GetPayout(ctx, o.bitnobID)
		if err != nil {
			s.Log.Error("sweep: get payout", "payout", o.id, "err", err)
			continue
		}
		switch status := ours(bp.Status); status {
		case "success", "expired", "failed":
			if err := s.finish(ctx, o.id, status, "", raw); err != nil {
				s.Log.Error("sweep: finish payout", "payout", o.id, "err", err)
			}
		case "processing":
			s.Pool.Exec(ctx, `update payouts set status = 'processing' where id = $1 and status = 'initialized'`, o.id)
		default:
			// Still QUOTE or INITIATED at Bitnob: finalize never landed. Once
			// the quote has lapsed nothing can move, so give the money back.
			// Any status we don't recognise is left alone rather than guessed at.
			known := strings.EqualFold(bp.Status, "QUOTE") || strings.EqualFold(bp.Status, "INITIATED")
			if !known {
				s.Log.Error("NEEDS REVIEW: payout in unrecognised Bitnob status", "payout", o.id, "status", bp.Status)
				continue
			}
			if !bp.ExpiresAt.IsZero() && time.Now().After(bp.ExpiresAt.Add(time.Minute)) {
				if err := s.finish(ctx, o.id, "expired", "The payout was not completed in time.", raw); err != nil {
					s.Log.Error("sweep: expire payout", "payout", o.id, "err", err)
				}
			}
		}
	}
	return nil
}

// Get returns one of the user's payouts, for the status screen.
func (s *Service) Get(ctx context.Context, userID, id uuid.UUID) (Payout, error) {
	var p Payout
	var status string
	err := s.Pool.QueryRow(ctx,
		`select p.id, p.status::text, q.from_asset::text, q.from_amount, q.to_currency, q.to_amount, b.display_name, p.created_at
		 from payouts p join quotes q on q.id = p.quote_id join beneficiaries b on b.id = p.beneficiary_id
		 where p.id = $1 and p.user_id = $2`, id, userID,
	).Scan(&p.ID, &status, &p.FromAsset, &p.FromAmount, &p.ToCurrency, &p.ToAmount, &p.Beneficiary, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrPayoutNotFound
	}
	if status == "initialized" {
		status = "processing"
	}
	p.Status = status
	return p, err
}

type SavedBeneficiary struct {
	ID          uuid.UUID         `json:"id"`
	Country     string            `json:"country"`
	Currency    string            `json:"currency"`
	Rail        string            `json:"rail"`
	AccountName string            `json:"account_name"`
	Fields      map[string]string `json:"fields"`
}

// Beneficiaries lists who the user has paid before, most recent first, one
// row per distinct destination.
func (s *Service) Beneficiaries(ctx context.Context, userID uuid.UUID) ([]SavedBeneficiary, error) {
	rows, err := s.Pool.Query(ctx,
		`select distinct on (country, rail, details) id, country, currency, rail, display_name, details, created_at
		 from beneficiaries where user_id = $1
		 order by country, rail, details, created_at desc`, userID)
	if err != nil {
		return nil, err
	}
	type row struct {
		SavedBeneficiary
		created time.Time
	}
	all, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var b row
		var details []byte
		if err := r.Scan(&b.ID, &b.Country, &b.Currency, &b.Rail, &b.AccountName, &details, &b.created); err != nil {
			return b, err
		}
		return b, json.Unmarshal(details, &b.Fields)
	})
	if err != nil {
		return nil, err
	}
	// Newest first, capped: this feeds a "recent" list.
	for i := 1; i < len(all); i++ {
		for j := i; j > 0 && all[j].created.After(all[j-1].created); j-- {
			all[j], all[j-1] = all[j-1], all[j]
		}
	}
	out := []SavedBeneficiary{}
	for i, b := range all {
		if i == 10 {
			break
		}
		out = append(out, b.SavedBeneficiary)
	}
	return out, nil
}
