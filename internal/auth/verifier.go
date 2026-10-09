package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
)

type contextKey struct{}

// WithUserID stores an authenticated user identity in the context. The
// verifier uses it after token validation and tests use it to fake sign-in.
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, contextKey{}, userID)
}

// defaultIssuerBase is the issuer WorkOS stamps on AuthKit browser-flow
// access tokens: https://api.workos.com/user_management/<client-id>. Custom
// auth domains override it through WORKOS_ISSUER.
const defaultIssuerBase = "https://api.workos.com/user_management/"

func NewVerifier() *Verifier {
	clientID := os.Getenv("WORKOS_CLIENT_ID")
	issuer := envOr("WORKOS_ISSUER", "")
	if issuer == "" && clientID != "" {
		issuer = defaultIssuerBase + clientID
	}
	return &Verifier{
		clientID: clientID,
		issuer:   issuer,
		required: strings.EqualFold(os.Getenv("AUTH_REQUIRED"), "true"),
		local:    NewLocalAuth(),
	}
}

type Verifier struct {
	clientID string
	issuer   string
	required bool
	local    *LocalAuth
	mu       sync.Mutex
	keys     keyfunc.Keyfunc
}

// Local returns the local-account issuer that shares this verifier's secret.
func (v *Verifier) Local() *LocalAuth { return v.local }

func (v *Verifier) Verify(ctx context.Context, authorization string) (context.Context, error) {
	token := strings.TrimPrefix(authorization, "Bearer ")
	if token == "" || token == authorization {
		if v.required {
			return ctx, errors.New("sign in required")
		}
		return WithUserID(ctx, "guest"), nil
	}
	if userID, ok := v.local.parse(token); ok {
		return WithUserID(ctx, userID), nil
	}
	if v.clientID == "" {
		return ctx, errors.New("WORKOS_CLIENT_ID is not configured")
	}
	keys, err := v.keyfunc(ctx)
	if err != nil {
		slog.Warn("workos signing keys unavailable", "error", err)
		return ctx, errors.New("cannot load WorkOS signing keys")
	}
	parsed, err := jwt.Parse(token, keys.Keyfunc,
		jwt.WithExpirationRequired(), jwt.WithValidMethods([]string{"RS256"}),
	)
	if err != nil || !parsed.Valid {
		// The exact reason is logged for operators; the browser only sees a
		// generic message so claim details never leak to untrusted clients.
		slog.Warn("workos token rejected", "reason", parseReason(err))
		return ctx, errors.New("invalid WorkOS access token")
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return ctx, errors.New("invalid WorkOS claims")
	}
	issuer, _ := claims["iss"].(string)
	if !issuerMatches(issuer, v.issuer) {
		// WorkOS docs show the issuer both with and without a trailing slash,
		// so the comparison normalizes instead of hard-failing on formatting.
		slog.Warn("workos issuer mismatch", "expected", v.issuer, "actual", issuer)
		return ctx, errors.New("invalid WorkOS access token")
	}
	if claims["client_id"] != v.clientID {
		return ctx, errors.New("WorkOS token belongs to another client")
	}
	userID, _ := claims["sub"].(string)
	if userID == "" {
		return ctx, errors.New("WorkOS token has no user subject")
	}
	return WithUserID(ctx, userID), nil
}

func parseReason(err error) string {
	if err == nil {
		return "token invalid"
	}
	return err.Error()
}

func issuerMatches(actual, expected string) bool {
	if actual == "" || expected == "" {
		return false
	}
	return strings.TrimRight(actual, "/") == strings.TrimRight(expected, "/")
}

func (v *Verifier) keyfunc(ctx context.Context) (keyfunc.Keyfunc, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.keys != nil {
		return v.keys, nil
	}
	url := "https://api.workos.com/sso/jwks/" + v.clientID
	keys, err := keyfunc.NewDefaultCtx(ctx, []string{url})
	if err != nil {
		return nil, fmt.Errorf("load WorkOS signing keys: %w", err)
	}
	v.keys = keys
	return keys, nil
}

func UserID(ctx context.Context) string {
	value, _ := ctx.Value(contextKey{}).(string)
	return value
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
