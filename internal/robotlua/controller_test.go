package robotlua

import (
	"context"
	"testing"

	"github.com/kryxen/cloud-robot/internal/engine"
)

func TestControllerProducesIntent(t *testing.T) {
	source := `function tick(robot)
  local enemy = robot.scan(700)
  robot.turn_toward(enemy.x, enemy.y)
  robot.move(3)
  robot.fire()
end`
	controller, err := New(source)
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()
	self := engine.RobotState{RobotID: "a", Team: "red", X: 10, Y: 10, Alive: true}
	other := engine.RobotState{RobotID: "b", Team: "blue", X: 100, Y: 10, Alive: true}
	intent, err := controller.Tick(context.Background(), self, []engine.RobotState{self, other})
	if err != nil {
		t.Fatal(err)
	}
	if !intent.Fire || intent.Move != 3 || intent.TargetX == nil {
		t.Fatalf("unexpected intent: %+v", intent)
	}
}

func TestForbiddenLibraries(t *testing.T) {
	controller, err := New(`function tick(robot) os.execute("whoami") end`)
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()
	self := engine.RobotState{RobotID: "a", Team: "red", Alive: true}
	if _, err := controller.Tick(context.Background(), self, []engine.RobotState{self}); err == nil {
		t.Fatal("expected os to be unavailable at runtime")
	}
}
