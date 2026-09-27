package api

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/coder/websocket/wsjson"
	"io"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"
)

type v4Equip struct {
	Container string `json:"container"`
	Kind      string `json:"kind"`
	Slot      uint   `json:"slot"`
}
type v4EquipmentSlot struct {
	Group string `json:"group"`
	Slot  uint   `json:"slot"`
}
type v4Action struct {
	Equip            *v4Equip         `json:"equip,omitempty"`
	DropEquipment    *v4EquipmentSlot `json:"dropEquipment,omitempty"`
	PickupPriorities *[]string        `json:"pickupPriorities,omitempty"`
	Throttle         float64          `json:"throttle,omitempty"`
	Brake            bool             `json:"brake,omitempty"`
	Turn             float64          `json:"turn,omitempty"`
	Aim              *float64         `json:"aim,omitempty"`
	Fire             bool             `json:"fire,omitempty"`
	Cruise           bool             `json:"cruise,omitempty"`
	Dash             bool             `json:"dash,omitempty"`
	Scan             bool             `json:"scan,omitempty"`
	Weapon           *uint            `json:"weapon,omitempty"`
	Utility          *string          `json:"utility,omitempty"`
	Consume          *uint            `json:"consume,omitempty"`
	Pickup           *string          `json:"pickup,omitempty"`
	DropSlot         *uint            `json:"dropSlot,omitempty"`
	Transit          *string          `json:"transit,omitempty"`
	Message          *string          `json:"message,omitempty"`
	Label            *string          `json:"label,omitempty"`
}

// v4DebugMark is an owner-only drawing a script attaches to its input. Marks
// stay in the API: they are shown in the owner's live view and never reach
// the simulation, so they cannot affect determinism or other players.
type v4DebugMark struct {
	Kind  string  `json:"kind"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	X2    float64 `json:"x2,omitempty"`
	Y2    float64 `json:"y2,omitempty"`
	R     float64 `json:"r,omitempty"`
	Text  string  `json:"text,omitempty"`
	Color string  `json:"color,omitempty"`
}

const maxDebugMarks = 24

type v4Input struct {
	Debug            []v4DebugMark `json:"debug,omitempty"`
	GeometryRevision *uint32       `json:"geometryRevision,omitempty"`
	Type             string        `json:"type"`
	Version          int           `json:"version"`
	SDKVersion       string        `json:"sdkVersion"`
	Sequence         uint64        `json:"sequence"`
	ObservedTick     uint32        `json:"observedTick"`
	Action           v4Action      `json:"action"`
}
type v4Mailbox struct {
	mu               sync.Mutex
	last             v4Input
	received         time.Time
	sentTick         uint32
	consumed         uint64
	out              chan json.RawMessage
	rejections       chan json.RawMessage
	geometryRevision uint32
	geometry         json.RawMessage
	debug            []v4DebugMark
	// rejectLogged rate-limits rejection logs per code to one per 5 s.
	rejectLogged map[string]time.Time
}

// debugMarks returns the marks from the latest accepted input.
func (s *AgentSession) debugMarks() []v4DebugMark {
	s.mailbox.mu.Lock()
	defer s.mailbox.mu.Unlock()
	return append([]v4DebugMark(nil), s.mailbox.debug...)
}

func (s *AgentSession) readV4(ctx context.Context) error {
	s.connection.SetReadLimit(16 * 1024)
	s.mailbox.rejections = make(chan json.RawMessage, 8)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.closed:
				return
			case raw := <-s.mailbox.rejections:
				writeCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
				s.writeMu.Lock()
				err := wsjson.Write(writeCtx, s.connection, raw)
				s.writeMu.Unlock()
				cancel()
				if err != nil {
					s.connection.CloseNow()
					return
				}
			case raw := <-s.mailbox.out:
				s.mailbox.mu.Lock()
				acknowledged := s.mailbox.geometryRevision
				geometry := s.mailbox.geometry
				s.mailbox.mu.Unlock()
				raw = observationWithGeometry(raw, geometry, acknowledged)
				writeCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
				s.writeMu.Lock()
				err := wsjson.Write(writeCtx, s.connection, raw)
				s.writeMu.Unlock()
				cancel()
				if err != nil {
					s.connection.CloseNow()
					return
				}
			}
		}
	}()
	defer close(s.closed)
	for {
		var raw json.RawMessage
		if err := wsjson.Read(ctx, s.connection, &raw); err != nil {
			return err
		}
		var input v4Input
		d := json.NewDecoder(bytes.NewReader(raw))
		d.DisallowUnknownFields()
		if err := d.Decode(&input); err != nil {
			s.rejectV4(0, "MALFORMED_ACTION")
			continue
		}
		if err := d.Decode(&struct{}{}); err != io.EOF {
			s.rejectV4(input.Sequence, "MALFORMED_ACTION")
			continue
		}
		code := validateV4Input(input)
		s.mailbox.mu.Lock()
		if code == "" {
			switch {
			case input.Sequence <= s.mailbox.last.Sequence:
				code = "DUPLICATE_SEQUENCE"
			case input.ObservedTick > s.mailbox.sentTick:
				code = "FUTURE_OBSERVATION"
			case s.mailbox.sentTick-input.ObservedTick > 5:
				code = "STALE_OBSERVATION"
			default:
				s.mailbox.last = input
				s.mailbox.debug = input.Debug
				if input.GeometryRevision != nil {
					s.mailbox.geometryRevision = *input.GeometryRevision
				}
				s.mailbox.received = time.Now()
			}
		}
		s.mailbox.mu.Unlock()
		if code != "" {
			s.rejectV4(input.Sequence, code)
		}

	}
}
func (s *AgentSession) sendObservation(raw json.RawMessage, tick uint32) {
	if tick%2 != 0 {
		return
	}
	s.mailbox.mu.Lock()
	s.mailbox.sentTick = tick
	s.mailbox.mu.Unlock()
	select {
	case s.mailbox.out <- raw:
	default:
		select {
		case <-s.mailbox.out:
		default:
		}
		select {
		case s.mailbox.out <- raw:
		default:
		}
	}
}
func (s *AgentSession) takeV4Action(tick uint32) json.RawMessage {
	s.mailbox.mu.Lock()
	defer s.mailbox.mu.Unlock()
	if time.Since(s.mailbox.received) > 250*time.Millisecond || tick > s.mailbox.last.ObservedTick+5 {
		return json.RawMessage(`{}`)
	}
	a := s.mailbox.last.Action
	if s.mailbox.consumed == s.mailbox.last.Sequence {
		a.Dash = false
		a.Scan = false
		a.Utility = nil
		a.Consume = nil
		a.Pickup = nil
		a.DropSlot = nil
		a.Equip = nil
		a.DropEquipment = nil
		a.PickupPriorities = nil
		a.Transit = nil
		a.Message = nil
		a.Weapon = nil
	}
	s.mailbox.consumed = s.mailbox.last.Sequence
	raw, err := json.Marshal(a)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

func validateV4Input(input v4Input) string {
	if input.Type != "action" || input.Version != 4 || input.SDKVersion != "0.4.0" {
		return "UNSUPPORTED_VERSION"
	}
	a := input.Action
	if a.Equip != nil && (a.Equip.Slot > 1 || len(a.Equip.Container) == 0 || len(a.Equip.Container) > 128 || len(a.Equip.Kind) == 0 || len(a.Equip.Kind) > 128) {
		return "OUT_OF_RANGE"
	}
	if a.DropEquipment != nil && (a.DropEquipment.Slot > 1 || (a.DropEquipment.Group != "weapon" && a.DropEquipment.Group != "module" && a.DropEquipment.Group != "utility")) {
		return "OUT_OF_RANGE"
	}
	if a.PickupPriorities != nil {
		if len(*a.PickupPriorities) > 32 {
			return "OUT_OF_RANGE"
		}
		for _, p := range *a.PickupPriorities {
			if len(p) > 128 {
				return "OUT_OF_RANGE"
			}
		}
	}
	if math.IsNaN(a.Throttle) || math.IsInf(a.Throttle, 0) || math.IsNaN(a.Turn) || math.IsInf(a.Turn, 0) || math.Abs(a.Throttle) > 1 || math.Abs(a.Turn) > 1 || a.Aim != nil && (math.IsInf(*a.Aim, 0) || math.IsNaN(*a.Aim)) || a.Weapon != nil && *a.Weapon > 1 || a.Consume != nil && *a.Consume > 3 || a.DropSlot != nil && *a.DropSlot > 3 || a.Message != nil && len(*a.Message) > 128 || a.Label != nil && len(*a.Label) > 64 {
		return "OUT_OF_RANGE"
	}
	for _, value := range []*string{a.Pickup, a.Transit, a.Utility} {
		if value != nil && (len(*value) == 0 || len(*value) > 128) {
			return "OUT_OF_RANGE"
		}
	}
	if len(input.Debug) > maxDebugMarks {
		return "OUT_OF_RANGE"
	}
	for _, m := range input.Debug {
		if !validDebugMark(m) {
			return "OUT_OF_RANGE"
		}
	}
	return ""
}

func validDebugMark(m v4DebugMark) bool {
	switch m.Kind {
	case "point", "line", "circle", "text":
	default:
		return false
	}
	for _, v := range []float64{m.X, m.Y, m.X2, m.Y2, m.R} {
		if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 1e6 {
			return false
		}
	}
	if m.R < 0 || m.R > 5000 || len(m.Text) > 40 {
		return false
	}
	if m.Color != "" {
		if len(m.Color) != 7 || m.Color[0] != '#' {
			return false
		}
		for _, c := range m.Color[1:] {
			if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
				return false
			}
		}
	}
	return true
}
func (s *AgentSession) rejectV4(sequence uint64, code string) {
	s.mailbox.mu.Lock()
	if s.mailbox.rejectLogged == nil {
		s.mailbox.rejectLogged = map[string]time.Time{}
	}
	logIt := time.Since(s.mailbox.rejectLogged[code]) > 5*time.Second
	if logIt {
		s.mailbox.rejectLogged[code] = time.Now()
	}
	s.mailbox.mu.Unlock()
	if logIt {
		slog.Warn("agent input rejected", "source", "agent", "match", s.matchID, "robot", s.robotID, "code", code)
	}
	raw, _ := json.Marshal(map[string]any{"type": "action_rejected", "version": 4, "sequence": sequence, "code": code})
	select {
	case s.mailbox.rejections <- raw:
	default:
	}
}

// Geometry is omitted only after an accepted action acknowledges its revision.
// Reconnects start with no acknowledgement and therefore receive a full baseline.
func observationForDelivery(raw json.RawMessage, acknowledged uint32) json.RawMessage {
	if acknowledged == 0 {
		return raw
	}
	var metadata struct {
		Revision uint32 `json:"revision"`
	}
	if json.Unmarshal(raw, &metadata) != nil || metadata.Revision != acknowledged {
		return raw
	}
	var observation map[string]json.RawMessage
	if json.Unmarshal(raw, &observation) != nil {
		return raw
	}
	for _, key := range []string{"obstacles", "hazards", "sites", "transit"} {
		delete(observation, key)
	}
	encoded, err := json.Marshal(observation)
	if err != nil {
		return raw
	}
	return encoded
}

func observationWithGeometry(raw, geometry json.RawMessage, acknowledged uint32) json.RawMessage {
	var metadata struct {
		Revision uint32 `json:"revision"`
	}
	if json.Unmarshal(raw, &metadata) != nil {
		return raw
	}
	if acknowledged != 0 && acknowledged == metadata.Revision {
		return observationForDelivery(raw, acknowledged)
	}
	if len(geometry) == 0 {
		return raw
	}
	var observation, layout map[string]json.RawMessage
	if json.Unmarshal(raw, &observation) != nil || json.Unmarshal(geometry, &layout) != nil {
		return raw
	}
	for _, key := range []string{"obstacles", "hazards", "sites", "transit"} {
		if value, ok := layout[key]; ok {
			observation[key] = value
		}
	}
	encoded, err := json.Marshal(observation)
	if err != nil {
		return raw
	}
	return encoded
}
