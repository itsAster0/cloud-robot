package engine

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"testing"
)

type fixedController struct{ intent Intent }

type worldCaptureController struct {
	fixedController
	world WorldState
}

func (c *worldCaptureController) SetWorld(world WorldState) { c.world = world }

func (c *fixedController) Tick(context.Context, RobotState, []RobotState) (Intent, error) {
	return c.intent, nil
}

func TestMapDimensionsObstacleCollisionAndLineOfSight(t *testing.T) {
	m := MapDefinition{ID: "test", Width: 600, Height: 400, Obstacles: []Obstacle{{ID: "wall", Shape: "aabb", X: 280, Y: 0, Width: 40, Height: 400}}}
	config := DefaultConfig()
	config.Map, config.Width, config.Height = m, 600, 400
	robots := []RobotState{{RobotID: "a", Team: "one", X: 250, Y: 200, Heading: 0, HP: 100, Alive: true}, {RobotID: "b", Team: "two", X: 500, Y: 200, HP: 100, Alive: true}}
	a := NewWithConfig("map", robots, map[string]Controller{"a": &fixedController{intent: Intent{Move: 8, Fire: true}}, "b": &fixedController{}}, config)
	if a.LineOfSight(250, 200, 500, 200) {
		t.Fatal("wall should block line of sight")
	}
	for range 10 {
		a.Step(context.Background())
	}
	if a.Robots[0].X > 280-RobotRadius || a.Robots[1].HP != 100 {
		t.Fatalf("wall did not block movement and projectile: %+v", a.Robots)
	}
}

func TestCustomArenaScalesMapGeometry(t *testing.T) {
	config := DefaultConfig()
	config.Width, config.Height = 1200, 800
	arena := NewWithConfig("scaled", nil, nil, config)
	spawn := arena.Config.Map.SpawnPoints["blue"][0]
	zone := arena.Config.Map.ItemSpawnZones[0]
	if spawn.X != 1050 || spawn.Y != 400 || zone.X != 360 || zone.Width != 480 {
		t.Fatalf("map geometry did not scale: spawn=%+v zone=%+v", spawn, zone)
	}
}

func TestArenaSuppliesWorldStateBeforeControllerTick(t *testing.T) {
	controller := &worldCaptureController{}
	config := DefaultConfig()
	config.Map = StarterMaps()["four-corners"]
	config.Width, config.Height = 900, 600
	arena := NewWithConfig("world", []RobotState{{RobotID: "a", Team: "red", Alive: true, HP: 100}, {RobotID: "b", Team: "blue", Alive: true, HP: 100}}, map[string]Controller{"a": controller, "b": &fixedController{}}, config)
	arena.Items = []Item{{ItemID: "heal", Type: "heal", Active: true}}
	arena.Step(context.Background())
	if controller.world.MapID != "four-corners" || controller.world.Width != 900 || len(controller.world.Obstacles) == 0 || len(controller.world.Items) != 1 {
		t.Fatalf("incomplete world state: %+v", controller.world)
	}
}

func TestItemsHonorPickupPreferencesAndApplyEffects(t *testing.T) {
	no := false
	robots := []RobotState{{RobotID: "a", Team: "one", X: 100, Y: 100, HP: 50, Alive: true}, {RobotID: "b", Team: "two", X: 700, Y: 400, HP: 100, Alive: true}}
	a := New("items", robots, map[string]Controller{"a": &fixedController{intent: Intent{AutoPickup: &no}}, "b": &fixedController{}})
	a.Items = []Item{{ItemID: "heal", Type: "heal", X: 100, Y: 100, Active: true, PickupRadius: 20}}
	a.Step(context.Background())
	if !a.Items[0].Active || a.Robots[0].HP != 50 {
		t.Fatal("disabled pickup consumed item")
	}
	yes := true
	a.controllers["a"] = &fixedController{intent: Intent{AutoPickup: &yes, PickupTypes: []string{"heal"}}}
	snapshot := a.Step(context.Background())
	if a.Items[0].Active || a.Robots[0].HP != 75 {
		t.Fatalf("heal not applied: %+v", a.Robots[0])
	}
	if !hasEvent(snapshot.Events, "item_picked_up") {
		t.Fatal("pickup event missing")
	}
}

func TestSpawnerItemRespawnsAndShieldDecaysOverTwentySeconds(t *testing.T) {
	config := DefaultConfig()
	config.Items.RespawnMinTicks, config.Items.RespawnMaxTicks = 1, 1
	a := NewWithConfig("items", []RobotState{{RobotID: "a", Team: "red", X: 100, Y: 100, HP: 100, Alive: true}}, map[string]Controller{"a": &fixedController{}}, config)
	a.Items = []Item{{ItemID: "shield", Type: "shield", X: 100, Y: 100, Active: true, PickupRadius: 20, Source: "spawner"}}
	events := a.pickupItems()
	if len(events) != 1 || a.Items[0].Active || a.Items[0].RespawnTick != 1 || a.Robots[0].Shield != 50 {
		t.Fatalf("pickup did not schedule respawn: %+v %+v", a.Items[0], a.Robots[0])
	}
	a.TickNumber = 1
	if events = a.spawnItems(); len(events) != 1 || events[0].Type != "item_respawned" || !a.Items[0].Active {
		t.Fatalf("item did not respawn: %+v %+v", events, a.Items[0])
	}
	for tick := 0; tick < 200; tick++ {
		a.applyEffects(&a.Robots[0], &events)
	}
	if a.Robots[0].Shield != 0 {
		t.Fatalf("shield should decay after 200 ticks, got %d", a.Robots[0].Shield)
	}
}

func TestHazardDamageInsideZoneOnly(t *testing.T) {
	for _, kind := range []string{"damage", "damage-edge"} {
		m := DefaultMap(800, 500)
		m.Hazards = []Hazard{{ID: "lava", Type: kind, X: 300, Y: 200, Width: 200, Height: 100, Damage: 3}}
		config := DefaultConfig()
		config.Map, config.Width, config.Height = m, 800, 500
		robots := []RobotState{{RobotID: "a", Team: "one", X: 400, Y: 250, HP: 100, Alive: true}, {RobotID: "b", Team: "two", X: 100, Y: 100, HP: 100, Alive: true}}
		a := NewWithConfig("hazards", robots, map[string]Controller{"a": &fixedController{}, "b": &fixedController{}}, config)
		sawEvent := false
		for range 11 {
			snapshot := a.Step(context.Background())
			for _, event := range snapshot.Events {
				if event.Type == "hazard_damage" && event.TargetID == "a" && event.Damage == 3 && event.Message == "lava" {
					sawEvent = true
				}
			}
		}
		if !sawEvent || a.Robots[0].HP != 94 || a.Robots[1].HP != 100 {
			t.Fatalf("hazard %s did not tick inside the zone: inside=%d outside=%d event=%v", kind, a.Robots[0].HP, a.Robots[1].HP, sawEvent)
		}
		for range 10 {
			a.Step(context.Background())
		}
		if a.Robots[0].HP != 91 || a.Robots[1].HP != 100 {
			t.Fatalf("hazard %s damage not cumulative: inside=%d outside=%d", kind, a.Robots[0].HP, a.Robots[1].HP)
		}
	}
}

func TestCraterEdgeHazardDamagesRobots(t *testing.T) {
	config := DefaultConfig()
	config.Map = StarterMaps()["crater"]
	a := NewWithConfig("crater", []RobotState{{RobotID: "a", Team: "red", X: 90, Y: 300, HP: 100, Alive: true}, {RobotID: "b", Team: "blue", X: 810, Y: 300, HP: 100, Alive: true}}, map[string]Controller{"a": &fixedController{}, "b": &fixedController{}}, config)
	for range 11 {
		a.Step(context.Background())
	}
	if a.Robots[0].HP != 96 || a.Robots[1].HP != 96 {
		t.Fatalf("crater edge hazard did not tick: %d %d", a.Robots[0].HP, a.Robots[1].HP)
	}
	hits := 0
	for _, event := range a.EventLog() {
		if event.Type == "hazard_damage" {
			hits++
		}
	}
	if hits != 4 {
		t.Fatalf("expected hazard events for both robots, got %d", hits)
	}
}

func TestNTeamValidationAndSpawn(t *testing.T) {
	robots := []RobotState{{RobotID: "a", Team: "alpha"}, {RobotID: "b", Team: "beta"}, {RobotID: "c", Team: "gamma"}, {RobotID: "d", Team: "alpha"}}
	if err := ValidateTeams(robots); err != nil {
		t.Fatal(err)
	}
	spawned := SpawnPositionsForMap(robots, DefaultMap(1200, 800), 1200, 800)
	positions := map[[2]float64]bool{}
	for _, r := range spawned {
		key := [2]float64{r.X, r.Y}
		if positions[key] {
			t.Fatal("spawn overlap")
		}
		positions[key] = true
	}
}

func TestOvertimeZoneStatsAndDeterministicEvents(t *testing.T) {
	config := DefaultConfig()
	config.MaxTicks = 2
	config.OvertimeTicks = 2
	config.Zone = ZoneConfig{Enabled: true, StartTick: 0, EndTick: 1, EndRadius: 10, Damage: 1, DamageInterval: 1}
	config.Items.MaxConcurrent = 1
	config.Items.SpawnMinTicks, config.Items.SpawnMaxTicks = 1, 1
	robots := []RobotState{{RobotID: "a", Team: "one", X: 20, Y: 20, HP: 100, Alive: true}, {RobotID: "b", Team: "two", X: 780, Y: 480, HP: 100, Alive: true}}
	controllers := map[string]Controller{"a": &fixedController{}, "b": &fixedController{}}
	run := func() ([]byte, Snapshot) {
		a := NewWithConfig("seeded", robots, controllers, config)
		var s Snapshot
		for !a.Finished() {
			s = a.Step(context.Background())
		}
		encoded, err := json.Marshal(a.EventLog())
		if err != nil {
			t.Fatal(err)
		}
		return encoded, s
	}
	first, firstSnapshot := run()
	second, _ := run()
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("event logs differ:\n%s\n%s", first, second)
	}
	if firstSnapshot.Tick != 4 || firstSnapshot.WinnerTeam != "draw" {
		t.Fatalf("overtime result wrong: %+v", firstSnapshot)
	}
}

func TestWeaponsAndBotController(t *testing.T) {
	if WeaponByName("cannon").Damage != 60 || !WeaponByName("railgun").Hitscan {
		t.Fatal("weapon table incomplete")
	}
	bot := NewBotController("bot", BotSharpshooter, PersonalityAggressive, DefaultMap(800, 500))
	self := RobotState{RobotID: "bot", Team: "red", X: 100, Y: 100, Heading: 0, Alive: true, Weapon: "plasma"}
	enemy := RobotState{RobotID: "enemy", Team: "blue", X: 300, Y: 100, Alive: true}
	intent, err := bot.Tick(context.Background(), self, []RobotState{self, enemy})
	if err != nil || !intent.Fire || intent.TargetX == nil {
		t.Fatalf("bot did not target visible enemy: %+v %v", intent, err)
	}
}

func hasEvent(events []Event, kind string) bool {
	for _, event := range events {
		if event.Type == kind {
			return true
		}
	}
	return false
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

func TestWithdrawEliminatesRobotAndEndsMatch(t *testing.T) {
	robots := SpawnPositions([]RobotState{
		{RobotID: "red-1", Name: "Red", Team: "red"},
		{RobotID: "blue-1", Name: "Blue", Team: "blue"},
	})
	controllers := map[string]Controller{
		"red-1":  &fixedController{intent: Intent{Move: 8}},
		"blue-1": &fixedController{intent: Intent{Move: 8}},
	}
	arena := New("withdraw", robots, controllers)
	arena.Step(context.Background())

	if !arena.RequestWithdraw("red-1") {
		t.Fatal("withdraw request rejected")
	}
	// The concession applies at the next tick, not immediately.
	for _, robot := range arena.Robots {
		if robot.RobotID == "red-1" && !robot.Alive {
			t.Fatal("withdraw applied before the next tick")
		}
	}
	snapshot := arena.Step(context.Background())

	var withdrawn, survivor *RobotState
	for i := range snapshot.Robots {
		switch snapshot.Robots[i].RobotID {
		case "red-1":
			withdrawn = &snapshot.Robots[i]
		case "blue-1":
			survivor = &snapshot.Robots[i]
		}
	}
	if withdrawn == nil || survivor == nil {
		t.Fatal("missing robots in snapshot")
	}
	if withdrawn.Alive || withdrawn.HP != 0 || withdrawn.Failed {
		t.Fatalf("withdrawn robot should be eliminated without failure: %+v", withdrawn)
	}
	if survivor.Kills != 0 {
		t.Fatalf("withdrawal must not credit a kill: %+v", survivor)
	}
	if !arena.Finished() || arena.Winner() != "blue" {
		t.Fatalf("opponent should win by elimination: finished=%v winner=%q", arena.Finished(), arena.Winner())
	}
	found := false
	for _, event := range snapshot.Events {
		if event.Type == "robot_withdrawn" && event.RobotID == "red-1" {
			found = true
		}
		if event.Type == "robot_destroyed" {
			t.Fatalf("withdrawal must not emit robot_destroyed: %+v", event)
		}
	}
	if !found {
		t.Fatal("expected robot_withdrawn event")
	}

	// Repeated or unknown withdrawals after the match ends stay no-ops.
	arena.RequestWithdraw("red-1")
	arena.RequestWithdraw("ghost")
	after := arena.Step(context.Background())
	for _, event := range after.Events {
		if event.Type == "robot_withdrawn" {
			t.Fatalf("duplicate withdrawal event: %+v", event)
		}
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

func TestMovementSlidesAlongObstacles(t *testing.T) {
	config := DefaultConfig()
	config.Width, config.Height = 800, 500
	config.Map = DefaultMap(800, 500)
	config.Map.Obstacles = []Obstacle{{ID: "wall", Shape: "aabb", X: 400, Y: 0, Width: 40, Height: 260}}
	robots := []RobotState{
		{RobotID: "red-1", Name: "Red", Team: "red", X: 380, Y: 200, Heading: 45, HP: 100, Alive: true},
		{RobotID: "blue-1", Name: "Blue", Team: "blue", X: 700, Y: 400, Heading: 180, HP: 100, Alive: true},
	}
	controllers := map[string]Controller{
		"red-1":  &fixedController{intent: Intent{Move: MaxMovePerTick}},
		"blue-1": &fixedController{},
	}
	arena := NewWithConfig("slide", robots, controllers, config)
	for range 60 {
		arena.Step(context.Background())
	}
	if arena.Robots[0].X <= 440 {
		t.Fatalf("robot dead-stopped against the wall instead of sliding: %+v", arena.Robots[0])
	}
}

func TestBotEscapesBlockedPath(t *testing.T) {
	m := DefaultMap(800, 500)
	m.Obstacles = []Obstacle{{ID: "wall", Shape: "aabb", X: 140, Y: 150, Width: 60, Height: 200}}
	bot := NewBotController("bot-1", BotFighter, PersonalityAggressive, m)
	self := RobotState{RobotID: "bot-1", Team: "red", X: 100, Y: 250, Heading: 0, HP: 100, Alive: true, Weapon: "plasma"}
	// Visible enemy whose line of sight stays clear of the wall ahead.
	enemy := RobotState{RobotID: "enemy", Team: "blue", X: 139, Y: 100, Heading: 180, HP: 100, Alive: true}
	intent, err := bot.Tick(context.Background(), self, []RobotState{self, enemy})
	if err != nil {
		t.Fatal(err)
	}
	if intent.Move != -MaxMovePerTick/2 || intent.TargetX != nil {
		t.Fatalf("blocked bot must back off without a chase target: %+v", intent)
	}
	if math.Abs(intent.Turn) < 12 || math.Abs(intent.Turn) > 24 {
		t.Fatalf("escape turn out of range: %v", intent.Turn)
	}
}

func TestBotIntentsAreDeterministicPerSeed(t *testing.T) {
	run := func() []Intent {
		bot := NewBotController("bot-1", BotFighter, PersonalityAggressive, DefaultMap(800, 500))
		intents := []Intent{}
		self := RobotState{RobotID: "bot-1", Team: "red", X: 100, Y: 100, Heading: 0, HP: 100, Alive: true, Weapon: "plasma"}
		enemy := RobotState{RobotID: "enemy", Team: "blue", X: 300, Y: 220, Heading: 90, HP: 100, Alive: true}
		for range 50 {
			intent, err := bot.Tick(context.Background(), self, []RobotState{self, enemy})
			if err != nil {
				t.Fatal(err)
			}
			intents = append(intents, intent)
		}
		return intents
	}
	if !reflect.DeepEqual(run(), run()) {
		t.Fatal("bot intents diverged for identical seeds")
	}
}
