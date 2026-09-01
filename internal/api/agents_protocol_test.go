package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/kryxen/cloud-robot/internal/engine"
)

// loopbackAgent runs AgentSession.Tick against an in-memory websocket server
// that answers every observation with an action built by respond. Captured
// observations are raw decoded JSON maps, so tests can assert the exact wire
// shape of the v3 protocol.
func loopbackAgent(t *testing.T, respond func(map[string]any) map[string]any) (*AgentSession, *[]map[string]any) {
	t.Helper()
	observations := &[]map[string]any{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for {
			var observation map[string]any
			if err := wsjson.Read(ctx, conn, &observation); err != nil {
				return
			}
			*observations = append(*observations, observation)
			if reply := respond(observation); reply != nil {
				reply["type"] = "action"
				reply["requestId"] = observation["requestId"]
				if err := wsjson.Write(ctx, conn, reply); err != nil {
					return
				}
			}
		}
	}))
	t.Cleanup(server.Close)
	client, _, err := websocket.Dial(context.Background(), "ws://"+server.Listener.Addr().String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.CloseNow() })
	session := newAgentSession("robot-1", "match-1", client)
	go session.readLoop(context.Background())
	return session, observations
}

func v3Self() engine.RobotState {
	return engine.RobotState{
		RobotID: "robot-1", Team: "red", HP: 100, Alive: true,
		DashCharges:  2,
		MineCharges:  1,
		ScanResult:   &engine.ScanReport{Radius: 80, Items: []engine.ScannedItem{{ItemID: "item-1", Type: "heal"}}},
		RecentEvents: []engine.Event{{Type: "dash", Tick: 7}},
		Messages:     []string{"go left"},
	}
}

func TestAgentObservationCarriesProtocolV3(t *testing.T) {
	session, observations := loopbackAgent(t, func(map[string]any) map[string]any { return map[string]any{"move": 8.0} })
	world := engine.WorldState{Tick: 42, MapID: "open-field", Width: 800, Height: 500, Mines: []engine.MineState{{MineID: "mine-1", OwnerID: "robot-1", Team: "red", Active: true}}}
	intent, err := session.Tick(context.Background(), v3Self(), []engine.RobotState{v3Self()}, world)
	if err != nil {
		t.Fatal(err)
	}
	if intent.Dash || intent.Deploy != "" || intent.Scan != nil || intent.Message != "" {
		t.Fatalf("legacy action must map to zero v3 intent fields: %+v", intent)
	}
	if len(*observations) != 1 {
		t.Fatalf("expected one captured observation, got %d", len(*observations))
	}
	raw, err := json.Marshal((*observations)[0])
	if err != nil {
		t.Fatal(err)
	}
	wire := string(raw)
	for _, fragment := range []string{`"version":3`, `"tick":42`, `"dashCharges":2`, `"mineCharges":1`, `"scanResult":{`, `"items":[`, `"mineId":"mine-1"`, `"events":[`, `"messages":["go left"]`} {
		if !strings.Contains(wire, fragment) {
			t.Fatalf("observation JSON missing %s:\n%s", fragment, wire)
		}
	}
}

func TestAgentActionMapsDashDeployScanMessage(t *testing.T) {
	reply := map[string]any{
		"move": 4.0, "fire": true, "dash": true, "deploy": "mine",
		"scan":    map[string]any{"x": 10.0, "y": 20.0, "radius": 80.0},
		"message": "  flank wide  ",
	}
	session, _ := loopbackAgent(t, func(map[string]any) map[string]any { return reply })
	intent, err := session.Tick(context.Background(), v3Self(), nil, engine.WorldState{})
	if err != nil {
		t.Fatal(err)
	}
	if !intent.Dash || intent.Deploy != "mine" || !intent.Fire || intent.Move != 4 {
		t.Fatalf("v3 action fields not mapped: %+v", intent)
	}
	if intent.Scan == nil || intent.Scan.X != 10 || intent.Scan.Y != 20 || intent.Scan.Radius != 80 {
		t.Fatalf("scan request not mapped: %+v", intent.Scan)
	}
	if intent.Message != "flank wide" {
		t.Fatalf("message must be trimmed: %q", intent.Message)
	}
}

func TestAgentActionMessageTrimsAndCapsAt128Bytes(t *testing.T) {
	reply := map[string]any{"message": "  " + strings.Repeat("x", 300) + "  "}
	session, _ := loopbackAgent(t, func(map[string]any) map[string]any { return reply })
	intent, err := session.Tick(context.Background(), v3Self(), nil, engine.WorldState{})
	if err != nil {
		t.Fatal(err)
	}
	if len(intent.Message) != engine.MaxMessageBytes {
		t.Fatalf("message must cap at %d bytes, got %d", engine.MaxMessageBytes, len(intent.Message))
	}
	if strings.TrimSpace(intent.Message) != intent.Message {
		t.Fatalf("message must be trimmed: %q", intent.Message)
	}
}

func TestAgentActionWithoutV3FieldsStillValid(t *testing.T) {
	// The v0.2.0 SDK sends only the legacy fields; nothing may break.
	session, observations := loopbackAgent(t, func(map[string]any) map[string]any { return map[string]any{"move": 8.0, "fire": true} })
	self := engine.RobotState{RobotID: "robot-1", Team: "red", HP: 100, Alive: true}
	intent, err := session.Tick(context.Background(), self, []engine.RobotState{self}, engine.WorldState{})
	if err != nil {
		t.Fatal(err)
	}
	if !intent.Fire || intent.Move != 8 || intent.Dash || intent.Deploy != "" || intent.Scan != nil || intent.Message != "" {
		t.Fatalf("legacy action must keep working unchanged: %+v", intent)
	}
	raw, err := json.Marshal((*observations)[0])
	if err != nil {
		t.Fatal(err)
	}
	wire := string(raw)
	for _, fragment := range []string{`"mines":`, `"scanResult":`, `"messages":`, `"dashCharges":`} {
		if strings.Contains(wire, fragment) {
			t.Fatalf("empty v3 fields must be omitted from the wire, found %s:\n%s", fragment, wire)
		}
	}
}
