package api

import "sync"

type Hub struct {
	mu          sync.RWMutex
	subscribers map[string]map[chan any]struct{}
	last        map[string]any
}

func NewHub() *Hub {
	return &Hub{subscribers: make(map[string]map[chan any]struct{}), last: make(map[string]any)}
}

func (h *Hub) Publish(matchID string, event any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.last[matchID] = event
	for channel := range h.subscribers[matchID] {
		select {
		case channel <- event:
		default:
			select {
			case <-channel:
			default:
			}
			select {
			case channel <- event:
			default:
			}
		}
	}
}

func (h *Hub) Subscribe(matchID string) (<-chan any, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	channel := make(chan any, 2)
	if h.subscribers[matchID] == nil {
		h.subscribers[matchID] = make(map[chan any]struct{})
	}
	h.subscribers[matchID][channel] = struct{}{}
	if last, ok := h.last[matchID]; ok {
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

// Forget drops the cached event for a completed match. Match state, results,
// and replays already live in the store, so retaining a terminal websocket
// event for every match only grows the API process over time.
func (h *Hub) Forget(matchID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.last, matchID)
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
