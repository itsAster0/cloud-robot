package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	robotauth "github.com/kryxen/cloud-robot/internal/auth"
	"github.com/kryxen/cloud-robot/internal/model"
)

// leaderboardScanLimit bounds how many recent matches the on-the-fly
// leaderboard reads. It is a Phase 1 aggregate, not a stored ranking.
const leaderboardScanLimit = 500

// playerHandle is the public name for an account: a short hash, so
// account IDs never appear in public responses.
func playerHandle(playerID string) string {
	sum := sha256.Sum256([]byte("player:" + playerID))
	return hex.EncodeToString(sum[:6])
}

type playerLine struct {
	Handle      string `json:"handle"`
	Name        string `json:"name"`
	Matches     int    `json:"matches"`
	Wins        int    `json:"wins"`
	Kills       int    `json:"kills"`
	Deaths      int    `json:"deaths"`
	Score       int    `json:"score"`
	DamageDealt int    `json:"damageDealt"`
	BestScore   int    `json:"bestScore"`
	playerID    string
	lastSeen    string
}

// v4PlayerLines aggregates finished Arena V2 matches per human player.
// mode "arena" counts only arena sessions; anything else counts all modes.
func (s *Server) v4PlayerLines(r *http.Request, mode string) (map[string]*playerLine, []model.Match, error) {
	matches, err := s.store.ListMatches(r.Context(), model.MatchFinished, leaderboardScanLimit)
	if err != nil {
		return nil, nil, err
	}
	matches = append(matches, s.arenaStatMatches(r.Context())...)
	lines := map[string]*playerLine{}
	kept := []model.Match{}
	for _, m := range matches {
		if m.EngineVersion != 4 || (mode == "arena" && m.Mode != "arena") {
			continue
		}
		kept = append(kept, m)
		for _, sum := range m.RobotSummaries {
			if sum.PlayerID == "" || sum.Bot {
				continue
			}
			line := lines[sum.PlayerID]
			if line == nil {
				line = &playerLine{Handle: playerHandle(sum.PlayerID), playerID: sum.PlayerID}
				lines[sum.PlayerID] = line
			}
			// Keep the most recent display name.
			if at := m.CreatedAt.String(); at >= line.lastSeen {
				line.Name, line.lastSeen = sum.Name, at
			}
			line.Matches++
			if m.WinnerTeam != "" && m.WinnerTeam == sum.Team {
				line.Wins++
			}
			line.Kills += sum.Kills
			line.Deaths += sum.Deaths
			line.Score += sum.Score
			line.DamageDealt += sum.DamageDealt
			line.BestScore = max(line.BestScore, sum.Score)
		}
	}
	return lines, kept, nil
}

func (s *Server) v4Leaderboard(w http.ResponseWriter, r *http.Request) {
	mode := r.URL.Query().Get("mode")
	if mode != "arena" {
		mode = "all"
	}
	lines, _, err := s.v4PlayerLines(r, mode)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	ranked := make([]*playerLine, 0, len(lines))
	for _, line := range lines {
		ranked = append(ranked, line)
	}
	sort.Slice(ranked, func(i, j int) bool {
		a, b := ranked[i], ranked[j]
		if mode == "arena" && a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.Wins != b.Wins {
			return a.Wins > b.Wins
		}
		if a.Kills != b.Kills {
			return a.Kills > b.Kills
		}
		return a.Handle < b.Handle
	})
	if limit := parseLimit(r, 50); len(ranked) > limit {
		ranked = ranked[:limit]
	}
	writeJSON(w, http.StatusOK, map[string]any{"mode": mode, "players": ranked, "scanned": leaderboardScanLimit})
}

// v4Player returns one player's totals and recent matches by handle.
func (s *Server) v4Player(w http.ResponseWriter, r *http.Request) {
	handle := strings.TrimSpace(r.PathValue("handle"))
	if len(handle) != 12 {
		writeError(w, http.StatusBadRequest, "invalid player handle")
		return
	}
	lines, matches, err := s.v4PlayerLines(r, "all")
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	for _, line := range lines {
		if line.Handle != handle {
			continue
		}
		recent := []model.Match{}
		for _, m := range matches {
			for _, sum := range m.RobotSummaries {
				if sum.PlayerID == line.playerID {
					recent = append(recent, publicMatch(m))
					break
				}
			}
			if len(recent) == 20 {
				break
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"player": line, "recentMatches": recent})
		return
	}
	writeError(w, http.StatusNotFound, "no finished matches for this player yet")
}

// v4Me tells a signed-in player their public handle.
func (s *Server) v4Me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"handle": playerHandle(robotauth.UserID(r.Context()))})
}

func arenaStatsKey(matchID string) string { return "replays/" + matchID + "/v4/arena-stats.json" }

// arenaStatMatches returns endless arena sessions (running, or stopped by a
// restart) with the player stats their worker last saved, so the leaderboard
// counts arena play without waiting for an end that never comes.
func (s *Server) arenaStatMatches(ctx context.Context) []model.Match {
	out := []model.Match{}
	for _, status := range []model.MatchStatus{model.MatchRunning, model.MatchFailed} {
		matches, err := s.store.ListMatches(ctx, status, 100)
		if err != nil {
			continue
		}
		for _, m := range matches {
			if m.Mode != "arena" || m.EngineVersion != 4 {
				continue
			}
			raw, err := s.store.GetReplayObject(ctx, arenaStatsKey(m.MatchID))
			if err != nil {
				continue
			}
			var stats []model.RobotSummary
			if json.Unmarshal([]byte(raw), &stats) != nil {
				continue
			}
			m.RobotSummaries = stats
			out = append(out, m)
		}
	}
	return out
}
