// Package store owns the Postgres connection pool.
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	cfg.MaxConnIdleTime = 5 * time.Minute
	// Works through Supabase's transaction pooler (port 6543), which cannot
	// keep named prepared statements across pooled connections.
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeCacheDescribe

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// The first connection can be slow on a poor network; try a few times
	// before giving up.
	for attempt := 1; ; attempt++ {
		pingCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		err = pool.Ping(pingCtx)
		cancel()
		if err == nil {
			break
		}
		if attempt == 4 || ctx.Err() != nil {
			pool.Close()
			return nil, fmt.Errorf("ping database: %w", err)
		}
	}
	return pool, nil
}
