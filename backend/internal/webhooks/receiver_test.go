package webhooks

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"testing"
)

func TestVerify(t *testing.T) {
	body := []byte(`{"event":"payouts.withdrawal.success","event_id":"evt_1"}`)
	mac := hmac.New(sha512.New, []byte("secret"))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))

	if !Verify("secret", body, sig) {
		t.Error("valid signature rejected")
	}
	if Verify("other", body, sig) {
		t.Error("wrong secret accepted")
	}
	if Verify("secret", append(body, ' '), sig) {
		t.Error("tampered body accepted")
	}
	if Verify("secret", body, "") {
		t.Error("empty signature accepted")
	}
}
