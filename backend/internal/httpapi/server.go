// Package httpapi is the JSON API the mobile app talks to.
package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deecaulcrick/neobank/backend/internal/accounts"
	"github.com/deecaulcrick/neobank/backend/internal/activity"
	"github.com/deecaulcrick/neobank/backend/internal/auth"
	"github.com/deecaulcrick/neobank/backend/internal/bitnob"
	"github.com/deecaulcrick/neobank/backend/internal/cards"
	"github.com/deecaulcrick/neobank/backend/internal/config"
	"github.com/deecaulcrick/neobank/backend/internal/crypto"
	"github.com/deecaulcrick/neobank/backend/internal/limits"
	"github.com/deecaulcrick/neobank/backend/internal/payouts"
	"github.com/deecaulcrick/neobank/backend/internal/prices"
	"github.com/deecaulcrick/neobank/backend/internal/swaps"
)

type Server struct {
	Cfg      config.Config
	Pool     *pgxpool.Pool
	Bitnob   *bitnob.Client
	Accounts *accounts.Service
	Swaps    *swaps.Service
	Prices   *prices.Service
	Payouts  *payouts.Service
	Crypto   *crypto.Service
	Activity *activity.Service
	Limits   *limits.Service
	Cards    *cards.Service
	Verifier *auth.Verifier
	Webhooks http.Handler
	Log      *slog.Logger
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.health)
	// Bitnob reaches us only here. Authenticated by signature, not JWT.
	mux.Handle("POST /webhooks/bitnob", s.Webhooks)

	authed := func(pattern string, h http.HandlerFunc) {
		mux.Handle(pattern, s.Verifier.Middleware(h))
	}

	// M1 — accounts
	authed("GET /v1/me", s.getMe)
	authed("PUT /v1/me/tag", s.setTag)
	authed("PUT /v1/me/display-currency", s.setDisplayCurrency)
	authed("PUT /v1/me/pin", s.setPIN)
	authed("GET /v1/limits", s.getLimits)
	authed("POST /v1/devices", s.registerDevice)
	authed("GET /v1/balances", s.getBalances)
	authed("POST /v1/onboarding/kyc", s.submitKYC)
	authed("GET /v1/virtual-account", s.getVirtualAccount)

	// M2 — swaps
	authed("GET /v1/prices", s.getPrices)
	authed("POST /v1/swaps/quotes", s.createSwapQuote)
	authed("POST /v1/swaps", s.executeSwap)

	// M3 — payouts
	authed("GET /v1/payouts/countries", s.payoutCountries)
	authed("GET /v1/payouts/countries/{country}", s.payoutCountry)
	authed("GET /v1/payouts/account-lookup", s.payoutAccountLookup)
	authed("GET /v1/beneficiaries", s.listBeneficiaries)
	authed("POST /v1/payouts/quotes", s.createPayoutQuote)
	authed("POST /v1/payouts", s.sendPayout)
	authed("GET /v1/payouts/{id}", s.getPayout)

	// M4 — crypto in/out
	authed("GET /v1/crypto/networks", s.cryptoNetworks)
	authed("POST /v1/crypto/addresses", s.cryptoAddress)
	authed("POST /v1/crypto/withdrawals/preview", s.cryptoPreview)
	authed("POST /v1/crypto/withdrawals", s.cryptoWithdraw)

	// Virtual card
	authed("GET /v1/card", s.getCard)
	authed("POST /v1/card/kyc", s.cardKYC)
	authed("POST /v1/card", s.createCard)
	authed("POST /v1/card/fund", s.moveCard("fund"))
	authed("POST /v1/card/withdraw", s.moveCard("withdraw"))
	authed("POST /v1/card/reveal", s.revealCard)
	authed("POST /v1/card/lock", s.lockCard)
	authed("GET /v1/card/transactions", s.cardTransactions)

	// M5 — social
	authed("POST /v1/transfers", s.createTransfer)
	authed("GET /v1/people", s.findPeople)
	authed("GET /v1/activity", s.listActivity)
	authed("GET /v1/activity/{id}", s.getActivity)

	if s.Cfg.Env != "production" {
		// Credits a fake deposit so the app is usable before Bitnob is wired up.
		authed("POST /v1/dev/fund", s.devFund)
		// Pays NGN 1,000 into the user's account through the Bitnob sandbox.
		authed("POST /v1/dev/simulate-deposit", s.devSimulateDeposit)
	}

	return s.logRequests(deadline(mux))
}

// deadline bounds every request. Without it a query on a dead connection
// would hang for as long as the client cared to wait; with it the query is
// cancelled, the connection discarded and the caller told to retry.
func deadline(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.Log.Info("http", "method", r.Method, "path", r.URL.Path, "status", rec.status, "ms", time.Since(start).Milliseconds())
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// writeProblem is writeError with a machine-readable code the app branches on.
func writeProblem(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": msg, "code": code})
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.Pool.Ping(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database unreachable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
