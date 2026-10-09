package api

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/kryxen/cloud-robot/internal/enginev4"
	"github.com/kryxen/cloud-robot/internal/model"
)

// Opt-in: real WebSocket agents through the production mailbox and Rust worker.
// This deliberately bypasses account provisioning. It tests match transport,
// not SSH isolation or WorkOS, and does not qualify a remote Linux host.
func TestV4NetworkLoad(t *testing.T) {
	raw := os.Getenv("ARENA_LOAD_ROBOTS")
	if raw == "" {
		t.Skip("set ARENA_LOAD_ROBOTS=16,64,128,256")
	}
	count, err := strconv.Atoi(raw)
	if err != nil || count < 1 || count > 256 {
		t.Fatal("invalid ARENA_LOAD_ROBOTS")
	}
	seconds := 10
	if value := os.Getenv("ARENA_LOAD_SECONDS"); value != "" {
		seconds, err = strconv.Atoi(value)
		if err != nil || seconds < 10 || seconds > 1080 {
			t.Fatal("invalid ARENA_LOAD_SECONDS")
		}
	}
	executable, err := filepath.Abs("../../crates/arena-engine/target/release/arena-engine")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ARENA_ENGINE_PATH", executable)
	h := newHarness(t)
	config := enginev4.Config{MatchID: "load", Mode: "sandbox", Capacity: count, Width: 2400, Height: 1500, DurationSeconds: seconds, Seed: 42}
	config.Defaults()
	m := model.Match{MatchID: "load", Status: model.MatchQueued, Mode: "sandbox", EngineVersion: 4, Practice: true, ArenaConfig: mustJSON(config)}
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("load-%03d", i)
		m.Robots = append(m.Robots, model.RobotSubmission{RobotID: id, DisplayName: id, Team: id, SDKVersion: "0.4.0"})
	}
	h.store.matches[m.MatchID] = m
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(seconds*3+30)*time.Second)
	defer cancel()
	var connected sync.WaitGroup
	connected.Add(count)
	var clients sync.WaitGroup
	var received, bytes atomic.Uint64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		id := strings.TrimPrefix(r.URL.Path, "/")
		session := newAgentSession(id, "load", c)
		session.v4 = true
		session.mailbox = &v4Mailbox{out: make(chan json.RawMessage, 1)}
		h.app.agents.Attach(id, session)
		defer h.app.agents.Detach(id, session)
		connected.Done()
		_ = session.readV4(ctx)
	}))
	defer server.Close()
	sockets := make([]*websocket.Conn, 0, count)
	defer func() {
		for _, c := range sockets {
			c.CloseNow()
		}
		cancel()
		clients.Wait()
	}()
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("load-%03d", i)
		c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/"+id, nil)
		if err != nil {
			t.Fatal(err)
		}
		c.SetReadLimit(16 * 1024 * 1024)
		sockets = append(sockets, c)
		clients.Add(1)
		go func(c *websocket.Conn) {
			defer clients.Done()
			var sequence uint64
			for {
				var raw json.RawMessage
				if err := wsjson.Read(ctx, c, &raw); err != nil {
					return
				}
				var o struct {
					Type     string                 `json:"type"`
					Revision uint32                 `json:"revision"`
					Tick     uint32                 `json:"tick"`
					Self     struct{ X, Y float64 } `json:"self"`
				}
				if json.Unmarshal(raw, &o) != nil || o.Type != "observation" {
					continue
				}
				received.Add(1)
				bytes.Add(uint64(len(raw)))
				sequence++
				aim := math.Atan2(750-o.Self.Y, 1200-o.Self.X) * 180 / math.Pi
				action := v4Input{Type: "action", Version: 4, SDKVersion: "0.4.0", Sequence: sequence, ObservedTick: o.Tick, GeometryRevision: &o.Revision, Action: v4Action{Throttle: 1, Turn: 0.1, Aim: &aim, Fire: true}}
				if wsjson.Write(ctx, c, action) != nil {
					return
				}
			}
		}(c)
	}
	connected.Wait()
	started := time.Now()
	if err := h.app.runV4Match(ctx, m); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(started)
	if h.store.matches["load"].Status != model.MatchFinished {
		t.Fatal("match did not finish")
	}
	if received.Load() < uint64(count*seconds*5) {
		t.Fatalf("too few observations: %d", received.Load())
	}
	t.Logf("robots=%d simulatedSeconds=%d elapsedSeconds=%.3f observations=%d observationBytes=%d", count, seconds, elapsed.Seconds(), received.Load(), bytes.Load())
	if elapsed > time.Duration(seconds)*time.Second+5*time.Second {
		t.Fatalf("simulation fell behind wall clock: %s", elapsed)
	}
}
