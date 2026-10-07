package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/deecaulcrick/neobank/backend/internal/accounts"
	"github.com/deecaulcrick/neobank/backend/internal/activity"
	"github.com/deecaulcrick/neobank/backend/internal/auth"
	"github.com/deecaulcrick/neobank/backend/internal/cards"
	"github.com/deecaulcrick/neobank/backend/internal/crypto"
	"github.com/deecaulcrick/neobank/backend/internal/ledger"
	"github.com/deecaulcrick/neobank/backend/internal/limits"
	"github.com/deecaulcrick/neobank/backend/internal/money"
	"github.com/deecaulcrick/neobank/backend/internal/notify"
	"github.com/deecaulcrick/neobank/backend/internal/payouts"
	"github.com/deecaulcrick/neobank/backend/internal/pin"
	"github.com/deecaulcrick/neobank/backend/internal/swaps"
)

type meResponse struct {
	ID              uuid.UUID `json:"id"`
	Phone           string    `json:"phone"`
	Email           *string   `json:"email"`
	Tag             *string   `json:"tag"`
	FirstName       *string   `json:"first_name"`
	LastName        *string   `json:"last_name"`
	KYCTier         int       `json:"kyc_tier"`
	DisplayCurrency string    `json:"display_currency"`
	// HasPIN is false until the user sets a transaction PIN.
	HasPIN bool `json:"has_pin"`
}

// errNotInvited: the closed beta is on and this phone isn't on the list.
var errNotInvited = errors.New("not invited")

// ensureUser creates our users row and ledger accounts the first time a
// Supabase-authenticated user calls the API.
func (s *Server) ensureUser(ctx context.Context, u auth.User) (meResponse, error) {
	var me meResponse
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `select exists (select 1 from users where id = $1)`, u.ID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			// Closed beta: a new account needs its phone number on the list.
			// The session's phone has no leading "+".
			phone := strings.TrimPrefix(u.Phone, "+")
			tag, err := tx.Exec(ctx,
				`update beta_invites set claimed_at = coalesce(claimed_at, now()) where phone = $1`, phone)
			if err != nil {
				return err
			}
			if s.Cfg.BetaInviteOnly && tag.RowsAffected() == 0 {
				return errNotInvited
			}
			if _, err := tx.Exec(ctx,
				`insert into users (id, phone) values ($1, $2) on conflict (id) do nothing`, u.ID, u.Phone); err != nil {
				return err
			}
		}
		if err := ledger.EnsureUserAccounts(ctx, tx, u.ID); err != nil {
			return err
		}
		return tx.QueryRow(ctx,
			`select id, phone, email::text, tag::text, first_name, last_name, kyc_tier, display_currency, pin_hash is not null
			 from users where id = $1`, u.ID,
		).Scan(&me.ID, &me.Phone, &me.Email, &me.Tag, &me.FirstName, &me.LastName, &me.KYCTier, &me.DisplayCurrency, &me.HasPIN)
	})
	return me, err
}

func (s *Server) getMe(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	if u.Phone == "" {
		writeError(w, http.StatusBadRequest, "a phone-verified session is required")
		return
	}
	me, err := s.ensureUser(r.Context(), u)
	if errors.Is(err, errNotInvited) {
		writeProblem(w, http.StatusForbidden, "not_invited", "We're in a closed beta and your number isn't on the list yet.")
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, me)
}

// requirePIN checks the transaction PIN sent in X-Pin. Every handler that
// moves money calls it first; it writes the response itself on failure.
func (s *Server) requirePIN(w http.ResponseWriter, r *http.Request, userID uuid.UUID) bool {
	err := pin.Verify(r.Context(), s.Pool, userID, r.Header.Get("X-Pin"))
	if err == nil {
		return true
	}
	s.pinError(w, r, err)
	return false
}

func (s *Server) pinError(w http.ResponseWriter, r *http.Request, err error) {
	var (
		wrong   *pin.WrongError
		locked  *pin.LockedError
		invalid *pin.InvalidError
	)
	switch {
	case errors.Is(err, pin.ErrRequired):
		writeProblem(w, http.StatusUnauthorized, "pin_required", "Enter your PIN to continue.")
	case errors.Is(err, pin.ErrNotSet):
		writeProblem(w, http.StatusForbidden, "pin_not_set", "Set a PIN before moving money.")
	case errors.As(err, &wrong):
		writeProblem(w, http.StatusUnauthorized, "pin_wrong",
			fmt.Sprintf("Wrong PIN. %d %s left.", wrong.Left, map[bool]string{true: "try", false: "tries"}[wrong.Left == 1]))
	case errors.As(err, &locked):
		mins := int(time.Until(locked.Until).Minutes()) + 1
		writeProblem(w, http.StatusTooManyRequests, "pin_locked",
			fmt.Sprintf("Too many wrong PINs. Try again in %d minutes.", mins))
	case errors.As(err, &invalid):
		writeError(w, http.StatusUnprocessableEntity, invalid.Message)
	default:
		s.fail(w, r, err)
	}
}

// setPIN sets the transaction PIN, or changes it given the current one.
func (s *Server) setPIN(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var in struct {
		PIN        string `json:"pin"`
		CurrentPIN string `json:"current_pin"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if err := pin.Set(r.Context(), s.Pool, u.ID, in.CurrentPIN, in.PIN); err != nil {
		s.pinError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"has_pin": true})
}

// getLimits reports today's usage against the user's limits, in kobo.
func (s *Server) getLimits(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	usage, err := s.Limits.Usage(r.Context(), u.ID)
	if err != nil {
		s.limitError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, usage)
}

// limitError writes a limits refusal, reporting whether err was one.
func (s *Server) limitError(w http.ResponseWriter, r *http.Request, err error) bool {
	var limit *limits.Error
	if errors.As(err, &limit) {
		writeProblem(w, http.StatusUnprocessableEntity, "limit", limit.Message)
		return true
	}
	return false
}

// registerDevice stores the phone's push token.
func (s *Server) registerDevice(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var in struct {
		Token    string `json:"token"`
		Platform string `json:"platform"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Token == "" || len(in.Token) > 300 {
		writeError(w, http.StatusBadRequest, "invalid token")
		return
	}
	if err := notify.RegisterDevice(r.Context(), s.Pool, u.ID, in.Token, in.Platform); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

var tagPattern = regexp.MustCompile(`^[a-z0-9_]{3,20}$`)

func (s *Server) setTag(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var in struct {
		Tag string `json:"tag"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	tag := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(in.Tag), "@"))
	if !tagPattern.MatchString(tag) {
		writeError(w, http.StatusBadRequest, "tags are 3-20 characters: letters, numbers and underscores")
		return
	}
	ct, err := s.Pool.Exec(r.Context(), `update users set tag = $2 where id = $1`, u.ID, tag)
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr) && pgErr.Code == "23505":
		writeError(w, http.StatusConflict, "that tag is taken")
	case err != nil:
		s.fail(w, r, err)
	case ct.RowsAffected() == 0:
		writeError(w, http.StatusNotFound, "user not found")
	default:
		writeJSON(w, http.StatusOK, map[string]string{"tag": tag})
	}
}

// setDisplayCurrency chooses what Home totals the user's assets in.
func (s *Server) setDisplayCurrency(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var in struct {
		Currency string `json:"currency"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Currency != "NGN" && in.Currency != "USD" {
		writeError(w, http.StatusBadRequest, "currency must be NGN or USD")
		return
	}
	if _, err := s.Pool.Exec(r.Context(),
		`update users set display_currency = $2 where id = $1`, u.ID, in.Currency); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"display_currency": in.Currency})
}

// getPrices returns indicative rates for valuing balances. Display only.
func (s *Server) getPrices(w http.ResponseWriter, r *http.Request) {
	rates, err := s.Prices.Rates(r.Context())
	if err != nil {
		s.Log.Warn("prices unavailable", "err", err)
		writeError(w, http.StatusServiceUnavailable, "Prices are not available right now.")
		return
	}
	writeJSON(w, http.StatusOK, rates)
}

type balanceResponse struct {
	ledger.Balance
	Decimals int `json:"decimals"`
}

func (s *Server) getBalances(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	bals, err := ledger.UserBalances(r.Context(), s.Pool, u.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]balanceResponse, len(bals))
	for i, b := range bals {
		out[i] = balanceResponse{Balance: b, Decimals: b.Asset.Decimals()}
	}
	writeJSON(w, http.StatusOK, map[string]any{"balances": out})
}

// createTransfer is the in-app send: a pure ledger move between two users,
// with no Bitnob call.
func (s *Server) createTransfer(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var in struct {
		ToTag          string `json:"to_tag"`
		Asset          string `json:"asset"`
		Amount         string `json:"amount"` // decimal string in major units
		Note           string `json:"note"`
		IdempotencyKey string `json:"idempotency_key"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	asset, err := money.ParseAsset(in.Asset)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	amount, err := money.Parse(asset, in.Amount)
	if err != nil || amount == 0 {
		writeError(w, http.StatusBadRequest, "invalid amount")
		return
	}
	if in.IdempotencyKey == "" {
		writeError(w, http.StatusBadRequest, "idempotency_key is required")
		return
	}
	if !s.requirePIN(w, r, u.ID) {
		return
	}

	ctx := r.Context()
	var receiver uuid.UUID
	err = s.Pool.QueryRow(ctx, `select id from users where tag = $1`,
		strings.TrimPrefix(strings.TrimSpace(in.ToTag), "@")).Scan(&receiver)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "no one has that tag")
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if receiver == u.ID {
		writeError(w, http.StatusBadRequest, "you can't send to yourself")
		return
	}
	if err := s.Limits.Check(ctx, u.ID, limits.Transfer, asset, amount); err != nil {
		if !s.limitError(w, r, err) {
			s.fail(w, r, err)
		}
		return
	}

	var res ledger.Result
	err = pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var err error
		res, err = ledger.PostTx(ctx, tx, ledger.Entry{
			// Scoped to the sender so one user's keys can't collide with another's.
			IdempotencyKey: "p2p:" + u.ID.String() + ":" + in.IdempotencyKey,
			Kind:           "p2p_send",
			Lines: []ledger.Line{
				ledger.Debit(ledger.UserAccount(u.ID, asset), amount),
				ledger.Credit(ledger.UserAccount(receiver, asset), amount),
			},
		})
		if err != nil || !res.Posted {
			return err
		}
		_, err = tx.Exec(ctx,
			`insert into p2p_transfers (sender_id, receiver_id, asset, amount, note, entry_id)
			 values ($1, $2, $3, $4, nullif($5, ''), $6)`,
			u.ID, receiver, string(asset), amount, in.Note, res.EntryID)
		if err != nil {
			return err
		}
		var from string
		if err := tx.QueryRow(ctx, `select coalesce('@' || tag::text, 'someone') from users where id = $1`, u.ID).Scan(&from); err != nil {
			return err
		}
		return notify.Queue(ctx, tx, receiver, "You've got money",
			fmt.Sprintf("%s sent you %s.", from, money.Display(asset, amount)))
	})
	switch {
	case errors.Is(err, ledger.ErrInsufficientFunds):
		writeError(w, http.StatusUnprocessableEntity, "insufficient funds")
	case err != nil:
		s.fail(w, r, err)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"entry_id": res.EntryID, "status": "done"})
	}
}

// devFund posts a fake deposit. Registered outside production only.
func (s *Server) devFund(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var in struct {
		Asset  string `json:"asset"`
		Amount string `json:"amount"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	asset, err := money.ParseAsset(in.Asset)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	amount, err := money.Parse(asset, in.Amount)
	if err != nil || amount == 0 {
		writeError(w, http.StatusBadRequest, "invalid amount")
		return
	}
	res, err := ledger.Post(r.Context(), s.Pool, ledger.Entry{
		IdempotencyKey: "dev_fund:" + uuid.NewString(),
		Kind:           "dev_fund",
		Lines: []ledger.Line{
			ledger.Debit(ledger.Omnibus(asset), amount),
			ledger.Credit(ledger.UserAccount(u.ID, asset), amount),
		},
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entry_id": res.EntryID})
}

// submitKYC is tier-1 onboarding: Bitnob customer, then the NGN account number.
func (s *Server) submitKYC(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var in accounts.KYCInput
	if !readJSON(w, r, &in) {
		return
	}
	if _, err := s.ensureUser(r.Context(), u); err != nil {
		if errors.Is(err, errNotInvited) {
			writeProblem(w, http.StatusForbidden, "not_invited", "We're in a closed beta and your number isn't on the list yet.")
			return
		}
		s.fail(w, r, err)
		return
	}
	va, err := s.Accounts.Onboard(r.Context(), u.ID, u.Phone, in)
	if err != nil {
		s.accountsError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, va)
}

func (s *Server) getVirtualAccount(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	va, err := s.Accounts.VirtualAccount(r.Context(), u.ID)
	if err != nil {
		s.accountsError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, va)
}

func (s *Server) devSimulateDeposit(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	if err := s.Accounts.SimulateDeposit(r.Context(), u.ID); err != nil {
		s.accountsError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "done"})
}

func (s *Server) accountsError(w http.ResponseWriter, r *http.Request, err error) {
	var invalid *accounts.ValidationError
	switch {
	case errors.As(err, &invalid):
		writeError(w, http.StatusUnprocessableEntity, invalid.Message)
	case errors.Is(err, accounts.ErrIdentityInUse):
		writeError(w, http.StatusConflict, "That BVN or email is already linked to another account.")
	case errors.Is(err, accounts.ErrNoAccount):
		writeError(w, http.StatusNotFound, "You don't have an account number yet.")
	case errors.Is(err, accounts.ErrUnavailable):
		writeError(w, http.StatusServiceUnavailable, "Account services are not available right now.")
	default:
		s.fail(w, r, err)
	}
}

// createSwapQuote locks a rate for the review screen.
func (s *Server) createSwapQuote(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var in struct {
		From   string `json:"from"`
		To     string `json:"to"`
		Amount string `json:"amount"` // decimal string in major units
		// "pay" (default): amount is what the user gives, in From.
		// "get": amount is exactly what they want to receive, in To.
		Side string `json:"side"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	from, err := money.ParseAsset(in.From)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	to, err := money.ParseAsset(in.To)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	exactGet := in.Side == "get"
	unit := from
	if exactGet {
		unit = to
	}
	amount, err := money.Parse(unit, in.Amount)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid amount")
		return
	}
	q, err := s.Swaps.CreateQuote(r.Context(), u.ID, from, to, amount, exactGet)
	if err != nil {
		s.swapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, q)
}

// executeSwap trades against a quote. The response status is "completed",
// or "pending" when Bitnob hasn't confirmed yet.
func (s *Server) executeSwap(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var in struct {
		QuoteID uuid.UUID `json:"quote_id"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if !s.requirePIN(w, r, u.ID) {
		return
	}
	t, err := s.Swaps.Execute(r.Context(), u.ID, in.QuoteID)
	if err != nil {
		s.swapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) swapError(w http.ResponseWriter, r *http.Request, err error) {
	var invalid *swaps.ValidationError
	switch {
	case errors.As(err, &invalid):
		writeError(w, http.StatusUnprocessableEntity, invalid.Message)
	case errors.Is(err, ledger.ErrInsufficientFunds):
		writeError(w, http.StatusUnprocessableEntity, "You don't have enough for this swap.")
	case errors.Is(err, swaps.ErrQuoteExpired):
		writeError(w, http.StatusGone, "That rate expired. Here's a fresh one.")
	case errors.Is(err, swaps.ErrQuoteUsed):
		writeError(w, http.StatusConflict, "That swap was already submitted.")
	case errors.Is(err, swaps.ErrQuoteNotFound):
		writeError(w, http.StatusNotFound, "We couldn't find that quote.")
	case errors.Is(err, swaps.ErrUnavailable):
		writeError(w, http.StatusServiceUnavailable, "This swap isn't available right now. Try again later.")
	default:
		s.fail(w, r, err)
	}
}

func (s *Server) payoutCountries(w http.ResponseWriter, r *http.Request) {
	countries, err := s.Payouts.Countries(r.Context())
	if err != nil {
		s.payoutError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"countries": countries})
}

func (s *Server) payoutCountry(w http.ResponseWriter, r *http.Request) {
	details, err := s.Payouts.CountryDetails(r.Context(), r.PathValue("country"))
	if err != nil {
		s.payoutError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(details)
}

func (s *Server) payoutAccountLookup(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	name, err := s.Payouts.LookupAccount(r.Context(), q.Get("country"), q.Get("rail"), q.Get("provider"), q.Get("account"))
	if err != nil {
		s.payoutError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"account_name": name})
}

func (s *Server) listBeneficiaries(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	list, err := s.Payouts.Beneficiaries(r.Context(), u.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"beneficiaries": list})
}

// createPayoutQuote locks a payout rate. Send either amount (what the user
// pays, in from_asset) or settlement_amount (what the recipient gets).
func (s *Server) createPayoutQuote(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var in struct {
		Country          string `json:"country"`
		Currency         string `json:"currency"`
		FromAsset        string `json:"from_asset"`
		Amount           string `json:"amount"`
		SettlementAmount string `json:"settlement_amount"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	from, err := money.ParseAsset(in.FromAsset)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	qi := payouts.QuoteInput{Country: in.Country, ToCurrency: in.Currency, FromAsset: from, SettlementAmount: in.SettlementAmount}
	if in.Amount != "" {
		if qi.Amount, err = money.Parse(from, in.Amount); err != nil {
			writeError(w, http.StatusBadRequest, "invalid amount")
			return
		}
	}
	q, err := s.Payouts.CreateQuote(r.Context(), u.ID, qi)
	if err != nil {
		s.payoutError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, q)
}

// sendPayout commits a quoted payout. The status comes back "processing"
// until the rail confirms.
func (s *Server) sendPayout(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var in payouts.SendInput
	if !readJSON(w, r, &in) {
		return
	}
	if !s.requirePIN(w, r, u.ID) {
		return
	}
	p, err := s.Payouts.Send(r.Context(), u.ID, in)
	if err != nil {
		s.payoutError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) getPayout(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "We couldn't find that payout.")
		return
	}
	p, err := s.Payouts.Get(r.Context(), u.ID, id)
	if err != nil {
		s.payoutError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) payoutError(w http.ResponseWriter, r *http.Request, err error) {
	if s.limitError(w, r, err) {
		return
	}
	var invalid *payouts.ValidationError
	switch {
	case errors.As(err, &invalid):
		writeError(w, http.StatusUnprocessableEntity, invalid.Message)
	case errors.Is(err, ledger.ErrInsufficientFunds):
		writeError(w, http.StatusUnprocessableEntity, "You don't have enough to send this.")
	case errors.Is(err, payouts.ErrQuoteExpired):
		writeError(w, http.StatusGone, "That rate expired. Here's a fresh one.")
	case errors.Is(err, payouts.ErrQuoteUsed):
		writeError(w, http.StatusConflict, "That payout was already submitted.")
	case errors.Is(err, payouts.ErrQuoteNotFound), errors.Is(err, payouts.ErrPayoutNotFound):
		writeError(w, http.StatusNotFound, "We couldn't find that payout.")
	case errors.Is(err, payouts.ErrUnavailable):
		writeError(w, http.StatusServiceUnavailable, "Sending abroad isn't available right now. Try again later.")
	default:
		s.fail(w, r, err)
	}
}

func (s *Server) cryptoNetworks(w http.ResponseWriter, r *http.Request) {
	asset, err := money.ParseAsset(r.URL.Query().Get("asset"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	networks, err := s.Crypto.Networks(r.Context(), asset)
	if err != nil {
		s.cryptoError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"networks": networks})
}

// cryptoAddress returns the user's deposit address on a network, creating it
// on first use.
func (s *Server) cryptoAddress(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var in struct {
		Asset   string `json:"asset"`
		Network string `json:"network"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	asset, err := money.ParseAsset(in.Asset)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	address, err := s.Crypto.Address(r.Context(), u.ID, asset, in.Network)
	if err != nil {
		s.cryptoError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"asset": string(asset), "network": in.Network, "address": address})
}

type withdrawRequest struct {
	Asset          string `json:"asset"`
	Network        string `json:"network"`
	Address        string `json:"address"`
	Amount         string `json:"amount"` // decimal string in major units
	IdempotencyKey string `json:"idempotency_key"`
}

func (s *Server) withdrawInput(w http.ResponseWriter, r *http.Request) (crypto.WithdrawInput, bool) {
	var in withdrawRequest
	if !readJSON(w, r, &in) {
		return crypto.WithdrawInput{}, false
	}
	asset, err := money.ParseAsset(in.Asset)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return crypto.WithdrawInput{}, false
	}
	amount, err := money.Parse(asset, in.Amount)
	if err != nil || amount == 0 {
		writeError(w, http.StatusBadRequest, "invalid amount")
		return crypto.WithdrawInput{}, false
	}
	return crypto.WithdrawInput{
		Asset: asset, Network: in.Network, Address: in.Address, Amount: amount, IdempotencyKey: in.IdempotencyKey,
	}, true
}

// cryptoPreview prices a withdrawal for the confirm screen. Nothing moves.
func (s *Server) cryptoPreview(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	in, ok := s.withdrawInput(w, r)
	if !ok {
		return
	}
	p, err := s.Crypto.Preview(r.Context(), u.ID, in)
	if err != nil {
		s.cryptoError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// cryptoWithdraw sends crypto to an external address. The status comes back
// "pending" until the network confirms.
func (s *Server) cryptoWithdraw(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	in, ok := s.withdrawInput(w, r)
	if !ok {
		return
	}
	if !s.requirePIN(w, r, u.ID) {
		return
	}
	t, err := s.Crypto.Withdraw(r.Context(), u.ID, in)
	if err != nil {
		s.cryptoError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) cryptoError(w http.ResponseWriter, r *http.Request, err error) {
	if s.limitError(w, r, err) {
		return
	}
	var invalid *crypto.ValidationError
	switch {
	case errors.As(err, &invalid):
		writeError(w, http.StatusUnprocessableEntity, invalid.Message)
	case errors.Is(err, ledger.ErrInsufficientFunds):
		writeError(w, http.StatusUnprocessableEntity, "You don't have enough to send this, including the fee.")
	case errors.Is(err, crypto.ErrUnavailable):
		writeError(w, http.StatusServiceUnavailable, "That network isn't available right now. Try another or come back later.")
	default:
		s.fail(w, r, err)
	}
}

// findPeople powers the send screen: recent recipients with no query, tag
// prefix matches with one.
func (s *Server) findPeople(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	people, err := s.Activity.People(r.Context(), u.ID, r.URL.Query().Get("q"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"people": people})
}

// listActivity is the history feed. Filter with asset and kinds (comma
// separated); page with before set to the last item's created_at.
func (s *Server) listActivity(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	q := r.URL.Query()
	f := activity.Filter{Asset: q.Get("asset")}
	if kinds := q.Get("kinds"); kinds != "" {
		f.Kinds = strings.Split(kinds, ",")
	}
	if before := q.Get("before"); before != "" {
		t, err := time.Parse(time.RFC3339Nano, before)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid before")
			return
		}
		f.Before = t
	}
	items, err := s.Activity.List(r.Context(), u.ID, f)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) getActivity(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	d, err := s.Activity.Get(r.Context(), u.ID, r.PathValue("id"))
	if errors.Is(err, activity.ErrNotFound) {
		writeError(w, http.StatusNotFound, "We couldn't find that transaction.")
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) getCard(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	view, err := s.Cards.Get(r.Context(), u.ID)
	if err != nil {
		s.cardError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// cardKYC runs the fuller identity check a card needs.
func (s *Server) cardKYC(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var in cards.KYCInput
	if !readJSON(w, r, &in) {
		return
	}
	status, err := s.Cards.SubmitKYC(r.Context(), u.ID, in)
	if err != nil {
		s.cardError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"kyc_status": status})
}

type cardAmount struct {
	Amount         string `json:"amount"` // dollars, as a decimal string
	IdempotencyKey string `json:"idempotency_key"`
}

func (s *Server) cardAmount(w http.ResponseWriter, r *http.Request) (cardAmount, int64, bool) {
	var in cardAmount
	if !readJSON(w, r, &in) {
		return in, 0, false
	}
	amount, err := money.Parse(money.USDC, in.Amount)
	if err != nil || amount == 0 {
		writeError(w, http.StatusBadRequest, "invalid amount")
		return in, 0, false
	}
	return in, amount, true
}

// createCard issues the user's card, loaded from their USDC balance.
func (s *Server) createCard(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	in, amount, ok := s.cardAmount(w, r)
	if !ok || !s.requirePIN(w, r, u.ID) {
		return
	}
	t, err := s.Cards.Create(r.Context(), u.ID, amount, in.IdempotencyKey)
	if err != nil {
		s.cardError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// moveCard loads the card from USDC ("fund") or moves money back ("withdraw").
func (s *Server) moveCard(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, _ := auth.FromContext(r.Context())
		in, amount, ok := s.cardAmount(w, r)
		if !ok || !s.requirePIN(w, r, u.ID) {
			return
		}
		t, err := s.Cards.Move(r.Context(), u.ID, kind, amount, in.IdempotencyKey)
		if err != nil {
			s.cardError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, t)
	}
}

// revealCard returns the full card number and security code. PIN required;
// the response must not be cached or logged.
func (s *Server) revealCard(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	if !s.requirePIN(w, r, u.ID) {
		return
	}
	secrets, err := s.Cards.Reveal(r.Context(), u.ID)
	if err != nil {
		s.cardError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, secrets)
}

func (s *Server) lockCard(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	var in struct {
		Locked bool `json:"locked"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	status, err := s.Cards.SetLocked(r.Context(), u.ID, in.Locked)
	if err != nil {
		s.cardError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

func (s *Server) cardTransactions(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.FromContext(r.Context())
	txns, err := s.Cards.Transactions(r.Context(), u.ID)
	if err != nil {
		s.cardError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"transactions": txns})
}

func (s *Server) cardError(w http.ResponseWriter, r *http.Request, err error) {
	var invalid *cards.ValidationError
	switch {
	case errors.As(err, &invalid):
		writeError(w, http.StatusUnprocessableEntity, invalid.Message)
	case errors.Is(err, ledger.ErrInsufficientFunds):
		writeError(w, http.StatusUnprocessableEntity, "You don't have enough USDC for that, including the fee.")
	case errors.Is(err, cards.ErrNoCard):
		writeError(w, http.StatusNotFound, "You don't have a card yet.")
	case errors.Is(err, cards.ErrUnavailable):
		writeError(w, http.StatusServiceUnavailable, "Cards aren't available right now. Try again later.")
	default:
		s.fail(w, r, err)
	}
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	s.Log.Error("request failed", "path", r.URL.Path, "err", err)
	writeError(w, http.StatusInternalServerError, "something went wrong")
}
