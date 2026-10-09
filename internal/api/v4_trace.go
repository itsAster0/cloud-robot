package api

import (
	"context"
	"encoding/json"
	"fmt"
	robotauth "github.com/kryxen/cloud-robot/internal/auth"
	"github.com/kryxen/cloud-robot/internal/enginev4"
	"github.com/kryxen/cloud-robot/internal/model"
	"net/http"
	"strconv"
	"time"
)

func (s *Server) v4Trace(w http.ResponseWriter, r *http.Request) {
	m, err := s.store.GetMatch(r.Context(), r.PathValue("matchID"))
	if err != nil {
		writeError(w, 404, "match not found")
		return
	}
	if m.Status != model.MatchFinished || m.EngineVersion != 4 {
		writeError(w, 409, "traces unlock after completion")
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
		writeError(w, 403, "strategy traces are private to their owner")
		return
	}
	tick, err := strconv.Atoi(r.URL.Query().Get("tick"))
	if err != nil || tick < 0 || tick > 54000 {
		writeError(w, 400, "invalid tick")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	base := tick / 200 * 200
	checkpoint, err := s.store.GetReplayObject(ctx, fmt.Sprintf("replays/%s/v4/checkpoint-%06d.json", m.MatchID, base))
	if err != nil {
		writeError(w, 404, "checkpoint unavailable")
		return
	}
	client, result, err := enginev4.Restore(ctx, envOr("ARENA_ENGINE_PATH", "crates/arena-engine/target/release/arena-engine"), json.RawMessage(checkpoint))
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	defer client.Close()
	var accepted json.RawMessage = json.RawMessage(`{}`)
	for page := base / 100; page <= tick/100; page++ {
		raw, err := s.store.GetReplayObject(ctx, fmt.Sprintf("replays/%s/v4/inputs-%06d.json", m.MatchID, page))
		if err != nil {
			if tick == base {
				break
			}
			writeError(w, 404, "input record unavailable")
			return
		}
		var records []struct {
			Tick      int             `json:"tick"`
			Input     json.RawMessage `json:"input"`
			Edit      json.RawMessage `json:"edit"`
			StateHash string          `json:"stateHash"`
		}
		if err = json.Unmarshal([]byte(raw), &records); err != nil {
			writeError(w, 500, "invalid input record")
			return
		}
		for _, record := range records {
			if record.Tick < base || record.Tick >= tick {
				continue
			}
			kind := "step"
			input := record.Input
			if len(record.Edit) > 0 {
				kind = "edit"
				input = record.Edit
			}
			if err = client.Call(ctx, kind, input, &result); err != nil {
				writeError(w, 502, err.Error())
				return
			}
			if result.StateHash != record.StateHash {
				writeError(w, 500, "replay state hash mismatch")
				return
			}
			if kind == "step" {
				var batch struct {
					Actions map[string]json.RawMessage `json:"actions"`
				}
				_ = json.Unmarshal(input, &batch)
				if raw := batch.Actions[id]; len(raw) > 0 {
					accepted = raw
				} else {
					accepted = json.RawMessage(`{}`)
				}
			}
		}
	}
	var observation json.RawMessage
	if err = client.Call(ctx, "observe", map[string]string{"robotId": id}, &observation); err != nil {
		writeError(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"observation": observation, "acceptedAction": accepted, "stateHash": result.StateHash, "tick": tick})
}
