// Package httpapi is the JSON API the mobile app talks to.
package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/deecaulcrick/neobank/backend/internal/accounts"
	"github.com/deecaulcrick/neobank/backend/internal/auth"
	"github.com/deecaulcrick/neobank/backend/internal/bitnob"
	"github.com/deecaulcrick/neobank/backend/internal/config"
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
	authed("GET /v1/balances", s.getBalances)
	authed("POST /v1/onboarding/kyc", s.submitKYC)
	authed("GET /v1/virtual-account", s.getVirtualAccount)

	// M2 — swaps
	authed("GET /v1/prices", s.getPrices)
	authed("POST /v1/swaps/quotes", s.createSwapQuote)
	authed("POST /v1/swaps", s.executeSwap)

	// M3 — payouts
	authed("GET /v1/payouts/countries", notImplemented)
	authed("GET /v1/payouts/countries/{country}", notImplemented)
	authed("GET /v1/beneficiaries", notImplemented)
	authed("POST /v1/beneficiaries", notImplemented)
	authed("POST /v1/payouts/quotes", notImplemented)
	authed("POST /v1/payouts", notImplemented)

	// M4 — crypto in/out
	authed("POST /v1/crypto/addresses", notImplemented)
	authed("POST /v1/crypto/withdrawals", notImplemented)

	// M5 — social
	authed("POST /v1/transfers", s.createTransfer)
	authed("GET /v1/activity", notImplemented)
	authed("GET /v1/activity/{id}", notImplemented)

	if s.Cfg.Env != "production" {
		// Credits a fake deposit so the app is usable before Bitnob is wired up.
		authed("POST /v1/dev/fund", s.devFund)
		// Pays NGN 1,000 into the user's account through the Bitnob sandbox.
		authed("POST /v1/dev/simulate-deposit", s.devSimulateDeposit)
	}

	return s.logRequests(mux)
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

func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

func notImplemented(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotImplemented, "not implemented yet")
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.Pool.Ping(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database unreachable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
