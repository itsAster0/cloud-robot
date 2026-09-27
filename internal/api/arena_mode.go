package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/kryxen/cloud-robot/internal/enginev4"
	"github.com/kryxen/cloud-robot/internal/model"
)

// arenaOwner marks the always-on arena session the server keeps alive.
const arenaOwner = "system:arena"

// arenaConfig is the persistent arena: free-for-all, respawns, no zone, and
// long sessions that rotate to a fresh seed when they end. ARENA_CAPACITY is
// the smallest session; a busy previous session (peak players) grows the next
// one to twice its peak plus room for bots, up to 128 slots.
func arenaConfig(peak int) enginev4.Config {
	capacity := envInt("ARENA_CAPACITY", 24, 2, 128)
	capacity = min(128, max(capacity, (peak*2+8+7)/8*8))
	// Map area grows with capacity so density stays similar.
	width := 4000 + float64(capacity)*220
	return enginev4.Config{
		MatchID: uuid.NewString(), Mode: "arena", Capacity: capacity,
		Width: width, Height: width * 0.625,
		DurationSeconds: envInt("ARENA_SESSION_SECONDS", 1800, 60, 21600),
		SiteCount:       max(6, capacity/2), CoverPerSite: 8, LootPerSite: 16,
		Seed: uint64(time.Now().UnixNano()) & ((1 << 53) - 1),
	}
}

func envInt(name string, fallback, low, high int) int {
	value, err := strconv.Atoi(envOr(name, ""))
	if err != nil || value < low || value > high {
		return fallback
	}
	return value
}

// currentArena returns the live or queued system arena, if any.
func (s *Server) currentArena(ctx context.Context) (model.Match, bool) {
	for _, status := range []model.MatchStatus{model.MatchRunning, model.MatchQueued} {
		matches, err := s.store.ListMatches(ctx, status, 100)
		if err != nil {
			continue
		}
		for _, m := range matches {
			if m.Mode == "arena" && m.OwnerID == arenaOwner {
				return m, true
			}
		}
	}
	return model.Match{}, false
}

// ensureArena queues a new arena session when none is running or queued.
func (s *Server) ensureArena(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.currentArena(ctx); ok {
		return
	}
	c := arenaConfig(s.arenaPeak)
	s.arenaPeak = 0
	if err := c.Validate(); err != nil {
		slog.Error("arena config", "error", err)
		return
	}
	raw, _ := json.Marshal(c)
	m := model.Match{MatchID: c.MatchID, OwnerID: arenaOwner, Status: model.MatchQueued, Mode: c.Mode, EngineVersion: 4, ArenaConfig: raw, ArenaWidth: c.Width, ArenaHeight: c.Height, MapID: "world-v4", Practice: true, Seed: int64(c.Seed), TickRate: 20, CreatedAt: time.Now().UTC(), Robots: []model.RobotSubmission{}}
	if err := s.store.PutMatch(ctx, m); err != nil {
		slog.Error("create arena session", "error", err)
		return
	}
	if err := s.store.EnqueueMatch(ctx, m.MatchID); err != nil {
		m.Status, m.Error = model.MatchFailed, err.Error()
		_ = s.store.PutMatch(ctx, m)
		slog.Error("queue arena session", "error", err)
		return
	}
	slog.Info("arena session queued", "source", "arena", "match", m.MatchID, "capacity", c.Capacity)
}

// RunArena keeps one arena session alive. ARENA_ENABLED=false turns it off.
func (s *Server) RunArena(ctx context.Context) {
	if envOr("ARENA_ENABLED", "true") == "false" {
		return
	}
	// The first check waits one period: a job queued while the worker's
	// first receive is in flight was held by Floci until its visibility
	// timeout (two minutes) before any worker saw it.
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if envOr("MAINTENANCE_MODE", "false") != "true" {
			s.ensureArena(ctx)
		}
	}
}

// arenaStatus is the public entry point for the persistent arena.
func (s *Server) arenaStatus(w http.ResponseWriter, r *http.Request) {
	m, ok := s.currentArena(r.Context())
	if !ok {
		writeError(w, http.StatusNotFound, "the arena is starting; try again in a few seconds")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"match": publicMatch(m), "players": humanRobots(m.Robots)})
}
