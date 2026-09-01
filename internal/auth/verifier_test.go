package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/MicahParks/jwkset"
	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
)

const testClientID = "client_test"

// newTestVerifier signs and validates with one generated key so every path
// (issuer, client, expiry, subject) is exercised without network access.
func newTestVerifier(t *testing.T, required bool) (*Verifier, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	storage := jwkset.NewMemoryStorage()
	jwk, err := jwkset.NewJWKFromKey(key.Public(), jwkset.JWKOptions{Metadata: jwkset.JWKMetadataOptions{KID: "test-kid"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.KeyWrite(context.Background(), jwk); err != nil {
		t.Fatal(err)
	}
	keys, err := keyfunc.New(keyfunc.Options{Storage: storage})
	if err != nil {
		t.Fatal(err)
	}
	return &Verifier{clientID: testClientID, issuer: defaultIssuerBase + testClientID, required: required, keys: keys}, key
}

func signedToken(t *testing.T, key *rsa.PrivateKey, issuer, clientID, sub string, expires time.Time) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss":       issuer,
		"sub":       sub,
		"client_id": clientID,
		"exp":       expires.Unix(),
	})
	token.Header["kid"] = "test-kid"
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func TestVerifyAcceptsWorkOSBrowserIssuer(t *testing.T) {
	verifier, key := newTestVerifier(t, true)
	token := signedToken(t, key, defaultIssuerBase+testClientID, testClientID, "user_123", time.Now().Add(time.Hour))
	ctx, err := verifier.Verify(context.Background(), "Bearer "+token)
	if err != nil {
		t.Fatalf("browser-flow issuer rejected: %v", err)
	}
	if UserID(ctx) != "user_123" {
		t.Fatalf("unexpected subject %q", UserID(ctx))
	}
}

func TestVerifyAcceptsIssuerWithoutTrailingSlash(t *testing.T) {
	verifier, key := newTestVerifier(t, true)
	token := signedToken(t, key, "https://api.workos.com/user_management/"+testClientID, testClientID, "user_123", time.Now().Add(time.Hour))
	if _, err := verifier.Verify(context.Background(), "Bearer "+token); err != nil {
		t.Fatalf("issuer without trailing slash rejected: %v", err)
	}
}

func TestVerifyRejectsForeignIssuer(t *testing.T) {
	verifier, key := newTestVerifier(t, true)
	token := signedToken(t, key, "https://attacker.example/user_management/"+testClientID, testClientID, "user_123", time.Now().Add(time.Hour))
	if _, err := verifier.Verify(context.Background(), "Bearer "+token); err == nil {
		t.Fatal("foreign issuer accepted")
	}
}

func TestVerifyRejectsOtherClient(t *testing.T) {
	verifier, key := newTestVerifier(t, true)
	token := signedToken(t, key, defaultIssuerBase+testClientID, "client_other", "user_123", time.Now().Add(time.Hour))
	_, err := verifier.Verify(context.Background(), "Bearer "+token)
	if err == nil || err.Error() != "WorkOS token belongs to another client" {
		t.Fatalf("expected client mismatch, got %v", err)
	}
}

func TestVerifyRejectsExpired(t *testing.T) {
	verifier, key := newTestVerifier(t, true)
	token := signedToken(t, key, defaultIssuerBase+testClientID, testClientID, "user_123", time.Now().Add(-time.Hour))
	if _, err := verifier.Verify(context.Background(), "Bearer "+token); err == nil {
		t.Fatal("expired token accepted")
	}
}

func TestVerifyRejectsMissingSubject(t *testing.T) {
	verifier, key := newTestVerifier(t, true)
	token := signedToken(t, key, defaultIssuerBase+testClientID, testClientID, "", time.Now().Add(time.Hour))
	_, err := verifier.Verify(context.Background(), "Bearer "+token)
	if err == nil || err.Error() != "WorkOS token has no user subject" {
		t.Fatalf("expected missing subject, got %v", err)
	}
}

func TestVerifyGuestMode(t *testing.T) {
	verifier, _ := newTestVerifier(t, false)
	ctx, err := verifier.Verify(context.Background(), "")
	if err != nil || UserID(ctx) != "guest" {
		t.Fatalf("guest mode failed: %v", err)
	}
	verifier.required = true
	if _, err := verifier.Verify(context.Background(), ""); err == nil {
		t.Fatal("anonymous accepted while auth required")
	}
}

func TestIssuerMatches(t *testing.T) {
	cases := []struct {
		actual, expected string
		want             bool
	}{
		{"https://api.workos.com/user_management/c1", "https://api.workos.com/user_management/c1/", true},
		{"https://api.workos.com/user_management/c1/", "https://api.workos.com/user_management/c1", true},
		{"https://api.workos.com/user_management/c2", "https://api.workos.com/user_management/c1", false},
		{"", "https://api.workos.com", false},
		{"https://api.workos.com", "", false},
	}
	for _, tc := range cases {
		if got := issuerMatches(tc.actual, tc.expected); got != tc.want {
			t.Fatalf("issuerMatches(%q, %q) = %v, want %v", tc.actual, tc.expected, got, tc.want)
		}
	}
}
