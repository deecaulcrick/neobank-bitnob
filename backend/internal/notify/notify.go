// Package notify sends push notifications. A notification is queued in the
// same database transaction as the money movement it reports, then delivered
// by the worker through Expo's push service.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DBTX is satisfied by both *pgxpool.Pool and pgx.Tx.
type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Queue records a notification for userID. Pass the transaction that posts
// the ledger entry so the two commit together.
func Queue(ctx context.Context, db DBTX, userID uuid.UUID, title, body string) error {
	_, err := db.Exec(ctx, `insert into notifications (user_id, title, body) values ($1, $2, $3)`, userID, title, body)
	return err
}

// RegisterDevice stores an Expo push token for a user. A token moves with
// whoever signed in on the device last.
func RegisterDevice(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, token, platform string) error {
	_, err := pool.Exec(ctx,
		`insert into device_tokens (token, user_id, platform) values ($1, $2, $3)
		 on conflict (token) do update set user_id = excluded.user_id, platform = excluded.platform, updated_at = now()`,
		token, userID, platform)
	return err
}

type Sender struct {
	Pool *pgxpool.Pool
	Log  *slog.Logger
	// URL is Expo's push endpoint; overridable in tests.
	URL  string
	HTTP *http.Client
}

const expoPushURL = "https://exp.host/--/api/v2/push/send"

type message struct {
	To    string `json:"to"`
	Title string `json:"title"`
	Body  string `json:"body"`
	Sound string `json:"sound"`
}

// Deliver sends every queued notification once, oldest first.
func (s *Sender) Deliver(ctx context.Context) error {
	rows, err := s.Pool.Query(ctx,
		`select n.id, n.title, n.body,
		        coalesce(array_agg(d.token) filter (where d.token is not null), '{}')
		 from notifications n left join device_tokens d on d.user_id = n.user_id
		 where n.sent_at is null and n.attempts < 5
		 group by n.id order by n.id limit 50`)
	if err != nil {
		return err
	}
	type pending struct {
		id          int64
		title, body string
		tokens      []string
	}
	queue, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (pending, error) {
		var p pending
		return p, row.Scan(&p.id, &p.title, &p.body, &p.tokens)
	})
	if err != nil {
		return err
	}
	for _, n := range queue {
		sendErr := error(nil)
		if len(n.tokens) > 0 {
			sendErr = s.push(ctx, n.title, n.body, n.tokens)
		}
		switch {
		case sendErr != nil:
			s.Log.Warn("push failed", "notification", n.id, "err", sendErr)
			_, err = s.Pool.Exec(ctx,
				`update notifications set attempts = attempts + 1, last_error = $2 where id = $1`, n.id, sendErr.Error())
		case len(n.tokens) == 0:
			// Nowhere to send it; keep the row as the in-app record.
			_, err = s.Pool.Exec(ctx, `update notifications set sent_at = now(), last_error = 'no device registered' where id = $1`, n.id)
		default:
			_, err = s.Pool.Exec(ctx, `update notifications set sent_at = now(), last_error = null where id = $1`, n.id)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Sender) push(ctx context.Context, title, body string, tokens []string) error {
	msgs := make([]message, len(tokens))
	for i, t := range tokens {
		msgs[i] = message{To: t, Title: title, Body: body, Sound: "default"}
	}
	payload, _ := json.Marshal(msgs)
	url := s.URL
	if url == "" {
		url = expoPushURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := s.HTTP
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(res.Body, 500))
		return fmt.Errorf("expo push: http %d: %s", res.StatusCode, detail)
	}
	return nil
}
