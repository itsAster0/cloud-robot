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
	PutBox(ctx context.Context, userID string, box model.BoxRecord) error
	PutScript(ctx context.Context, key, source string) error
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
}

func NewServer(store Store, provisioner boxes.Provisioner) *Server {
	return &Server{store: store, boxes: provisioner, hub: NewHub(), agents: NewAgentManager(), auth: robotauth.NewVerifier()}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /readyz", s.ready)
	mux.HandleFunc("GET /api/cloud/status", s.cloudStatus)
	mux.Handle("POST /api/me/box", s.requireUser(http.HandlerFunc(s.ensureBox)))
	mux.Handle("GET /api/me/box", s.requireUser(http.HandlerFunc(s.getBox)))
	mux.Handle("GET /api/me/box/main.lua", s.requireUser(http.HandlerFunc(s.getBoxMain)))
	mux.Handle("PUT /api/me/box/main.lua", s.requireUser(http.HandlerFunc(s.deployBoxMain)))
	mux.HandleFunc("GET /api/scripts", s.listScripts)
	mux.Handle("PUT /api/me/box/ssh-key", s.requireUser(http.HandlerFunc(s.setBoxKey)))
	mux.Handle("POST /api/me/box/restart", s.requireUser(http.HandlerFunc(s.restartBox)))
	mux.Handle("POST /api/matches", s.requireUser(http.HandlerFunc(s.createMatch)))
	mux.HandleFunc("GET /api/matches/{matchID}", s.getMatch)
	mux.Handle("POST /api/matches/{matchID}/robots", s.requireUser(http.HandlerFunc(s.submitRobot)))
	mux.Handle("DELETE /api/matches/{matchID}/robots", s.requireUser(http.HandlerFunc(s.withdrawRobot)))
	mux.Handle("POST /api/matches/{matchID}/start", s.requireUser(http.HandlerFunc(s.startMatch)))
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

func (s *Server) createMatch(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	match := model.Match{MatchID: uuid.NewString(), OwnerID: robotauth.UserID(r.Context()), Status: model.MatchLobby, Mode: "duel", Seed: 42, TickRate: 10, CreatedAt: now, Robots: []model.RobotSubmission{}}
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
	if input.Team != "red" && input.Team != "blue" {
		writeError(w, http.StatusBadRequest, "team must be red or blue")
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
	if box.ActiveMatchID != "" {
		active, activeErr := s.store.GetMatch(r.Context(), box.ActiveMatchID)
		if activeErr == nil && active.Status != model.MatchFinished && active.Status != model.MatchFailed {
			writeError(w, http.StatusConflict, "box already has an active robot")
			return
		}
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
	if len(match.Robots) >= 8 {
		writeError(w, http.StatusConflict, "match roster is full")
		return
	}
	for _, robot := range match.Robots {
		if robot.OwnerBoxID == boxID {
			writeError(w, http.StatusConflict, "user already registered a robot in this match")
			return
		}
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
	match.Robots = append(match.Robots, model.RobotSubmission{
		RobotID: robotID, DisplayName: input.DisplayName, Team: input.Team,
		OwnerBoxID: boxID, ScriptObjectKey: scriptKey, StartCommand: input.StartCommand, Runtime: input.Runtime, SubmittedAt: time.Now().UTC(),
	})
	if err := s.store.PutMatch(r.Context(), match); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	agentBaseURL := envOr("ROBOT_AGENT_BASE_URL", "ws://host.docker.internal:8080")
	configured, err := s.boxes.ConfigureAgent(r.Context(), boxID, boxes.AgentConfig{RobotID: robotID, MatchID: match.MatchID, URL: strings.TrimRight(agentBaseURL, "/") + "/agent/connect/" + robotID, Token: token, StartCommand: input.StartCommand})
	if err != nil {
		// Roll the roster back so a broken provisioner never leaves a phantom
		// robot in a match that agents cannot join.
		match.Robots = match.Robots[:len(match.Robots)-1]
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

func (s *Server) ensureBox(w http.ResponseWriter, r *http.Request) {
	userID := robotauth.UserID(r.Context())
	box, err := s.boxes.Ensure(r.Context(), boxes.IDForUser(userID))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
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
	_ = s.store.PutBox(r.Context(), userID, box)
	writeJSON(w, http.StatusOK, box)
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
	writeJSON(w, http.StatusOK, map[string]string{"template": name, "source": written})
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
	if err := engine.ValidateTeams(states); err != nil {
		return match, err
	}
	for _, robot := range match.Robots {
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
		return match, err
	}
	s.hub.Publish(match.MatchID, map[string]any{"type": "match_state", "version": 1, "match": match})
	return match, nil
}

// autoStartIfReady removes the manual start step for the demo: once both
// teams are registered and every agent is connected, the match queues itself.
func (s *Server) autoStartIfReady(matchID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	match, err := s.store.GetMatch(ctx, matchID)
	if err != nil || match.Status != model.MatchLobby {
		return
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
			err := wsjson.Write(ctx, connection, event)
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
