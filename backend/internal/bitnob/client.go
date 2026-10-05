// Package bitnob is the only code that talks to Bitnob. The mobile app never
// calls Bitnob; money services go through this client with signed requests.
package bitnob

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

type Client struct {
	baseURL      string
	clientID     string
	clientSecret string
	http         *http.Client

	// Overridable in tests.
	now   func() time.Time
	nonce func() (string, error)
}

// Configured reports whether credentials were supplied. They are optional in
// development, so callers that need Bitnob should check first.
func (c *Client) Configured() bool { return c.clientID != "" && c.clientSecret != "" }

func New(baseURL, clientID, clientSecret string) *Client {
	return &Client{
		baseURL:      baseURL,
		clientID:     clientID,
		clientSecret: clientSecret,
		http:         &http.Client{Timeout: 30 * time.Second},
		now:          time.Now,
		nonce:        randomNonce,
	}
}

// APIError is any non-2xx response from Bitnob.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("bitnob: http %d: %s", e.Status, e.Body)
}

// IsDuplicate reports whether err is Bitnob refusing a reference it has
// already seen.
func IsDuplicate(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusConflict
}

// Detail is Bitnob's human-readable explanation, when the body carries one.
func (e *APIError) Detail() string {
	var problem struct {
		Detail string `json:"detail"`
	}
	json.Unmarshal([]byte(e.Body), &problem)
	return problem.Detail
}

// Sign returns the hex HMAC-SHA256 of "CLIENT_ID:TIMESTAMP:NONCE:PAYLOAD".
// PAYLOAD is the exact request body bytes, or empty for bodiless requests.
// https://bitnob.dev/api-reference/authentication
func Sign(clientID, secret, timestamp, nonce string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(clientID + ":" + timestamp + ":" + nonce + ":"))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

func randomNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// do sends a signed request. in is marshalled once and those exact bytes are
// both signed and sent; out, when non-nil, receives the decoded response.
func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body []byte
	if in != nil {
		var err error
		if body, err = json.Marshal(in); err != nil {
			return fmt.Errorf("bitnob: encode request: %w", err)
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	nonce, err := c.nonce()
	if err != nil {
		return fmt.Errorf("bitnob: nonce: %w", err)
	}
	ts := strconv.FormatInt(c.now().Unix(), 10)

	req.Header.Set("X-Auth-Client", c.clientID)
	req.Header.Set("X-Auth-Timestamp", ts)
	req.Header.Set("X-Auth-Nonce", nonce)
	req.Header.Set("X-Auth-Signature", Sign(c.clientID, c.clientSecret, ts, nonce, body))
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("bitnob: %s %s: %w", method, path, err)
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("bitnob: read response: %w", err)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &APIError{Status: res.StatusCode, Body: string(raw)}
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("bitnob: decode response: %w", err)
	}
	return nil
}

// Raw sends a signed request to any path and returns the response body.
// For endpoints that don't have a typed method yet.
func (c *Client) Raw(ctx context.Context, method, path string, in any) (json.RawMessage, error) {
	var out json.RawMessage
	return out, c.do(ctx, method, path, in, &out)
}
