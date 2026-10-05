package httpapi

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/deecaulcrick/neobank/backend/internal/accounts"
	"github.com/deecaulcrick/neobank/backend/internal/auth"
	"github.com/deecaulcrick/neobank/backend/internal/ledger"
	"github.com/deecaulcrick/neobank/backend/internal/money"
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
}

// ensureUser creates our users row and ledger accounts the first time a
// Supabase-authenticated user calls the API.
func (s *Server) ensureUser(ctx context.Context, u auth.User) (meResponse, error) {
	var me meResponse
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`insert into users (id, phone) values ($1, $2) on conflict (id) do nothing`, u.ID, u.Phone)
		if err != nil {
			return err
		}
		if err := ledger.EnsureUserAccounts(ctx, tx, u.ID); err != nil {
			return err
		}
		return tx.QueryRow(ctx,
			`select id, phone, email::text, tag::text, first_name, last_name, kyc_tier, display_currency
			 from users where id = $1`, u.ID,
		).Scan(&me.ID, &me.Phone, &me.Email, &me.Tag, &me.FirstName, &me.LastName, &me.KYCTier, &me.DisplayCurrency)
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
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, me)
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
//
// TODO(M5): velocity limits, new-account hold and PIN/biometric confirmation
// (spec: "Fraud and risk controls") before this is exposed to real users.
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
		return err
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

func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	s.Log.Error("request failed", "path", r.URL.Path, "err", err)
	writeError(w, http.StatusInternalServerError, "something went wrong")
}
