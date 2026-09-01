package engine

import (
	"context"
	"testing"
)

type fixedController struct{ intent Intent }

func (c *fixedController) Tick(context.Context, RobotState, []RobotState) (Intent, error) {
	return c.intent, nil
}
func (c *fixedController) Close() {}

func TestDeterministicDuel(t *testing.T) {
	robots := SpawnPositions([]RobotState{
		{RobotID: "red-1", Name: "Red", Team: "red"},
		{RobotID: "blue-1", Name: "Blue", Team: "blue"},
	})
	controllers := map[string]Controller{
		"red-1":  &fixedController{intent: Intent{Move: 8, Fire: true}},
		"blue-1": &fixedController{intent: Intent{Move: 2, Fire: true}},
	}

	first := New("match", robots, controllers)
	for !first.Finished() {
		first.Step(context.Background())
	}
	if first.Winner() == "" {
		t.Fatal("expected winner")
	}
	firstResult := first.snapshot(nil)

	second := New("match", robots, controllers)
	for !second.Finished() {
		second.Step(context.Background())
	}
	secondResult := second.snapshot(nil)

	if firstResult.WinnerTeam != secondResult.WinnerTeam || firstResult.Tick != secondResult.Tick {
		t.Fatalf("match not deterministic: first=%+v second=%+v", firstResult, secondResult)
	}
}

func TestValidateTeams(t *testing.T) {
	if ValidateTeams([]RobotState{{Team: "red"}}) == nil {
		t.Fatal("expected missing-team error")
	}
	if err := ValidateTeams([]RobotState{{Team: "red"}, {Team: "blue"}}); err != nil {
		t.Fatal(err)
	}
}

func TestProjectileTravelsAndDealsServerOwnedDamage(t *testing.T) {
	robots := []RobotState{
		{RobotID: "red-1", Name: "Red", Team: "red", X: 100, Y: 100, Heading: 0, HP: 100, Alive: true},
		{RobotID: "blue-1", Name: "Blue", Team: "blue", X: 170, Y: 100, Heading: 180, HP: 100, Alive: true},
	}
	controllers := map[string]Controller{
		"red-1":  &fixedController{intent: Intent{Fire: true}},
		"blue-1": &fixedController{},
	}
	arena := New("projectile", robots, controllers)
	first := arena.Step(context.Background())
	if len(first.Projectiles) != 1 || first.Robots[0].HP != 100 {
		t.Fatalf("shot should spawn before dealing damage: %+v", first)
	}
	for arena.Robots[0].HP == 100 {
		arena.Step(context.Background())
	}
	if arena.Robots[0].HP != 75 {
		t.Fatalf("expected server-owned 25 damage, got %d HP", arena.Robots[0].HP)
	}
}
