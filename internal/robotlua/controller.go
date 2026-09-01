package robotlua

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/kryxen/cloud-robot/internal/engine"
	"github.com/yuin/gopher-lua"
)

const (
	MaxScriptBytes = 16 * 1024
	MaxLogBytes    = 256
	TickTimeout    = 5 * time.Millisecond
)

type Controller struct{ state *lua.LState }

func New(source string) (*Controller, error) {
	if len(source) == 0 {
		return nil, errors.New("script is empty")
	}
	if len(source) > MaxScriptBytes {
		return nil, fmt.Errorf("script exceeds %d bytes", MaxScriptBytes)
	}

	state := lua.NewState(lua.Options{SkipOpenLibs: true})
	lua.OpenBase(state)
	lua.OpenTable(state)
	lua.OpenString(state)
	lua.OpenMath(state)
	for _, name := range []string{"dofile", "loadfile", "load", "loadstring", "collectgarbage", "require", "module"} {
		state.SetGlobal(name, lua.LNil)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	state.SetContext(ctx)
	if err := state.DoString(source); err != nil {
		state.Close()
		return nil, fmt.Errorf("lua validation failed: %w", err)
	}
	state.RemoveContext()
	if state.GetGlobal("tick").Type() != lua.LTFunction {
		state.Close()
		return nil, errors.New("script must define function tick(robot)")
	}
	return &Controller{state: state}, nil
}

func Validate(source string) error {
	controller, err := New(source)
	if err == nil {
		controller.Close()
	}
	return err
}

func (c *Controller) Tick(parent context.Context, self engine.RobotState, robots []engine.RobotState) (engine.Intent, error) {
	intent := engine.Intent{}
	robot := c.state.NewTable()

	c.state.SetField(robot, "move", c.state.NewFunction(func(state *lua.LState) int {
		intent.Move = float64(state.CheckNumber(1))
		return 0
	}))
	c.state.SetField(robot, "turn", c.state.NewFunction(func(state *lua.LState) int {
		intent.Turn = float64(state.CheckNumber(1))
		return 0
	}))
	c.state.SetField(robot, "turn_toward", c.state.NewFunction(func(state *lua.LState) int {
		x, y := float64(state.CheckNumber(1)), float64(state.CheckNumber(2))
		intent.TargetX, intent.TargetY = &x, &y
		return 0
	}))
	c.state.SetField(robot, "fire", c.state.NewFunction(func(state *lua.LState) int {
		intent.Fire = true
		return 0
	}))
	c.state.SetField(robot, "scan", c.state.NewFunction(func(state *lua.LState) int {
		rangeLimit := float64(state.OptNumber(1, 700))
		target, ok := nearestEnemy(self, robots, rangeLimit)
		if !ok {
			state.Push(lua.LNil)
			return 1
		}
		result := state.NewTable()
		state.SetField(result, "id", lua.LString(target.RobotID))
		state.SetField(result, "x", lua.LNumber(target.X))
		state.SetField(result, "y", lua.LNumber(target.Y))
		state.SetField(result, "hp", lua.LNumber(target.HP))
		state.SetField(result, "distance", lua.LNumber(math.Hypot(target.X-self.X, target.Y-self.Y)))
		state.Push(result)
		return 1
	}))
	c.state.SetField(robot, "self", c.state.NewFunction(func(state *lua.LState) int {
		result := state.NewTable()
		state.SetField(result, "x", lua.LNumber(self.X))
		state.SetField(result, "y", lua.LNumber(self.Y))
		state.SetField(result, "heading", lua.LNumber(self.Heading))
		state.SetField(result, "hp", lua.LNumber(self.HP))
		state.SetField(result, "cooldown", lua.LNumber(self.Cooldown))
		state.Push(result)
		return 1
	}))
	c.state.SetField(robot, "log", c.state.NewFunction(func(state *lua.LState) int {
		message := strings.ToValidUTF8(state.CheckString(1), "?")
		if len(message) > MaxLogBytes {
			message = message[:MaxLogBytes]
		}
		intent.Logs = append(intent.Logs, message)
		return 0
	}))

	ctx, cancel := context.WithTimeout(parent, TickTimeout)
	defer cancel()
	c.state.SetContext(ctx)
	err := c.state.CallByParam(lua.P{Fn: c.state.GetGlobal("tick"), NRet: 0, Protect: true}, robot)
	c.state.RemoveContext()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return engine.Intent{}, errors.New("script tick exceeded time limit")
		}
		return engine.Intent{}, fmt.Errorf("script tick failed: %w", err)
	}
	return intent, nil
}

func (c *Controller) Close() {
	if c.state != nil {
		c.state.Close()
	}
}

func nearestEnemy(self engine.RobotState, robots []engine.RobotState, rangeLimit float64) (engine.RobotState, bool) {
	var nearest engine.RobotState
	best := math.MaxFloat64
	for _, candidate := range robots {
		if !candidate.Alive || candidate.Team == self.Team || candidate.RobotID == self.RobotID {
			continue
		}
		distance := math.Hypot(candidate.X-self.X, candidate.Y-self.Y)
		if distance <= rangeLimit && distance < best {
			nearest, best = candidate, distance
		}
	}
	return nearest, best != math.MaxFloat64
}
