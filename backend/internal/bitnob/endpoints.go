package bitnob

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Every path and shape in this file was confirmed against the sandbox.

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
// Shapes confirmed against the sandbox. Quantities and prices are decimal
// strings in major units.

// TradingQuoteRequest asks for a locked rate. With side "sell", quantity is
// the amount of base_currency the customer gives up; every one of the twelve
// directions between NGN, USDT, USDC and BTC can be expressed that way.
type TradingQuoteRequest struct {
	BaseCurrency  string `json:"base_currency"`
	QuoteCurrency string `json:"quote_currency"`
	Side          string `json:"side"`
	Quantity      string `json:"quantity"`
}

// TradingQuote expires after about 30 seconds on NGN pairs and 5 minutes on
// crypto-only pairs; always read ExpiresAt.
type TradingQuote struct {
	ID        string    `json:"id"`
	Price     string    `json:"price"`
	ExpiresAt time.Time `json:"expires_at"`
	Exchange  *struct {
		SendQuantity    string `json:"send_quantity"`
		SendCurrency    string `json:"send_currency"`
		ReceiveQuantity string `json:"receive_quantity"`
		ReceiveCurrency string `json:"receive_currency"`
	} `json:"exchange"`
}

func (c *Client) CreateTradingQuote(ctx context.Context, in TradingQuoteRequest) (TradingQuote, json.RawMessage, error) {
	raw, err := c.Raw(ctx, http.MethodPost, "/api/trading/quotes", in)
	if err != nil {
		return TradingQuote{}, nil, err
	}
	var env envelope[struct {
		Quote TradingQuote `json:"quote"`
	}]
	if err := json.Unmarshal(raw, &env); err != nil {
		return TradingQuote{}, nil, err
	}
	return env.Data.Quote, raw, nil
}

type CreateOrderRequest struct {
	BaseCurrency  string `json:"base_currency"`
	QuoteCurrency string `json:"quote_currency"`
	Side          string `json:"side"`
	Quantity      string `json:"quantity"`
	Price         string `json:"price"`
	QuoteID       string `json:"quote_id"`
	Reference     string `json:"reference"`
}

// Order status is "pending", "filled", "rejected" or "cancelled". Trades are
// all-or-nothing.
type Order struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Reference string `json:"reference"`
}

// CreateOrder executes against a quote. A quote can be consumed once.
func (c *Client) CreateOrder(ctx context.Context, in CreateOrderRequest) (Order, json.RawMessage, error) {
	raw, err := c.Raw(ctx, http.MethodPost, "/api/trading/orders", in)
	if err != nil {
		return Order{}, nil, err
	}
	var env envelope[struct {
		Order Order `json:"order"`
	}]
	if err := json.Unmarshal(raw, &env); err != nil {
		return Order{}, nil, err
	}
	return env.Data.Order, raw, nil
}

func (c *Client) GetOrder(ctx context.Context, id string) (Order, error) {
	var env envelope[struct {
		Order Order `json:"order"`
	}]
	err := c.do(ctx, http.MethodGet, "/api/trading/orders/"+id, nil, &env)
	return env.Data.Order, err
}

// ListOrders returns recent orders. In the sandbox, orders on NGN pairs do
// not appear here, so a missing order is not proof it was never placed.
func (c *Client) ListOrders(ctx context.Context) ([]Order, error) {
	var env envelope[struct {
		Orders []Order `json:"orders"`
	}]
	err := c.do(ctx, http.MethodGet, "/api/trading/orders", nil, &env)
	return env.Data.Orders, err
}

// --- M3: payouts -----------------------------------------------------------
// Shapes confirmed against the sandbox.

// SupportedCountries is the live corridor list; never hardcode it.
func (c *Client) SupportedCountries(ctx context.Context) (json.RawMessage, error) {
	return c.Raw(ctx, http.MethodGet, "/api/payouts/supported-countries", nil)
}

// CountryDetails returns, per rail, the beneficiary fields to collect, the
// bank directory and the amount limits.
func (c *Client) CountryDetails(ctx context.Context, country string) (json.RawMessage, error) {
	return c.Raw(ctx, http.MethodGet, "/api/payouts/supported-countries/"+url.PathEscape(country), nil)
}

// AccountLookup resolves the name on a Nigerian bank account or a Ghanaian
// mobile-money wallet. query carries country, bank_code, account_number and,
// for mobile money, type.
func (c *Client) AccountLookup(ctx context.Context, query url.Values) (json.RawMessage, error) {
	return c.Raw(ctx, http.MethodGet, "/api/payouts/account-lookup?"+query.Encode(), nil)
}

// PayoutQuoteRequest sets either Amount (in FromAsset) or SettlementAmount (in
// ToCurrency). FromAsset may be NGN, USDT, USDC or BTC.
type PayoutQuoteRequest struct {
	Amount           string `json:"amount,omitempty"`
	SettlementAmount string `json:"settlement_amount,omitempty"`
	Country          string `json:"country"`
	FromAsset        string `json:"from_asset"`
	ToCurrency       string `json:"to_currency"`
	Source           string `json:"source"` // "offchain": paid from our Bitnob balance
	Reference        string `json:"reference"`
}

// Payout is Bitnob's payout record. Status runs QUOTE, INITIATED, PENDING,
// then SUCCESS, FAILED or EXPIRED. Amounts are decimal strings in major units.
type Payout struct {
	ID      string `json:"id"`       // UUID; used to fetch the payout
	QuoteID string `json:"quote_id"` // used in the initialize/finalize paths
	Status  string `json:"status"`
	// TotalAmount is what leaves our balance, in FromAsset, fees included.
	TotalAmount      string    `json:"total_amount"`
	SettlementAmount string    `json:"settlement_amount"`
	Reference        string    `json:"reference"`
	ExpiresAt        time.Time `json:"expires_at"`
}

func (c *Client) payout(ctx context.Context, method, path string, in any) (Payout, json.RawMessage, error) {
	raw, err := c.Raw(ctx, method, path, in)
	if err != nil {
		return Payout{}, nil, err
	}
	var env envelope[struct {
		Payout Payout `json:"payout"`
	}]
	if err := json.Unmarshal(raw, &env); err != nil {
		return Payout{}, nil, err
	}
	return env.Data.Payout, raw, nil
}

// CreatePayoutQuote locks a rate (about 16 minutes on stablecoin and NGN
// sources, 6 on BTC; read ExpiresAt).
func (c *Client) CreatePayoutQuote(ctx context.Context, in PayoutQuoteRequest) (Payout, json.RawMessage, error) {
	return c.payout(ctx, http.MethodPost, "/api/payouts/quotes", in)
}

// InitializePayout attaches the beneficiary. beneficiary is destination_type
// and country plus the rail's field keys exactly as Country Details names them.
func (c *Client) InitializePayout(ctx context.Context, quoteID, reference, reason string, beneficiary map[string]any) (Payout, json.RawMessage, error) {
	return c.payout(ctx, http.MethodPost, "/api/payouts/"+url.PathEscape(quoteID)+"/initialize", map[string]any{
		"quote_id":       quoteID,
		"reference":      reference,
		"payment_reason": reason,
		"beneficiary":    beneficiary,
	})
}

// FinalizePayout commits the payout; funds leave our balance.
func (c *Client) FinalizePayout(ctx context.Context, quoteID string) (Payout, json.RawMessage, error) {
	return c.payout(ctx, http.MethodPost, "/api/payouts/"+url.PathEscape(quoteID)+"/finalize", nil)
}

// GetPayout fetches by Payout.ID. It is what the sweeper polls.
func (c *Client) GetPayout(ctx context.Context, id string) (Payout, json.RawMessage, error) {
	return c.payout(ctx, http.MethodGet, "/api/payouts/"+url.PathEscape(id), nil)
}

// --- M4: crypto in/out -----------------------------------------------------
// Shapes confirmed against the sandbox.

// Chain is a network and the assets that move on it.
type Chain struct {
	Chain       string `json:"chain"`
	NativeToken struct {
		Symbol string `json:"symbol"`
	} `json:"native_token"`
	Stablecoins []struct {
		Symbol string `json:"symbol"`
	} `json:"stablecoins"`
}

// SupportedChains is the live network list. The sandbox offers a subset of
// production (no Tron, for one).
func (c *Client) SupportedChains(ctx context.Context) ([]Chain, error) {
	var env envelope[struct {
		Chains []Chain `json:"chains"`
	}]
	err := c.do(ctx, http.MethodGet, "/api/stablecoins/supported-chains", nil, &env)
	return env.Data.Chains, err
}

type Address struct {
	ID      string `json:"id"`
	Chain   string `json:"chain"`
	Address string `json:"address"`
}

// GenerateAddress mints a deposit address on chain. One address receives
// every supported asset on that chain.
func (c *Client) GenerateAddress(ctx context.Context, chain, label, reference string) (Address, error) {
	var env envelope[Address]
	err := c.do(ctx, http.MethodPost, "/api/addresses", map[string]string{
		"chain": chain, "label": label, "reference": reference,
	}, &env)
	return env.Data, err
}

// ValidateAddress reports whether address is well-formed for chain.
func (c *Client) ValidateAddress(ctx context.Context, chain, address string) (bool, error) {
	var env envelope[struct {
		Valid bool `json:"valid"`
	}]
	err := c.do(ctx, http.MethodPost, "/api/addresses/validate", map[string]string{
		"chain": chain, "address": address,
	}, &env)
	return env.Data.Valid, err
}

// WithdrawalRequest sends crypto to an external address. Amount is an integer
// string in the asset's smallest unit. Bitnob charges its fee on top.
type WithdrawalRequest struct {
	ToAddress string `json:"to_address"`
	Amount    string `json:"amount"`
	Currency  string `json:"currency"`
	Chain     string `json:"chain"`
	Reference string `json:"reference"`
}

type Withdrawal struct {
	TransactionID string `json:"transaction_id"`
	Status        string `json:"status"` // pending | completed | failed
}

// CreateWithdrawal queues a withdrawal. Repeating a reference does not return
// the original: it fails with 409 DUPLICATE_KEY_ERROR (see IsDuplicate), and
// there is no endpoint to fetch a withdrawal, so the outcome arrives by
// webhook or through Transactions.
func (c *Client) CreateWithdrawal(ctx context.Context, in WithdrawalRequest) (Withdrawal, json.RawMessage, error) {
	raw, err := c.Raw(ctx, http.MethodPost, "/api/withdrawals", in)
	if err != nil {
		return Withdrawal{}, nil, err
	}
	var env envelope[Withdrawal]
	if err := json.Unmarshal(raw, &env); err != nil {
		return Withdrawal{}, nil, err
	}
	return env.Data, raw, nil
}

// Transaction is one movement on our Bitnob balances. Amount and Fee are
// integer strings in the currency's smallest unit; Amount is negative for
// money out.
type Transaction struct {
	TransactionID string `json:"transaction_id"`
	Currency      string `json:"currency"`
	Type          string `json:"type"`  // DEPOSIT_CONFIRMED, WITHDRAWAL_INITIATED, PAYOUT, ...
	State         string `json:"state"` // SETTLED, ...
	Amount        string `json:"amount"`
	Fee           string `json:"fee"`
	Reference     string `json:"reference"`
	TradeID       string `json:"trade_id"`
	Metadata      struct {
		Address       string `json:"address"`
		Chain         string `json:"chain"`
		TxHash        string `json:"tx_hash"`
		PayoutID      string `json:"payout_id"`
		TransactionID string `json:"transaction_id"`
		ProviderTxID  string `json:"provider_tx_id"`
		Reference     string `json:"reference"`
	} `json:"metadata"`
	CreatedAt time.Time `json:"created_at"`
}

// Keys are the identifiers this movement may be known by on our side: its
// own id, the reference we or Bitnob gave it, and the ids of the payout,
// trade or deposit behind it.
func (t Transaction) Keys() []string {
	var keys []string
	for _, k := range []string{
		t.TransactionID, t.Reference, t.TradeID,
		t.Metadata.PayoutID, t.Metadata.TransactionID, t.Metadata.ProviderTxID, t.Metadata.Reference,
	} {
		if k != "" {
			keys = append(keys, k)
		}
	}
	return keys
}

// Transactions lists the most recent movements, newest first.
func (c *Client) Transactions(ctx context.Context, limit int) ([]Transaction, error) {
	var env envelope[struct {
		Transactions []Transaction `json:"transactions"`
	}]
	err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/transactions?limit=%d", limit), nil, &env)
	return env.Data.Transactions, err
}
