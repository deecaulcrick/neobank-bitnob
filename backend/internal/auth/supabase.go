// Package auth verifies Supabase Auth JWTs. The app signs in with phone + OTP
// against Supabase and sends the access token to this API as a bearer token.
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type User struct {
	ID    uuid.UUID
	Phone string
}

type ctxKey struct{}

// FromContext returns the authenticated user set by Middleware.
func FromContext(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(ctxKey{}).(User)
	return u, ok
}

type Verifier struct {
	keyfunc jwt.Keyfunc
	methods []string
}

// NewVerifier uses the legacy HS256 secret when one is given, otherwise the
// project's JWKS (asymmetric signing keys, the default for new projects).
func NewVerifier(ctx context.Context, supabaseURL, jwtSecret string) (*Verifier, error) {
	if jwtSecret != "" {
		secret := []byte(jwtSecret)
		return &Verifier{
			keyfunc: func(*jwt.Token) (any, error) { return secret, nil },
			methods: []string{"HS256"},
		}, nil
	}
	jwks, err := keyfunc.NewDefaultCtx(ctx, []string{supabaseURL + "/auth/v1/.well-known/jwks.json"})
	if err != nil {
		return nil, fmt.Errorf("auth: load JWKS: %w", err)
	}
	return &Verifier{keyfunc: jwks.Keyfunc, methods: []string{"ES256", "RS256"}}, nil
}

type claims struct {
	Phone string `json:"phone"`
	jwt.RegisteredClaims
}

func (v *Verifier) Verify(token string) (User, error) {
	var c claims
	_, err := jwt.ParseWithClaims(token, &c, v.keyfunc,
		jwt.WithValidMethods(v.methods),
		jwt.WithAudience("authenticated"),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return User{}, err
	}
	id, err := uuid.Parse(c.Subject)
	if err != nil {
		return User{}, errors.New("auth: subject is not a uuid")
	}
	return User{ID: id, Phone: c.Phone}, nil
}

func (v *Verifier) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			http.Error(w, `{"error":"missing bearer token"}`, http.StatusUnauthorized)
			return
		}
		user, err := v.Verify(token)
		if err != nil {
			http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, user)))
	})
}
