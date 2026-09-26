package api

import (
	"context"
	"encoding/json"
	"math"
	"sync"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type viewerRegion struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

func (v viewerRegion) valid() bool {
	for _, n := range []float64{v.X, v.Y, v.Width, v.Height} {
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return false
		}
	}
	return v.X >= 0 && v.Y >= 0 && v.X <= 48000 && v.Y <= 30000 && v.Width >= 100 && v.Height >= 100 && v.Width <= 48000 && v.Height <= 30000
}

type v4Viewer struct {
	mu           sync.Mutex
	region       viewerRegion
	revision     json.RawMessage
	overviewTick int
	started      bool
}

func (v *v4Viewer) read(ctx context.Context, c *websocket.Conn) {
	c.SetReadLimit(1024)
	for {
		var r viewerRegion
		if err := wsjson.Read(ctx, c, &r); err != nil {
			return
		}
		if !r.valid() {
			_ = c.Close(websocket.StatusPolicyViolation, "invalid camera region")
			return
		}
		v.mu.Lock()
		v.region = r
		v.mu.Unlock()
	}
}

// Input comes exclusively from the delayed public hub, never live worker state.
// Each update replaces regional entities; layout is resent on revision changes.
func (v *v4Viewer) project(raw []byte) []byte {
	var s map[string]json.RawMessage
	if json.Unmarshal(raw, &s) != nil || string(s["type"]) != `"snapshot"` {
		return raw
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	var tick int
	_ = json.Unmarshal(s["tick"], &tick)
	if !v.started || tick-v.overviewTick >= 20 {
		s["overview"] = s["robots"]
		v.overviewTick = tick
	}
	r := v.region
	if r.Width == 0 {
		r = viewerRegion{X: 1200, Y: 750, Width: 2400, Height: 1500}
	}
	for _, key := range []string{"robots", "projectiles", "items", "mines", "fields"} {
		var entities []json.RawMessage
		_ = json.Unmarshal(s[key], &entities)
		kept := make([]json.RawMessage, 0)
		for _, raw := range entities {
			var p struct{ X, Y, Radius float64 }
			if json.Unmarshal(raw, &p) == nil && math.Abs(p.X-r.X) <= r.Width/2+256+p.Radius && math.Abs(p.Y-r.Y) <= r.Height/2+256+p.Radius {
				kept = append(kept, raw)
			}
		}
		s[key], _ = json.Marshal(kept)
	}
	if v.started && string(v.revision) == string(s["revision"]) {
		for _, k := range staticLayoutKeys {
			delete(s, k)
		}
	} else {
		v.revision = append(v.revision[:0], s["revision"]...)
	}
	v.started = true
	result, err := json.Marshal(s)
	if err != nil {
		return raw
	}
	return result
}
