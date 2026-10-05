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

	"github.com/deecaulcrick/neobank/backend/internal/auth"
	"github.com/deecaulcrick/neobank/backend/internal/bitnob"
	"github.com/deecaulcrick/neobank/backend/internal/config"
	"github.com/deecaulcrick/neobank/backend/internal/httpapi"
	"github.com/deecaulcrick/neobank/backend/internal/store"
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

	srv := &httpapi.Server{
		Cfg:      cfg,
		Pool:     pool,
		Bitnob:   bitnob.New(cfg.BitnobBaseURL, cfg.BitnobClientID, cfg.BitnobClientSecret),
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
