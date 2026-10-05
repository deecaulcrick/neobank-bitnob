package bitnob

import (
	"context"
	"encoding/json"
	"net/http"
)

// Responses are kept as raw JSON until each milestone pins the real shapes
// against the sandbox; callers store them in the `raw` columns either way.
//
// Paths marked UNVERIFIED come from the spec's brief, not the API reference.
// Check each against https://bitnob.dev/api-reference/ before building on it.

// WhoAmI validates credentials (M0 health check).
func (c *Client) WhoAmI(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	return out, c.do(ctx, http.MethodGet, "/api/whoami", nil, &out)
}

// Balances is for reconciliation only; the app reads our ledger.
func (c *Client) Balances(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	return out, c.do(ctx, http.MethodGet, "/api/balances", nil, &out)
}

// --- M1: accounts ----------------------------------------------------------

type CreateCustomerRequest struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email,omitempty"`
	Phone     string `json:"phone"`
	// Open question: BVN, NIN or both, and the exact field names.
}

func (c *Client) CreateCustomer(ctx context.Context, in CreateCustomerRequest) (json.RawMessage, error) {
	var out json.RawMessage
	return out, c.do(ctx, http.MethodPost, "/api/customers", in, &out)
}

// CreateVirtualAccount issues the user's NGN account number. UNVERIFIED path.
func (c *Client) CreateVirtualAccount(ctx context.Context, in any) (json.RawMessage, error) {
	var out json.RawMessage
	return out, c.do(ctx, http.MethodPost, "/api/virtual-accounts/create", in, &out)
}

// --- M2: swaps -------------------------------------------------------------

type TradingQuoteRequest struct {
	FromAsset string `json:"from_asset"`
	ToAsset   string `json:"to_asset"`
	Amount    string `json:"amount"`
}

func (c *Client) CreateTradingQuote(ctx context.Context, in TradingQuoteRequest) (json.RawMessage, error) {
	var out json.RawMessage
	return out, c.do(ctx, http.MethodPost, "/api/trading/quotes", in, &out)
}

// CreateOrder executes against a quote; `trade.completed` finishes it. UNVERIFIED path.
func (c *Client) CreateOrder(ctx context.Context, in any) (json.RawMessage, error) {
	var out json.RawMessage
	return out, c.do(ctx, http.MethodPost, "/api/trading/orders", in, &out)
}

// --- M3: payouts -----------------------------------------------------------

// SupportedCountries is the live corridor list; never hardcode it. UNVERIFIED path.
func (c *Client) SupportedCountries(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	return out, c.do(ctx, http.MethodGet, "/api/payouts/countries", nil, &out)
}

// CountryDetails returns the beneficiary fields for a country. UNVERIFIED path.
func (c *Client) CountryDetails(ctx context.Context, country string) (json.RawMessage, error) {
	var out json.RawMessage
	return out, c.do(ctx, http.MethodGet, "/api/payouts/countries/"+country, nil, &out)
}

func (c *Client) CreatePayoutQuote(ctx context.Context, in any) (json.RawMessage, error) {
	var out json.RawMessage
	return out, c.do(ctx, http.MethodPost, "/api/payouts/quotes", in, &out)
}

// InitializePayout and FinalizePayout: UNVERIFIED paths.
func (c *Client) InitializePayout(ctx context.Context, in any) (json.RawMessage, error) {
	var out json.RawMessage
	return out, c.do(ctx, http.MethodPost, "/api/payouts/initialize", in, &out)
}

func (c *Client) FinalizePayout(ctx context.Context, in any) (json.RawMessage, error) {
	var out json.RawMessage
	return out, c.do(ctx, http.MethodPost, "/api/payouts/finalize", in, &out)
}

// GetPayout is what the sweeper polls for missed webhooks. UNVERIFIED path.
func (c *Client) GetPayout(ctx context.Context, id string) (json.RawMessage, error) {
	var out json.RawMessage
	return out, c.do(ctx, http.MethodGet, "/api/payouts/"+id, nil, &out)
}

// --- M4: crypto in/out -----------------------------------------------------

// GenerateAddress and CreateWithdrawal: UNVERIFIED paths.
func (c *Client) GenerateAddress(ctx context.Context, in any) (json.RawMessage, error) {
	var out json.RawMessage
	return out, c.do(ctx, http.MethodPost, "/api/addresses/generate", in, &out)
}

func (c *Client) CreateWithdrawal(ctx context.Context, in any) (json.RawMessage, error) {
	var out json.RawMessage
	return out, c.do(ctx, http.MethodPost, "/api/withdrawals", in, &out)
}
