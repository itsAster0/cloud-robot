package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func localAuthServer(t *testing.T) http.Handler {
	t.Helper()
	t.Setenv("LOCAL_AUTH_ENABLED", "true")
	t.Setenv("LOCAL_AUTH_SECRET", "test-secret")
	t.Setenv("AUTH_REQUIRED", "true")
	return NewServer(newFakeStore(), &fakeProvisioner{}).Handler()
}

func postJSON(h http.Handler, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestLocalAccountRegisterLoginAndUse(t *testing.T) {
	h := localAuthServer(t)
	rec := postJSON(h, "/api/auth/local/register", `{"username":"Alice","password":"hunter22!"}`, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", rec.Code, rec.Body)
	}
	if postJSON(h, "/api/auth/local/register", `{"username":"alice","password":"another-pass"}`, "").Code != http.StatusConflict {
		t.Fatal("duplicate username accepted")
	}
	if postJSON(h, "/api/auth/local/login", `{"username":"alice","password":"wrong-pass"}`, "").Code != http.StatusUnauthorized {
		t.Fatal("wrong password accepted")
	}
	rec = postJSON(h, "/api/auth/local/login", `{"username":"alice","password":"hunter22!"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}
	var session struct {
		Token string            `json:"token"`
		User  map[string]string `json:"user"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&session); err != nil || session.Token == "" || session.User["id"] != "local:alice" {
		t.Fatalf("session %+v, %v", session, err)
	}
	// The session token works on a sign-in-only route.
	req := httptest.NewRequest(http.MethodGet, "/api/v4/me/player", nil)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	got := httptest.NewRecorder()
	h.ServeHTTP(got, req)
	if got.Code != http.StatusOK {
		t.Fatalf("authenticated route: %d %s", got.Code, got.Body)
	}
}

func TestLocalAccountValidation(t *testing.T) {
	h := localAuthServer(t)
	if c := postJSON(h, "/api/auth/local/register", `{"username":"ab","password":"long-enough"}`, "").Code; c != http.StatusBadRequest {
		t.Fatalf("short username: %d", c)
	}
	if c := postJSON(h, "/api/auth/local/register", `{"username":"bob","password":"short"}`, "").Code; c != http.StatusBadRequest {
		t.Fatalf("short password: %d", c)
	}
}

func TestLocalAccountsDisabled(t *testing.T) {
	t.Setenv("LOCAL_AUTH_ENABLED", "false")
	h := NewServer(newFakeStore(), &fakeProvisioner{}).Handler()
	if c := postJSON(h, "/api/auth/local/register", `{"username":"alice","password":"hunter22!"}`, "").Code; c != http.StatusNotFound {
		t.Fatalf("disabled register: %d", c)
	}
}
