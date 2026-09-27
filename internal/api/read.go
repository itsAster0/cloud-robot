package api

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/kryxen/cloud-robot/internal/model"
)

func (s *Server) listMatches(w http.ResponseWriter, r *http.Request) {
	var status model.MatchStatus
	switch value := r.URL.Query().Get("status"); value {
	case "":
	case string(model.MatchLobby), string(model.MatchQueued), string(model.MatchRunning), string(model.MatchFinished), string(model.MatchFailed):
		status = model.MatchStatus(value)
	default:
		writeError(w, http.StatusBadRequest, "invalid match status")
		return
	}
	limit := parseLimit(r, 50)
	matches, err := s.store.ListMatches(r.Context(), status, limit)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	// Viewer counts must be read per request: Handler() wires routes once at
	// startup, so hub state captured there would never refresh.
	viewers := s.hub.ViewerCounts()
	entries := make([]matchListEntry, 0, len(matches))
	for _, match := range matches {
		entries = append(entries, matchListEntry{Match: publicMatch(match), Viewers: viewers[match.MatchID]})
	}
	writeJSON(w, http.StatusOK, map[string]any{"matches": entries})
}

type matchListEntry struct {
	model.Match
	Viewers int `json:"viewers"`
}

func (s *Server) getReplay(w http.ResponseWriter, r *http.Request) {
	match, err := s.store.GetMatch(r.Context(), r.PathValue("matchID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "match not found")
		return
	}
	if match.ReplayObjectKey == "" {
		writeJSON(w, http.StatusOK, map[string]any{"matchId": match.MatchID, "events": match.EventSummary, "complete": false})
		return
	}
	events, err := s.store.GetReplay(r.Context(), match.ReplayObjectKey)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"matchId": match.MatchID, "events": events, "complete": true})
}

func (s *Server) getProfile(w http.ResponseWriter, r *http.Request) {
	handle := strings.TrimSpace(r.PathValue("handle"))
	if handle == "" || len(handle) > 64 {
		writeError(w, http.StatusBadRequest, "handle must be 1 to 64 characters")
		return
	}
	stats, err := s.store.GetPlayerStats(r.Context(), handle)
	if err != nil {
		writeError(w, http.StatusNotFound, "profile not found")
		return
	}
	matches, _ := s.store.ListMatches(r.Context(), "", 100)
	recent := make([]model.Match, 0, 10)
	for _, match := range matches {
		for _, robot := range match.Robots {
			if robot.PlayerID == stats.PlayerID {
				recent = append(recent, publicMatch(match))
				break
			}
		}
		if len(recent) == 10 {
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"profile": stats, "recentMatches": recent})
}

func (s *Server) getLeaderboard(w http.ResponseWriter, r *http.Request) {
	mode := strings.TrimSpace(r.URL.Query().Get("mode"))
	if mode == "" {
		mode = "duel"
	}
	players, err := s.store.ListPlayerStats(r.Context(), parseLimit(r, 50))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	sort.Slice(players, func(i, j int) bool {
		left, right := players[i].Ratings[mode], players[j].Ratings[mode]
		if left == right {
			return players[i].Wins > players[j].Wins
		}
		return left > right
	})
	type entry struct {
		Rank   int               `json:"rank"`
		Player model.PlayerStats `json:"player"`
		Rating int               `json:"rating"`
	}
	entries := make([]entry, 0, len(players))
	for _, player := range players {
		entries = append(entries, entry{Rank: len(entries) + 1, Player: player, Rating: player.Ratings[mode]})
	}
	writeJSON(w, http.StatusOK, map[string]any{"mode": mode, "entries": entries})
}

func parseLimit(r *http.Request, fallback int) int {
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit < 1 {
		return fallback
	}
	if limit > 100 {
		return 100
	}
	return limit
}

// publicMatch never exposes a participant's private build or workspace metadata.
// Copy the roster before redacting so persistence retains the registration.
func publicMatch(m model.Match) model.Match {
	// Always an array: bot-only matches have no registrations, and a JSON
	// null here broke every client that lists matches.
	m.Robots = append(make([]model.RobotSubmission, 0, len(m.Robots)), m.Robots...)
	if m.EngineVersion != 4 {
		return m
	}
	for i := range m.Robots {
		m.Robots[i].Loadout = nil
		m.Robots[i].ScriptObjectKey = ""
		m.Robots[i].OwnerBoxID = ""
		m.Robots[i].StartCommand = ""
	}
	return m
}
