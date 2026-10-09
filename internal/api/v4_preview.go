package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"github.com/kryxen/cloud-robot/internal/enginev4"
)

// previewV4Map renders the deterministic map for a lobby config without
// starting a match, so the browser shows the arena immediately on creation.
// Generation stays in the Rust worker: the single source of truth.
func (s *Server) previewV4Map(w http.ResponseWriter, r *http.Request) {
	var c enginev4.Config
	if err := decodeJSON(w, r, &c); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	c.Defaults()
	if c.MatchID == "" {
		c.MatchID = "preview"
	}
	// Robots never affect generated geometry; preview ignores the roster.
	c.Robots = nil
	if err := c.Validate(); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	raw, _ := json.Marshal(c)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, envOr("ARENA_ENGINE_PATH", "crates/arena-engine/target/release/arena-engine"), "generate")
	cmd.Stdin = bytes.NewReader(raw)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			writeError(w, 504, "map preview timed out")
			return
		}
		writeError(w, 502, "map preview unavailable: build the Rust worker")
		return
	}
	if out.Len() > 4*1024*1024 {
		writeError(w, 502, "map preview too large")
		return
	}
	writeJSON(w, 200, json.RawMessage(out.Bytes()))
}

// catalogueCache holds the engine's equipment catalogue; it only changes when
// the worker binary changes, so one successful read serves the process.
var catalogueCache struct {
	sync.Mutex
	raw json.RawMessage
}

// v4Catalogue serves chassis, weapon, module, utility, and consumable stats
// straight from the Rust engine so the loadout page never drifts from rules.
func (s *Server) v4Catalogue(w http.ResponseWriter, r *http.Request) {
	catalogueCache.Lock()
	defer catalogueCache.Unlock()
	if catalogueCache.raw == nil {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, envOr("ARENA_ENGINE_PATH", "crates/arena-engine/target/release/arena-engine"), "catalogue").Output()
		if err != nil || !json.Valid(out) {
			writeError(w, 502, "catalogue unavailable: build the Rust worker")
			return
		}
		catalogueCache.raw = json.RawMessage(bytes.TrimSpace(out))
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(catalogueCache.raw)
}
