package api

import (
	"context"
	"github.com/kryxen/cloud-robot/internal/engine"
	"testing"
	"time"
)

type slowController struct{}

func (*slowController) Tick(ctx context.Context, _ engine.RobotState, _ []engine.RobotState) (engine.Intent, error) {
	select {
	case <-ctx.Done():
		return engine.Intent{}, ctx.Err()
	case <-time.After(150 * time.Millisecond):
		return engine.Intent{Move: 8, Dash: true}, nil
	}
}
func (*slowController) Close() {}
func TestAsyncControllerNeverWaitsForAgent(t *testing.T) {
	c := newAsyncController(&slowController{})
	defer c.Close()
	start := time.Now()
	for range 20 {
		_, _ = c.Tick(context.Background(), engine.RobotState{}, nil)
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Fatal("simulation waited for remote decisions")
	}
}
func TestV4ActionsExpireAndDiscreteActionsExecuteOnce(t *testing.T) {
	s := &AgentSession{mailbox: &v4Mailbox{last: v4Input{Sequence: 1, ObservedTick: 10, Action: v4Action{Throttle: 1, Dash: true}}, received: time.Now()}}
	first := string(s.takeV4Action(10))
	second := string(s.takeV4Action(11))
	if first != `{"throttle":1,"dash":true}` || second != `{"throttle":1}` {
		t.Fatalf("unexpected action handling %s %s", first, second)
	}
	if string(s.takeV4Action(16)) != `{}` {
		t.Fatal("stale action did not expire")
	}
}
