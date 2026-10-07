// Package config loads runtime configuration from the environment.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
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

	// Our margin on a swap, in basis points of what the user receives.
	SwapFeeBps int64
	// Our fee on a payout, in basis points of the amount sent.
	PayoutFeeBps int64

	// Fiat currencies we pay out in. Corridors in any other currency are hidden.
	PayoutCurrencies []string

	// Virtual card charges, in micro-dollars: what we charge the user to
	// issue and to load a card, and what Bitnob takes for each (so the
	// difference can be booked as revenue). Placeholders.
	CardCreationFee  int64
	CardCreationCost int64
	CardFundFee      int64
	CardFundCost     int64

	// When on, only phone numbers in beta_invites can create an account.
	BetaInviteOnly bool

	// Our own outflow limits, in whole naira. Bitnob has no account tiers;
	// these are risk controls we choose, and the amounts are placeholders.
	DailyLimitNaira      int64
	SingleLimitNaira     int64
	MaxSendsPerDay       int64
	NewAccountHoldHours  int64
	NewAccountDailyNaira int64
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
		SwapFeeBps:          50,
		PayoutFeeBps:        100,
		PayoutCurrencies:    []string{"NGN", "GHS", "KES", "RWF", "XOF", "XAF", "UGX", "GMD"},
		BetaInviteOnly:      os.Getenv("BETA_INVITE_ONLY") == "true",
		CardCreationFee:     2_000_000,
		CardCreationCost:    2_000_000,
		CardFundFee:         500_000,
		CardFundCost:        0,

		DailyLimitNaira:      50_000,
		SingleLimitNaira:     50_000,
		MaxSendsPerDay:       20,
		NewAccountHoldHours:  72,
		NewAccountDailyNaira: 10_000,
	}

	for name, dst := range map[string]*int64{"SWAP_FEE_BPS": &c.SwapFeeBps, "PAYOUT_FEE_BPS": &c.PayoutFeeBps} {
		v := os.Getenv(name)
		if v == "" {
			continue
		}
		bps, err := strconv.ParseInt(v, 10, 64)
		if err != nil || bps < 0 || bps > 1000 {
			return c, fmt.Errorf("%s must be a whole number from 0 to 1000", name)
		}
		*dst = bps
	}

	if v := os.Getenv("PAYOUT_CURRENCIES"); v != "" {
		c.PayoutCurrencies = strings.Split(strings.ToUpper(strings.ReplaceAll(v, " ", "")), ",")
	}
	for name, dst := range map[string]*int64{
		"LIMIT_DAILY_NGN": &c.DailyLimitNaira, "LIMIT_SINGLE_NGN": &c.SingleLimitNaira,
		"LIMIT_MAX_SENDS_PER_DAY": &c.MaxSendsPerDay, "LIMIT_NEW_ACCOUNT_HOLD_HOURS": &c.NewAccountHoldHours,
		"LIMIT_NEW_ACCOUNT_DAILY_NGN": &c.NewAccountDailyNaira,
		"CARD_CREATION_FEE_MICRO_USD": &c.CardCreationFee, "CARD_CREATION_COST_MICRO_USD": &c.CardCreationCost,
		"CARD_FUND_FEE_MICRO_USD": &c.CardFundFee, "CARD_FUND_COST_MICRO_USD": &c.CardFundCost,
	} {
		v := os.Getenv(name)
		if v == "" {
			continue
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			return c, fmt.Errorf("%s must be a whole number", name)
		}
		*dst = n
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
