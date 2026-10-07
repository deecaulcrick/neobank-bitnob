package bitnob

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// Virtual cards. Amounts are micro-dollars (1,000,000 = $1). Cards are funded
// from our USDC balance. Shapes confirmed against the sandbox except where a
// comment says otherwise.

// CardKYCRequest is the fuller identity check a card needs. The identity
// number goes inside Customer; the name must match the document.
type CardKYCRequest struct {
	Customer struct {
		CustomerType string `json:"customer_type"`
		FirstName    string `json:"first_name"`
		LastName     string `json:"last_name"`
		Email        string `json:"email"`
		PhoneNumber  string `json:"phone_number"`
		DialCode     string `json:"dial_code"`
		DateOfBirth  string `json:"date_of_birth"`
		Country      string `json:"country"`
		Line1        string `json:"line1"`
		City         string `json:"city"`
		State        string `json:"state"`
		PostalCode   string `json:"postal_code"`
		IDType       string `json:"id_type"`
		IDNumber     string `json:"id_number"`
	} `json:"customer"`
	IDType                string `json:"id_type"`
	IDNumber              string `json:"id_number"`
	Occupation            string `json:"occupation"`
	EmploymentStatus      string `json:"employment_status"`
	AccountPurpose        string `json:"account_purpose"`
	AnnualSalary          string `json:"annual_salary"`
	ExpectedMonthlyVolume string `json:"expected_monthly_volume"`
	TermsAccepted         bool   `json:"terms_of_service_accepted"`
}

type CardKYC struct {
	CustomerID string `json:"customer_id"`
	// NormalizedStatus is "approved" when a card can be created; anything
	// else is settled later by a virtualcard.user.kyc.* webhook.
	NormalizedStatus string `json:"normalized_status"`
}

func (c *Client) CardKYC(ctx context.Context, in CardKYCRequest) (CardKYC, error) {
	var env envelope[CardKYC]
	err := c.do(ctx, http.MethodPost, "/api/cards/kyc", in, &env)
	return env.Data, err
}

// Card is a card's public state. Status is pending, active, frozen or
// terminated; CreatedStatus goes processing, then completed or failed.
type Card struct {
	ID             string `json:"id"`
	Status         string `json:"status"`
	CreatedStatus  string `json:"created_status"`
	CardBrand      string `json:"card_brand"`
	LastFour       string `json:"last_four_digit"`
	BalanceAmount  string `json:"balance_amount"`
	Reference      string `json:"reference"`
	BillingAddress struct {
		Line1      string `json:"line1"`
		City       string `json:"city"`
		State      string `json:"state"`
		PostalCode string `json:"postal_code"`
		Country    string `json:"country"`
	} `json:"billing_address"`
}

type cardEnvelope = envelope[struct {
	Card Card `json:"card"`
}]

// CreateCard issues a virtual USD card loaded with amount. Issuing is
// asynchronous: the card comes back pending and virtualcard.created.* follows.
func (c *Client) CreateCard(ctx context.Context, customerID, name, reference string, amount int64) (Card, json.RawMessage, error) {
	raw, err := c.Raw(ctx, http.MethodPost, "/api/cards", map[string]any{
		"amount": amount, "card_type": "virtual", "currency": "USD", "card_brand": "visa",
		"name": name, "customer_id": customerID, "reference": reference,
	})
	if err != nil {
		return Card{}, nil, err
	}
	var env cardEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return Card{}, nil, err
	}
	return env.Data.Card, raw, nil
}

func (c *Client) GetCard(ctx context.Context, cardID string) (Card, error) {
	var env cardEnvelope
	err := c.do(ctx, http.MethodGet, "/api/cards/"+url.PathEscape(cardID), nil, &env)
	return env.Data.Card, err
}

// CardSecrets is the full card number, security code and expiry. Never log
// or store it. Bitnob allows two reads a minute per card.
type CardSecrets struct {
	CardNumber  string `json:"card_number"`
	CVV         string `json:"cvv"`
	ExpiryMonth string `json:"expiry_month"`
	ExpiryYear  string `json:"expiry_year"`
	Name        string `json:"name"`
}

func (c *Client) CardSecrets(ctx context.Context, cardID string) (CardSecrets, error) {
	var env envelope[struct {
		Details CardSecrets `json:"details"`
	}]
	err := c.do(ctx, http.MethodGet, "/api/cards/"+url.PathEscape(cardID)+"/secure", nil, &env)
	return env.Data.Details, err
}

// MoveCardBalance funds ("fund") or withdraws from ("withdraw") a card. It
// answers at once with the movement still pending; virtualcard.topup.* or
// virtualcard.withdrawal.* reports the outcome. Response shape from the docs.
func (c *Client) MoveCardBalance(ctx context.Context, cardID, kind, reference string, amount int64) (json.RawMessage, error) {
	return c.Raw(ctx, http.MethodPost, "/api/cards/"+url.PathEscape(cardID)+"/balance", map[string]any{
		"amount": amount, "type": kind, "reference": reference,
	})
}

// SetCardStatus freezes ("frozen") or unfreezes ("active") a card. Request
// shape from the docs.
func (c *Client) SetCardStatus(ctx context.Context, cardID, status string) error {
	return c.do(ctx, http.MethodPost, "/api/cards/"+url.PathEscape(cardID)+"/status", map[string]string{"status": status}, nil)
}

// CardTransaction is one line on a card's statement: a load, a fee, a
// purchase, a refund. Amount and FeeAmount are micro-dollar strings.
type CardTransaction struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Status      string `json:"status"`
	Description string `json:"description"`
	Amount      string `json:"amount"`
	FeeAmount   string `json:"fee_amount"`
	Reference   string `json:"reference"`
	CreatedAt   string `json:"created_at"`
}

func (c *Client) CardTransactions(ctx context.Context, cardID string) ([]CardTransaction, error) {
	var env envelope[struct {
		Transactions []CardTransaction `json:"transactions"`
	}]
	err := c.do(ctx, http.MethodGet, "/api/cards/"+url.PathEscape(cardID)+"/transactions", nil, &env)
	return env.Data.Transactions, err
}
