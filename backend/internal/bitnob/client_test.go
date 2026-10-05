package bitnob

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDoSignsExactBody(t *testing.T) {
	const id, secret = "client_123", "shh"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		ts, nonce := r.Header.Get("X-Auth-Timestamp"), r.Header.Get("X-Auth-Nonce")

		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(id + ":" + ts + ":" + nonce + ":" + string(body)))
		want := hex.EncodeToString(mac.Sum(nil))

		if r.Header.Get("X-Auth-Client") != id {
			t.Errorf("X-Auth-Client = %q", r.Header.Get("X-Auth-Client"))
		}
		if ts != "1791072000" || nonce != "abc" {
			t.Errorf("timestamp/nonce = %q/%q", ts, nonce)
		}
		if got := r.Header.Get("X-Auth-Signature"); got != want {
			t.Errorf("signature = %s, want %s", got, want)
		}
		w.Write([]byte(`{"success":true,"data":{"id":"cus_1"}}`))
	}))
	defer srv.Close()

	c := New(srv.URL, id, secret)
	c.now = func() time.Time { return time.Unix(1791072000, 0) }
	c.nonce = func() (string, error) { return "abc", nil }

	if _, _, err := c.CreateCustomer(context.Background(), CreateCustomerRequest{FirstName: "Dee", PhoneNumber: "8000000000"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.WhoAmI(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestDoReturnsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"invalid signature"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := New(srv.URL, "id", "secret").WhoAmI(context.Background())
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Status != http.StatusUnauthorized {
		t.Fatalf("err = %v, want APIError 401", err)
	}
}
