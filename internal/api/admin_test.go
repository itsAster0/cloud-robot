package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kryxen/cloud-robot/internal/logbuf"
	"github.com/kryxen/cloud-robot/internal/model"
)

func adminRequest(t *testing.T, h *harness, method, path, body, session string) (int, map[string]any) {
	t.Helper()
	request, _ := http.NewRequest(method, h.server.URL+path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if session != "" {
		request.Header.Set("X-Admin-Session", session)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var payload map[string]any
	_ = json.NewDecoder(response.Body).Decode(&payload)
	return response.StatusCode, payload
}

func TestAdminLoginDisabledWithoutPassword(t *testing.T) {
	t.Setenv("ADMIN_PASSWORD", "")
	h := newHarness(t)
	if status, _ := adminRequest(t, h, "POST", "/api/admin/login", `{"username":"admin","password":""}`, ""); status != http.StatusServiceUnavailable {
		t.Fatalf("login without configured password: %d", status)
	}
}

func TestAdminSessionGuardsConsole(t *testing.T) {
	t.Setenv("ADMIN_USERNAME", "ops")
	t.Setenv("ADMIN_PASSWORD", "correct horse")
	h := newHarness(t)
	buffer := logbuf.New(100)
	h.app.SetLogs(buffer)
	buffer.Add(logbuf.Entry{Level: "ERROR", Source: "worker", MatchID: "m1", Message: "worker exited"})

	if status, _ := adminRequest(t, h, "GET", "/api/admin/overview", "", ""); status != http.StatusUnauthorized {
		t.Fatalf("overview without session: %d", status)
	}
	if status, _ := adminRequest(t, h, "GET", "/api/admin/overview", "", "9999999999.deadbeef"); status != http.StatusUnauthorized {
		t.Fatalf("forged session accepted: %d", status)
	}
	if status, _ := adminRequest(t, h, "POST", "/api/admin/login", `{"username":"ops","password":"wrong"}`, ""); status != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", status)
	}
	status, login := adminRequest(t, h, "POST", "/api/admin/login", `{"username":"ops","password":"correct horse"}`, "")
	token, _ := login["token"].(string)
	if status != http.StatusOK || token == "" {
		t.Fatalf("login: %d %v", status, login)
	}
	if status, overview := adminRequest(t, h, "GET", "/api/admin/overview", "", token); status != http.StatusOK || overview["server"] == nil {
		t.Fatalf("overview: %d %v", status, overview)
	}
	status, logs := adminRequest(t, h, "GET", "/api/admin/logs?match=m1&level=error", "", token)
	entries, _ := logs["entries"].([]any)
	if status != http.StatusOK || len(entries) != 1 {
		t.Fatalf("logs: %d %v", status, logs)
	}
	expired := h.app.admin.sign(time.Now().Add(-time.Minute).Unix())
	if status, _ := adminRequest(t, h, "GET", "/api/admin/logs", "", expired); status != http.StatusUnauthorized {
		t.Fatalf("expired session accepted: %d", status)
	}
}

func TestAdminLoginLocksAfterRepeatedFailures(t *testing.T) {
	t.Setenv("ADMIN_PASSWORD", "secret-value")
	h := newHarness(t)
	for i := 0; i < adminFailureLimit; i++ {
		adminRequest(t, h, "POST", "/api/admin/login", `{"username":"admin","password":"nope"}`, "")
	}
	if status, _ := adminRequest(t, h, "POST", "/api/admin/login", `{"username":"admin","password":"secret-value"}`, ""); status != http.StatusTooManyRequests {
		t.Fatalf("login allowed during lockout: %d", status)
	}
}

func TestLogHandlerCapturesMatchLogs(t *testing.T) {
	buffer := logbuf.New(10)
	logger := slog.New(logbuf.NewHandler(slog.DiscardHandler, buffer))
	logger.Error("match failed", "source", "worker", "matchId", "m9", "error", "boom")
	entries, _ := buffer.Find(logbuf.Query{MatchID: "m9", Source: "worker"})
	if len(entries) != 1 || entries[0].Attrs["error"] != "boom" {
		t.Fatalf("captured: %+v", entries)
	}
}

func TestPublicMatchEncodesBotOnlyRobotsAsArray(t *testing.T) {
	for _, version := range []int{0, 4} {
		raw, _ := json.Marshal(publicMatch(model.Match{MatchID: "m", EngineVersion: version}))
		if !strings.Contains(string(raw), `"robots":[]`) {
			t.Fatalf("engine %d bot-only match encoded as %s", version, raw)
		}
	}
}

func TestOpenTeamFillsPlayerTeamsBeforeCreatingNew(t *testing.T) {
	robots := []model.RobotSubmission{{Team: "red"}, {Team: "red"}, {Team: "blue"}, {Team: "bots-00", Bot: true}}
	if got := openTeam(robots, 2); got != "blue" {
		t.Fatalf("expected the open player team, got %q", got)
	}
	if got := openTeam(robots[:2], 2); got != "team-02" {
		t.Fatalf("expected a new team when all are full, got %q", got)
	}
}

func TestRegistrationRecordsBoxBindingEvenBeforeSupervisorReports(t *testing.T) {
	h := newHarness(t)
	if err := h.store.PutMatch(context.Background(), model.Match{MatchID: "m1", OwnerID: testUserID, Status: model.MatchLobby}); err != nil {
		t.Fatal(err)
	}
	h.provisioner.box = readyBox()
	h.provisioner.readMain = "-- player main.lua"
	h.provisioner.lateMarkers = true
	if response, payload := h.request(t, http.MethodPost, "/api/matches/m1/robots", `{"displayName":"Ada","team":"red","startCommand":"lua main.lua"}`); response.StatusCode != http.StatusCreated {
		t.Fatalf("register: %d %v", response.StatusCode, payload)
	}
	// The supervisor has not reported the binding yet; the API must still
	// report the match so the browser can offer "Leave match".
	response, box := h.request(t, http.MethodGet, "/api/me/box", "")
	if response.StatusCode != http.StatusOK || box["activeMatchId"] != "m1" || box["activeRobotId"] == "" {
		t.Fatalf("binding lost: %d %v", response.StatusCode, box)
	}
}
