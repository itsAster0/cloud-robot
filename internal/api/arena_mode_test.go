package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/kryxen/cloud-robot/internal/cloud"

	"github.com/kryxen/cloud-robot/internal/enginev4"
	"github.com/kryxen/cloud-robot/internal/model"
)

func runningArena(t *testing.T, h *harness) *v4Control {
	t.Helper()
	c := arenaConfig(0)
	c.MatchID = "arena"
	h.store.matches["arena"] = model.Match{MatchID: "arena", OwnerID: arenaOwner, Mode: "arena", EngineVersion: 4, Status: model.MatchRunning, ArenaConfig: mustJSON(c),
		Robots: []model.RobotSubmission{{RobotID: "bot-000", Bot: true, Team: "bot-000"}}}
	control := &v4Control{edits: make(chan editRequest, 1)}
	h.app.v4["arena"] = control
	return control
}

func TestArenaGrowsWithLastSessionPeak(t *testing.T) {
	small, busy := arenaConfig(0), arenaConfig(100)
	if small.Capacity != 128 || busy.Capacity != 208 || busy.Width <= small.Width {
		t.Fatalf("want 128 then 208 slots on a wider map, got %d (%.0f) and %d (%.0f)", small.Capacity, small.Width, busy.Capacity, busy.Width)
	}
	if arenaConfig(500).Capacity != 256 {
		t.Fatal("arena capacity is capped at 256")
	}
	if err := busy.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestArenaConfigValidates(t *testing.T) {
	c := arenaConfig(0)
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.DurationSeconds = 21601
	if c.Validate() == nil {
		t.Fatal("arena sessions are capped at six hours")
	}
	c.Mode, c.DurationSeconds = "br-solo", 3600
	if c.Validate() == nil {
		t.Fatal("other modes keep the short duration cap")
	}
}

func TestArenaJoinWhileRunningQueuesEngineJoin(t *testing.T) {
	h := newHarness(t)
	control := runningArena(t, h)
	h.provisioner.box = readyBox()
	h.provisioner.readMain = "-- bot"
	body := `{"displayName":"Ada","startCommand":"lua main.lua","sdkVersion":"0.4.0","loadout":{"chassis":"generalist","weapon":"plasma"}}`
	response, payload := h.request(t, http.MethodPost, "/api/matches/arena/robots", body)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("arena join failed: %d %v", response.StatusCode, payload)
	}
	if len(control.joins) != 1 || control.joins[0].Registration.Name != "Ada" {
		t.Fatalf("join not queued for the worker: %+v", control.joins)
	}
	if n := humanRobots(h.store.matches["arena"].Robots); n != 1 {
		t.Fatalf("stored roster has %d humans", n)
	}
}

func TestArenaLeaveFreesBoxImmediately(t *testing.T) {
	h := newHarness(t)
	control := runningArena(t, h)
	h.provisioner.box = readyBox()
	h.provisioner.readMain = "-- bot"
	body := `{"displayName":"Ada","startCommand":"lua main.lua","sdkVersion":"0.4.0","loadout":{"chassis":"generalist","weapon":"plasma"}}`
	if response, payload := h.request(t, http.MethodPost, "/api/matches/arena/robots", body); response.StatusCode != http.StatusCreated {
		t.Fatalf("arena join failed: %d %v", response.StatusCode, payload)
	}
	robotID := control.joins[0].Registration.RobotID
	response, payload := h.request(t, http.MethodPost, "/api/me/box/release", "")
	if response.StatusCode != http.StatusOK || payload["status"] != "released" {
		t.Fatalf("leave failed: %d %v", response.StatusCode, payload)
	}
	if len(control.withdrawals) != 1 || control.withdrawals[0] != robotID {
		t.Fatalf("robot not withdrawn from the engine: %v", control.withdrawals)
	}
	if boxOwnsRobot(h.store.matches["arena"], testBoxID) {
		t.Fatal("box still bound to the arena")
	}
}

func TestEnsureArenaQueuesOneSession(t *testing.T) {
	h := newHarness(t)
	h.app.ensureArena(context.Background())
	h.app.ensureArena(context.Background())
	count := 0
	for _, m := range h.store.matches {
		if m.Mode == "arena" && m.OwnerID == arenaOwner {
			count++
			if m.Status != model.MatchQueued {
				t.Fatalf("arena not queued: %s", m.Status)
			}
			if _, err := enginev4.DecodeConfig(m.ArenaConfig); err != nil {
				t.Fatal(err)
			}
		}
	}
	if count != 1 {
		t.Fatalf("want exactly one arena session, got %d", count)
	}
}

func TestAgentForFinishedMatchIsRetired(t *testing.T) {
	h := newHarness(t)
	token := "retire-me"
	hash := sha256.Sum256([]byte(token))
	h.store.credentials["gone"] = cloud.AgentCredential{RobotID: "gone", MatchID: "old", TokenHash: base64.RawURLEncoding.EncodeToString(hash[:])}
	h.store.matches["old"] = model.Match{MatchID: "old", EngineVersion: 4, Status: model.MatchFinished}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(h.server.URL, "http")+"/agent/connect/gone", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + token}, "X-Robot-SDK-Features": {"retire"}}, Subprotocols: []string{"robot-arena.v4"}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	var message map[string]string
	if err := wsjson.Read(ctx, c, &message); err != nil || message["type"] != "retired" {
		t.Fatalf("want retired message, got %v %v", message, err)
	}
}

func TestOldAgentForFinishedMatchGetsAnError(t *testing.T) {
	h := newHarness(t)
	token := "old-sdk"
	hash := sha256.Sum256([]byte(token))
	h.store.credentials["old"] = cloud.AgentCredential{RobotID: "old", MatchID: "done", TokenHash: base64.RawURLEncoding.EncodeToString(hash[:])}
	h.store.matches["done"] = model.Match{MatchID: "done", EngineVersion: 4, Status: model.MatchFinished}
	request, _ := http.NewRequest(http.MethodGet, h.server.URL+"/agent/connect/old", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("SDKs without retirement back off on an error; got %d", response.StatusCode)
	}
}
