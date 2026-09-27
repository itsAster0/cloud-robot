package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kryxen/cloud-robot/internal/model"
)

func TestV4LeaderboardRanksPlayersWithoutLeakingAccountIDs(t *testing.T) {
	h := newHarness(t)
	at := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	h.store.matches["a1"] = model.Match{MatchID: "a1", EngineVersion: 4, Mode: "arena", Status: model.MatchFinished, CreatedAt: at, WinnerTeam: "t-ada",
		RobotSummaries: []model.RobotSummary{
			{RobotID: "r1", Name: "Ada", Team: "t-ada", PlayerID: "user-ada", Kills: 5, Deaths: 1, Score: 900},
			{RobotID: "r2", Name: "Bo", Team: "t-bo", PlayerID: "user-bo", Kills: 7, Deaths: 4, Score: 700},
			{RobotID: "bot-1", Name: "Game Bot 1", Team: "bot-1", Bot: true, Kills: 20, Score: 2000},
		}}
	h.store.matches["d1"] = model.Match{MatchID: "d1", EngineVersion: 4, Mode: "quick-duel", Status: model.MatchFinished, CreatedAt: at.Add(time.Hour), WinnerTeam: "t-bo",
		RobotSummaries: []model.RobotSummary{{RobotID: "r3", Name: "Bo", Team: "t-bo", PlayerID: "user-bo", Kills: 1}}}

	response, payload := h.request(t, http.MethodGet, "/api/v4/leaderboard?mode=arena", "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("leaderboard: %d %v", response.StatusCode, payload)
	}
	raw, _ := json.Marshal(payload)
	if strings.Contains(string(raw), "user-ada") || strings.Contains(string(raw), "Game Bot") {
		t.Fatalf("leaderboard leaks account IDs or lists bots: %s", raw)
	}
	players := payload["players"].([]any)
	if len(players) != 2 || players[0].(map[string]any)["name"] != "Ada" {
		t.Fatalf("arena ranks by score: %s", raw)
	}
	_, all := h.request(t, http.MethodGet, "/api/v4/leaderboard", "")
	first := all["players"].([]any)[0].(map[string]any)
	if first["name"] != "Bo" || first["matches"].(float64) != 2 {
		t.Fatalf("all modes rank by wins then kills: %v", all)
	}
	handle := first["handle"].(string)
	response, player := h.request(t, http.MethodGet, "/api/v4/players/"+handle, "")
	if response.StatusCode != http.StatusOK || len(player["recentMatches"].([]any)) != 2 {
		t.Fatalf("player page: %d %v", response.StatusCode, player)
	}
	if response, _ := h.request(t, http.MethodGet, "/api/v4/players/000000000000", ""); response.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown handle: %d", response.StatusCode)
	}
}
