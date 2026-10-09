package api

import (
	"context"
	"github.com/kryxen/cloud-robot/internal/engine"
	"sync"
	"time"
)

type controllerObservation struct {
	self   engine.RobotState
	robots []engine.RobotState
	world  engine.WorldState
}
type asyncController struct {
	inner        engine.Controller
	ctx          context.Context
	cancel       context.CancelFunc
	observations chan controllerObservation
	mu           sync.Mutex
	world        engine.WorldState
	intent       engine.Intent
	err          error
	received     time.Time
	fresh        bool
}

func newAsyncController(inner engine.Controller) *asyncController {
	ctx, cancel := context.WithCancel(context.Background())
	c := &asyncController{inner: inner, ctx: ctx, cancel: cancel, observations: make(chan controllerObservation, 1)}
	go c.run()
	return c
}
func (c *asyncController) SetWorld(w engine.WorldState) { c.mu.Lock(); c.world = w; c.mu.Unlock() }
func (c *asyncController) Tick(_ context.Context, self engine.RobotState, robots []engine.RobotState) (engine.Intent, error) {
	c.mu.Lock()
	w := c.world
	intent, err := c.intent, c.err
	if time.Since(c.received) > 250*time.Millisecond {
		intent = engine.Intent{}
	} else if !c.fresh {
		intent.Dash = false
		intent.Deploy = ""
		intent.Scan = nil
		intent.Message = ""
	}
	c.fresh = false
	c.mu.Unlock()
	o := controllerObservation{self, robots, w}
	select {
	case c.observations <- o:
	default:
		select {
		case <-c.observations:
		default:
		}
		select {
		case c.observations <- o:
		default:
		}
	}
	return intent, err
}
func (c *asyncController) run() {
	for {
		select {
		case <-c.ctx.Done():
			return
		case o := <-c.observations:
			if aware, ok := c.inner.(engine.WorldAwareController); ok {
				aware.SetWorld(o.world)
			}
			intent, err := c.inner.Tick(c.ctx, o.self, o.robots)
			c.mu.Lock()
			c.intent = intent
			c.err = err
			c.received = time.Now()
			c.fresh = true
			c.mu.Unlock()
		}
	}
}
func (c *asyncController) Close() { c.cancel(); c.inner.Close() }
