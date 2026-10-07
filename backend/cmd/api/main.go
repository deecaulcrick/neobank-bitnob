// Command api serves the mobile app's JSON API and the Bitnob webhook receiver.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/deecaulcrick/neobank/backend/internal/accounts"
	"github.com/deecaulcrick/neobank/backend/internal/activity"
	"github.com/deecaulcrick/neobank/backend/internal/auth"
	"github.com/deecaulcrick/neobank/backend/internal/bitnob"
	"github.com/deecaulcrick/neobank/backend/internal/cards"
	"github.com/deecaulcrick/neobank/backend/internal/config"
	"github.com/deecaulcrick/neobank/backend/internal/crypto"
	"github.com/deecaulcrick/neobank/backend/internal/httpapi"
	"github.com/deecaulcrick/neobank/backend/internal/limits"
	"github.com/deecaulcrick/neobank/backend/internal/money"
	"github.com/deecaulcrick/neobank/backend/internal/payouts"
	"github.com/deecaulcrick/neobank/backend/internal/prices"
	"github.com/deecaulcrick/neobank/backend/internal/screening"
	"github.com/deecaulcrick/neobank/backend/internal/store"
	"github.com/deecaulcrick/neobank/backend/internal/swaps"
	"github.com/deecaulcrick/neobank/backend/internal/webhooks"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	pool, err := store.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	verifier, err := auth.NewVerifier(ctx, cfg.SupabaseURL, cfg.SupabaseJWTSecret)
	if err != nil {
		return err
	}

	bn := bitnob.New(cfg.BitnobBaseURL, cfg.BitnobClientID, cfg.BitnobClientSecret)
	rates := &prices.Service{Bitnob: bn, TTL: time.Minute}
	lim := &limits.Service{Pool: pool, Prices: rates, Cfg: limits.Config{
		Daily:           cfg.DailyLimitNaira * 100,
		Single:          cfg.SingleLimitNaira * 100,
		MaxSends:        int(cfg.MaxSendsPerDay),
		NewAccountHold:  time.Duration(cfg.NewAccountHoldHours) * time.Hour,
		NewAccountDaily: cfg.NewAccountDailyNaira * 100,
	}}
	// The services call back into limits and screening just before they
	// reserve a user's funds.
	check := func(kind limits.Kind) func(context.Context, uuid.UUID, money.Asset, int64) error {
		return func(ctx context.Context, userID uuid.UUID, asset money.Asset, amount int64) error {
			return lim.Check(ctx, userID, kind, asset, amount)
		}
	}
	srv := &httpapi.Server{
		Cfg:      cfg,
		Pool:     pool,
		Bitnob:   bn,
		Accounts: &accounts.Service{Pool: pool, Bitnob: bn, HashKey: cfg.KYCHashKey},
		Activity: &activity.Service{Pool: pool},
		Crypto:   &crypto.Service{Pool: pool, Bitnob: bn, Log: log, Check: check(limits.Crypto)},
		Limits:   lim,
		Cards:    &cards.Service{Pool: pool, Bitnob: bn, Fees: cardFees(cfg), HashKey: cfg.KYCHashKey, Log: log},
		Payouts: &payouts.Service{
			Pool: pool, Bitnob: bn, FeeBps: cfg.PayoutFeeBps, Log: log,
			Currencies: cfg.PayoutCurrencies,
			Check:      check(limits.Payout),
			Screen: func(ctx context.Context, name string) (string, bool, error) {
				return screening.Check(ctx, pool, name)
			},
		},
		Prices:   rates,
		Swaps:    &swaps.Service{Pool: pool, Bitnob: bn, FeeBps: cfg.SwapFeeBps, Log: log},
		Verifier: verifier,
		Webhooks: webhooks.NewReceiver(pool, cfg.BitnobWebhookSecret, log),
		Log:      log,
	}
	httpServer := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errc := make(chan error, 1)
	go func() { errc <- httpServer.ListenAndServe() }()
	log.Info("api listening", "addr", httpServer.Addr, "env", cfg.Env)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func cardFees(cfg config.Config) cards.Fees {
	return cards.Fees{
		CreationFee: cfg.CardCreationFee, CreationCost: cfg.CardCreationCost,
		FundFee: cfg.CardFundFee, FundCost: cfg.CardFundCost,
	}
}
