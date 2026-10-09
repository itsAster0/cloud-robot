package auth

import (
	"context"
	"testing"
	"time"
)

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(hash, "correct horse") {
		t.Fatal("matching password rejected")
	}
	if CheckPassword(hash, "wrong horse") {
		t.Fatal("wrong password accepted")
	}
	if CheckPassword("plain", "plain") {
		t.Fatal("malformed hash accepted")
	}
}

func TestNormalizeUsername(t *testing.T) {
	if got, err := NormalizeUsername("  Alice_1 "); err != nil || got != "alice_1" {
		t.Fatalf("got %q, %v", got, err)
	}
	for _, bad := range []string{"ab", "-dash", "has space", "émile", "averyveryverylongusername1"} {
		if _, err := NormalizeUsername(bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
}

func TestLocalSessionVerifies(t *testing.T) {
	t.Setenv("LOCAL_AUTH_ENABLED", "true")
	t.Setenv("LOCAL_AUTH_SECRET", "test-secret")
	t.Setenv("WORKOS_CLIENT_ID", "")
	v := NewVerifier()
	token, _, err := v.Local().Issue("alice", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := v.Verify(context.Background(), "Bearer "+token)
	if err != nil {
		t.Fatal(err)
	}
	if UserID(ctx) != "local:alice" {
		t.Fatalf("user %q", UserID(ctx))
	}
}

func TestLocalSessionRejectsForgedAndExpired(t *testing.T) {
	t.Setenv("LOCAL_AUTH_ENABLED", "true")
	t.Setenv("LOCAL_AUTH_SECRET", "secret-one")
	issuer := NewLocalAuth()
	forged, _, _ := issuer.Issue("mallory", time.Now())
	expired, _, _ := issuer.Issue("mallory", time.Now().Add(-31*24*time.Hour))

	t.Setenv("LOCAL_AUTH_SECRET", "secret-two")
	v := NewVerifier()
	if _, err := v.Verify(context.Background(), "Bearer "+forged); err == nil {
		t.Fatal("token signed with another secret accepted")
	}
	t.Setenv("LOCAL_AUTH_SECRET", "secret-one")
	v = NewVerifier()
	if _, err := v.Verify(context.Background(), "Bearer "+expired); err == nil {
		t.Fatal("expired token accepted")
	}
}

func TestLocalSessionsOffByDefault(t *testing.T) {
	t.Setenv("LOCAL_AUTH_ENABLED", "")
	t.Setenv("LOCAL_AUTH_SECRET", "s")
	if NewLocalAuth().Enabled() {
		t.Fatal("local accounts enabled without LOCAL_AUTH_ENABLED")
	}
	t.Setenv("LOCAL_AUTH_ENABLED", "true")
	token, _, _ := NewLocalAuth().Issue("alice", time.Now())
	t.Setenv("LOCAL_AUTH_ENABLED", "false")
	if _, ok := NewLocalAuth().parse(token); ok {
		t.Fatal("disabled local auth accepted a token")
	}
}
