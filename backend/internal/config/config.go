// Package config loads runtime configuration from the environment.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

type Config struct {
	Env  string // development | production
	Port string

	// Supabase Postgres connection string (session pooler or direct).
	DatabaseURL string

	// Supabase project URL, used to fetch the JWKS that signs user JWTs.
	SupabaseURL string
	// Optional: legacy HS256 JWT secret. When set it is used instead of JWKS.
	SupabaseJWTSecret string

	BitnobBaseURL       string
	BitnobClientID      string
	BitnobClientSecret  string
	BitnobWebhookSecret string

	// Key for the HMAC that stands in for a BVN in our database.
	KYCHashKey string
}

func Load() (Config, error) {
	c := Config{
		Env:                 getenv("APP_ENV", "development"),
		Port:                getenv("PORT", "8080"),
		DatabaseURL:         os.Getenv("DATABASE_URL"),
		SupabaseURL:         strings.TrimRight(os.Getenv("SUPABASE_URL"), "/"),
		SupabaseJWTSecret:   os.Getenv("SUPABASE_JWT_SECRET"),
		BitnobBaseURL:       strings.TrimRight(getenv("BITNOB_BASE_URL", "https://api.bitnob.com"), "/"),
		BitnobClientID:      os.Getenv("BITNOB_CLIENT_ID"),
		BitnobClientSecret:  os.Getenv("BITNOB_CLIENT_SECRET"),
		BitnobWebhookSecret: os.Getenv("BITNOB_WEBHOOK_SECRET"),
		KYCHashKey:          os.Getenv("KYC_HASH_KEY"),
	}

	var missing []string
	if c.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if c.SupabaseURL == "" && c.SupabaseJWTSecret == "" {
		missing = append(missing, "SUPABASE_URL (or SUPABASE_JWT_SECRET)")
	}
	// Bitnob credentials are optional in development so the ledger, in-app
	// sends and the app can run before sandbox keys exist.
	if c.Env == "production" {
		if c.BitnobClientID == "" {
			missing = append(missing, "BITNOB_CLIENT_ID")
		}
		if c.BitnobClientSecret == "" {
			missing = append(missing, "BITNOB_CLIENT_SECRET")
		}
	}
	if len(missing) > 0 {
		return c, fmt.Errorf("missing required env: %s", strings.Join(missing, ", "))
	}
	if c.Env == "production" {
		if c.BitnobWebhookSecret == "" {
			return c, errors.New("BITNOB_WEBHOOK_SECRET is required in production")
		}
		if c.KYCHashKey == "" {
			return c, errors.New("KYC_HASH_KEY is required in production")
		}
	} else if c.KYCHashKey == "" {
		c.KYCHashKey = "development-only-kyc-hash-key"
	}
	return c, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
