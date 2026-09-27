package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/kryxen/cloud-robot/internal/enginev4"
	"github.com/kryxen/cloud-robot/internal/model"
)

func runningArena(t *testing.T, h *harness) *v4Control {
	t.Helper()
	c := arenaConfig()
	c.MatchID = "arena"
	h.store.matches["arena"] = model.Match{MatchID: "arena", OwnerID: arenaOwner, Mode: "arena", EngineVersion: 4, Status: model.MatchRunning, ArenaConfig: mustJSON(c),
		Robots: []model.RobotSubmission{{RobotID: "bot-000", Bot: true, Team: "bot-000"}}}
	control := &v4Control{edits: make(chan editRequest, 1)}
	h.app.v4["arena"] = control
	return control
}

func TestArenaConfigValidates(t *testing.T) {
	c := arenaConfig()
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
