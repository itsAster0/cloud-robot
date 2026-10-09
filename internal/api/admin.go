package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kryxen/cloud-robot/internal/boxes"
	"github.com/kryxen/cloud-robot/internal/logbuf"
	"github.com/kryxen/cloud-robot/internal/model"
)

// adminAuth guards the operations console with a username and password from
// the environment, independent of WorkOS player sign-in. Sessions are
// stateless HMAC tokens that expire after adminSessionTTL.
type adminAuth struct {
	username string
	password string
	secret   []byte
	mu       sync.Mutex
	failures []time.Time
}

const (
	adminSessionTTL   = 8 * time.Hour
	adminFailureLimit = 5
	adminFailureSpan  = time.Minute
)

func newAdminAuth() *adminAuth {
	secret := []byte(os.Getenv("ADMIN_SESSION_SECRET"))
	if len(secret) < 16 {
		// Without a configured secret, sessions last until the API restarts.
		secret = make([]byte, 32)
		_, _ = rand.Read(secret)
	}
	return &adminAuth{username: envOr("ADMIN_USERNAME", "admin"), password: os.Getenv("ADMIN_PASSWORD"), secret: secret}
}

// equal compares fixed-length digests so timing does not reveal lengths.
func equal(a, b string) bool {
	x, y := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(x[:], y[:]) == 1
}

func (a *adminAuth) sign(expires int64) string {
	mac := hmac.New(sha256.New, a.secret)
	fmt.Fprintf(mac, "admin|%d", expires)
	return strconv.FormatInt(expires, 10) + "." + hex.EncodeToString(mac.Sum(nil))
}

func (a *adminAuth) valid(token string) bool {
	expiresText, _, ok := strings.Cut(token, ".")
	expires, err := strconv.ParseInt(expiresText, 10, 64)
	if !ok || err != nil || time.Now().Unix() > expires {
		return false
	}
	return hmac.Equal([]byte(a.sign(expires)), []byte(token))
}

// allow reports whether another login attempt is permitted: after
// adminFailureLimit failures within adminFailureSpan, logins pause.
func (a *adminAuth) allow(now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	recent := a.failures[:0]
	for _, t := range a.failures {
		if now.Sub(t) < adminFailureSpan {
			recent = append(recent, t)
		}
	}
	a.failures = recent
	return len(recent) < adminFailureLimit
}

func (a *adminAuth) fail(now time.Time) {
	a.mu.Lock()
	a.failures = append(a.failures, now)
	a.mu.Unlock()
}

func clientAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) adminLogin(w http.ResponseWriter, r *http.Request) {
	if s.admin.password == "" {
		writeError(w, http.StatusServiceUnavailable, "admin login is disabled: set ADMIN_PASSWORD")
		return
	}
	now := time.Now()
	if !s.admin.allow(now) {
		writeError(w, http.StatusTooManyRequests, "too many failed logins; wait a minute")
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid login request")
		return
	}
	// Evaluate both comparisons so a wrong username costs the same time.
	userOK, passOK := equal(input.Username, s.admin.username), equal(input.Password, s.admin.password)
	if !userOK || !passOK {
		s.admin.fail(now)
		slog.Warn("admin login failed", "source", "admin", "username", input.Username, "client", clientAddress(r))
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	expires := now.Add(adminSessionTTL)
	slog.Info("admin login", "source", "admin", "username", input.Username, "client", clientAddress(r))
	writeJSON(w, http.StatusOK, map[string]any{"token": s.admin.sign(expires.Unix()), "expiresAt": expires.UTC()})
}

func (s *Server) requireAdminSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.admin.valid(r.Header.Get("X-Admin-Session")) {
			writeError(w, http.StatusUnauthorized, "admin session required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) adminOverview(w http.ResponseWriter, r *http.Request) {
	matches, err := s.store.ListMatches(r.Context(), "", 200)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	counts := map[model.MatchStatus]int{}
	for _, m := range matches {
		counts[m.Status]++
	}
	s.queue.mu.Lock()
	queued := 0
	for _, users := range s.queue.waiting {
		queued += len(users)
	}
	s.queue.mu.Unlock()
	type worker struct {
		MatchID string `json:"matchId"`
		Tick    uint32 `json:"tick"`
		Paused  bool   `json:"paused"`
		Agents  int    `json:"agents"`
	}
	workers := []worker{}
	s.mu.Lock()
	for id, control := range s.v4 {
		control.mu.Lock()
		workers = append(workers, worker{MatchID: id, Tick: control.tick, Paused: control.paused, Agents: len(control.observations)})
		control.mu.Unlock()
	}
	s.mu.Unlock()
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	errors15, warnings15 := 0, 0
	if s.logs != nil {
		recent, _ := s.logs.Find(logbuf.Query{Level: "WARN", Limit: 2000})
		for _, e := range recent {
			if time.Since(e.Time) <= 15*time.Minute {
				if e.Level == "ERROR" {
					errors15++
				} else {
					warnings15++
				}
			}
		}
	}
	host, _ := os.Hostname()
	writeJSON(w, http.StatusOK, map[string]any{
		"server": map[string]any{
			"host": host, "goVersion": runtime.Version(), "uptimeSeconds": int(time.Since(s.startedAt).Seconds()),
			"goroutines": runtime.NumGoroutine(), "heapMB": memory.HeapAlloc / (1 << 20), "cpus": runtime.NumCPU(),
		},
		"cloud":   s.store.Status(),
		"matches": counts,
		"workers": workers,
		"agents":  s.agents.Count(),
		"viewers": s.hub.ViewerCounts(),
		"queue":   queued,
		"logs":    map[string]int{"errors15m": errors15, "warnings15m": warnings15},
		// Warn the console while the Compose dummy password is still set.
		"defaultAdminPassword": s.admin.password == "local-admin-change-me",
		"settings":             map[string]string{"authRequired": os.Getenv("AUTH_REQUIRED"), "maintenanceMode": envOr("MAINTENANCE_MODE", "false"), "queueBotFillSeconds": envOr("QUEUE_BOT_FILL_SECONDS", "45"), "engine": envOr("ARENA_ENGINE_PATH", "crates/arena-engine/target/release/arena-engine")},
	})
}

func (s *Server) adminLogs(w http.ResponseWriter, r *http.Request) {
	if s.logs == nil {
		writeJSON(w, http.StatusOK, map[string]any{"entries": []logbuf.Entry{}, "latest": 0})
		return
	}
	q := r.URL.Query()
	after, _ := strconv.ParseUint(q.Get("after"), 10, 64)
	limit, _ := strconv.Atoi(q.Get("limit"))
	entries, latest := s.logs.Find(logbuf.Query{After: after, Source: q.Get("source"), MatchID: q.Get("match"), Level: q.Get("level"), Search: q.Get("q"), Limit: limit})
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries, "latest": latest})
}

func (s *Server) adminMatches(w http.ResponseWriter, r *http.Request) {
	matches, err := s.store.ListMatches(r.Context(), model.MatchStatus(r.URL.Query().Get("status")), 100)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	// Admins see full records; only normalize bot-only matches to an array.
	for i := range matches {
		if matches[i].Robots == nil {
			matches[i].Robots = []model.RobotSubmission{}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"matches": matches})
}

// boxAdmin is implemented by the provisioner client; test fakes may omit it.
type boxAdmin interface {
	List(ctx context.Context) ([]boxes.BoxSummary, error)
	Logs(ctx context.Context, boxID string, tail int) (string, error)
}

func (s *Server) adminBoxes(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.boxes.(boxAdmin)
	if !ok {
		writeError(w, http.StatusNotImplemented, "provisioner does not support box listing")
		return
	}
	list, err := admin.List(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"boxes": list})
}

func (s *Server) adminBoxLogs(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.boxes.(boxAdmin)
	if !ok {
		writeError(w, http.StatusNotImplemented, "provisioner does not support box logs")
		return
	}
	boxID := r.PathValue("boxID")
	if !strings.HasPrefix(boxID, "robot-box-") || len(boxID) != len("robot-box-")+16 {
		writeError(w, http.StatusBadRequest, "invalid box id")
		return
	}
	tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
	logs, err := admin.Logs(r.Context(), boxID, tail)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"logs": logs})
}
