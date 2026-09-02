package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	robotauth "github.com/kryxen/cloud-robot/internal/auth"
	"github.com/kryxen/cloud-robot/internal/boxes"
	"github.com/kryxen/cloud-robot/internal/cloud"
	"github.com/kryxen/cloud-robot/internal/model"
)

type queueRequest struct {
	DisplayName  string `json:"displayName"`
	Mode         string `json:"mode"`
	Runtime      string `json:"runtime"`
	StartCommand string `json:"startCommand"`
}

type queueEntry struct {
	UserID   string          `json:"-"`
	Box      model.BoxRecord `json:"-"`
	Source   string          `json:"-"`
	Request  queueRequest    `json:"-"`
	JoinedAt time.Time       `json:"joinedAt"`
	Status   string          `json:"status"`
	MatchID  string          `json:"matchId,omitempty"`
}

type matchQueue struct {
	mu      sync.Mutex
	entries map[string]*queueEntry
	waiting map[string][]string
}

func newMatchQueue() *matchQueue {
	return &matchQueue{entries: map[string]*queueEntry{}, waiting: map[string][]string{}}
}

func (s *Server) joinQueue(w http.ResponseWriter, r *http.Request) {
	if strings.EqualFold(envOr("MAINTENANCE_MODE", "false"), "true") {
		writeError(w, http.StatusServiceUnavailable, "maintenance mode: queue is closed")
		return
	}
	var input queueRequest
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.StartCommand = strings.TrimSpace(input.StartCommand)
	input.Mode = strings.TrimSpace(input.Mode)
	if input.Mode == "" {
		input.Mode = "duel"
	}
	if input.Runtime == "" {
		input.Runtime = "lua5.4"
	}
	if len(input.DisplayName) < 1 || len(input.DisplayName) > 32 {
		writeError(w, http.StatusBadRequest, "displayName must be 1 to 32 characters")
		return
	}
	if input.Mode != "duel" {
		writeError(w, http.StatusBadRequest, "mode must be duel")
		return
	}
	if input.Runtime != "lua5.4" {
		writeError(w, http.StatusBadRequest, "runtime must be lua5.4")
		return
	}
	if input.StartCommand == "" || len(input.StartCommand) > 256 {
		writeError(w, http.StatusBadRequest, "startCommand must be 1 to 256 characters")
		return
	}

	userID := robotauth.UserID(r.Context())
	boxID := boxes.IDForUser(userID)
	box, err := s.boxes.Status(r.Context(), boxID)
	if err != nil || box.Status != "running" || box.KeyFingerprint == "" {
		writeError(w, http.StatusConflict, "provision a running SSH box with a public key before queueing")
		return
	}
	// Same rule as registration: the store decides whether the box is truly
	// bound, because the supervisor copy of the markers (agent.json) never
	// clears on its own. A marker whose match is gone, over, or no longer
	// lists this box's robot is stale and must not block queueing.
	s.overlayBoxMatchState(r.Context(), userID, &box)
	if box.ActiveMatchID != "" {
		if active, activeErr := s.store.GetMatch(r.Context(), box.ActiveMatchID); activeErr == nil && boxOwnsRobot(active, boxID) {
			writeError(w, http.StatusConflict, "box already has an active robot")
			return
		}
		box.ActiveRobotID, box.ActiveMatchID = "", ""
		_ = s.store.PutBox(r.Context(), userID, box)
	}
	source, err := s.boxes.ReadMain(r.Context(), box.BoxID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "read /workspace/main.lua: "+err.Error())
		return
	}

	s.queue.mu.Lock()
	if existing := s.queue.entries[userID]; existing != nil {
		s.queue.mu.Unlock()
		writeError(w, http.StatusConflict, "already queued")
		return
	}
	entry := &queueEntry{UserID: userID, Box: box, Source: source, Request: input, JoinedAt: time.Now().UTC(), Status: "waiting"}
	s.queue.entries[userID] = entry
	s.queue.waiting[input.Mode] = append(s.queue.waiting[input.Mode], userID)
	if len(s.queue.waiting[input.Mode]) < 2 {
		s.queue.mu.Unlock()
		go s.fillQueueWithBotAfterWait(userID, input.Mode)
		writeJSON(w, http.StatusAccepted, entry)
		return
	}
	users := append([]string(nil), s.queue.waiting[input.Mode][:2]...)
	s.queue.waiting[input.Mode] = s.queue.waiting[input.Mode][2:]
	first, second := s.queue.entries[users[0]], s.queue.entries[users[1]]
	first.Status, second.Status = "pairing", "pairing"
	s.queue.mu.Unlock()

	match, err := s.createQueuedMatch(r.Context(), first, second)
	if err != nil {
		s.queue.mu.Lock()
		delete(s.queue.entries, first.UserID)
		delete(s.queue.entries, second.UserID)
		s.queue.mu.Unlock()
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	s.queue.mu.Lock()
	first.Status, second.Status = "matched", "matched"
	first.MatchID, second.MatchID = match.MatchID, match.MatchID
	s.queue.mu.Unlock()
	go s.enforceConnectGrace(match.MatchID, []string{first.UserID, second.UserID})
	writeJSON(w, http.StatusCreated, map[string]any{"status": "matched", "match": match})
}

func (s *Server) fillQueueWithBotAfterWait(userID, mode string) {
	seconds, err := strconv.Atoi(envOr("QUEUE_BOT_FILL_SECONDS", "45"))
	if err != nil || seconds < 1 {
		seconds = 45
	}
	timer := time.NewTimer(time.Duration(seconds) * time.Second)
	defer timer.Stop()
	<-timer.C
	s.queue.mu.Lock()
	entry := s.queue.entries[userID]
	if entry == nil || entry.Status != "waiting" {
		s.queue.mu.Unlock()
		return
	}
	users := s.queue.waiting[mode]
	for i, queuedUser := range users {
		if queuedUser == userID {
			s.queue.waiting[mode] = append(users[:i], users[i+1:]...)
			break
		}
	}
	entry.Status = "pairing"
	s.queue.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	match, err := s.createQueuedMatch(ctx, entry)
	if err != nil {
		s.queue.mu.Lock()
		delete(s.queue.entries, userID)
		s.queue.mu.Unlock()
		return
	}
	match.Robots = append(match.Robots, model.RobotSubmission{RobotID: uuid.NewString(), DisplayName: "Fighter Bot", Team: "blue", Runtime: "server-bot", StartCommand: "bot:fighter", Bot: true, SubmittedAt: time.Now().UTC()})
	if s.store.PutMatch(ctx, match) != nil {
		// The stale pairing entry has no match id yet; drop it so the
		// cancellation below can rebuild a fresh queue entry for the player.
		s.queue.mu.Lock()
		delete(s.queue.entries, userID)
		s.queue.mu.Unlock()
		s.cancelQueuedMatch(ctx, match.MatchID, "bot fill failed")
		return
	}
	s.queue.mu.Lock()
	entry.Status, entry.MatchID = "matched", match.MatchID
	s.queue.mu.Unlock()
	s.hub.Publish(match.MatchID, map[string]any{"type": "match_state", "version": 1, "match": match})
	go s.autoStartIfReady(match.MatchID)
	go s.enforceConnectGrace(match.MatchID, []string{userID})
}

func (s *Server) createQueuedMatch(ctx context.Context, entries ...*queueEntry) (model.Match, error) {
	now := time.Now().UTC()
	match := model.Match{MatchID: uuid.NewString(), OwnerID: entries[0].UserID, Status: model.MatchLobby, Mode: entries[0].Request.Mode, MapID: "random-bunkers", ArenaWidth: 1200, ArenaHeight: 750, Seed: now.UnixNano(), TickRate: 10, CreatedAt: now, Robots: []model.RobotSubmission{}}
	for index, entry := range entries {
		robotID := uuid.NewString()
		token, err := randomToken()
		if err != nil {
			return model.Match{}, err
		}
		hash := sha256.Sum256([]byte(token))
		credential := cloud.AgentCredential{RobotID: robotID, MatchID: match.MatchID, TokenHash: base64.RawURLEncoding.EncodeToString(hash[:])}
		if err := s.store.PutAgentCredential(ctx, credential); err != nil {
			return model.Match{}, err
		}
		key := fmt.Sprintf("scripts/%s/%s/%s/main.lua", entry.Box.BoxID, match.MatchID, robotID)
		if err := s.store.PutScript(ctx, key, entry.Source); err != nil {
			return model.Match{}, err
		}
		team := "red"
		if index%2 == 1 {
			team = "blue"
		}
		match.Robots = append(match.Robots, model.RobotSubmission{RobotID: robotID, PlayerID: entry.UserID, OwnerBoxID: entry.Box.BoxID, DisplayName: entry.Request.DisplayName, Team: team, ScriptObjectKey: key, StartCommand: entry.Request.StartCommand, Runtime: entry.Request.Runtime, SubmittedAt: now})
		agentBaseURL := envOr("ROBOT_AGENT_BASE_URL", "ws://host.docker.internal:8080")
		configured, err := s.boxes.ConfigureAgent(ctx, entry.Box.BoxID, boxes.AgentConfig{RobotID: robotID, MatchID: match.MatchID, URL: strings.TrimRight(agentBaseURL, "/") + "/agent/connect/" + robotID, Token: token, StartCommand: entry.Request.StartCommand})
		if err != nil {
			return model.Match{}, fmt.Errorf("configure box agent: %w", err)
		}
		configured.ActiveRobotID, configured.ActiveMatchID = robotID, match.MatchID
		_ = s.store.PutBox(ctx, entry.UserID, configured)
	}
	if err := s.store.PutMatch(ctx, match); err != nil {
		return model.Match{}, err
	}
	s.hub.Publish(match.MatchID, map[string]any{"type": "match_state", "version": 1, "match": match})
	return match, nil
}

func (s *Server) queueStatus(w http.ResponseWriter, r *http.Request) {
	userID := robotauth.UserID(r.Context())
	s.queue.mu.Lock()
	entry := s.queue.entries[userID]
	s.queue.mu.Unlock()
	if entry == nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "idle"})
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func (s *Server) leaveQueue(w http.ResponseWriter, r *http.Request) {
	userID := robotauth.UserID(r.Context())
	s.queue.mu.Lock()
	entry := s.queue.entries[userID]
	if entry == nil {
		s.queue.mu.Unlock()
		writeError(w, http.StatusNotFound, "not queued")
		return
	}
	if entry.Status == "matched" {
		matchID := entry.MatchID
		for queuedUser, queuedEntry := range s.queue.entries {
			if queuedEntry.MatchID == matchID {
				delete(s.queue.entries, queuedUser)
			}
		}
		s.queue.mu.Unlock()
		// The leaver opted out; the abandoned partner is requeued instead of
		// being dropped with the cancelled match.
		s.cancelQueuedMatch(r.Context(), matchID, "queue cancelled by player", userID)
		writeJSON(w, http.StatusOK, map[string]string{"status": "idle"})
		return
	}
	if entry.Status != "waiting" {
		s.queue.mu.Unlock()
		writeError(w, http.StatusConflict, "queue entry is being paired")
		return
	}
	delete(s.queue.entries, userID)
	users := s.queue.waiting[entry.Request.Mode]
	for i, queuedUser := range users {
		if queuedUser == userID {
			s.queue.waiting[entry.Request.Mode] = append(users[:i], users[i+1:]...)
			break
		}
	}
	s.queue.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"status": "idle"})
}

// cancelQueuedMatch fails a duel-queue match that never started and returns
// its human players to the queue with their original robot selections. Users
// listed in skipRequeue (e.g. a player who explicitly left) are not requeued.
func (s *Server) cancelQueuedMatch(ctx context.Context, matchID, reason string, skipRequeue ...string) {
	s.mu.Lock()
	match, err := s.store.GetMatch(ctx, matchID)
	if err != nil || match.Status != model.MatchLobby {
		s.mu.Unlock()
		return
	}
	match.Status, match.Error = model.MatchFailed, reason
	now := time.Now().UTC()
	match.FinishedAt = &now
	_ = s.store.PutMatch(ctx, match)
	s.releaseBoxes(ctx, match)
	s.mu.Unlock()
	s.hub.Publish(matchID, map[string]any{"type": "match_state", "version": 1, "match": match})
	skip := make(map[string]bool, len(skipRequeue))
	for _, userID := range skipRequeue {
		skip[userID] = true
	}
	requeueCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s.requeueAfterCancel(requeueCtx, match, skip)
}

// requeueIfQueued requeues players after a match failed before the arena
// started, but only when the duel queue still holds entries for the match —
// manual and practice matches never have those, so they are untouched.
func (s *Server) requeueIfQueued(ctx context.Context, match model.Match) {
	s.queue.mu.Lock()
	for _, entry := range s.queue.entries {
		if entry.MatchID == match.MatchID {
			s.queue.mu.Unlock()
			s.requeueAfterCancel(ctx, match, nil)
			return
		}
	}
	s.queue.mu.Unlock()
}

// requeueAfterCancel rebuilds queue entries for the human robots of a cancelled
// duel-queue match from the stored match and script snapshots, then follows the
// normal FIFO flow: two waiting players pair immediately, a lone player waits
// for the bot-fill timer. Repeated cancellations therefore re-enter the queue
// at the back instead of dropping players.
func (s *Server) requeueAfterCancel(ctx context.Context, match model.Match, skip map[string]bool) {
	for _, robot := range match.Robots {
		if robot.Bot || robot.PlayerID == "" || skip[robot.PlayerID] {
			continue
		}
		// Drop only the stale entry that points at this cancelled match;
		// a player actively waiting or pairing elsewhere is left alone.
		s.queue.mu.Lock()
		if existing := s.queue.entries[robot.PlayerID]; existing != nil {
			if existing.Status != "matched" || existing.MatchID != match.MatchID {
				s.queue.mu.Unlock()
				continue
			}
			delete(s.queue.entries, robot.PlayerID)
		}
		s.queue.mu.Unlock()
		box, err := s.store.GetBox(ctx, robot.PlayerID)
		if err != nil || box.Status != "running" || box.KeyFingerprint == "" {
			continue
		}
		// Never requeue a robot whose box has moved to another live match.
		if box.ActiveMatchID != "" && box.ActiveMatchID != match.MatchID {
			other, otherErr := s.store.GetMatch(ctx, box.ActiveMatchID)
			if otherErr == nil && other.Status != model.MatchFinished && other.Status != model.MatchFailed {
				continue
			}
			box.ActiveRobotID, box.ActiveMatchID = "", ""
			_ = s.store.PutBox(ctx, robot.PlayerID, box)
		}
		source, err := s.store.GetScript(ctx, robot.ScriptObjectKey)
		if err != nil {
			continue
		}
		entry := &queueEntry{
			UserID: robot.PlayerID, Box: box, Source: source,
			Request:  queueRequest{DisplayName: robot.DisplayName, Mode: match.Mode, Runtime: robot.Runtime, StartCommand: robot.StartCommand},
			JoinedAt: time.Now().UTC(), Status: "waiting",
		}
		mode := entry.Request.Mode
		s.queue.mu.Lock()
		if s.queue.entries[robot.PlayerID] != nil {
			s.queue.mu.Unlock()
			continue
		}
		s.queue.entries[robot.PlayerID] = entry
		s.queue.waiting[mode] = append(s.queue.waiting[mode], robot.PlayerID)
		var first, second *queueEntry
		if len(s.queue.waiting[mode]) >= 2 {
			if a, b := s.queue.entries[s.queue.waiting[mode][0]], s.queue.entries[s.queue.waiting[mode][1]]; a != nil && b != nil && a != b {
				s.queue.waiting[mode] = s.queue.waiting[mode][2:]
				first, second = a, b
				first.Status, second.Status = "pairing", "pairing"
			}
		}
		s.queue.mu.Unlock()
		if first == nil || second == nil {
			go s.fillQueueWithBotAfterWait(robot.PlayerID, mode)
			continue
		}
		paired, err := s.createQueuedMatch(ctx, first, second)
		if err != nil {
			s.queue.mu.Lock()
			delete(s.queue.entries, first.UserID)
			delete(s.queue.entries, second.UserID)
			s.queue.mu.Unlock()
			continue
		}
		s.queue.mu.Lock()
		first.Status, second.Status = "matched", "matched"
		first.MatchID, second.MatchID = paired.MatchID, paired.MatchID
		s.queue.mu.Unlock()
		go s.enforceConnectGrace(paired.MatchID, []string{first.UserID, second.UserID})
	}
}

func (s *Server) enforceConnectGrace(matchID string, userIDs []string) {
	seconds, err := strconv.Atoi(envOr("QUEUE_CONNECT_GRACE_SECONDS", "30"))
	if err != nil || seconds < 1 {
		seconds = 30
	}
	timer := time.NewTimer(time.Duration(seconds) * time.Second)
	defer timer.Stop()
	<-timer.C
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.mu.Lock()
	match, err := s.store.GetMatch(ctx, matchID)
	if err != nil || match.Status != model.MatchLobby {
		s.mu.Unlock()
		return
	}
	expired := false
	for _, robot := range match.Robots {
		if robot.Bot {
			continue
		}
		if !s.agents.Connected(robot.RobotID) {
			expired = true
			break
		}
	}
	s.mu.Unlock()
	if expired {
		// Failing through cancelQueuedMatch also requeues the players.
		s.cancelQueuedMatch(ctx, matchID, "agent connection grace window expired")
		return
	}
	s.queue.mu.Lock()
	for _, userID := range userIDs {
		delete(s.queue.entries, userID)
	}
	s.queue.mu.Unlock()
}
