// Package webhooks receives Bitnob events (store first, return 200 fast) and
// processes them from the webhook_events table, which doubles as the queue.
package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

const signatureHeader = "x-bitnob-signature"

type envelope struct {
	Event   string `json:"event"`
	EventID string `json:"event_id"`
}

type Receiver struct {
	pool   *pgxpool.Pool
	secret string
	log    *slog.Logger
}

// NewReceiver takes the webhook signing secret. An empty secret disables
// verification; config refuses to start that way in production.
func NewReceiver(pool *pgxpool.Pool, secret string, log *slog.Logger) *Receiver {
	if secret == "" {
		log.Warn("BITNOB_WEBHOOK_SECRET is empty: webhook signatures are NOT verified")
	}
	return &Receiver{pool: pool, secret: secret, log: log}
}

// Verify checks the hex HMAC-SHA512 of the raw body.
//
// Open question in the spec: Bitnob's docs name the header and algorithm
// ("HMAC SHA512 ... signed with your secret key") but not the encoding;
// hex over the raw body is assumed here. Confirm against a sandbox event.
func Verify(secret string, body []byte, signature string) bool {
	mac := hmac.New(sha512.New, []byte(secret))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(want), []byte(signature))
}

func (rc *Receiver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
		return
	}
	if rc.secret != "" && !Verify(rc.secret, body, r.Header.Get(signatureHeader)) {
		rc.log.Error("webhook signature mismatch", "remote", r.RemoteAddr)
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	var env envelope
	if err := json.Unmarshal(body, &env); err != nil || env.Event == "" {
		http.Error(w, "malformed event", http.StatusBadRequest)
		return
	}
	if env.EventID == "" {
		// Not every event type is confirmed to carry event_id; a content hash
		// still dedupes exact redeliveries.
		sum := sha256.Sum256(body)
		env.EventID = "sha256:" + hex.EncodeToString(sum[:])
	}

	// A duplicate insert means "already seen": still a 200.
	tag, err := rc.pool.Exec(r.Context(),
		`insert into webhook_events (event_id, event, payload) values ($1, $2, $3)
		 on conflict (event_id) do nothing`,
		env.EventID, env.Event, body)
	if err != nil {
		// Non-200 makes Bitnob retry, which is what we want if we couldn't store it.
		rc.log.Error("store webhook", "err", err, "event_id", env.EventID)
		http.Error(w, "try again", http.StatusServiceUnavailable)
		return
	}
	rc.log.Info("webhook received", "event", env.Event, "event_id", env.EventID, "duplicate", tag.RowsAffected() == 0)
	w.WriteHeader(http.StatusOK)
}
