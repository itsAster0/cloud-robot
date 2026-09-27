package api

import (
	"context"
	"encoding/json"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/kryxen/cloud-robot/internal/model"
	"math"
	"strings"
	"testing"
	"time"
)

func TestV4ViewerRegionAndReconnectBaseline(t *testing.T) {
	raw := []byte(`{"type":"snapshot","tick":600,"revision":1,"robots":[{"robotId":"near","x":100,"y":100},{"robotId":"far","x":20000,"y":20000}],"obstacles":[{"x":1}],"projectiles":[],"items":[]}`)
	v := &v4Viewer{region: viewerRegion{X: 100, Y: 100, Width: 1000, Height: 1000}}
	var s map[string]json.RawMessage
	_ = json.Unmarshal(v.project(raw), &s)
	var robots []json.RawMessage
	_ = json.Unmarshal(s["robots"], &robots)
	if len(robots) != 1 || len(s["overview"]) == 0 || len(s["obstacles"]) == 0 {
		t.Fatal("missing regional baseline or overview")
	}
	s = nil
	_ = json.Unmarshal(v.project(raw), &s)
	if s["overview"] != nil || s["obstacles"] != nil {
		t.Fatal("repeated layout/overview")
	}
	v.region = viewerRegion{X: 20000, Y: 20000, Width: 1000, Height: 1000}
	s = nil
	_ = json.Unmarshal(v.project(raw), &s)
	_ = json.Unmarshal(s["robots"], &robots)
	if len(robots) != 1 || string(robots[0]) != `{"robotId":"far","x":20000,"y":20000}` {
		t.Fatal("camera move retained stale entities")
	}
	fresh := &v4Viewer{}
	s = nil
	_ = json.Unmarshal(fresh.project(raw), &s)
	if s["obstacles"] == nil || s["overview"] == nil {
		t.Fatal("reconnect lacks baseline")
	}
	raw = []byte(`{"type":"snapshot","tick":620,"revision":2,"robots":[],"obstacles":[]}`)
	s = nil
	_ = json.Unmarshal(v.project(raw), &s)
	if s["overview"] == nil || s["obstacles"] == nil {
		t.Fatal("revision or overview cadence lost")
	}
}
func TestV4CameraBounds(t *testing.T) {
	for _, r := range []viewerRegion{{Width: -1, Height: 100}, {X: 48001, Width: 100, Height: 100}, {Width: 48001, Height: 100}} {
		if r.valid() {
			t.Fatal("invalid camera accepted")
		}
	}
}

func TestV4RegionalWebSocketBaseline(t *testing.T) {
	h := newHarness(t)
	h.store.matches["regional"] = model.Match{MatchID: "regional", EngineVersion: 4, Status: model.MatchRunning}
	// Only the authorized delayed snapshot enters the hub. Live worker state is
	// deliberately different and must never become a spectator baseline.
	h.app.v4["regional"] = &v4Control{observations: map[string]json.RawMessage{"secret": json.RawMessage(`{"tick":900}`)}}
	h.app.hub.Publish("regional", json.RawMessage(`{"type":"snapshot","tick":300,"revision":1,"robots":[],"obstacles":[]}`))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(h.server.URL, "http")+"/ws/matches/regional", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	var event map[string]json.RawMessage
	if err = wsjson.Read(ctx, c, &event); err != nil {
		t.Fatal(err)
	}
	if err = wsjson.Read(ctx, c, &event); err != nil {
		t.Fatal(err)
	}
	if string(event["tick"]) != "300" || event["overview"] == nil || event["obstacles"] == nil {
		t.Fatalf("invalid delayed baseline: %s", event)
	}
	if err = wsjson.Write(ctx, c, viewerRegion{X: 1, Y: 1, Width: -1, Height: 100}); err != nil {
		t.Fatal(err)
	}
	if err = wsjson.Read(ctx, c, &event); websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Fatalf("invalid camera not rejected: %v", err)
	}
}

func TestV4RegionalPayloadMeasurement(t *testing.T) {
	robots := make([]map[string]any, 256)
	items := make([]map[string]any, 1024)
	for i := range robots {
		robots[i] = map[string]any{"robotId": i, "x": float64(i%16) * 2600, "y": float64(i/16) * 1600, "hp": 100, "alive": true}
	}
	for i := range items {
		items[i] = map[string]any{"itemId": i, "x": float64(i%32) * 1300, "y": float64(i/32) * 800}
	}
	v := &v4Viewer{region: viewerRegion{X: 1300, Y: 800, Width: 1800, Height: 1200}}
	full, regional := 0, 0
	for tick := 0; tick < 200; tick += 2 {
		raw, _ := json.Marshal(map[string]any{"type": "snapshot", "tick": tick, "revision": 1, "robots": robots, "items": items, "obstacles": []any{}})
		full += len(raw)
		regional += len(v.project(raw))
	}
	if regional >= full {
		t.Fatal("regional projection did not reduce spread-world fixture")
	}
	t.Logf("10-second synthetic spread fixture: full=%d regional=%d bytes reduction=%.2f%%", full, regional, 100*(1-float64(regional)/float64(full)))
}

func TestWithoutStaticLayoutKeepsEntities(t *testing.T) {
	frame := json.RawMessage(`{"tick":20,"revision":1,"robots":[{"robotId":"r"}],"obstacles":[{"id":"o"}],"hazards":[],"transit":[],"sites":[{"id":"s"}]}`)
	var got map[string]json.RawMessage
	if err := json.Unmarshal(withoutStaticLayout(frame), &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range staticLayoutKeys {
		if _, ok := got[key]; ok {
			t.Fatalf("%s kept in stripped frame", key)
		}
	}
	if string(got["robots"]) != `[{"robotId":"r"}]` || string(got["tick"]) != "20" {
		t.Fatalf("entities changed: %s", got["robots"])
	}
	if bad := withoutStaticLayout(json.RawMessage(`not json`)); string(bad) != "not json" {
		t.Fatal("invalid frame must pass through unchanged")
	}
}

func TestDebugMarksValidatedAndAttachedToOwnerView(t *testing.T) {
	base := v4Input{Type: "action", Version: 4, SDKVersion: "0.4.0"}
	ok := base
	ok.Debug = []v4DebugMark{{Kind: "line", X: 1, Y: 2, X2: 3, Y2: 4, Color: "#ff00AA"}, {Kind: "text", X: 5, Y: 6, Text: "GOAL"}}
	if code := validateV4Input(ok); code != "" {
		t.Fatalf("valid marks rejected: %s", code)
	}
	for name, mark := range map[string]v4DebugMark{
		"kind":  {Kind: "polygon"},
		"nan":   {Kind: "point", X: math.NaN()},
		"color": {Kind: "point", Color: "red"},
		"text":  {Kind: "text", Text: strings.Repeat("x", 41)},
	} {
		bad := base
		bad.Debug = []v4DebugMark{mark}
		if validateV4Input(bad) != "OUT_OF_RANGE" {
			t.Fatalf("%s mark accepted", name)
		}
	}
	many := base
	many.Debug = make([]v4DebugMark, maxDebugMarks+1)
	for i := range many.Debug {
		many.Debug[i] = v4DebugMark{Kind: "point"}
	}
	if validateV4Input(many) != "OUT_OF_RANGE" {
		t.Fatal("too many marks accepted")
	}
	view := withDebugMarks(json.RawMessage(`{"tick":3}`), ok.Debug)
	var got struct {
		Tick  int           `json:"tick"`
		Debug []v4DebugMark `json:"debug"`
	}
	if err := json.Unmarshal(view, &got); err != nil || got.Tick != 3 || len(got.Debug) != 2 || got.Debug[1].Text != "GOAL" {
		t.Fatalf("marks not attached: %s", view)
	}
	if string(withDebugMarks(json.RawMessage(`{"tick":3}`), nil)) != `{"tick":3}` {
		t.Fatal("empty marks changed the view")
	}
}
