package enginev4

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEnvelopeRoundTrip(t *testing.T) {
	var b bytes.Buffer
	if err := WriteEnvelope(&b, "start", []byte(`{"capacity":256}`)); err != nil {
		t.Fatal(err)
	}
	kind, payload, err := ReadEnvelope(&b)
	if err != nil || kind != "start" || string(payload) != `{"capacity":256}` {
		t.Fatalf("%s %s %v", kind, payload, err)
	}
}
func TestMalformedFrames(t *testing.T) {
	for _, data := range [][]byte{{}, {0, 0, 0, 1}, {255, 255, 255, 255}, {0, 0, 0, 2, 8, 3}} {
		if _, _, err := ReadEnvelope(bytes.NewReader(data)); err == nil {
			t.Fatalf("accepted %x", data)
		}
	}
}

type shortWriter struct{ bytes.Buffer }

func (w *shortWriter) Write(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return w.Buffer.Write(p)
}
func TestPartialWrites(t *testing.T) {
	w := &shortWriter{}
	if err := WriteEnvelope(w, "step", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadEnvelope(&w.Buffer); err != nil {
		t.Fatal(err)
	}
}
func TestRustWorkerLifecycle(t *testing.T) {
	path := os.Getenv("ARENA_ENGINE_TEST_PATH")
	if path == "" {
		path = "../../crates/arena-engine/target/debug/arena-engine"
	}
	path, _ = filepath.Abs(path)
	if _, err := os.Stat(path); err != nil {
		t.Skip("build Rust worker before running integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c := Config{MatchID: "integration", Mode: "quick-duel", Capacity: 2, Width: 2400, Height: 1500, DurationSeconds: 10, Seed: 42}
	worker, result, err := Start(ctx, path, c)
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	if len(result.Snapshot) == 0 || result.StateHash == "" {
		t.Fatal("missing initial state")
	}
	var snapshot struct {
		Tick    int    `json:"tick"`
		Status  string `json:"status"`
		Version int    `json:"version"`
	}
	for i := 0; i < 200; i++ {
		if err = worker.Call(ctx, "step", map[string]any{"actions": map[string]any{}, "withdrawals": []string{}}, &result); err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(result.Snapshot, &snapshot); err != nil {
			t.Fatal(err)
		}
		if snapshot.Status == "finished" {
			break
		}
	}
	if snapshot.Status != "finished" || snapshot.Version != 4 {
		t.Fatalf("match did not finish: %+v", snapshot)
	}
	var checkpoint json.RawMessage
	if err = worker.Call(ctx, "checkpoint", map[string]any{}, &checkpoint); err != nil {
		t.Fatal(err)
	}
	if !json.Valid(checkpoint) {
		t.Fatal("invalid checkpoint")
	}
	restored, restoredResult, err := Restore(ctx, path, checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if result.StateHash != restoredResult.StateHash {
		t.Fatal("cross-process restore changed state hash")
	}

}

var _ io.Reader = (*bytes.Buffer)(nil)
