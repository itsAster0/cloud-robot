package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"
	robotauth "github.com/kryxen/cloud-robot/internal/auth"
	"github.com/kryxen/cloud-robot/internal/boxes"
	"github.com/kryxen/cloud-robot/internal/cloud"
	"github.com/kryxen/cloud-robot/internal/engine"
	"github.com/kryxen/cloud-robot/internal/model"
	"github.com/kryxen/cloud-robot/internal/scripts"
)

// Store is the persistence surface the API needs. The cloud.Store
// implementation talks to Floci/AWS-compatible services; tests supply fakes.
type Store interface {
	Ready(ctx context.Context) error
	Status() map[string]string
	PutMatch(ctx context.Context, match model.Match) error
	GetMatch(ctx context.Context, matchID string) (model.Match, error)
	ListMatches(ctx context.Context, status model.MatchStatus, limit int) ([]model.Match, error)
	PutPlayerStats(ctx context.Context, stats model.PlayerStats) error
	GetPlayerStats(ctx context.Context, playerID string) (model.PlayerStats, error)
	ListPlayerStats(ctx context.Context, limit int) ([]model.PlayerStats, error)
	PutReplay(ctx context.Context, key string, events []model.MatchEvent) error
	GetReplay(ctx context.Context, key string) ([]model.MatchEvent, error)
	PutBox(ctx context.Context, userID string, box model.BoxRecord) error
	GetBox(ctx context.Context, userID string) (model.BoxRecord, error)
	PutScript(ctx context.Context, key, source string) error
	GetScript(ctx context.Context, key string) (string, error)
	ListScriptVersions(ctx context.Context, boxID string, limit int) ([]model.ScriptVersion, error)
	PutAgentCredential(ctx context.Context, credential cloud.AgentCredential) error
	GetAgentCredential(ctx context.Context, robotID string) (cloud.AgentCredential, error)
	EnqueueMatch(ctx context.Context, matchID string) error
	ReceiveJob(ctx context.Context) (cloud.Job, bool, error)
	DeleteJob(ctx context.Context, receiptHandle string) error
}

// IdentityVerifier authenticates requests and places the user ID in context.
// The WorkOS verifier implements it; tests install a fake.
type IdentityVerifier interface {
	Verify(ctx context.Context, authorization string) (context.Context, error)
}

type Server struct {
	store  Store
	hub    *Hub
	agents *AgentManager
	auth   IdentityVerifier
	boxes  boxes.Provisioner
	mu     sync.Mutex
	queue  *matchQueue
	// arenas maps running matches to their engine so HTTP handlers can request
	// mid-match actions (withdrawal) without owning tick state.
	arenas map[string]*engine.Arena
	rateMu sync.Mutex
	rates  map[string]time.Time
}

func NewServer(store Store, provisioner boxes.Provisioner) *Server {
	return &Server{store: store, boxes: provisioner, hub: NewHub(), agents: NewAgentManager(), auth: robotauth.NewVerifier(), queue: newMatchQueue(), arenas: map[string]*engine.Arena{}, rates: map[string]time.Time{}}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /readyz", s.ready)
	mux.HandleFunc("GET /api/cloud/status", s.cloudStatus)
	mux.Handle("GET /api/admin/status", s.requireUser(s.requireAdmin(http.HandlerFunc(s.adminStatus))))
	mux.Handle("POST /api/me/box", s.requireUser(http.HandlerFunc(s.ensureBox)))
	mux.Handle("GET /api/me/box", s.requireUser(http.HandlerFunc(s.getBox)))
	mux.Handle("GET /api/me/box/main.lua", s.requireUser(http.HandlerFunc(s.getBoxMain)))
	mux.Handle("PUT /api/me/box/main.lua", s.requireUser(http.HandlerFunc(s.deployBoxMain)))
	mux.Handle("GET /api/me/box/scripts", s.requireUser(http.HandlerFunc(s.listScriptVersions)))
	mux.Handle("POST /api/me/box/scripts/{versionID}/restore", s.requireUser(http.HandlerFunc(s.restoreScriptVersion)))
	mux.HandleFunc("GET /api/scripts", s.listScripts)
	mux.Handle("PUT /api/me/box/ssh-key", s.requireUser(http.HandlerFunc(s.setBoxKey)))
	mux.Handle("POST /api/me/box/restart", s.requireUser(http.HandlerFunc(s.restartBox)))
	mux.Handle("POST /api/me/box/release", s.requireUser(http.HandlerFunc(s.releaseBox)))
	mux.Handle("POST /api/matches", s.requireUser(s.rateLimit("create-match", http.HandlerFunc(s.createMatch))))
	mux.HandleFunc("GET /api/matches", s.listMatches)
	mux.HandleFunc("GET /api/matches/{matchID}", s.getMatch)
	mux.HandleFunc("GET /api/matches/{matchID}/replay", s.getReplay)
	mux.HandleFunc("GET /api/profiles/{handle}", s.getProfile)
	mux.HandleFunc("GET /api/leaderboard", s.getLeaderboard)
	mux.Handle("GET /api/queue", s.requireUser(http.HandlerFunc(s.queueStatus)))
	mux.Handle("POST /api/queue", s.requireUser(s.rateLimit("queue", http.HandlerFunc(s.joinQueue))))
	mux.Handle("DELETE /api/queue", s.requireUser(http.HandlerFunc(s.leaveQueue)))
	mux.Handle("POST /api/matches/{matchID}/robots", s.requireUser(http.HandlerFunc(s.submitRobot)))
	mux.Handle("DELETE /api/matches/{matchID}/robots", s.requireUser(http.HandlerFunc(s.withdrawRobot)))
	mux.Handle("POST /api/matches/{matchID}/start", s.requireUser(http.HandlerFunc(s.startMatch)))
	mux.Handle("POST /api/matches/{matchID}/withdraw", s.requireUser(http.HandlerFunc(s.withdrawFromMatch)))
	mux.HandleFunc("GET /ws/matches/{matchID}", s.watchMatch)
	mux.HandleFunc("GET /agent/connect/{robotID}", s.connectAgent)
	return withRequestLogging(mux)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Ready(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) cloudStatus(w http.ResponseWriter, r *http.Request) {
	status := s.store.Status()
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Ready(ctx); err != nil {
		status["status"] = "unavailable"
		status["error"] = err.Error()
	} else {
		status["status"] = "ready"
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) adminStatus(w http.ResponseWriter, r *http.Request) {
	matches, err := s.store.ListMatches(r.Context(), "", 100)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	counts := map[model.MatchStatus]int{}
	for _, match := range matches {
		counts[match.Status]++
	}
	s.queue.mu.Lock()
	queued := 0
	for _, users := range s.queue.waiting {
		queued += len(users)
	}
	s.queue.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"matches": counts, "queueDepth": queued, "viewers": s.hub.ViewerCounts(), "agents": s.agents.Count(), "cloud": s.store.Status()})
}

func (s *Server) createMatch(w http.ResponseWriter, r *http.Request) {
	if strings.EqualFold(os.Getenv("MAINTENANCE_MODE"), "true") {
		writeError(w, http.StatusServiceUnavailable, "maintenance mode: new matches are disabled")
		return
	}
	now := time.Now().UTC()
	input := struct {
		Mode            string  `json:"mode"`
		MapID           string  `json:"mapId"`
		ArenaWidth      float64 `json:"arenaWidth"`
		ArenaHeight     float64 `json:"arenaHeight"`
		Practice        bool    `json:"practice"`
		Bots            int     `json:"bots"`
		BotDifficulty   string  `json:"botDifficulty"`
		BotPersonality  string  `json:"botPersonality"`
		FriendlyFire    bool    `json:"friendlyFire"`
		RegenPerTick    int     `json:"regenPerTick"`
		RegenDelayTicks int     `json:"regenDelayTicks"`
		RammingDamage   bool    `json:"rammingDamage"`
	}{}
	if r.ContentLength != 0 {
		if err := decodeJSON(w, r, &input); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if input.Mode == "" {
		input.Mode = "duel"
	}
	if input.Mode != "duel" && input.Mode != "squad" && input.Mode != "solo" {
		writeError(w, http.StatusBadRequest, "mode must be duel, squad, or solo")
		return
	}
	if input.MapID == "" {
		if input.Mode == "squad" {
			input.MapID = "corridors"
		} else {
			// Procedural cover walls by default: duels and solo runs spawn on
			// a seed-stable bunker layout unless a mapId is given.
			input.MapID = "random-bunkers"
		}
	}
	selected, ok := engine.StarterMaps()[input.MapID]
	if !ok {
		if _, random := engine.ProceduralMapStyles[input.MapID]; !random {
			writeError(w, http.StatusBadRequest, "unknown mapId")
			return
		}
		// Procedural maps generate in the worker from the persisted match seed;
		// only the default arena size is needed here.
		selected = engine.DefaultMap(1200, 750)
	}
	if input.ArenaWidth == 0 {
		input.ArenaWidth = selected.Width
	}
	if input.ArenaHeight == 0 {
		input.ArenaHeight = selected.Height
	}
	if input.ArenaWidth < 400 || input.ArenaWidth > 2000 || input.ArenaHeight < 300 || input.ArenaHeight > 1400 {
		writeError(w, http.StatusBadRequest, "arena size must be within 400x300 and 2000x1400")
		return
	}
	// Squad always fields the full 5v5 bot roster at creation; the bots count
	// input only applies to duel and solo modes.
	if input.Mode == "squad" {
		input.Bots = 0
	}
	if input.Bots < 0 || input.Bots > 7 {
		writeError(w, http.StatusBadRequest, "bots must be between 0 and 7")
		return
	}
	if input.BotDifficulty == "" {
		input.BotDifficulty = "fighter"
	}
	if input.BotDifficulty != "dummy" && input.BotDifficulty != "rookie" && input.BotDifficulty != "fighter" && input.BotDifficulty != "sharpshooter" {
		writeError(w, http.StatusBadRequest, "invalid botDifficulty")
		return
	}
	if input.BotPersonality == "" {
		input.BotPersonality = "aggressive"
	}
	if input.BotPersonality != "aggressive" && input.BotPersonality != "evasive" && input.BotPersonality != "camper" && input.BotPersonality != "mixed" {
		writeError(w, http.StatusBadRequest, "botPersonality must be aggressive, evasive, camper, or mixed")
		return
	}
	if input.RegenPerTick < 0 || input.RegenPerTick > 10 {
		writeError(w, http.StatusBadRequest, "regenPerTick must be between 0 and 10")
		return
	}
	if input.RegenDelayTicks < 0 || input.RegenDelayTicks > 600 {
		writeError(w, http.StatusBadRequest, "regenDelayTicks must be between 0 and 600")
		return
	}
	// Squad and solo lobbies are unranked practice: they fight bots and never
	// touch player ratings, so the duel ladder stays clean for Review 1.
	match := model.Match{MatchID: uuid.NewString(), OwnerID: robotauth.UserID(r.Context()), Status: model.MatchLobby, Mode: input.Mode, MapID: input.MapID, ArenaWidth: input.ArenaWidth, ArenaHeight: input.ArenaHeight, Practice: input.Practice || input.Mode != "duel", Seed: now.UnixNano(), TickRate: 10, FriendlyFire: input.FriendlyFire, RegenPerTick: input.RegenPerTick, RegenDelayTicks: input.RegenDelayTicks, RammingDamage: input.RammingDamage, BotPersonality: input.BotPersonality, CreatedAt: now, Robots: []model.RobotSubmission{}}
	// Squad always opens with a full 5v5 bot roster; every human registration
	// later displaces one bot from the team it joins. Solo seeds one bot per
	// requested opponent, each on its own team for a free-for-all.
	botCount := input.Bots
	if input.Mode == "squad" {
		botCount = 10
	}
	name := map[string]string{"dummy": "Dummy", "rookie": "Rookie", "fighter": "Fighter", "sharpshooter": "Sharpshooter"}[input.BotDifficulty]
	for index := 0; index < botCount; index++ {
		team := "blue"
		switch {
		case input.Mode == "squad":
			if index < 5 {
				team = "red"
			}
		case input.Mode == "solo":
			team = fmt.Sprintf("solo-%02d", index+1)
		default:
			if index%2 == 1 {
				team = "red"
			}
		}
		match.Robots = append(match.Robots, model.RobotSubmission{RobotID: uuid.NewString(), DisplayName: fmt.Sprintf("%s Bot %d", name, index+1), Team: team, Runtime: "server-bot", StartCommand: "bot:" + input.BotDifficulty, Bot: true, SubmittedAt: now})
	}
	if err := s.store.PutMatch(r.Context(), match); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, match)
}

func (s *Server) getMatch(w http.ResponseWriter, r *http.Request) {
	match, err := s.store.GetMatch(r.Context(), r.PathValue("matchID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "match not found")
		return
	}
	writeJSON(w, http.StatusOK, match)
}

type robotRequest struct {
	DisplayName  string `json:"displayName"`
	Team         string `json:"team"`
	StartCommand string `json:"startCommand"`
	Runtime      string `json:"runtime"`
}

type agentEnrollment struct {
	RobotID string `json:"robotId"`
	Status  string `json:"status"`
}

type robotResponse struct {
	Match model.Match     `json:"match"`
	Agent agentEnrollment `json:"agent"`
}

func (s *Server) submitRobot(w http.ResponseWriter, r *http.Request) {
	var input robotRequest
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	if len(input.DisplayName) < 1 || len(input.DisplayName) > 32 {
		writeError(w, http.StatusBadRequest, "displayName must be 1 to 32 characters")
		return
	}
	input.StartCommand = strings.TrimSpace(input.StartCommand)
	if input.StartCommand == "" || len(input.StartCommand) > 256 {
		writeError(w, http.StatusBadRequest, "startCommand must be 1 to 256 characters")
		return
	}
	if input.Runtime == "" {
		input.Runtime = "lua5.4"
	}
	if input.Runtime != "lua5.4" {
		writeError(w, http.StatusBadRequest, "Phase 1 runtime must be lua5.4")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	userID := robotauth.UserID(r.Context())
	boxID := boxes.IDForUser(userID)
	box, err := s.boxes.Status(r.Context(), boxID)
	if err != nil || box.Status != "running" {
		writeError(w, http.StatusConflict, "provision a running SSH box before registering a robot")
		return
	}
	if box.KeyFingerprint == "" {
		writeError(w, http.StatusConflict, "add an SSH public key before registering a robot")
		return
	}
	// The supervisor copy of the markers (agent.json) never clears on its
	// own, so the store decides whether the box is truly bound. A marker
	// whose match is gone, already over, or that no longer lists this box's
	// robot is stale and must not block registration; ConfigureAgent below
	// rewrites agent.json with the new match either way.
	s.overlayBoxMatchState(r.Context(), userID, &box)
	if box.ActiveMatchID != "" {
		if active, activeErr := s.store.GetMatch(r.Context(), box.ActiveMatchID); activeErr == nil && boxOwnsRobot(active, boxID) {
			writeError(w, http.StatusConflict, "box already has an active robot")
			return
		}
		box.ActiveRobotID, box.ActiveMatchID = "", ""
		_ = s.store.PutBox(r.Context(), userID, box)
	}
	match, err := s.store.GetMatch(r.Context(), r.PathValue("matchID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "match not found")
		return
	}
	if match.Status != model.MatchLobby {
		writeError(w, http.StatusConflict, "match is not accepting robots")
		return
	}
	for _, robot := range match.Robots {
		if robot.OwnerBoxID == boxID {
			writeError(w, http.StatusConflict, "user already registered a robot in this match")
			return
		}
	}
	rosterCap := 8
	if match.Mode == "squad" {
		rosterCap = 10
	}
	var displacedBot *model.RobotSubmission
	switch match.Mode {
	case "squad":
		if input.Team != "red" && input.Team != "blue" {
			writeError(w, http.StatusBadRequest, "team must be red or blue")
			return
		}
		// Squad joins displace the newest bot on the chosen team so the human
		// takes its slot; teams hold five robots no matter the mix.
		for i := len(match.Robots) - 1; i >= 0; i-- {
			if match.Robots[i].Bot && match.Robots[i].Team == input.Team {
				displaced := match.Robots[i]
				displacedBot = &displaced
				match.Robots = append(match.Robots[:i], match.Robots[i+1:]...)
				break
			}
		}
		if countTeamRobots(match.Robots, input.Team) >= 5 {
			writeError(w, http.StatusConflict, "team is full: five robots per side")
			return
		}
	case "solo":
		// Free-for-all: the server assigns each robot its own team and the
		// requested team is ignored.
		input.Team = fmt.Sprintf("solo-%02d", len(match.Robots)+1)
	default:
		if input.Team != "red" && input.Team != "blue" {
			writeError(w, http.StatusBadRequest, "team must be red or blue")
			return
		}
	}
	// Squad displacement already made room, so the cap is checked after mode
	// handling: a 10-robot squad roster still accepts a player who replaces a
	// bot, but not one who would exceed the ten-robot arena.
	if len(match.Robots) >= rosterCap {
		writeError(w, http.StatusConflict, "match roster is full")
		return
	}
	source, err := s.boxes.ReadMain(r.Context(), boxID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "read /workspace/main.lua: "+err.Error())
		return
	}

	robotID := uuid.NewString()
	token, err := randomToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "generate agent token")
		return
	}
	hash := sha256.Sum256([]byte(token))
	if err := s.store.PutAgentCredential(r.Context(), cloud.AgentCredential{RobotID: robotID, MatchID: match.MatchID, TokenHash: base64.RawURLEncoding.EncodeToString(hash[:])}); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	scriptKey := fmt.Sprintf("scripts/%s/%s/%s/main.lua", boxID, match.MatchID, robotID)
	if err := s.store.PutScript(r.Context(), scriptKey, source); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	versionKey := fmt.Sprintf("versions/%s/%s/main.lua", boxID, uuid.NewString())
	if err := s.store.PutScript(r.Context(), versionKey, source); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	match.Robots = append(match.Robots, model.RobotSubmission{
		RobotID: robotID, DisplayName: input.DisplayName, Team: input.Team,
		PlayerID: userID, OwnerBoxID: boxID, ScriptObjectKey: scriptKey, StartCommand: input.StartCommand, Runtime: input.Runtime, SubmittedAt: time.Now().UTC(),
	})
	if err := s.store.PutMatch(r.Context(), match); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	agentBaseURL := envOr("ROBOT_AGENT_BASE_URL", "ws://host.docker.internal:8080")
	configured, err := s.boxes.ConfigureAgent(r.Context(), boxID, boxes.AgentConfig{RobotID: robotID, MatchID: match.MatchID, URL: strings.TrimRight(agentBaseURL, "/") + "/agent/connect/" + robotID, Token: token, StartCommand: input.StartCommand})
	if err != nil {
		// Roll the roster back so a broken provisioner never leaves a phantom
		// robot in a match that agents cannot join. A squad join also returns
		// the bot it displaced.
		match.Robots = match.Robots[:len(match.Robots)-1]
		if displacedBot != nil {
			match.Robots = append(match.Robots, *displacedBot)
		}
		if revertErr := s.store.PutMatch(r.Context(), match); revertErr != nil {
			slog.Error("revert robot after agent configure failure", "matchId", match.MatchID, "error", revertErr)
		}
		writeError(w, http.StatusBadGateway, "configure box agent: "+err.Error())
		return
	}
	_ = s.store.PutBox(r.Context(), userID, configured)
	s.hub.Publish(match.MatchID, map[string]any{"type": "match_state", "version": 1, "match": match})
	writeJSON(w, http.StatusCreated, robotResponse{Match: match, Agent: agentEnrollment{RobotID: robotID, Status: configured.AgentStatus}})
}

type sshKeyRequest struct {
	PublicKey string `json:"publicKey"`
}

// boxOwnsRobot reports whether the box still owns a registration in the match.
// A live match marker without a matching roster entry is stale supervisor
// state, not a real commitment.
func boxOwnsRobot(match model.Match, boxID string) bool {
	for _, robot := range match.Robots {
		if robot.OwnerBoxID == boxID {
			return true
		}
	}
	return false
}

func countTeamRobots(robots []model.RobotSubmission, team string) int {
	count := 0
	for _, robot := range robots {
		if robot.Team == team {
			count++
		}
	}
	return count
}

// withdrawRobot removes the caller's robot from a lobby match so the box can
// be reused elsewhere or re-registered with a different team. Removing robots
// from running matches is rejected: the authoritative worker owns that state.
func (s *Server) withdrawRobot(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	userID := robotauth.UserID(r.Context())
	boxID := boxes.IDForUser(userID)
	match, err := s.store.GetMatch(r.Context(), r.PathValue("matchID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "match not found")
		return
	}
	if match.Status != model.MatchLobby {
		writeError(w, http.StatusConflict, "robots can only withdraw before the match starts")
		return
	}
	index := -1
	for i, robot := range match.Robots {
		if robot.OwnerBoxID == boxID {
			index = i
			break
		}
	}
	if index < 0 {
		writeError(w, http.StatusNotFound, "no registered robot for this account in the match")
		return
	}
	match.Robots = append(match.Robots[:index], match.Robots[index+1:]...)
	if err := s.store.PutMatch(r.Context(), match); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	box, boxErr := s.boxes.Status(r.Context(), boxID)
	if boxErr == nil {
		box.ActiveRobotID, box.ActiveMatchID = "", ""
		_ = s.store.PutBox(r.Context(), userID, box)
	}
	s.hub.Publish(match.MatchID, map[string]any{"type": "match_state", "version": 1, "match": match})
	writeJSON(w, http.StatusOK, match)
}

// registerActiveArena exposes the worker's engine to HTTP handlers for the
// lifetime of the match; unregistering happens when runMatch returns.
func (s *Server) registerActiveArena(matchID string, arena *engine.Arena) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.arenas[matchID] = arena
}

func (s *Server) unregisterActiveArena(matchID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.arenas, matchID)
}

// withdrawFromMatch concedes a running match: the caller's robot is destroyed
// at the next engine tick and the remaining robots fight on under normal
// elimination rules. The running state belongs to the worker, so the handler
// only forwards the request to the registered arena.
func (s *Server) withdrawFromMatch(w http.ResponseWriter, r *http.Request) {
	userID := robotauth.UserID(r.Context())
	match, err := s.store.GetMatch(r.Context(), r.PathValue("matchID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "match not found")
		return
	}
	if match.Status != model.MatchRunning {
		writeError(w, http.StatusConflict, "only running matches can be withdrawn")
		return
	}
	boxID := boxes.IDForUser(userID)
	robotID := ""
	for _, robot := range match.Robots {
		if robot.OwnerBoxID == boxID {
			robotID = robot.RobotID
			break
		}
	}
	if robotID == "" {
		writeError(w, http.StatusNotFound, "no registered robot for this account in the match")
		return
	}
	s.mu.Lock()
	arena := s.arenas[match.MatchID]
	s.mu.Unlock()
	if arena == nil || !arena.RequestWithdraw(robotID) {
		writeError(w, http.StatusConflict, "match is not accepting withdrawals")
		return
	}
	s.hub.Publish(match.MatchID, map[string]any{"type": "match_state", "version": 1, "match": match})
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "withdrawing", "robotId": robotID})
}

// releaseBox unbinds the caller's box from its active match so queueing and
// registration work again. It backs the "exit active match" action offered
// when the arena answers "box already has an active robot": a lobby
// registration is dropped, a running match is conceded through the live
// engine, and a binding referencing a vanished or ended match is stale, so
// clearing the markers is the whole release.
func (s *Server) releaseBox(w http.ResponseWriter, r *http.Request) {
	userID := robotauth.UserID(r.Context())
	boxID := boxes.IDForUser(userID)
	box, err := s.boxes.Status(r.Context(), boxID)
	if err != nil || box.Status != "running" {
		writeError(w, http.StatusConflict, "provision a running SSH box before releasing a robot")
		return
	}
	s.overlayBoxMatchState(r.Context(), userID, &box)
	_ = s.store.PutBox(r.Context(), userID, box)
	if box.ActiveMatchID == "" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "idle"})
		return
	}
	match, matchErr := s.store.GetMatch(r.Context(), box.ActiveMatchID)
	if matchErr != nil || (match.Status != model.MatchLobby && match.Status != model.MatchRunning) {
		box.ActiveRobotID, box.ActiveMatchID = "", ""
		_ = s.store.PutBox(r.Context(), userID, box)
		writeJSON(w, http.StatusOK, map[string]string{"status": "released"})
		return
	}
	if match.Status == model.MatchLobby {
		for index, robot := range match.Robots {
			if robot.OwnerBoxID != boxID {
				continue
			}
			match.Robots = append(match.Robots[:index], match.Robots[index+1:]...)
			if err := s.store.PutMatch(r.Context(), match); err != nil {
				writeError(w, http.StatusBadGateway, err.Error())
				return
			}
			s.hub.Publish(match.MatchID, map[string]any{"type": "match_state", "version": 1, "match": match})
			break
		}
		box.ActiveRobotID, box.ActiveMatchID = "", ""
		_ = s.store.PutBox(r.Context(), userID, box)
		writeJSON(w, http.StatusOK, map[string]string{"status": "released"})
		return
	}
	// Running match: the concession applies at the next engine tick and the
	// binding clears when the match resolves by normal elimination rules.
	s.mu.Lock()
	arena := s.arenas[match.MatchID]
	s.mu.Unlock()
	if arena == nil {
		// The worker never picked the match up or died mid-run (stack
		// restart), so it can never resolve on its own. Failing it frees the
		// box instead of blocking the player forever.
		s.failMatch(r.Context(), match, errors.New("match worker unavailable"))
		writeJSON(w, http.StatusOK, map[string]string{"status": "released"})
		return
	}
	if !arena.RequestWithdraw(box.ActiveRobotID) {
		writeError(w, http.StatusConflict, "match is not accepting withdrawals")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "withdrawing", "matchId": match.MatchID})
}

func (s *Server) ensureBox(w http.ResponseWriter, r *http.Request) {
	userID := robotauth.UserID(r.Context())
	box, err := s.boxes.Ensure(r.Context(), boxes.IDForUser(userID))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	s.overlayBoxMatchState(r.Context(), userID, &box)
	if err := s.store.PutBox(r.Context(), userID, box); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, box)
}

func (s *Server) getBox(w http.ResponseWriter, r *http.Request) {
	userID := robotauth.UserID(r.Context())
	box, err := s.boxes.Status(r.Context(), boxes.IDForUser(userID))
	if err != nil {
		writeError(w, http.StatusNotFound, "box not provisioned")
		return
	}
	s.overlayBoxMatchState(r.Context(), userID, &box)
	_ = s.store.PutBox(r.Context(), userID, box)
	writeJSON(w, http.StatusOK, box)
}

// overlayBoxMatchState replaces the supervisor's active robot/match markers
// with the store copy. The supervisor learns markers from agent.json and never
// clears them, so the store — which the API updates on registration,
// withdrawal, and match end — is authoritative for what the browser shows.
// Markers referencing a match that vanished or already ended are cleared so a
// stale binding can never block queueing or offer a doomed withdraw button.
func (s *Server) overlayBoxMatchState(ctx context.Context, userID string, box *model.BoxRecord) {
	stored, err := s.store.GetBox(ctx, userID)
	if err != nil {
		// No stored record means the store no longer knows about any active
		// registration; persisting the supervisor's agent.json markers here
		// would resurrect a binding after emulator state loss.
		box.ActiveRobotID, box.ActiveMatchID = "", ""
		return
	}
	if stored.ActiveMatchID != "" {
		match, matchErr := s.store.GetMatch(ctx, stored.ActiveMatchID)
		if matchErr != nil || match.Status == model.MatchFinished || match.Status == model.MatchFailed {
			stored.ActiveRobotID, stored.ActiveMatchID = "", ""
			_ = s.store.PutBox(ctx, userID, stored)
		}
	}
	box.ActiveRobotID, box.ActiveMatchID = stored.ActiveRobotID, stored.ActiveMatchID
}

// getBoxMain shows the browser the /workspace/main.lua the box supervisor
// would snapshot at registration. Same 16 KiB cap as submitRobot applies.
func (s *Server) getBoxMain(w http.ResponseWriter, r *http.Request) {
	source, err := s.boxes.ReadMain(r.Context(), boxes.IDForUser(robotauth.UserID(r.Context())))
	if err != nil {
		writeError(w, http.StatusBadGateway, "read /workspace/main.lua: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"source": source})
}

func (s *Server) listScripts(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"scripts": scripts.List()})
}

type scriptDeployRequest struct {
	Template string `json:"template"`
}

// deployBoxMain overwrites /workspace/main.lua with a curated demo template.
// The next registration snapshots it unchanged.
func (s *Server) deployBoxMain(w http.ResponseWriter, r *http.Request) {
	var input scriptDeployRequest
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	name := strings.TrimSpace(input.Template)
	source, err := scripts.Get(name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	written, err := s.boxes.WriteMain(r.Context(), boxes.IDForUser(robotauth.UserID(r.Context())), source)
	if err != nil {
		writeError(w, http.StatusBadGateway, "write /workspace/main.lua: "+err.Error())
		return
	}
	boxID := boxes.IDForUser(robotauth.UserID(r.Context()))
	versionKey := fmt.Sprintf("versions/%s/%s/main.lua", boxID, uuid.NewString())
	if err := s.store.PutScript(r.Context(), versionKey, written); err != nil {
		writeError(w, http.StatusBadGateway, "store script version: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"template": name, "source": written})
}

func (s *Server) listScriptVersions(w http.ResponseWriter, r *http.Request) {
	boxID := boxes.IDForUser(robotauth.UserID(r.Context()))
	versions, err := s.store.ListScriptVersions(r.Context(), boxID, parseLimit(r, 50))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": versions})
}

func (s *Server) restoreScriptVersion(w http.ResponseWriter, r *http.Request) {
	boxID := boxes.IDForUser(robotauth.UserID(r.Context()))
	versionID := strings.TrimSpace(r.PathValue("versionID"))
	if _, err := uuid.Parse(versionID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid script version")
		return
	}
	versions, err := s.store.ListScriptVersions(r.Context(), boxID, 100)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	key := ""
	for _, version := range versions {
		if version.VersionID == versionID {
			key = version.ObjectKey
			break
		}
	}
	if key == "" {
		writeError(w, http.StatusNotFound, "script version not found")
		return
	}
	source, err := s.store.GetScript(r.Context(), key)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	written, err := s.boxes.WriteMain(r.Context(), boxID, source)
	if err != nil {
		writeError(w, http.StatusBadGateway, "write /workspace/main.lua: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"versionId": versionID, "source": written})
}

func (s *Server) setBoxKey(w http.ResponseWriter, r *http.Request) {
	var input sshKeyRequest
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	key, fingerprint, err := boxes.ValidatePublicKey(input.PublicKey)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	userID := robotauth.UserID(r.Context())
	box, err := s.boxes.SetKey(r.Context(), boxes.IDForUser(userID), key)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	box.KeyFingerprint = fingerprint
	s.overlayBoxMatchState(r.Context(), userID, &box)
	_ = s.store.PutBox(r.Context(), userID, box)
	writeJSON(w, http.StatusOK, box)
}

func (s *Server) restartBox(w http.ResponseWriter, r *http.Request) {
	userID := robotauth.UserID(r.Context())
	box, err := s.boxes.Restart(r.Context(), boxes.IDForUser(userID))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	s.overlayBoxMatchState(r.Context(), userID, &box)
	_ = s.store.PutBox(r.Context(), userID, box)
	writeJSON(w, http.StatusOK, box)
}

func (s *Server) startMatch(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	match, err := s.store.GetMatch(r.Context(), r.PathValue("matchID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "match not found")
		return
	}
	if match.Status != model.MatchLobby {
		writeError(w, http.StatusConflict, "match already started")
		return
	}
	if userID := robotauth.UserID(r.Context()); match.OwnerID != "" && match.OwnerID != userID {
		writeError(w, http.StatusForbidden, "only the match owner can start the match")
		return
	}
	started, err := s.beginMatch(r.Context(), match)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, started)
}

// beginMatch validates readiness, queues the match, and publishes the state
// change. It is shared by the owner's manual start and the automatic start
// that fires when every registered agent has connected.
func (s *Server) beginMatch(ctx context.Context, match model.Match) (model.Match, error) {
	states := make([]engine.RobotState, 0, len(match.Robots))
	for _, robot := range match.Robots {
		states = append(states, engine.RobotState{RobotID: robot.RobotID, Name: robot.DisplayName, Team: robot.Team})
	}
	if err := engine.ValidateTeamsForMode(match.Mode, states); err != nil {
		return match, err
	}
	for _, robot := range match.Robots {
		if robot.Bot {
			continue
		}
		if !s.agents.Connected(robot.RobotID) {
			return match, errors.New("all robot agents must be connected before match start")
		}
	}
	match.Status = model.MatchQueued
	if err := s.store.PutMatch(ctx, match); err != nil {
		match.Status = model.MatchLobby
		return match, err
	}
	if err := s.store.EnqueueMatch(ctx, match.MatchID); err != nil {
		match.Status, match.Error = model.MatchFailed, err.Error()
		_ = s.store.PutMatch(ctx, match)
		s.releaseBoxes(ctx, match)
		return match, err
	}
	s.hub.Publish(match.MatchID, map[string]any{"type": "match_state", "version": 1, "match": match})
	return match, nil
}

// autoStartIfReady removes the manual start step for the demo: once both
// teams are registered and every agent is connected, the match queues itself.
// Squad lobbies additionally wait for a human on each side so friends can
// fill both rosters before the bots get overrun; the owner can always force
// the start from the UI.
func (s *Server) autoStartIfReady(matchID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	match, err := s.store.GetMatch(ctx, matchID)
	if err != nil || match.Status != model.MatchLobby {
		return
	}
	if match.Mode == "squad" {
		humanTeams := map[string]bool{}
		for _, robot := range match.Robots {
			if !robot.Bot {
				humanTeams[robot.Team] = true
			}
		}
		if len(humanTeams) < 2 {
			return
		}
	}
	if _, err := s.beginMatch(ctx, match); err != nil {
		slog.Debug("auto start not ready", "matchId", matchID, "reason", err)
	}
}

func (s *Server) connectAgent(w http.ResponseWriter, r *http.Request) {
	robotID := r.PathValue("robotID")
	credential, err := s.store.GetAgentCredential(r.Context(), robotID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid robot credential")
		return
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	hash := sha256.Sum256([]byte(token))
	expected, err := base64.RawURLEncoding.DecodeString(credential.TokenHash)
	if err != nil || len(expected) != len(hash) || subtle.ConstantTimeCompare(expected, hash[:]) != 1 {
		writeError(w, http.StatusUnauthorized, "invalid robot credential")
		return
	}
	connection, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"localhost:*", "127.0.0.1:*"},
		Subprotocols:   []string{"robot-arena.v1"},
	})
	if err != nil {
		return
	}
	session := newAgentSession(robotID, credential.MatchID, connection)
	s.agents.Attach(robotID, session)
	s.hub.Publish(credential.MatchID, map[string]any{"type": "agent_status", "version": 1, "robotId": robotID, "connected": true})
	go s.autoStartIfReady(credential.MatchID)
	defer func() {
		s.agents.Detach(robotID, session)
		s.hub.Publish(credential.MatchID, map[string]any{"type": "agent_status", "version": 1, "robotId": robotID, "connected": false})
		connection.CloseNow()
	}()
	_ = session.readLoop(r.Context())
}

func randomToken() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func (s *Server) watchMatch(w http.ResponseWriter, r *http.Request) {
	connection, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"localhost:*", "127.0.0.1:*"}})
	if err != nil {
		return
	}
	defer connection.CloseNow()
	matchID := r.PathValue("matchID")
	match, err := s.store.GetMatch(r.Context(), matchID)
	if err != nil {
		_ = connection.Close(websocket.StatusPolicyViolation, "match not found")
		return
	}
	robotIDs := make([]string, 0, len(match.Robots))
	for _, robot := range match.Robots {
		robotIDs = append(robotIDs, robot.RobotID)
	}
	initial := map[string]any{"type": "agent_status_snapshot", "version": 1, "agents": s.agents.Status(robotIDs)}
	initialContext, cancelInitial := context.WithTimeout(r.Context(), 2*time.Second)
	if err := wsjson.Write(initialContext, connection, initial); err != nil {
		cancelInitial()
		return
	}
	cancelInitial()
	channel, unsubscribe := s.hub.Subscribe(matchID)
	defer unsubscribe()
	sentArenaLayout := false
	for {
		select {
		case <-r.Context().Done():
			connection.Close(websocket.StatusNormalClosure, "client disconnected")
			return
		case event, ok := <-channel:
			if !ok {
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			if !sentArenaLayout && len(event.layout) > 0 {
				if err := connection.Write(ctx, websocket.MessageText, event.layout); err != nil {
					cancel()
					return
				}
				sentArenaLayout = true
			}
			err := connection.Write(ctx, websocket.MessageText, event.payload)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 20*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}

func (s *Server) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, err := s.auth.Verify(r.Context(), r.Header.Get("Authorization"))
		if err != nil {
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := robotauth.UserID(r.Context())
		for _, allowed := range strings.Split(os.Getenv("ADMIN_USER_IDS"), ",") {
			if strings.TrimSpace(allowed) == userID {
				next.ServeHTTP(w, r)
				return
			}
		}
		writeError(w, http.StatusForbidden, "admin access required")
	})
}

func (s *Server) rateLimit(scope string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := scope + ":" + robotauth.UserID(r.Context())
		now := time.Now()
		s.rateMu.Lock()
		last := s.rates[key]
		if now.Sub(last) < 250*time.Millisecond {
			s.rateMu.Unlock()
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusTooManyRequests, "request rate limit exceeded")
			return
		}
		s.rates[key] = now
		s.rateMu.Unlock()
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func withRequestLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		slog.Info("http request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(started))
	})
}
