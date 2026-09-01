package api

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"
	"github.com/kryxen/cloud-robot/internal/engine"
)

type agentObservation struct {
	Type        string              `json:"type"`
	Version     int                 `json:"version"`
	RequestID   string              `json:"requestId"`
	MatchID     string              `json:"matchId"`
	SentAt      int64               `json:"sentAt"`
	Self        engine.RobotState   `json:"self"`
	Robots      []engine.RobotState `json:"robots"`
	Tick        int                 `json:"tick"`
	MapID       string              `json:"mapId"`
	ArenaWidth  float64             `json:"arenaWidth"`
	ArenaHeight float64             `json:"arenaHeight"`
	Obstacles   []engine.Obstacle   `json:"obstacles"`
	Items       []engine.Item       `json:"items"`
	Hazards     []engine.Hazard     `json:"hazards"`
	Zone        *engine.ZoneState   `json:"zone,omitempty"`
	Overtime    bool                `json:"overtime"`
}

type agentAction struct {
	Type        string   `json:"type"`
	RequestID   string   `json:"requestId"`
	Move        float64  `json:"move"`
	Turn        float64  `json:"turn"`
	Fire        bool     `json:"fire"`
	TargetX     *float64 `json:"targetX,omitempty"`
	TargetY     *float64 `json:"targetY,omitempty"`
	Logs        []string `json:"logs,omitempty"`
	ComputeMS   float64  `json:"computeMs,omitempty"`
	MemoryMB    float64  `json:"memoryMb,omitempty"`
	Equipment   []string `json:"equipment,omitempty"`
	SDKVersion  string   `json:"sdkVersion,omitempty"`
	AutoPickup  *bool    `json:"autoPickup,omitempty"`
	PickupTypes []string `json:"pickupTypes,omitempty"`
}

type AgentSession struct {
	robotID    string
	matchID    string
	connection *websocket.Conn
	responses  chan agentAction
	closed     chan struct{}
	writeMu    sync.Mutex
	requestMu  sync.Mutex
	last       engine.Intent
}

func newAgentSession(robotID, matchID string, connection *websocket.Conn) *AgentSession {
	return &AgentSession{robotID: robotID, matchID: matchID, connection: connection, responses: make(chan agentAction, 4), closed: make(chan struct{})}
}

func (s *AgentSession) readLoop(ctx context.Context) error {
	defer close(s.closed)
	for {
		var action agentAction
		if err := wsjson.Read(ctx, s.connection, &action); err != nil {
			return err
		}
		if action.Type != "action" || action.RequestID == "" {
			continue
		}
		select {
		case s.responses <- action:
		default:
			select {
			case <-s.responses:
			default:
			}
			s.responses <- action
		}
	}
}

func (s *AgentSession) Tick(ctx context.Context, self engine.RobotState, robots []engine.RobotState, world engine.WorldState) (engine.Intent, error) {
	s.requestMu.Lock()
	defer s.requestMu.Unlock()
	requestID := uuid.NewString()
	sent := time.Now()
	observation := agentObservation{Type: "observation", Version: 2, RequestID: requestID, MatchID: s.matchID, SentAt: sent.UnixMilli(), Self: self, Robots: robots, Tick: world.Tick, MapID: world.MapID, ArenaWidth: world.Width, ArenaHeight: world.Height, Obstacles: world.Obstacles, Items: world.Items, Hazards: world.Hazards, Zone: world.Zone, Overtime: world.Overtime}

	writeCtx, cancelWrite := context.WithTimeout(ctx, 100*time.Millisecond)
	s.writeMu.Lock()
	err := wsjson.Write(writeCtx, s.connection, observation)
	s.writeMu.Unlock()
	cancelWrite()
	if err != nil {
		return engine.Intent{}, errors.New("robot agent disconnected")
	}

	timer := time.NewTimer(150 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return engine.Intent{}, ctx.Err()
		case <-s.closed:
			return engine.Intent{}, errors.New("robot agent disconnected")
		case <-timer.C:
			fallback := s.last
			fallback.ResponseMS = 150
			fallback.Logs = append(fallback.Logs, "response deadline missed; reusing last action")
			return fallback, nil
		case action := <-s.responses:
			if action.RequestID != requestID {
				continue
			}
			logs := append([]string(nil), action.Logs...)
			if action.SDKVersion == "" {
				logs = append(logs, "SDK version missing; server protocol is 0.2.0")
			} else if sdkVersionOlder(action.SDKVersion, "0.2.0") {
				logs = append(logs, "SDK version "+action.SDKVersion+" is older than server protocol 0.2.0")
			}
			intent := engine.Intent{
				Move: action.Move, Turn: action.Turn, Fire: action.Fire, TargetX: action.TargetX, TargetY: action.TargetY,
				Logs: logs, ResponseMS: float64(time.Since(sent).Microseconds()) / 1000,
				ComputeMS: action.ComputeMS, MemoryMB: action.MemoryMB, Equipment: action.Equipment,
				AutoPickup: action.AutoPickup, PickupTypes: action.PickupTypes,
			}
			s.last = intent
			return intent, nil
		}
	}
}

type AgentManager struct {
	mu             sync.RWMutex
	sessions       map[string]*AgentSession
	last           map[string]engine.Intent
	disconnectedAt map[string]time.Time
}

func NewAgentManager() *AgentManager {
	return &AgentManager{sessions: make(map[string]*AgentSession), last: make(map[string]engine.Intent), disconnectedAt: make(map[string]time.Time)}
}

func (m *AgentManager) Attach(robotID string, session *AgentSession) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if previous := m.sessions[robotID]; previous != nil {
		_ = previous.connection.Close(websocket.StatusPolicyViolation, "new agent connection replaced this session")
	}
	m.sessions[robotID] = session
	delete(m.disconnectedAt, robotID)
}

func (m *AgentManager) Detach(robotID string, session *AgentSession) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[robotID] == session {
		session.requestMu.Lock()
		m.last[robotID] = session.last
		session.requestMu.Unlock()
		m.disconnectedAt[robotID] = time.Now()
		delete(m.sessions, robotID)
	}
}

func (m *AgentManager) Connected(robotID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[robotID] != nil
}

func (m *AgentManager) Status(robotIDs []string) map[string]bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	status := make(map[string]bool, len(robotIDs))
	for _, robotID := range robotIDs {
		status[robotID] = m.sessions[robotID] != nil
	}
	return status
}

func (m *AgentManager) Controller(robotID string) engine.Controller {
	return &remoteController{manager: m, robotID: robotID}
}

func (m *AgentManager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

type remoteController struct {
	manager *AgentManager
	robotID string
	worldMu sync.RWMutex
	world   engine.WorldState
}

func (r *remoteController) SetWorld(world engine.WorldState) {
	r.worldMu.Lock()
	r.world = world
	r.worldMu.Unlock()
}

func (r *remoteController) Tick(ctx context.Context, self engine.RobotState, robots []engine.RobotState) (engine.Intent, error) {
	r.manager.mu.RLock()
	session := r.manager.sessions[r.robotID]
	r.manager.mu.RUnlock()
	if session == nil {
		r.manager.mu.RLock()
		last, hasLast := r.manager.last[r.robotID]
		disconnectedAt := r.manager.disconnectedAt[r.robotID]
		r.manager.mu.RUnlock()
		if hasLast && time.Since(disconnectedAt) <= 30*time.Second {
			last.Logs = append(last.Logs, "agent disconnected; reusing last action during reconnect grace")
			return last, nil
		}
		return engine.Intent{}, errors.New("robot agent reconnect grace expired")
	}
	r.worldMu.RLock()
	world := r.world
	r.worldMu.RUnlock()
	return session.Tick(ctx, self, robots, world)
}

func (r *remoteController) Close() {}

func sdkVersionOlder(version, minimum string) bool {
	parse := func(value string) [3]int {
		var parts [3]int
		_, _ = fmt.Sscanf(value, "%d.%d.%d", &parts[0], &parts[1], &parts[2])
		return parts
	}
	left, right := parse(version), parse(minimum)
	for i := range left {
		if left[i] != right[i] {
			return left[i] < right[i]
		}
	}
	return false
}
