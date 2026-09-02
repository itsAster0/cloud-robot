package api

import (
	"testing"

	"github.com/kryxen/cloud-robot/internal/engine"
)

func BenchmarkSnapshotJSON(b *testing.B) {
	robots := make([]engine.RobotState, 10)
	for index := range robots {
		robots[index] = engine.RobotState{RobotID: "robot", Name: "benchmark", Team: "red", HP: 100, MaxHP: 100, Alive: true}
	}
	snapshot := engine.Snapshot{
		Type: "snapshot", Version: 3, MatchID: "benchmark", Sequence: 100, Tick: 100,
		MapID: "open-field", Width: 1200, Height: 750, Robots: robots,
		Obstacles: engine.DefaultMap(1200, 750).Obstacles,
	}
	b.ReportAllocs()
	for range b.N {
		if _, ok := encodeHubEvent(snapshot.MatchID, snapshot, false); !ok {
			b.Fatal("snapshot encoding failed")
		}
	}
}
