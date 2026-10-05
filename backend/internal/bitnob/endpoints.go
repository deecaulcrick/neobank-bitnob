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
// Shapes below were confirmed against the sandbox.

// envelope is the wrapper around every successful response.
type envelope[T any] struct {
	Data T `json:"data"`
}

// CreateCustomerRequest carries the KYC a naira virtual account needs. The
// name and date of birth must match the BVN's registered details.
type CreateCustomerRequest struct {
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	Email        string `json:"email"`
	CustomerType string `json:"customer_type"` // "individual"
	PhoneNumber  string `json:"phone_number"`  // national number, no dial code
	DialCode     string `json:"dial_code"`     // "+234"
	DateOfBirth  string `json:"date_of_birth"` // YYYY-MM-DD, must be 18+
	IDType       string `json:"id_type"`       // "bvn"
	IDNumber     string `json:"id_number"`
	Country      string `json:"country"` // ISO alpha-3, "NGA"
	Reference    string `json:"reference"`
}

type Customer struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	KYCStatus string `json:"kyc_status"`
}

// CreateCustomer returns the customer and the raw response for storage.
//
// Bitnob does not reject a duplicate: if the BVN or email already belongs to
// a customer it returns that existing customer with a success status. Callers
// must compare the returned email with the one they sent.
func (c *Client) CreateCustomer(ctx context.Context, in CreateCustomerRequest) (Customer, json.RawMessage, error) {
	raw, err := c.Raw(ctx, http.MethodPost, "/api/customers", in)
	if err != nil {
		return Customer{}, nil, err
	}
	var env envelope[Customer]
	if err := json.Unmarshal(raw, &env); err != nil {
		return Customer{}, nil, err
	}
	return env.Data, raw, nil
}

type VirtualAccount struct {
	ID            string `json:"id"`
	AccountNumber string `json:"account_number"`
	AccountName   string `json:"account_name"`
	BankName      string `json:"bank_name"`
	CustomerID    string `json:"customer_id"`
	Currency      string `json:"currency"`
	Status        string `json:"status"`
}

// CreateVirtualAccount issues the customer's NGN account number. reference is
// an idempotency key: repeating it returns the same account.
func (c *Client) CreateVirtualAccount(ctx context.Context, customerID, reference string) (VirtualAccount, json.RawMessage, error) {
	raw, err := c.Raw(ctx, http.MethodPost, "/api/virtual-accounts", map[string]string{
		"reference":   reference,
		"currency":    "NGN",
		"customer_id": customerID,
	})
	if err != nil {
		return VirtualAccount{}, nil, err
	}
	var env envelope[struct {
		VirtualAccount VirtualAccount `json:"virtual_account"`
	}]
	if err := json.Unmarshal(raw, &env); err != nil {
		return VirtualAccount{}, nil, err
	}
	return env.Data.VirtualAccount, raw, nil
}

// AccountTransaction is one movement on a virtual account. Amount is an
// integer string in kobo.
type AccountTransaction struct {
	ID                    string `json:"id"`
	VirtualAccountID      string `json:"virtual_account_id"`
	ProviderTransactionID string `json:"provider_transaction_id"`
	LedgerTransactionID   string `json:"ledger_transaction_id"`
	Amount                string `json:"amount"`
	Currency              string `json:"currency"`
	Type                  string `json:"type"`   // "credit" for deposits
	Status                string `json:"status"` // "completed" when settled
	Reference             string `json:"reference"`
}

// VirtualAccountTransactions lists the most recent movements on an account.
// It is how missed deposit webhooks are recovered.
func (c *Client) VirtualAccountTransactions(ctx context.Context, accountID string) ([]AccountTransaction, error) {
	var env envelope[struct {
		Transactions []AccountTransaction `json:"transactions"`
	}]
	err := c.do(ctx, http.MethodGet, "/api/virtual-accounts/"+accountID+"/transactions", nil, &env)
	return env.Data.Transactions, err
}

// SimulateDeposit credits a sandbox virtual account with a fixed NGN 1,000.
// Bitnob rejects it in production.
func (c *Client) SimulateDeposit(ctx context.Context, accountID string) (AccountTransaction, error) {
	var env envelope[struct {
		Transaction AccountTransaction `json:"transaction"`
	}]
	err := c.do(ctx, http.MethodPost, "/api/virtual-accounts/"+accountID+"/simulate-deposit", nil, &env)
	return env.Data.Transaction, err
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
