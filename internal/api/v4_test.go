package api

import (
	"context"
	"encoding/json"
	"github.com/kryxen/cloud-robot/internal/model"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestV4CreateLimitsAndUnranked(t *testing.T) {
	for _, test := range []struct {
		body   string
		status int
	}{{`{"capacity":257}`, 400}, {`{"mode":"br-squad","capacity":63}`, 400}, {`{"capacity":256,"mode":"br-solo"}`, 201}} {
		t.Run(test.body, func(t *testing.T) {
			h := newHarness(t)
			response, body := h.request(t, http.MethodPost, "/api/v4/matches", test.body)
			if response.StatusCode != test.status {
				t.Fatalf("%d %+v", response.StatusCode, body)
			}
			if test.status == 201 && (body["engineVersion"] != float64(4) || body["practice"] != true) {
				t.Fatalf("invalid v4 match %+v", body)
			}
		})
	}
}
func TestV4PrivateViewRequiresOwnership(t *testing.T) {
	h := newHarness(t)
	h.store.matches["m"] = model.Match{MatchID: "m", EngineVersion: 4, Status: model.MatchRunning}
	h.app.v4["m"] = &v4Control{observations: map[string]json.RawMessage{"secret": json.RawMessage(`{"secret":true}`)}}
	response, _ := h.request(t, http.MethodGet, "/api/v4/matches/m/view", "")
	if response.StatusCode != 403 {
		t.Fatal("unowned observation disclosed")
	}
}
func TestV4LoadoutPersistsAndRejectsOverspend(t *testing.T) {
	h := newHarness(t)
	response, _ := h.request(t, http.MethodPut, "/api/v4/me/loadout", `{"chassis":"heavy","weapon":"railgun","modules":["optics","capacitor"],"utilities":[]}`)
	if response.StatusCode != 400 {
		t.Fatal("over-budget accepted")
	}
	response, _ = h.request(t, http.MethodPut, "/api/v4/me/loadout", `{"chassis":"scout","weapon":"plasma","modules":["optics"],"utilities":[]}`)
	if response.StatusCode != 200 {
		t.Fatal("valid build rejected")
	}
	_, body := h.request(t, http.MethodGet, "/api/v4/me/loadout", "")
	if body["chassis"] != "scout" {
		t.Fatal("build not saved")
	}
}
func TestV4WorkerPersistsCompleteMatch(t *testing.T) {
	path, err := filepath.Abs("../../crates/arena-engine/target/debug/arena-engine")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); err != nil {
		t.Skip("build Rust worker before integration tests")
	}
	t.Setenv("ARENA_ENGINE_PATH", path)
	h := newHarness(t)
	_, body := h.request(t, http.MethodPost, "/api/v4/matches", `{"mode":"sandbox","capacity":2,"width":2400,"height":1500,"durationSeconds":10,"seed":42}`)
	id := body["matchId"].(string)
	m := h.store.matches[id]
	m.Status = model.MatchQueued
	h.store.matches[id] = m
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err = h.app.runMatch(ctx, id); err != nil {
		t.Fatal(err)
	}
	m = h.store.matches[id]
	if m.Status != model.MatchFinished || len(m.RobotSummaries) != 2 {
		t.Fatalf("incomplete result: %+v", m)
	}
	if _, ok := h.store.scripts["replays/"+id+"/v4/final.json"]; !ok {
		t.Fatal("missing replay state")
	}
	if _, ok := h.store.scripts["replays/"+id+"/v4/inputs-000000.json"]; !ok {
		t.Fatal("missing accepted inputs")
	}
	// Associate the generated robot with the test owner only after simulation,
	// so this exercises trace authorization without adding a fake live agent.
	m.Robots = []model.RobotSubmission{{RobotID: "bot-000", PlayerID: testUserID}}
	h.store.matches[id] = m
	for _, tick := range []string{"0", "37", "137", "200"} {
		h.app.rateMu.Lock()
		delete(h.app.rates, "trace:"+testUserID)
		h.app.rateMu.Unlock()
		response, trace := h.request(t, http.MethodGet, "/api/v4/matches/"+id+"/trace?tick="+tick, "")
		if response.StatusCode != 200 {
			t.Fatalf("trace tick %s: %d %+v", tick, response.StatusCode, trace)
		}
	}
	response, frames := h.request(t, http.MethodGet, "/api/v4/matches/"+id+"/replay?page=0", "")
	if response.StatusCode != 200 || len(frames["frames"].([]any)) != 10 {
		t.Fatalf("invalid replay frames %+v", frames)
	}

}

func TestV4PublicMetadataRedactsPrivateBuild(t *testing.T) {
	h := newHarness(t)
	original := model.Match{MatchID: "private-build", EngineVersion: 4, Status: model.MatchLobby, Robots: []model.RobotSubmission{{RobotID: "r", Loadout: json.RawMessage(`{"weapon":"railgun"}`), ScriptObjectKey: "private-script", OwnerBoxID: "private-box", StartCommand: "private-command"}}}
	h.store.matches[original.MatchID] = original
	for _, path := range []string{"/api/matches/private-build", "/api/matches"} {
		response, body := h.request(t, http.MethodGet, path, "")
		if response.StatusCode != 200 {
			t.Fatal(response.StatusCode)
		}
		raw, _ := json.Marshal(body)
		for _, secret := range []string{"railgun", "private-script", "private-box", "private-command"} {
			if strings.Contains(string(raw), secret) {
				t.Fatalf("%s leaked %s", path, secret)
			}
		}
	}
	if len(h.store.matches[original.MatchID].Robots[0].Loadout) == 0 {
		t.Fatal("redaction mutated persisted roster")
	}
}
func TestV4ActionBoundaryRejections(t *testing.T) {
	valid := v4Input{Type: "action", Version: 4, SDKVersion: "0.4.0", Sequence: 1}
	if validateV4Input(valid) != "" {
		t.Fatal("valid action rejected")
	}
	invalid := valid
	invalid.Action.Throttle = math.NaN()
	if validateV4Input(invalid) != "OUT_OF_RANGE" {
		t.Fatal("nonfinite accepted")
	}
	invalid = valid
	invalid.Version = 3
	if validateV4Input(invalid) != "UNSUPPORTED_VERSION" {
		t.Fatal("old protocol accepted")
	}
	invalid = valid
	slot := uint(4)
	invalid.Action.Consume = &slot
	if validateV4Input(invalid) != "OUT_OF_RANGE" {
		t.Fatal("invalid slot accepted")
	}
}

func TestV4GeometryAcknowledgement(t *testing.T) {
	raw := json.RawMessage(`{"revision":3,"obstacles":[{"id":"wall"}],"transit":[],"sites":[],"hazards":[],"self":{"robotId":"r"}}`)
	if string(observationForDelivery(raw, 0)) != string(raw) {
		t.Fatal("initial geometry missing")
	}
	if string(observationForDelivery(raw, 2)) != string(raw) {
		t.Fatal("new revision omitted before acknowledgement")
	}
	reduced := observationForDelivery(raw, 3)
	if strings.Contains(string(reduced), "obstacles") || !strings.Contains(string(reduced), "self") {
		t.Fatal("incorrect geometry projection")
	}
}
