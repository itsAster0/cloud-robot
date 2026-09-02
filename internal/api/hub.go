package api

import (
	"encoding/json"
	"sync"

	"github.com/kryxen/cloud-robot/internal/engine"
)

// hubMessage holds wire-ready JSON. A snapshot is encoded once per tick and
// shared by every viewer instead of being marshaled inside each WebSocket.
// Layout is a separate, cached message because map geometry never changes.
type hubMessage struct {
	payload []byte
	layout  []byte
}

type Hub struct {
	mu          sync.RWMutex
	subscribers map[string]map[chan hubMessage]struct{}
	last        map[string]hubMessage
	layouts     map[string][]byte
}

func NewHub() *Hub {
	return &Hub{
		subscribers: make(map[string]map[chan hubMessage]struct{}),
		last:        make(map[string]hubMessage),
		layouts:     make(map[string][]byte),
	}
}

func (h *Hub) Publish(matchID string, event any) {
	h.mu.RLock()
	_, hasLayout := h.layouts[matchID]
	h.mu.RUnlock()
	message, ok := encodeHubEvent(matchID, event, !hasLayout)
	if !ok {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(message.layout) == 0 {
		message.layout = h.layouts[matchID]
	} else {
		h.layouts[matchID] = message.layout
	}
	h.last[matchID] = message
	for channel := range h.subscribers[matchID] {
		select {
		case channel <- message:
		default:
			select {
			case <-channel:
			default:
			}
			select {
			case channel <- message:
			default:
			}
		}
	}
}

func encodeHubEvent(matchID string, event any, includeLayout bool) (hubMessage, bool) {
	message := hubMessage{}
	if snapshot, ok := event.(engine.Snapshot); ok {
		if includeLayout && len(snapshot.Obstacles) > 0 {
			layout := struct {
				Type      string            `json:"type"`
				Version   int               `json:"version"`
				MatchID   string            `json:"matchId"`
				MapID     string            `json:"mapId"`
				Width     float64           `json:"width"`
				Height    float64           `json:"height"`
				Obstacles []engine.Obstacle `json:"obstacles"`
			}{"arena_layout", 1, matchID, snapshot.MapID, snapshot.Width, snapshot.Height, snapshot.Obstacles}
			var err error
			message.layout, err = json.Marshal(layout)
			if err != nil {
				return hubMessage{}, false
			}
		}
		snapshot.Obstacles = nil
		event = snapshot
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return hubMessage{}, false
	}
	message.payload = payload
	return message, true
}

func (h *Hub) Subscribe(matchID string) (<-chan hubMessage, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	channel := make(chan hubMessage, 2)
	if h.subscribers[matchID] == nil {
		h.subscribers[matchID] = make(map[chan hubMessage]struct{})
	}
	h.subscribers[matchID][channel] = struct{}{}
	if last, ok := h.last[matchID]; ok {
		if len(last.layout) == 0 {
			last.layout = h.layouts[matchID]
		}
		channel <- last
	}
	return channel, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.subscribers[matchID], channel)
		if len(h.subscribers[matchID]) == 0 {
			delete(h.subscribers, matchID)
		}
		close(channel)
	}
}

// Forget drops cached wire data for a completed match. Match state, results,
// and replays already live in the store.
func (h *Hub) Forget(matchID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.last, matchID)
	delete(h.layouts, matchID)
}

func (h *Hub) ViewerCounts() map[string]int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	counts := make(map[string]int, len(h.subscribers))
	for matchID, subscribers := range h.subscribers {
		counts[matchID] = len(subscribers)
	}
	return counts
}
