package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	robotauth "github.com/kryxen/cloud-robot/internal/auth"
	"github.com/kryxen/cloud-robot/internal/enginev4"
	"github.com/kryxen/cloud-robot/internal/model"
)

// v4SpectatorDelayTicks keeps public snapshots behind simulation: 100 ticks
// at 20 Hz is 5 seconds of anti-scouting delay.
const v4SpectatorDelayTicks = 100

type v4Control struct {
	geometry     json.RawMessage
	mu           sync.Mutex
	paused       bool
	steps        int
	withdrawals  []string
	tick         uint32
	observations map[string]json.RawMessage
	edits        chan editRequest
}

func (c *v4Control) RequestWithdraw(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.withdrawals) >= 256 {
		return false
	}
	c.withdrawals = append(c.withdrawals, id)
	return true
}
func (s *Server) createV4Match(w http.ResponseWriter, r *http.Request) {
	if envOr("MAINTENANCE_MODE", "false") == "true" {
		writeError(w, 503, "maintenance mode")
		return
	}
	var c enginev4.Config
	if err := decodeJSON(w, r, &c); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	c.Defaults()
	if len(c.Robots) > 0 {
		writeError(w, 400, "register human robots through the robot endpoint; empty slots are filled by server bots")
		return
	}
	if err := c.Validate(); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	c.MatchID = uuid.NewString()
	if c.Seed == 0 {
		c.Seed = uint64(time.Now().UnixNano())
	}
	c.Seed &= (1 << 53) - 1
	raw, _ := json.Marshal(c)
	m := model.Match{MatchID: c.MatchID, OwnerID: robotauth.UserID(r.Context()), Status: model.MatchLobby, Mode: c.Mode, EngineVersion: 4, ArenaConfig: raw, ArenaWidth: c.Width, ArenaHeight: c.Height, MapID: "world-v4", Practice: true, Seed: int64(c.Seed), TickRate: 20, CreatedAt: time.Now().UTC(), Robots: []model.RobotSubmission{}}
	if err := s.store.PutMatch(r.Context(), m); err != nil {
		writeError(w, 502, err.Error())
		return
	}
	writeJSON(w, 201, publicMatch(m))
}
func (s *Server) controlV4(w http.ResponseWriter, r *http.Request) {
	m, err := s.store.GetMatch(r.Context(), r.PathValue("matchID"))
	if err != nil {
		writeError(w, 404, "match not found")
		return
	}
	if m.OwnerID != robotauth.UserID(r.Context()) {
		writeError(w, 403, "only the owner can control a sandbox")
		return
	}
	if m.Mode != "sandbox" {
		writeError(w, 409, "pause and step require sandbox mode")
		return
	}
	var input struct {
		Command string `json:"command"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	s.mu.Lock()
	control := s.v4[m.MatchID]
	s.mu.Unlock()
	if control == nil {
		writeError(w, 409, "sandbox is not running")
		return
	}
	control.mu.Lock()
	defer control.mu.Unlock()
	switch input.Command {
	case "pause":
		control.paused = true
	case "resume":
		control.paused = false
	case "step":
		control.paused = true
		control.steps = 1
	default:
		writeError(w, 400, "command must be pause, resume or step")
		return
	}
	writeJSON(w, 200, map[string]any{"paused": control.paused, "tick": control.tick})
}

func (s *Server) runV4Match(ctx context.Context, m model.Match) error {
	config, err := enginev4.DecodeConfig(m.ArenaConfig)
	if err != nil {
		return s.failMatch(ctx, m, err)
	}
	// Fill remaining v4 roster slots at start. Bots use same Rust sensing and
	// action rules as human robots, so an unregistered practice lobby still moves.
	target := config.Capacity
	if config.Bots != nil {
		target = min(config.Capacity, humanRobots(m.Robots)+*config.Bots)
	}
	for bot := 0; len(m.Robots) < target; bot++ {
		i := len(m.Robots)
		team := fmt.Sprintf("bot-%03d", i)
		if config.Mode == "br-squad" {
			// Bot teams get their own names so they never merge into a
			// player-named team.
			team = fmt.Sprintf("bots-%02d", bot/config.SquadSize())
		}
		m.Robots = append(m.Robots, model.RobotSubmission{
			RobotID: fmt.Sprintf("bot-%03d", i), DisplayName: fmt.Sprintf("Game Bot %d", i+1),
			Team: team, Runtime: "server-bot", StartCommand: "bot:balanced", Bot: true,
			SubmittedAt: time.Now().UTC(),
		})
	}
	for _, r := range m.Robots {
		var loadout enginev4.Loadout
		if len(r.Loadout) > 0 {
			if err = json.Unmarshal(r.Loadout, &loadout); err != nil {
				return s.failMatch(ctx, m, err)
			}
		}
		loadout.Defaults()
		config.Robots = append(config.Robots, enginev4.Registration{RobotID: r.RobotID, Name: r.DisplayName, Team: r.Team, Bot: r.Bot, Loadout: loadout})
	}
	var workerLog io.Writer = os.Stderr
	if s.logs != nil {
		lines := s.logs.LineWriter("worker", m.MatchID, os.Stderr)
		defer lines.Close()
		workerLog = lines
	}
	slog.Info("match starting", "source", "worker", "match", m.MatchID, "mode", config.Mode, "capacity", config.Capacity, "humans", humanRobots(m.Robots), "seed", config.Seed)
	started := time.Now()
	client, result, err := enginev4.StartLogged(ctx, envOr("ARENA_ENGINE_PATH", "crates/arena-engine/target/release/arena-engine"), config, workerLog)
	if err != nil {
		return s.failMatch(ctx, m, fmt.Errorf("start Rust engine: %w", err))
	}
	defer client.Close()
	recorder := s.newV4Recorder(ctx)
	defer recorder.Close()
	configRaw, _ := json.Marshal(config)
	if err = recorder.Add(fmt.Sprintf("replays/%s/v4/config.json", m.MatchID), string(configRaw)); err != nil {
		return s.failMatch(ctx, m, err)
	}
	var initialCheckpoint json.RawMessage
	if err = client.Call(ctx, "checkpoint", map[string]any{}, &initialCheckpoint); err != nil {
		return s.failMatch(ctx, m, err)
	}
	if err = recorder.Add(fmt.Sprintf("replays/%s/v4/checkpoint-%06d.json", m.MatchID, 0), string(initialCheckpoint)); err != nil {
		return s.failMatch(ctx, m, err)
	}
	now := time.Now().UTC()
	m.Status = model.MatchRunning
	m.StartedAt = &now
	if err = s.store.PutMatch(ctx, m); err != nil {
		return s.failMatch(ctx, m, err)
	}
	control := &v4Control{edits: make(chan editRequest, 8)}
	s.mu.Lock()
	s.v4[m.MatchID] = control
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.v4, m.MatchID); s.mu.Unlock() }()
	s.hub.Publish(m.MatchID, map[string]any{"type": "match_state", "version": 4, "match": publicMatch(m)})
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	type snap struct {
		Tick       uint32 `json:"tick"`
		Status     string `json:"status"`
		WinnerTeam string `json:"winnerTeam"`
		Revision   int    `json:"revision"`
		Robots     []struct {
			RobotID     string  `json:"robotId"`
			Name        string  `json:"name"`
			Team        string  `json:"team"`
			HP          float64 `json:"hp"`
			Alive       bool    `json:"alive"`
			DamageDealt float64 `json:"damageDealt"`
			DamageTaken float64 `json:"damageTaken"`
			Kills       int     `json:"kills"`
		} `json:"robots"`
	}
	type delayed struct {
		tick uint32
		raw  json.RawMessage
	}
	history := []delayed{}
	inputs := []any{}
	chunk := 0
	missing := map[string]time.Time{}
	var state snap
	pendingEdits := []json.RawMessage{}
	frames := []json.RawMessage{}
	framePage := 0
	frameRevision := -1
	// Full-world live data stays private for a short spectator delay. Pausing a
	// sandbox also pauses its spectator delay.
	for {
		if err = recorder.Err(); err != nil {
			return s.failMatch(ctx, m, err)
		}
		if err = json.Unmarshal(result.Snapshot, &state); err != nil {
			return s.failMatch(ctx, m, err)
		}
		if state.Tick%10 == 0 {
			frame := append(json.RawMessage(nil), result.Snapshot...)
			// Live edits change the revision mid-page; keep that frame's layout.
			if len(frames) > 0 && state.Revision == frameRevision {
				frame = withoutStaticLayout(frame)
			}
			frameRevision = state.Revision
			frames = append(frames, frame)
		}
		if len(frames) >= 10 {
			packed, packErr := packFrames(frames)
			if packErr != nil {
				return s.failMatch(ctx, m, packErr)
			}
			if err = recorder.Add(fmt.Sprintf("replays/%s/v4/frames-%06d.gz.b64", m.MatchID, framePage), packed); err != nil {
				return s.failMatch(ctx, m, err)
			}
			frames = nil
			framePage++
		}
		if state.Tick%2 == 0 {
			history = append(history, delayed{state.Tick, append(json.RawMessage(nil), result.Snapshot...)})
			for len(history) > 0 && history[0].tick+v4SpectatorDelayTicks <= state.Tick {
				s.hub.Publish(m.MatchID, history[0].raw)
				history = history[1:]
			}
		}
		for id, observation := range result.Observations {
			s.agents.mu.RLock()
			session := s.agents.sessions[id]
			s.agents.mu.RUnlock()
			if session != nil && session.v4 {
				session.mailbox.mu.Lock()
				session.mailbox.geometry = result.Geometry
				session.mailbox.mu.Unlock()
				session.sendObservation(observation, state.Tick)
				delete(missing, id)
			} else if _, ok := missing[id]; !ok {
				missing[id] = time.Now()
			}
		}
		control.mu.Lock()
		control.tick = state.Tick
		if state.Tick%2 == 0 || state.Status == "finished" {
			control.observations = result.Observations
			control.geometry = result.Geometry
		}
		control.mu.Unlock()
		if state.Status == "finished" {
			break
		}
		var advance bool
		for !advance {
			select {
			case <-ctx.Done():
				return s.failMatch(context.WithoutCancel(ctx), m, ctx.Err())
			case req := <-control.edits:
				var preview json.RawMessage
				editErr := client.Call(ctx, "previewEdit", req.raw, &preview)
				if editErr == nil && req.apply {
					if len(pendingEdits) > 0 {
						editErr = fmt.Errorf("another edit is scheduled")
					} else {
						editErr = s.store.PutReplayObject(ctx, fmt.Sprintf("replays/%s/v4/edit-%06d.json", m.MatchID, state.Tick), string(req.raw))
						if editErr == nil {
							pendingEdits = append(pendingEdits, req.raw)
						}
					}
				}
				req.reply <- editReply{preview, editErr}
			case <-ticker.C:
				control.mu.Lock()
				advance = !control.paused || control.steps > 0
				if control.steps > 0 {
					control.steps--
				}
				control.mu.Unlock()
			}
		}
		actions := map[string]json.RawMessage{}
		for _, r := range m.Robots {
			if r.Bot {
				continue
			}
			s.agents.mu.RLock()
			session := s.agents.sessions[r.RobotID]
			s.agents.mu.RUnlock()
			if session != nil && session.v4 {
				actions[r.RobotID] = session.takeV4Action(state.Tick)
			} else if at, ok := missing[r.RobotID]; ok && time.Since(at) > 30*time.Second {
				control.RequestWithdraw(r.RobotID)
			}
		}
		control.mu.Lock()
		withdrawals := control.withdrawals
		control.withdrawals = nil
		control.mu.Unlock()
		for _, edit := range pendingEdits {
			var timing struct {
				EffectiveTick uint32 `json:"effectiveTick"`
			}
			_ = json.Unmarshal(edit, &timing)
			if timing.EffectiveTick == state.Tick {
				var changed enginev4.Result
				if err = client.Call(ctx, "edit", edit, &changed); err != nil {
					var rejection *enginev4.Rejection
					if !errors.As(err, &rejection) {
						return s.failMatch(ctx, m, err)
					}
					if err = recorder.Add(fmt.Sprintf("replays/%s/v4/edit-rejected-%06d.json", m.MatchID, state.Tick), string(mustJSON(map[string]any{"edit": edit, "reason": rejection.Message}))); err != nil {
						return s.failMatch(ctx, m, err)
					}
					continue
				}
				inputs = append(inputs, map[string]any{"tick": state.Tick, "edit": edit, "stateHash": changed.StateHash})
			}
		}
		kept := pendingEdits[:0]
		for _, edit := range pendingEdits {
			var timing struct {
				EffectiveTick uint32 `json:"effectiveTick"`
			}
			_ = json.Unmarshal(edit, &timing)
			if timing.EffectiveTick > state.Tick {
				kept = append(kept, edit)
			}
		}
		pendingEdits = kept
		input := map[string]any{"actions": actions, "withdrawals": withdrawals}
		if withdrawals == nil {
			input["withdrawals"] = []string{}
		}
		var next enginev4.Result
		if err = client.Call(ctx, "step", input, &next); err != nil {
			return s.failMatch(ctx, m, err)
		}
		inputs = append(inputs, map[string]any{"tick": state.Tick, "input": input, "stateHash": next.StateHash})
		if (state.Tick+1)%100 == 0 {
			raw, _ := json.Marshal(inputs)
			if err = recorder.Add(fmt.Sprintf("replays/%s/v4/inputs-%06d.json", m.MatchID, chunk), string(raw)); err != nil {
				return s.failMatch(ctx, m, err)
			}
			inputs = nil
			chunk++
		}
		if (state.Tick+1)%200 == 0 {
			var checkpoint json.RawMessage
			if err = client.Call(ctx, "checkpoint", map[string]any{}, &checkpoint); err != nil {
				return s.failMatch(ctx, m, err)
			}
			if err = recorder.Add(fmt.Sprintf("replays/%s/v4/checkpoint-%06d.json", m.MatchID, state.Tick+1), string(checkpoint)); err != nil {
				return s.failMatch(ctx, m, err)
			}
		}
		result = next
	}
	if len(inputs) > 0 {
		raw, _ := json.Marshal(inputs)
		if err = recorder.Add(fmt.Sprintf("replays/%s/v4/inputs-%06d.json", m.MatchID, chunk), string(raw)); err != nil {
			return s.failMatch(ctx, m, err)
		}
	}
	if len(frames) > 0 {
		packed, packErr := packFrames(frames)
		if packErr != nil {
			return s.failMatch(ctx, m, packErr)
		}
		if err = recorder.Add(fmt.Sprintf("replays/%s/v4/frames-%06d.gz.b64", m.MatchID, framePage), packed); err != nil {
			return s.failMatch(ctx, m, err)
		}
	}
	if err = recorder.Close(); err != nil {
		return s.failMatch(ctx, m, err)
	}
	finished := time.Now().UTC()
	m.Status = model.MatchFinished
	m.FinishedAt = &finished
	m.WinnerTeam = state.WinnerTeam
	for _, r := range state.Robots {
		m.RobotSummaries = append(m.RobotSummaries, model.RobotSummary{RobotID: r.RobotID, Name: r.Name, Team: r.Team, HP: int(r.HP), Alive: r.Alive, DamageDealt: int(r.DamageDealt), DamageTaken: int(r.DamageTaken), Kills: r.Kills})
	}
	if err = s.store.PutReplayObject(ctx, fmt.Sprintf("replays/%s/v4/final.json", m.MatchID), string(result.Snapshot)); err != nil {
		return s.failMatch(ctx, m, err)
	}
	if err = s.store.PutMatch(ctx, m); err != nil {
		return s.failMatch(ctx, m, err)
	}
	slog.Info("match finished", "source", "worker", "match", m.MatchID, "winner", m.WinnerTeam, "ticks", state.Tick, "seconds", int(time.Since(started).Seconds()))
	s.releaseBoxes(ctx, m)
	s.hub.Publish(m.MatchID, result.Snapshot)
	s.hub.Publish(m.MatchID, map[string]any{"type": "match_finished", "version": 4, "match": publicMatch(m)})
	s.hub.Forget(m.MatchID)
	return nil
}

func (s *Server) v4Final(w http.ResponseWriter, r *http.Request) {
	m, err := s.store.GetMatch(r.Context(), r.PathValue("matchID"))
	if err != nil {
		writeError(w, 404, "match not found")
		return
	}
	if m.Status != model.MatchFinished {
		writeError(w, 409, "replay unlocks after completion")
		return
	}
	raw, err := s.store.GetReplayObject(r.Context(), fmt.Sprintf("replays/%s/v4/final.json", m.MatchID))
	if err != nil {
		writeError(w, 404, "replay not found")
		return
	}
	writeJSON(w, 200, json.RawMessage(raw))
}
func mustJSON(v any) json.RawMessage { raw, _ := json.Marshal(v); return raw }

func (s *Server) v4View(w http.ResponseWriter, r *http.Request) {
	m, err := s.store.GetMatch(r.Context(), r.PathValue("matchID"))
	if err != nil {
		writeError(w, 404, "match not found")
		return
	}
	id := ""
	for _, robot := range m.Robots {
		if robot.PlayerID == robotauth.UserID(r.Context()) {
			id = robot.RobotID
			break
		}
	}
	if id == "" {
		writeError(w, 403, "a registered robot is required for a live view")
		return
	}
	s.mu.Lock()
	c := s.v4[m.MatchID]
	s.mu.Unlock()
	if c == nil {
		writeError(w, 409, "match not running")
		return
	}
	c.mu.Lock()
	obs := append(json.RawMessage(nil), c.observations[id]...)
	geometry := c.geometry
	c.mu.Unlock()
	if len(obs) == 0 {
		writeError(w, 409, "robot has no live observation; use delayed spectating")
		return
	}
	s.agents.mu.RLock()
	session := s.agents.sessions[id]
	s.agents.mu.RUnlock()
	var marks []v4DebugMark
	if session != nil {
		marks = session.debugMarks()
	}
	writeJSON(w, 200, withDebugMarks(observationWithGeometry(obs, geometry, 0), marks))
}

// withDebugMarks adds the owner's latest script drawings to a live view.
func withDebugMarks(raw json.RawMessage, marks []v4DebugMark) json.RawMessage {
	var fields map[string]json.RawMessage
	if len(marks) == 0 || json.Unmarshal(raw, &fields) != nil {
		return raw
	}
	encoded, err := json.Marshal(marks)
	if err != nil {
		return raw
	}
	fields["debug"] = encoded
	out, err := json.Marshal(fields)
	if err != nil {
		return raw
	}
	return out
}

type editRequest struct {
	raw   json.RawMessage
	apply bool
	reply chan editReply
}
type editReply struct {
	value json.RawMessage
	err   error
}

func (s *Server) v4Edit(w http.ResponseWriter, r *http.Request) {
	m, err := s.store.GetMatch(r.Context(), r.PathValue("matchID"))
	if err != nil {
		writeError(w, 404, "match not found")
		return
	}
	config, err := enginev4.DecodeConfig(m.ArenaConfig)
	if err != nil || !config.LiveEdit || !m.Practice {
		writeError(w, 409, "live edits require an enabled practice match")
		return
	}
	var raw json.RawMessage
	if err = decodeJSON(w, r, &raw); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	s.mu.Lock()
	c := s.v4[m.MatchID]
	s.mu.Unlock()
	if c == nil {
		writeError(w, 409, "match not running")
		return
	}
	req := editRequest{raw: raw, apply: r.URL.Query().Get("apply") == "true", reply: make(chan editReply, 1)}
	select {
	case c.edits <- req:
	default:
		writeError(w, 429, "edit queue full")
		return
	}
	select {
	case result := <-req.reply:
		if result.err != nil {
			writeError(w, 409, result.err.Error())
			return
		}
		writeJSON(w, 200, result.value)
	case <-r.Context().Done():
		return
	case <-time.After(8 * time.Second):
		writeError(w, 504, "edit response timed out; inspect match revision before retrying")
	}
}

func humanRobots(robots []model.RobotSubmission) int {
	n := 0
	for _, r := range robots {
		if !r.Bot {
			n++
		}
	}
	return n
}
