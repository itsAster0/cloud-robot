package engine

import (
	"context"
	"fmt"
	"testing"
)

type benchmarkController struct{ intent Intent }

func (c *benchmarkController) Tick(context.Context, RobotState, []RobotState) (Intent, error) {
	return c.intent, nil
}

func (c *benchmarkController) Close() {}

func BenchmarkArenaStepSquad(b *testing.B) {
	config := DefaultConfig()
	config.MaxTicks = b.N + 1
	config.Zone.Enabled = false
	config.PowerSurgeEveryTicks = 0
	config.Items.MaxConcurrent = 0
	robots := make([]RobotState, 0, 10)
	controllers := make(map[string]Controller, 10)
	for index := 0; index < 10; index++ {
		id := fmt.Sprintf("bot-%02d", index)
		team := "red"
		if index >= 5 {
			team = "blue"
		}
		robots = append(robots, RobotState{RobotID: id, Team: team})
		controllers[id] = &benchmarkController{intent: Intent{Move: 4, Turn: 2}}
	}
	robots = SpawnPositionsForMap(robots, config.Map, config.Width, config.Height)
	arena := NewWithConfig("benchmark-squad", robots, controllers, config)
	b.Cleanup(arena.Close)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		arena.Step(context.Background())
		if arena.Finished() {
			b.Fatal("benchmark arena finished before its tick budget")
		}
	}
}
