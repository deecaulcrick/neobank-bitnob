// Command worker processes stored webhooks and runs the sweeper and reconciler.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/deecaulcrick/neobank/backend/internal/accounts"
	"github.com/deecaulcrick/neobank/backend/internal/bitnob"
	"github.com/deecaulcrick/neobank/backend/internal/cards"
	"github.com/deecaulcrick/neobank/backend/internal/config"
	"github.com/deecaulcrick/neobank/backend/internal/crypto"
	"github.com/deecaulcrick/neobank/backend/internal/jobs"
	"github.com/deecaulcrick/neobank/backend/internal/notify"
	"github.com/deecaulcrick/neobank/backend/internal/payouts"
	"github.com/deecaulcrick/neobank/backend/internal/store"
	"github.com/deecaulcrick/neobank/backend/internal/swaps"
	"github.com/deecaulcrick/neobank/backend/internal/webhooks"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
	pool, err := store.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	bn := bitnob.New(cfg.BitnobBaseURL, cfg.BitnobClientID, cfg.BitnobClientSecret)
	acct := &accounts.Service{Pool: pool, Bitnob: bn, HashKey: cfg.KYCHashKey}
	swap := &swaps.Service{Pool: pool, Bitnob: bn, FeeBps: cfg.SwapFeeBps, Log: log}
	pay := &payouts.Service{Pool: pool, Bitnob: bn, FeeBps: cfg.PayoutFeeBps, Log: log}
	chain := &crypto.Service{Pool: pool, Bitnob: bn, Log: log}
	card := &cards.Service{Pool: pool, Bitnob: bn, HashKey: cfg.KYCHashKey, Log: log, Fees: cards.Fees{
		CreationFee: cfg.CardCreationFee, CreationCost: cfg.CardCreationCost,
		FundFee: cfg.CardFundFee, FundCost: cfg.CardFundCost,
	}}
	j := &jobs.Jobs{Pool: pool, Bitnob: bn, Log: log}
	processor := webhooks.NewProcessor(pool, log)
	pusher := &notify.Sender{Pool: pool, Log: log}

	var wg sync.WaitGroup
	start := func(fn func()) {
		wg.Add(1)
		go func() { defer wg.Done(); fn() }()
	}
	start(func() { processor.Run(ctx) })
	start(func() { jobs.Every(ctx, 5*time.Second, "push", log, pusher.Deliver) })
	start(func() { jobs.Every(ctx, 30*time.Second, "sweep-swaps", log, swap.Sweep) })
	start(func() { jobs.Every(ctx, 30*time.Second, "sweep-payouts", log, pay.Sweep) })
	start(func() { jobs.Every(ctx, time.Minute, "sweep-crypto", log, chain.Sweep) })
	start(func() { jobs.Every(ctx, 30*time.Second, "sweep-cards", log, card.Sweep) })
	// Catches deposits whose webhook never arrived (always the case locally,
	// where Bitnob can't reach the receiver).
	start(func() { jobs.Every(ctx, time.Minute, "sync-deposits", log, acct.SyncAllDeposits) })
	start(func() { jobs.Every(ctx, 24*time.Hour, "reconcile", log, j.Reconcile) })

	log.Info("worker running")
	wg.Wait()
}
