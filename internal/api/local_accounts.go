package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	robotauth "github.com/kryxen/cloud-robot/internal/auth"
)

// Local account records live in the artifact bucket next to scripts and
// replays, so Floci (or S3) persists them with no new table or service.
const localAccountPrefix = "accounts/"

const (
	localFailureSpan  = time.Minute
	localFailureLimit = 10
)

type localAccount struct {
	Username     string    `json:"username"`
	PasswordHash string    `json:"passwordHash"`
	CreatedAt    time.Time `json:"createdAt"`
}

type localAccounts struct {
	// register serializes the read-then-write that claims a username.
	register sync.Mutex
	mu       sync.Mutex
	failures map[string][]time.Time
}

func (l *localAccounts) allow(client string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	recent := l.failures[client][:0]
	for _, t := range l.failures[client] {
		if now.Sub(t) < localFailureSpan {
			recent = append(recent, t)
		}
	}
	l.failures[client] = recent
	return len(recent) < localFailureLimit
}

func (l *localAccounts) fail(client string, now time.Time) {
	l.mu.Lock()
	l.failures[client] = append(l.failures[client], now)
	l.mu.Unlock()
}

func (s *Server) authProviders(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{
		"local":  s.local.Enabled(),
		"workos": os.Getenv("WORKOS_CLIENT_ID") != "",
	})
}

type localCredentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) readLocalCredentials(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	if !s.local.Enabled() {
		writeError(w, http.StatusNotFound, "local accounts are disabled")
		return "", "", false
	}
	var input localCredentials
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid sign-in request")
		return "", "", false
	}
	username, err := robotauth.NormalizeUsername(input.Username)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return "", "", false
	}
	return username, input.Password, true
}

func (s *Server) loadLocalAccount(r *http.Request, username string) (localAccount, bool, error) {
	raw, err := s.store.GetReplayObject(r.Context(), localAccountPrefix+username+".json")
	if err != nil {
		if isMissingObject(err) {
			return localAccount{}, false, nil
		}
		return localAccount{}, false, err
	}
	var account localAccount
	if err := json.Unmarshal([]byte(raw), &account); err != nil {
		return localAccount{}, false, err
	}
	return account, true, nil
}

// isMissingObject recognizes S3 "no such key" errors from AWS and Floci
// without coupling this package to the SDK's error types.
func isMissingObject(err error) bool {
	text := err.Error()
	return strings.Contains(text, "NoSuchKey") || strings.Contains(text, "StatusCode: 404") || strings.Contains(text, "not found")
}

func (s *Server) localRegister(w http.ResponseWriter, r *http.Request) {
	username, password, ok := s.readLocalCredentials(w, r)
	if !ok {
		return
	}
	if err := robotauth.ValidatePassword(password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.accounts.register.Lock()
	defer s.accounts.register.Unlock()
	_, exists, err := s.loadLocalAccount(r, username)
	if err != nil {
		slog.Error("read local account", "error", err)
		writeError(w, http.StatusServiceUnavailable, "account storage is unavailable")
		return
	}
	if exists {
		writeError(w, http.StatusConflict, "that username is taken")
		return
	}
	hash, err := robotauth.HashPassword(password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create account")
		return
	}
	record, _ := json.Marshal(localAccount{Username: username, PasswordHash: hash, CreatedAt: time.Now().UTC()})
	if err := s.store.PutReplayObject(r.Context(), localAccountPrefix+username+".json", string(record)); err != nil {
		slog.Error("store local account", "error", err)
		writeError(w, http.StatusServiceUnavailable, "account storage is unavailable")
		return
	}
	slog.Info("local account created", "source", "auth", "username", username)
	s.writeLocalSession(w, http.StatusCreated, username)
}

func (s *Server) localLogin(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	client := clientAddress(r)
	if !s.accounts.allow(client, now) {
		writeError(w, http.StatusTooManyRequests, "too many failed sign-ins; wait a minute")
		return
	}
	username, password, ok := s.readLocalCredentials(w, r)
	if !ok {
		return
	}
	account, exists, err := s.loadLocalAccount(r, username)
	if err != nil {
		slog.Error("read local account", "error", err)
		writeError(w, http.StatusServiceUnavailable, "account storage is unavailable")
		return
	}
	if !exists || !robotauth.CheckPassword(account.PasswordHash, password) {
		s.accounts.fail(client, now)
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	s.writeLocalSession(w, http.StatusOK, username)
}

func (s *Server) writeLocalSession(w http.ResponseWriter, status int, username string) {
	token, expires, err := s.local.Issue(username, time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not start session")
		return
	}
	writeJSON(w, status, map[string]any{
		"token": token, "expiresAt": expires.UTC(),
		"user": map[string]string{"id": robotauth.LocalUserID(username), "username": username},
	})
}
