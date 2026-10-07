// Package pin is the transaction PIN: set once, then required on every
// money movement. Only a bcrypt hash is stored.
package pin

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

const (
	maxAttempts = 5
	lockFor     = 15 * time.Minute
)

var (
	ErrNotSet = errors.New("pin: not set")
	// ErrRequired means the request carried no PIN at all.
	ErrRequired = errors.New("pin: required")
)

// WrongError is a failed attempt. Left is how many tries remain before the
// PIN locks.
type WrongError struct{ Left int }

func (e *WrongError) Error() string { return fmt.Sprintf("pin: wrong, %d left", e.Left) }

// LockedError means too many wrong attempts; nothing is accepted until Until.
type LockedError struct{ Until time.Time }

func (e *LockedError) Error() string { return "pin: locked until " + e.Until.Format(time.RFC3339) }

// InvalidError is a PIN we won't accept as a new PIN. Safe to show.
type InvalidError struct{ Message string }

func (e *InvalidError) Error() string { return e.Message }

var fourDigits = regexp.MustCompile(`^\d{4}$`)

// weak is the handful of PINs people try first.
var weak = map[string]bool{"0000": true, "1111": true, "2222": true, "3333": true, "4444": true, "5555": true,
	"6666": true, "7777": true, "8888": true, "9999": true, "1234": true, "4321": true, "1212": true, "0123": true}

// Set stores a new PIN. Changing an existing one requires the current PIN.
func Set(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, current, next string) error {
	if !fourDigits.MatchString(next) {
		return &InvalidError{"Your PIN is 4 digits."}
	}
	if weak[next] {
		return &InvalidError{"That PIN is too easy to guess. Pick another."}
	}
	var has bool
	if err := pool.QueryRow(ctx, `select pin_hash is not null from users where id = $1`, userID).Scan(&has); err != nil {
		return err
	}
	if has {
		if err := Verify(ctx, pool, userID, current); err != nil {
			return err
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(next), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx,
		`update users set pin_hash = $2, pin_failed_attempts = 0, pin_locked_until = null where id = $1`,
		userID, string(hash))
	return err
}

// Verify checks a PIN, counting failures and locking after too many.
func Verify(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, candidate string) error {
	if candidate == "" {
		return ErrRequired
	}
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var (
			hash   *string
			locked *time.Time
			failed int
		)
		// Row lock, so parallel guesses can't each get a fresh attempt.
		if err := tx.QueryRow(ctx,
			`select pin_hash, pin_locked_until, pin_failed_attempts from users where id = $1 for update`, userID,
		).Scan(&hash, &locked, &failed); err != nil {
			return err
		}
		if hash == nil {
			return ErrNotSet
		}
		if locked != nil && time.Now().Before(*locked) {
			return &LockedError{Until: *locked}
		}
		if bcrypt.CompareHashAndPassword([]byte(*hash), []byte(candidate)) == nil {
			if failed > 0 || locked != nil {
				_, err := tx.Exec(ctx, `update users set pin_failed_attempts = 0, pin_locked_until = null where id = $1`, userID)
				return err
			}
			return nil
		}

		failed++
		result := error(&WrongError{Left: maxAttempts - failed})
		var until *time.Time
		if failed >= maxAttempts {
			t := time.Now().Add(lockFor)
			until, failed, result = &t, 0, &LockedError{Until: t}
		}
		// The failure must be recorded even though we report an error, so the
		// transaction is committed here before the error is returned.
		if _, err := tx.Exec(ctx,
			`update users set pin_failed_attempts = $2, pin_locked_until = $3 where id = $1`, userID, failed, until); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return result
	})
}
