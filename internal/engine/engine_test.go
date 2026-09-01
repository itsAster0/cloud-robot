package engine

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"strings"
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
	config := DefaultConfig()
	config.CriticalChance = 0 // exact damage math, no crit spikes
	arena := NewWithConfig("projectile", robots, controllers, config)
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

func TestSoloSandboxRunsToTickLimit(t *testing.T) {
	config := DefaultConfig()
	config.MaxTicks = 3
	config.OvertimeTicks = 0
	robots := SpawnPositionsForMap([]RobotState{{RobotID: "a", Team: "solo-01"}}, DefaultMap(800, 500), 800, 500)
	arena := NewWithConfig("solo-sandbox", robots, map[string]Controller{"a": &fixedController{}}, config)
	var snap Snapshot
	for !arena.Finished() {
		snap = arena.Step(context.Background())
	}
	if snap.Tick != 3 || snap.WinnerTeam != "solo-01" {
		t.Fatalf("single-team match should run to the tick limit: tick=%d winner=%q", snap.Tick, snap.WinnerTeam)
	}
}

func TestSoloSandboxEndsWhenRobotDies(t *testing.T) {
	robots := SpawnPositionsForMap([]RobotState{{RobotID: "a", Team: "solo-01"}}, DefaultMap(800, 500), 800, 500)
	arena := NewWithConfig("solo-sandbox", robots, map[string]Controller{"a": &fixedController{}}, DefaultConfig())
	if arena.Finished() {
		t.Fatal("solo sandbox must not finish at tick 0")
	}
	if !arena.RequestWithdraw("a") {
		t.Fatal("withdraw request rejected")
	}
	snap := arena.Step(context.Background())
	if !arena.Finished() || arena.Winner() != "draw" {
		t.Fatalf("dead solo robot should end the match as a draw: finished=%v winner=%q", arena.Finished(), arena.Winner())
	}
	_ = snap
}

func TestSoloFreeForAllLastStandingWins(t *testing.T) {
	robots := SpawnPositionsForMap([]RobotState{
		{RobotID: "a", Team: "solo-01"},
		{RobotID: "b", Team: "solo-02"},
		{RobotID: "c", Team: "solo-03"},
	}, DefaultMap(800, 500), 800, 500)
	arena := New("solo-ffa", robots, map[string]Controller{"a": &fixedController{}, "b": &fixedController{}, "c": &fixedController{}})
	if !arena.RequestWithdraw("a") || !arena.RequestWithdraw("b") {
		t.Fatal("withdraw requests rejected")
	}
	arena.Step(context.Background())
	if !arena.Finished() || arena.Winner() != "solo-03" {
		t.Fatalf("last standing robot should win: finished=%v winner=%q", arena.Finished(), arena.Winner())
	}
}

func TestSoloValidateTeamsAllowsSingleTeam(t *testing.T) {
	if err := ValidateTeamsForMode("solo", []RobotState{{Team: "solo-01"}}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateTeamsForMode("solo", []RobotState{{Team: ""}}); err == nil {
		t.Fatal("empty team must stay invalid")
	}
	if err := ValidateTeamsForMode("squad", []RobotState{{Team: "solo-01"}}); err == nil {
		t.Fatal("non-solo modes still need two teams")
	}
}

func TestSpawnWithoutMapPointsAvoidsObstacles(t *testing.T) {
	for _, mapID := range []string{"four-corners", "pillars", "corridors"} {
		m := StarterMaps()[mapID]
		robots := SpawnPositionsForMap([]RobotState{
			{RobotID: "a", Team: "solo-01"},
			{RobotID: "b", Team: "solo-02"},
			{RobotID: "c", Team: "solo-03"},
		}, m, m.Width, m.Height)
		for _, r := range robots {
			if collidesRobot(m.Obstacles, r.X, r.Y) {
				t.Fatalf("%s: robot %s spawned inside an obstacle at (%.0f, %.0f)", mapID, r.RobotID, r.X, r.Y)
			}
		}
	}
}

// duelFixture returns a two-robot arena where red fires each tick from a fixed
// range. Robots are sorted by ID inside the arena, so tests must look robots
// up by ID instead of index.
func duelFixture(matchID string, criticalChance int) *Arena {
	robots := []RobotState{
		{RobotID: "red-1", Name: "Red", Team: "red", X: 100, Y: 100, Heading: 0, HP: 100, Alive: true},
		{RobotID: "blue-1", Name: "Blue", Team: "blue", X: 170, Y: 100, Heading: 180, HP: 100, Alive: true},
	}
	config := DefaultConfig()
	config.CriticalChance = criticalChance
	return NewWithConfig(matchID, robots, map[string]Controller{"red-1": &fixedController{intent: Intent{Fire: true}}, "blue-1": &fixedController{}}, config)
}

func arenaRobot(a *Arena, id string) *RobotState {
	for i := range a.Robots {
		if a.Robots[i].RobotID == id {
			return &a.Robots[i]
		}
	}
	return nil
}

func TestCriticalChanceZeroKeepsStockDamage(t *testing.T) {
	arena := duelFixture("no-crit", 0)
	blue := arenaRobot(arena, "blue-1")
	for blue.HP == 100 {
		arena.Step(context.Background())
	}
	if blue.HP != 75 {
		t.Fatalf("expected stock 25 plasma damage, got %d HP", blue.HP)
	}
	for _, event := range arena.EventLog() {
		if event.Type == "critical_hit" {
			t.Fatalf("crits must be disabled at 0%%: %+v", event)
		}
	}
}

func TestCriticalHitsAlwaysTriggerAtHundredPercent(t *testing.T) {
	arena := duelFixture("always-crit", 100)
	blue := arenaRobot(arena, "blue-1")
	for blue.HP == 100 {
		arena.Step(context.Background())
	}
	if blue.HP != 63 {
		t.Fatalf("expected 25*3/2 = 37 crit damage, got %d HP", blue.HP)
	}
	crits := 0
	for _, event := range arena.EventLog() {
		if event.Type == "critical_hit" {
			crits++
			if event.Damage != 37 || event.TargetID != "blue-1" {
				t.Fatalf("unexpected critical event: %+v", event)
			}
		}
	}
	if crits == 0 {
		t.Fatal("no critical_hit event at 100% chance")
	}
}

// Same seed must produce the same crit pattern; different seeds should
// diverge (checked loosely, since a 50% chance can coincide by chance).
func TestCriticalPatternIsDeterministicPerSeed(t *testing.T) {
	run := func() int {
		arena := duelFixture("crit-seed", 50)
		blue := arenaRobot(arena, "blue-1")
		for blue.HP > 20 {
			arena.Step(context.Background())
		}
		crits := 0
		for _, event := range arena.EventLog() {
			if event.Type == "critical_hit" {
				crits++
			}
		}
		return crits
	}
	if first := run(); first != run() {
		t.Fatalf("crit pattern differs across same-seed runs: %d", first)
	}
}

func TestSpawnerDropsWeaponsSupplyDropsAndStaysDeterministic(t *testing.T) {
	config := DefaultConfig()
	config.Items.SpawnMinTicks, config.Items.SpawnMaxTicks = 1, 1
	config.Items.MaxConcurrent = 60
	run := func() ([]string, int) {
		arena := NewWithConfig("loot", []RobotState{{RobotID: "a", Team: "red", X: 100, Y: 100, HP: 100, Alive: true}}, map[string]Controller{"a": &fixedController{}}, config)
		for range 600 {
			arena.Step(context.Background())
		}
		kinds := []string{}
		active := 0
		for _, item := range arena.Items {
			kinds = append(kinds, item.Type)
			if item.Active {
				active++
			}
		}
		drops := 0
		for _, event := range arena.EventLog() {
			if event.Type == "supply_drop" {
				drops++
			}
		}
		return kinds, drops
	}
	// Robot idles far from the spawn zone, so nothing is picked up and every
	// spawned item stays active for inspection.
	kinds, drops := run()
	again, againDrops := run()
	weapons := 0
	for _, kind := range kinds {
		if strings.HasPrefix(kind, "weapon_") {
			weapons++
		}
	}
	if weapons == 0 {
		t.Fatalf("no weapon items in %d spawner drops", len(kinds))
	}
	if drops == 0 {
		t.Fatal("no supply drop bursts in 600 ticks")
	}
	if !reflect.DeepEqual(kinds, again) || drops != againDrops {
		t.Fatal("loot sequence differs across same-seed runs")
	}
}

func TestApplyItemArenaGrantsNewItems(t *testing.T) {
	a := New("item-grants", []RobotState{{RobotID: "a", Team: "red", X: 100, Y: 100, HP: 40, Alive: true}}, map[string]Controller{"a": &fixedController{}})
	r := &a.Robots[0]
	if value, _ := a.applyItemArena(r, "medkit"); value != 60 || r.HP != 100 {
		t.Fatalf("medkit should heal 60: value=%d hp=%d", value, r.HP)
	}
	if value, _ := a.applyItemArena(r, "medkit"); value != -1 {
		t.Fatalf("medkit at full HP must stay on the floor, got %d", value)
	}
	if value, _ := a.applyItemArena(r, "nano_repair"); value != 150 || !effectActive(*r, "regen") {
		t.Fatalf("nano_repair missing regen: value=%d effects=%+v", value, r.Effects)
	}
	if value, _ := a.applyItemArena(r, "armor_plate"); value != 150 || effectMultiplier(*r, "armor", 1) != 0.6 {
		t.Fatalf("armor_plate missing armor effect: value=%d effects=%+v", value, r.Effects)
	}
	r.Cooldown = 7
	upsertEffect(r, "emp", 30, 1, "")
	upsertEffect(r, "slow", 50, .5, "")
	upsertEffect(r, "burn", 50, 2, "")
	if value, _ := a.applyItemArena(r, "battery"); value != 1 || r.Cooldown != 0 || effectActive(*r, "emp") || effectActive(*r, "slow") || effectActive(*r, "burn") {
		t.Fatalf("battery must clear cooldown and debuffs: value=%d cooldown=%d effects=%+v", value, r.Cooldown, r.Effects)
	}
	if value, _ := a.applyItemArena(r, "cloak"); value != 90 || !isCloaked(*r) {
		t.Fatalf("cloak item missing cloak effect: value=%d effects=%+v", value, r.Effects)
	}
	if value, _ := a.applyItemArena(r, "scanner"); value != 150 || !effectActive(*r, "radar") {
		t.Fatalf("scanner missing radar effect: value=%d effects=%+v", value, r.Effects)
	}
	if value, _ := a.applyItemArena(r, "berserker_charm"); value != 80 || !effectActive(*r, "berserk") {
		t.Fatalf("berserker_charm missing berserk: value=%d effects=%+v", value, r.Effects)
	}
	if value, _ := a.applyItemArena(r, "vampiric_fang"); value != 100 || !effectActive(*r, "vampiric") {
		t.Fatalf("vampiric_fang missing vampiric: value=%d effects=%+v", value, r.Effects)
	}
	if value, _ := a.applyItemArena(r, "frenzy"); value != 60 || effectMultiplier(*r, "overdrive", 1) != 1.5 || effectMultiplier(*r, "rapid_fire", 1) != 0.5 {
		t.Fatalf("frenzy must upsert overdrive and rapid_fire: value=%d effects=%+v", value, r.Effects)
	}
}

func TestTeleportBeaconRelocatesToOpenSpot(t *testing.T) {
	m := DefaultMap(800, 500)
	m.Obstacles = []Obstacle{{ID: "slab", Shape: "aabb", X: 0, Y: 0, Width: 400, Height: 250}}
	config := DefaultConfig()
	config.Map, config.Width, config.Height = m, 800, 500
	a := NewWithConfig("teleport", []RobotState{{RobotID: "a", Team: "red", X: 100, Y: 100, HP: 100, Alive: true}}, map[string]Controller{"a": &fixedController{}}, config)
	r := &a.Robots[0]
	value, extra := a.applyItemArena(r, "teleport_beacon")
	if value != 1 || len(extra) != 1 || extra[0].Type != "teleport" || extra[0].RobotID != "a" {
		t.Fatalf("teleport_beacon should emit one teleport event: value=%d extra=%+v", value, extra)
	}
	if r.X < RobotRadius || r.X > a.Config.Width-RobotRadius || r.Y < RobotRadius || r.Y > a.Config.Height-RobotRadius {
		t.Fatalf("teleport landed outside the arena: %+v", r)
	}
	if collidesRobot(a.Config.Map.Obstacles, r.X, r.Y) {
		t.Fatalf("teleport landed inside an obstacle: %+v", r)
	}
}

func TestRegenEffectHealsAndExpires(t *testing.T) {
	a := New("regen", []RobotState{{RobotID: "a", Team: "red", X: 100, Y: 100, HP: 50, Alive: true}}, map[string]Controller{"a": &fixedController{}})
	r := &a.Robots[0]
	upsertEffect(r, "regen", 150, 2, "")
	events := []Event{}
	for range 150 {
		a.applyEffects(r, &events)
	}
	if r.HP != 80 || len(events) != 15 {
		t.Fatalf("regen should heal 2 HP every 10 ticks: hp=%d events=%d", r.HP, len(events))
	}
	for _, event := range events {
		if event.Type != "regen_tick" || event.TargetID != "a" || event.Value != 2 {
			t.Fatalf("bad regen event: %+v", event)
		}
	}
	if effectActive(*r, "regen") {
		t.Fatal("regen must expire after its ticks run out")
	}
}

func TestArmorReducesIncomingDamage(t *testing.T) {
	a := New("armor", []RobotState{{RobotID: "a", Team: "red", X: 100, Y: 100, HP: 100, Alive: true}}, map[string]Controller{"a": &fixedController{}})
	r := &a.Robots[0]
	upsertEffect(r, "armor", 150, 0.6, "")
	a.damage(nil, r, 25, Weapon{})
	if r.HP != 85 {
		t.Fatalf("armor 0.6 should cut 25 damage to 15, got HP %d", r.HP)
	}
}

func TestBerserkBoostsOutgoingAndIncoming(t *testing.T) {
	a := New("berserk", []RobotState{{RobotID: "a", Team: "red", X: 100, Y: 100, Heading: 0, HP: 100, Alive: true}, {RobotID: "b", Team: "blue", X: 700, Y: 400, HP: 100, Alive: true}}, map[string]Controller{"a": &fixedController{}, "b": &fixedController{}})
	shooter, victim := arenaRobot(a, "a"), arenaRobot(a, "b")
	upsertEffect(shooter, "berserk", 80, 1, "")
	a.fire(shooter)
	if len(a.Projectiles) != 1 || a.Projectiles[0].Damage != 38 {
		t.Fatalf("berserk plasma should carry 38 damage (25*1.5): %+v", a.Projectiles)
	}
	upsertEffect(victim, "berserk", 80, 1, "")
	a.damage(nil, victim, 25, Weapon{})
	if victim.HP != 69 {
		t.Fatalf("berserk victim should take 31 damage (25*1.25), got HP %d", victim.HP)
	}
}

func TestVampiricHealsSourceOnHit(t *testing.T) {
	a := New("vampiric", []RobotState{{RobotID: "a", Team: "red", X: 100, Y: 100, HP: 50, Alive: true}, {RobotID: "b", Team: "blue", X: 700, Y: 400, HP: 100, Alive: true}}, map[string]Controller{"a": &fixedController{}, "b": &fixedController{}})
	source, target := arenaRobot(a, "a"), arenaRobot(a, "b")
	upsertEffect(source, "vampiric", 100, 1, "")
	events := a.damage(source, target, 25, Weapon{})
	if target.HP != 75 || source.HP != 56 {
		t.Fatalf("vampiric should heal source by 25/4: target=%d source=%d", target.HP, source.HP)
	}
	heals := 0
	for _, event := range events {
		if event.Type == "vampiric_heal" {
			heals++
			if event.RobotID != "a" || event.Value != 6 {
				t.Fatalf("bad vampiric_heal event: %+v", event)
			}
		}
	}
	if heals != 1 {
		t.Fatalf("expected one vampiric_heal event, got %d", heals)
	}
}

func TestCloakBreaksOnFireAndOnHit(t *testing.T) {
	a := New("cloak", []RobotState{{RobotID: "a", Team: "red", X: 100, Y: 100, Heading: 0, HP: 100, Alive: true}, {RobotID: "b", Team: "blue", X: 700, Y: 400, HP: 100, Alive: true}}, map[string]Controller{"a": &fixedController{}, "b": &fixedController{}})
	upsertEffect(arenaRobot(a, "a"), "cloak", 90, 1, "")
	if !isCloaked(*arenaRobot(a, "a")) {
		t.Fatal("cloak effect should be active")
	}
	a.fire(arenaRobot(a, "a"))
	if isCloaked(*arenaRobot(a, "a")) {
		t.Fatal("firing must break cloak")
	}
	upsertEffect(arenaRobot(a, "b"), "cloak", 90, 1, "")
	a.damage(nil, arenaRobot(a, "b"), 10, Weapon{})
	if isCloaked(*arenaRobot(a, "b")) {
		t.Fatal("taking a hit must break cloak")
	}
}

func TestShotgunFiresFivePellets(t *testing.T) {
	a := New("shotgun", []RobotState{{RobotID: "a", Team: "red", X: 100, Y: 100, Heading: 0, HP: 100, Alive: true, Weapon: "shotgun"}}, map[string]Controller{"a": &fixedController{}})
	events := a.fire(&a.Robots[0])
	if len(a.Projectiles) != 5 {
		t.Fatalf("shotgun should spawn 5 pellets, got %d", len(a.Projectiles))
	}
	for _, p := range a.Projectiles {
		if p.OwnerID != "a" || p.Kind != "shotgun" || p.Damage != 10 {
			t.Fatalf("bad pellet: %+v", p)
		}
	}
	if !hasEvent(events, "shot_fired") {
		t.Fatal("shot_fired event missing")
	}
}

func TestGrenadeBlastReplacesDirectHitAndSkipsFriendlies(t *testing.T) {
	config := DefaultConfig()
	config.CriticalChance = 0
	config.FriendlyFire = false
	robots := []RobotState{
		{RobotID: "shooter", Team: "red", X: 400, Y: 250, Heading: 0, HP: 100, Alive: true, Weapon: "grenade"},
		{RobotID: "enemy", Team: "blue", X: 450, Y: 250, HP: 100, Alive: true},
		{RobotID: "mate", Team: "red", X: 410, Y: 250, HP: 100, Alive: true},
		{RobotID: "far", Team: "blue", X: 700, Y: 400, HP: 100, Alive: true},
	}
	a := NewWithConfig("grenade", robots, map[string]Controller{}, config)
	a.fire(arenaRobot(a, "shooter"))
	if len(a.Projectiles) != 1 {
		t.Fatalf("grenade should spawn one projectile: %+v", a.Projectiles)
	}
	events := []Event{}
	for len(a.Projectiles) > 0 {
		events = append(events, a.advanceProjectiles()...)
	}
	blasted := false
	for _, event := range events {
		if event.Type == "explosion" {
			blasted = true
			if event.RobotID != "shooter" || event.Value != 60 {
				t.Fatalf("bad explosion event: %+v", event)
			}
		}
	}
	if !blasted {
		t.Fatal("explosion event missing")
	}
	if arenaRobot(a, "enemy").HP != 40 {
		t.Fatalf("enemy inside the blast should take 60 once, got HP %d", arenaRobot(a, "enemy").HP)
	}
	if arenaRobot(a, "shooter").HP != 100 {
		t.Fatalf("owner must not take self-damage, got HP %d", arenaRobot(a, "shooter").HP)
	}
	if arenaRobot(a, "mate").HP != 100 {
		t.Fatalf("teammate must be safe with friendly fire off, got HP %d", arenaRobot(a, "mate").HP)
	}
	if arenaRobot(a, "far").HP != 100 {
		t.Fatalf("robot outside the blast radius should be untouched, got HP %d", arenaRobot(a, "far").HP)
	}
}

func TestMineLayerGrantsChargesAndFireIsNoOp(t *testing.T) {
	a := New("mines", []RobotState{{RobotID: "a", Team: "red", X: 100, Y: 100, HP: 100, Alive: true}}, map[string]Controller{"a": &fixedController{}})
	r := &a.Robots[0]
	if value, _ := a.applyItemArena(r, "weapon_mine_layer"); value != 1 || r.Weapon != "mine_layer" || r.MineCharges != 3 {
		t.Fatalf("mine_layer pickup should grant 3 charges: value=%d weapon=%q charges=%d", value, r.Weapon, r.MineCharges)
	}
	if events := a.fire(r); events != nil || len(a.Projectiles) != 0 || r.Cooldown != 20 {
		t.Fatalf("mine_layer fire must be a no-op: events=%v projectiles=%d cooldown=%d", events, len(a.Projectiles), r.Cooldown)
	}
}

func TestNewItemsProduceByteStableEventLogs(t *testing.T) {
	config := DefaultConfig()
	config.MaxTicks = 90
	config.OvertimeTicks = 0
	config.Items.MaxConcurrent = 14
	config.Items.SpawnMinTicks, config.Items.SpawnMaxTicks = 1, 1
	config.Items.RespawnMinTicks, config.Items.RespawnMaxTicks = 2, 2
	robots := []RobotState{{RobotID: "a", Team: "red", X: 100, Y: 100, HP: 60, Alive: true}, {RobotID: "b", Team: "blue", X: 450, Y: 100, Heading: 180, HP: 100, Alive: true}}
	// Grenade goes early so its blasts land before the duel ends; the beacon
	// comes last so every earlier pickup lands before the robot relocates to
	// a seeded random spot. b sits off the pickup path's tail (164px from the
	// last item) so it never steals a pre-seeded kind.
	kinds := []string{"medkit", "weapon_grenade", "nano_repair", "armor_plate", "battery", "cloak", "scanner", "berserker_charm", "vampiric_fang", "dash_cell", "frenzy", "weapon_shotgun", "weapon_mine_layer", "teleport_beacon"}
	run := func() (*Arena, []byte) {
		a := NewWithConfig("expanded-loot", robots, map[string]Controller{"a": &fixedController{intent: Intent{Move: 8, Fire: true}}, "b": &fixedController{intent: Intent{Fire: true}}}, config)
		for i, kind := range kinds {
			a.addItem(kind, 130+float64(i)*12, 100, "drop")
		}
		for !a.Finished() {
			a.Step(context.Background())
		}
		encoded, err := json.Marshal(a.EventLog())
		if err != nil {
			t.Fatal(err)
		}
		return a, encoded
	}
	first, firstLog := run()
	if _, secondLog := run(); !reflect.DeepEqual(firstLog, secondLog) {
		t.Fatalf("expanded item event logs differ across same-seed runs:\n%s\n%s", firstLog, secondLog)
	}
	picked := map[string]bool{}
	eventTypes := map[string]bool{}
	for _, event := range first.EventLog() {
		eventTypes[event.Type] = true
		if event.Type == "item_picked_up" {
			picked[event.Message] = true
		}
	}
	for _, kind := range kinds {
		if !picked[kind] {
			t.Fatalf("kind %s was never picked up", kind)
		}
	}
	for _, kind := range []string{"teleport", "regen_tick", "explosion", "vampiric_heal"} {
		if !eventTypes[kind] {
			t.Fatalf("expected %s events in the expanded match log", kind)
		}
	}
}

// viewCaptureController records the robot slice the engine hands the
// controller, so tests can assert per-observer visibility filtering.
type viewCaptureController struct {
	fixedController
	seen [][]RobotState
}

func (c *viewCaptureController) Tick(ctx context.Context, self RobotState, robots []RobotState) (Intent, error) {
	c.seen = append(c.seen, robots)
	return c.fixedController.Tick(ctx, self, robots)
}

// recordingController records the self state of every tick, so tests can
// inspect per-tick deliveries like team messages.
type recordingController struct {
	fixedController
	selves []RobotState
}

func (c *recordingController) Tick(ctx context.Context, self RobotState, robots []RobotState) (Intent, error) {
	c.selves = append(c.selves, self)
	return c.fixedController.Tick(ctx, self, robots)
}

// onceMessageController sends its fixed intent's message on the first tick
// only, so later ticks can verify the relay clears.
type onceMessageController struct {
	recordingController
	sent bool
}

func (c *onceMessageController) Tick(ctx context.Context, self RobotState, robots []RobotState) (Intent, error) {
	intent, err := c.recordingController.Tick(ctx, self, robots)
	if c.sent {
		intent.Message = ""
	}
	c.sent = true
	return intent, err
}

func TestDashConsumesChargeRespectsCooldownAndSlides(t *testing.T) {
	config := DefaultConfig()
	config.Width, config.Height = 800, 500
	config.Map = DefaultMap(800, 500)
	config.Map.Obstacles = []Obstacle{{ID: "wall", Shape: "aabb", X: 130, Y: 0, Width: 40, Height: 260}}
	robots := []RobotState{
		{RobotID: "red-1", Name: "Red", Team: "red", X: 100, Y: 200, Heading: 45, HP: 100, Alive: true, DashCharges: 2},
		{RobotID: "blue-1", Name: "Blue", Team: "blue", X: 700, Y: 400, HP: 100, Alive: true},
	}
	controllers := map[string]Controller{"red-1": &fixedController{intent: Intent{Dash: true}}, "blue-1": &fixedController{}}
	arena := NewWithConfig("dash", robots, controllers, config)
	arena.Step(context.Background())
	red := arenaRobot(arena, "red-1")
	// A 45-degree dash into the wall slides along it like a normal move: the
	// blocked X component stays put while the free Y component carries.
	wantY := 200 + DashDistance*math.Sin(math.Pi/4)
	if red.X != 100 || math.Abs(red.Y-wantY) > 0.0001 {
		t.Fatalf("dash should slide along the wall, got x=%v y=%v want x=100 y=%v", red.X, red.Y, wantY)
	}
	if red.DashCharges != 1 {
		t.Fatalf("dash should consume one charge, got %d", red.DashCharges)
	}
	arena.Step(context.Background())
	if red.DashCharges != 1 || math.Abs(red.Y-wantY) > 0.0001 {
		t.Fatalf("dash must be gated by its cooldown: charges=%d y=%v", red.DashCharges, red.Y)
	}
	for arena.TickNumber < DashCooldownTicks {
		arena.Step(context.Background())
	}
	arena.Step(context.Background())
	if red.DashCharges != 0 {
		t.Fatalf("dash should fire again after cooldown, charges=%d", red.DashCharges)
	}
	dashes := 0
	for _, event := range arena.EventLog() {
		if event.Type == "dash" {
			dashes++
		}
	}
	if dashes != 2 {
		t.Fatalf("expected two dash events, got %d", dashes)
	}
}

func TestMineDeploysArmsExplodesAndExpires(t *testing.T) {
	config := DefaultConfig()
	config.CriticalChance = 0
	robots := []RobotState{
		{RobotID: "a", Name: "Red", Team: "red", X: 100, Y: 100, HP: 100, Alive: true, MineCharges: 2},
		{RobotID: "b", Name: "Blue", Team: "blue", X: 500, Y: 500, HP: 100, Alive: true},
	}
	controllers := map[string]Controller{"a": &fixedController{intent: Intent{Deploy: "mine"}}, "b": &fixedController{}}
	arena := NewWithConfig("mines", robots, controllers, config)
	snapshot := arena.Step(context.Background())
	if !hasEvent(snapshot.Events, "mine_deployed") || len(arena.Mines) != 1 {
		t.Fatalf("deploy should place one mine: events=%+v mines=%+v", snapshot.Events, arena.Mines)
	}
	mine := arena.Mines[0]
	if mine.OwnerID != "a" || mine.Team != "red" || !mine.Active || mine.ArmTick != mine.SpawnTick+MineArmTicks {
		t.Fatalf("bad mine state: %+v", mine)
	}
	if arenaRobot(arena, "a").MineCharges != 1 {
		t.Fatalf("deploy should consume one charge, got %d", arenaRobot(arena, "a").MineCharges)
	}
	// Standing on an unarmed mine is safe.
	arenaRobot(arena, "b").X, arenaRobot(arena, "b").Y = 118, 100
	arenaRobot(arena, "a").X, arenaRobot(arena, "a").Y = 300, 300
	arena.TickNumber = mine.SpawnTick + MineArmTicks - 1
	if events := arena.advanceMines(); len(events) != 0 || len(arena.Mines) != 1 {
		t.Fatalf("unarmed mine must not trigger: events=%+v mines=%+v", events, arena.Mines)
	}
	// Once armed, the enemy inside the trigger radius detonates it.
	arena.TickNumber = mine.SpawnTick + MineArmTicks
	events := arena.advanceMines()
	if !hasEvent(events, "mine_exploded") || len(arena.Mines) != 0 {
		t.Fatalf("armed mine should explode and be removed: events=%+v mines=%+v", events, arena.Mines)
	}
	if arenaRobot(arena, "b").HP != 50 || arenaRobot(arena, "a").HP != 100 {
		t.Fatalf("blast should hit the enemy for 50 and spare the owner: a=%d b=%d", arenaRobot(arena, "a").HP, arenaRobot(arena, "b").HP)
	}
	for _, event := range events {
		if event.Type == "mine_exploded" && (event.RobotID != "a" || event.TargetID != "b" || event.Value != MineDamage || event.X != 100 || event.Y != 100) {
			t.Fatalf("bad mine_exploded event: %+v", event)
		}
	}
	// A mine nobody steps on expires after its lifetime.
	arena.Mines = append(arena.Mines, MineState{MineID: "mine-a-2", OwnerID: "a", Team: "red", X: 400, Y: 250, SpawnTick: mine.SpawnTick, ArmTick: mine.SpawnTick + MineArmTicks, Active: true})
	arena.TickNumber = mine.SpawnTick + MineLifetimeTicks
	events = arena.advanceMines()
	if len(events) != 1 || events[0].Type != "mine_expired" || events[0].RobotID != "a" || len(arena.Mines) != 0 {
		t.Fatalf("stale mine should expire silently: events=%+v mines=%+v", events, arena.Mines)
	}
}

func TestMineBlastHonorsFriendlyFireRules(t *testing.T) {
	for _, friendlyFire := range []bool{false, true} {
		config := DefaultConfig()
		config.CriticalChance = 0
		config.FriendlyFire = friendlyFire
		robots := []RobotState{
			{RobotID: "enemy", Name: "Enemy", Team: "blue", X: 500, Y: 500, HP: 100, Alive: true},
			{RobotID: "mate", Name: "Mate", Team: "red", X: 600, Y: 500, HP: 100, Alive: true},
			{RobotID: "owner", Name: "Owner", Team: "red", X: 100, Y: 100, HP: 100, Alive: true, MineCharges: 1},
		}
		controllers := map[string]Controller{"owner": &fixedController{intent: Intent{Deploy: "mine"}}, "mate": &fixedController{}, "enemy": &fixedController{}}
		arena := NewWithConfig("mine-ff", robots, controllers, config)
		arena.Step(context.Background())
		mine := arena.Mines[0]
		arenaRobot(arena, "enemy").X, arenaRobot(arena, "enemy").Y = 118, 100
		arenaRobot(arena, "mate").X, arenaRobot(arena, "mate").Y = 160, 100
		arenaRobot(arena, "owner").X, arenaRobot(arena, "owner").Y = 300, 300
		arena.TickNumber = mine.ArmTick
		arena.advanceMines()
		if arenaRobot(arena, "enemy").HP != 50 {
			t.Fatalf("enemy should take 50 blast damage, got %d", arenaRobot(arena, "enemy").HP)
		}
		if arenaRobot(arena, "owner").HP != 100 {
			t.Fatalf("owner must never take mine damage, got %d", arenaRobot(arena, "owner").HP)
		}
		want := 100
		if friendlyFire {
			want = 50
		}
		if arenaRobot(arena, "mate").HP != want {
			t.Fatalf("teammate HP with friendlyFire=%v: got %d want %d", friendlyFire, arenaRobot(arena, "mate").HP, want)
		}
	}
}

func TestMineCapRefusesWhenFull(t *testing.T) {
	arena := New("mine-cap", []RobotState{{RobotID: "a", Team: "red", X: 100, Y: 100, HP: 100, Alive: true, MineCharges: 3}}, map[string]Controller{"a": &fixedController{intent: Intent{Deploy: "mine"}}})
	for i := range MaxMines {
		arena.Mines = append(arena.Mines, MineState{MineID: "mine-fill-" + itoa(i), OwnerID: "a", Team: "red", X: 700, Y: 450, SpawnTick: 0, ArmTick: MineArmTicks, Active: true})
	}
	snapshot := arena.Step(context.Background())
	if hasEvent(snapshot.Events, "mine_deployed") {
		t.Fatal("deploy must be refused when the arena mine cap is reached")
	}
	if arenaRobot(arena, "a").MineCharges != 3 || len(arena.Mines) != MaxMines {
		t.Fatalf("refused deploy must keep charges and mines untouched: charges=%d mines=%d", arenaRobot(arena, "a").MineCharges, len(arena.Mines))
	}
}

func TestScanReportContentsSortingAndCooldown(t *testing.T) {
	m := DefaultMap(800, 500)
	m.Hazards = []Hazard{
		{ID: "lava", Type: "damage", X: 150, Y: 150, Width: 100, Height: 50, Damage: 1},
		{ID: "acid", Type: "damage", X: 600, Y: 400, Width: 50, Height: 50, Damage: 1},
	}
	config := DefaultConfig()
	config.Map, config.Width, config.Height = m, 800, 500
	config.CriticalChance = 0
	robots := []RobotState{
		{RobotID: "alpha", Name: "Alpha", Team: "blue", X: 200, Y: 250, HP: 90, Alive: true},
		{RobotID: "far", Name: "Far", Team: "blue", X: 700, Y: 100, HP: 100, Alive: true},
		{RobotID: "mate", Name: "Mate", Team: "red", X: 210, Y: 210, HP: 100, Alive: true},
		{RobotID: "scan", Name: "Scan", Team: "red", X: 200, Y: 200, HP: 100, Alive: true},
		{RobotID: "zeta", Name: "Zeta", Team: "blue", X: 240, Y: 200, HP: 80, Alive: true},
	}
	arena := NewWithConfig("scan", robots, map[string]Controller{}, config)
	arena.Items = []Item{
		{ItemID: "item-a", Type: "shield", X: 200, Y: 260, Active: true},
		{ItemID: "item-b", Type: "heal", X: 220, Y: 200, Active: true},
		{ItemID: "item-c", Type: "heal", X: 250, Y: 200, Active: false},
	}
	arena.Mines = []MineState{
		{MineID: "mine-1", OwnerID: "alpha", Team: "blue", X: 190, Y: 190, SpawnTick: 0, ArmTick: 100, Active: true},
		{MineID: "mine-2", OwnerID: "zeta", Team: "blue", X: 230, Y: 230, SpawnTick: 0, ArmTick: 30, Active: true},
	}
	scanEvents := 0
	scanAt := func(tick int, req ScanRequest) *ScanReport {
		arena.TickNumber = tick
		events := arena.applyScans(map[string]Intent{"scan": {Scan: &req}})
		scanEvents += len(events)
		return arenaRobot(arena, "scan").ScanResult
	}
	report := scanAt(50, ScanRequest{X: 200, Y: 200, Radius: 100})
	if report.X != 200 || report.Y != 200 || report.Radius != 100 {
		t.Fatalf("scan report should echo the request: %+v", report)
	}
	if len(report.Items) != 2 || report.Items[0].ItemID != "item-a" || report.Items[1].ItemID != "item-b" {
		t.Fatalf("items must list active ones sorted by ID: %+v", report.Items)
	}
	if len(report.Enemies) != 2 || report.Enemies[0].RobotID != "alpha" || report.Enemies[1].RobotID != "zeta" {
		t.Fatalf("enemies must list living foes sorted by ID: %+v", report.Enemies)
	}
	if len(report.Mines) != 2 || report.Mines[0].MineID != "mine-1" || report.Mines[0].Armed || !report.Mines[1].Armed {
		t.Fatalf("mines must be sorted with correct armed flags: %+v", report.Mines)
	}
	if len(report.Hazards) != 1 || report.Hazards[0].ID != "lava" || report.Hazards[0].Width != 100 {
		t.Fatalf("hazards must use rect centers inside the radius: %+v", report.Hazards)
	}
	// On cooldown: ignored silently, the previous report stays.
	before := arenaRobot(arena, "scan").ScanResult
	if again := scanAt(50, ScanRequest{X: 200, Y: 200, Radius: 100}); again != before {
		t.Fatal("scan on cooldown must be ignored and keep the last report")
	}
	// Cloaked enemies stay hidden without radar.
	upsertEffect(arenaRobot(arena, "zeta"), "cloak", 90, 1, "")
	report = scanAt(110, ScanRequest{X: 200, Y: 200, Radius: 100})
	if len(report.Enemies) != 1 || report.Enemies[0].RobotID != "alpha" {
		t.Fatalf("cloaked enemy must be excluded without radar: %+v", report.Enemies)
	}
	// Radar reveals cloaked enemies with the flag set.
	upsertEffect(arenaRobot(arena, "scan"), "radar", 150, 1, "")
	report = scanAt(170, ScanRequest{X: 200, Y: 200, Radius: 100})
	if len(report.Enemies) != 2 || report.Enemies[1].RobotID != "zeta" || !report.Enemies[1].Cloaked || report.Enemies[0].Cloaked {
		t.Fatalf("radar must reveal the cloaked enemy: %+v", report.Enemies)
	}
	// Radius clamps to [40, 400].
	report = scanAt(230, ScanRequest{X: 200, Y: 200, Radius: 9999})
	if report.Radius != MaxScanRadius {
		t.Fatalf("oversized radius must clamp to 400, got %v", report.Radius)
	}
	report = scanAt(290, ScanRequest{X: 200, Y: 200, Radius: 5})
	if report.Radius != MinScanRadius || len(report.Enemies) != 1 || report.Enemies[0].RobotID != "zeta" || len(report.Items) != 1 || report.Items[0].ItemID != "item-b" {
		t.Fatalf("tiny radius must clamp to 40 and shrink the report: %+v", report)
	}
	if scanEvents != 5 {
		t.Fatalf("expected five scan_performed events, got %d", scanEvents)
	}
}

func TestCloakedEnemiesHiddenFromPerRobotViewsAndBots(t *testing.T) {
	robots := []RobotState{
		{RobotID: "a", Team: "red", X: 100, Y: 100, HP: 100, Alive: true},
		{RobotID: "b", Team: "blue", X: 200, Y: 100, HP: 100, Alive: true},
		{RobotID: "c", Team: "red", X: 100, Y: 200, HP: 100, Alive: true},
		{RobotID: "d", Team: "blue", X: 200, Y: 200, HP: 100, Alive: true},
	}
	controllers := map[string]Controller{"a": &viewCaptureController{}, "b": &fixedController{}, "c": &fixedController{}, "d": &fixedController{}}
	arena := New("cloak-views", robots, controllers)
	upsertEffect(arenaRobot(arena, "b"), "cloak", 90, 1, "")
	upsertEffect(arenaRobot(arena, "c"), "cloak", 90, 1, "")
	ids := func(view []RobotState) map[string]bool {
		out := map[string]bool{}
		for _, r := range view {
			out[r.RobotID] = true
		}
		return out
	}
	if view := ids(arena.visibleRobots(*arenaRobot(arena, "a"))); view["b"] || !view["c"] || !view["d"] {
		t.Fatalf("cloaked enemy hidden but teammates visible: %v", view)
	}
	if view := ids(arena.visibleRobots(*arenaRobot(arena, "d"))); view["c"] || !view["a"] {
		t.Fatalf("cloaked enemy hidden from enemy observers too: %v", view)
	}
	upsertEffect(arenaRobot(arena, "a"), "radar", 150, 1, "")
	if view := ids(arena.visibleRobots(*arenaRobot(arena, "a"))); !view["b"] {
		t.Fatalf("radar must reveal the cloaked enemy: %v", view)
	}
	// Controller Tick receives the filtered view through the engine loop; the
	// radar from the direct-view checks must be gone first.
	arenaRobot(arena, "a").Effects = nil
	capture := &viewCaptureController{}
	arena.controllers["a"] = capture
	arena.Step(context.Background())
	if view := ids(capture.seen[len(capture.seen)-1]); view["b"] || !view["c"] {
		t.Fatalf("controller view must filter cloaked enemies: %v", view)
	}
	upsertEffect(arenaRobot(arena, "a"), "radar", 150, 1, "")
	arena.Step(context.Background())
	if view := ids(capture.seen[len(capture.seen)-1]); !view["b"] {
		t.Fatalf("radar holder must see the cloaked enemy in its view: %v", view)
	}
	// Bots carry no radar and must ignore cloaked enemies.
	bot := NewBotController("bot-1", BotSharpshooter, PersonalityAggressive, DefaultMap(800, 500))
	self := RobotState{RobotID: "bot-1", Team: "red", X: 100, Y: 100, Heading: 0, HP: 100, Alive: true, Weapon: "plasma"}
	enemy := RobotState{RobotID: "enemy", Team: "blue", X: 300, Y: 100, Alive: true}
	upsertEffect(&enemy, "cloak", 90, 1, "")
	intent, err := bot.Tick(context.Background(), self, []RobotState{self, enemy})
	if err != nil || intent.Fire || intent.TargetX != nil {
		t.Fatalf("bot must not target a cloaked enemy: %+v %v", intent, err)
	}
}

func TestTeamMessagesRelayToTeammatesAndClear(t *testing.T) {
	robots := []RobotState{
		{RobotID: "a", Team: "red", X: 100, Y: 100, HP: 100, Alive: true},
		{RobotID: "b", Team: "blue", X: 400, Y: 400, HP: 100, Alive: true},
		{RobotID: "c", Team: "red", X: 160, Y: 100, HP: 100, Alive: true},
	}
	sender := &onceMessageController{recordingController: recordingController{fixedController: fixedController{intent: Intent{Message: "  push mid  "}}}}
	enemy := &recordingController{}
	receiver := &recordingController{}
	arena := New("relay", robots, map[string]Controller{"a": sender, "b": enemy, "c": receiver})
	arena.Step(context.Background())
	if len(receiver.selves[0].Messages) != 0 {
		t.Fatalf("messages must not arrive in the sending tick: %+v", receiver.selves[0].Messages)
	}
	arena.Step(context.Background())
	if len(receiver.selves[1].Messages) != 1 || receiver.selves[1].Messages[0] != "push mid" {
		t.Fatalf("teammate should receive the trimmed message: %+v", receiver.selves[1].Messages)
	}
	if len(sender.selves[1].Messages) != 0 || len(enemy.selves[1].Messages) != 0 {
		t.Fatalf("senders and enemies must not receive the message: %v %v", sender.selves[1].Messages, enemy.selves[1].Messages)
	}
	arena.Step(context.Background())
	if len(receiver.selves[2].Messages) != 0 {
		t.Fatalf("messages must clear once delivered: %+v", receiver.selves[2].Messages)
	}
}

func TestTeamMessageDeliveryCapsAtFivePerRobot(t *testing.T) {
	robots := []RobotState{{RobotID: "e", Team: "blue", HP: 100, Alive: true}}
	for _, id := range []string{"r", "s1", "s2", "s3", "s4", "s5", "s6"} {
		robots = append(robots, RobotState{RobotID: id, Team: "red", HP: 100, Alive: true})
	}
	controllers := map[string]Controller{}
	for _, r := range robots {
		controllers[r.RobotID] = &fixedController{}
	}
	arena := New("relay-cap", robots, controllers)
	arena.TeamMessages = map[string][]string{"s1": {"m1"}, "s2": {"m2"}, "s3": {"m3"}, "s4": {"m4"}, "s5": {"m5"}, "s6": {"m6"}}
	arena.deliverTeamMessages()
	receiver := arenaRobot(arena, "r")
	if len(receiver.Messages) != MaxTeamMessages {
		t.Fatalf("delivery must cap at %d messages, got %d", MaxTeamMessages, len(receiver.Messages))
	}
	if receiver.Messages[0] != "m1" || receiver.Messages[4] != "m5" {
		t.Fatalf("delivery must follow roster order of senders: %v", receiver.Messages)
	}
	if len(arenaRobot(arena, "e").Messages) != 0 {
		t.Fatal("other teams must not receive red's messages")
	}
}

func TestProtocolV3FeaturesProduceByteStableEventLogs(t *testing.T) {
	config := DefaultConfig()
	config.MaxTicks = 40
	config.OvertimeTicks = 0
	config.CriticalChance = 0
	robots := []RobotState{
		{RobotID: "a", Team: "red", X: 100, Y: 100, Heading: 0, HP: 100, Alive: true, DashCharges: 2, MineCharges: 3},
		{RobotID: "b", Team: "red", X: 300, Y: 100, HP: 100, Alive: true, MineCharges: 1},
		{RobotID: "c", Team: "blue", X: 700, Y: 250, Heading: 180, HP: 100, Alive: true},
	}
	controllers := map[string]Controller{
		"a": &fixedController{intent: Intent{Move: 8, Dash: true, Deploy: "mine", Message: "push mid", Scan: &ScanRequest{X: 150, Y: 100, Radius: 120}}},
		"b": &fixedController{intent: Intent{Message: "  covering  "}},
		"c": &fixedController{intent: Intent{Move: 8, Fire: true}},
	}
	run := func() []byte {
		arena := NewWithConfig("v3-stable", robots, controllers, config)
		// A pre-placed armed mine sits on c's westward path: walking over it
		// mid-match exercises the proximity trigger and blast damage.
		arena.Mines = append(arena.Mines, MineState{MineID: "mine-pre", OwnerID: "a", Team: "red", X: 400, Y: 250, SpawnTick: 0, ArmTick: 20, Active: true})
		for !arena.Finished() {
			arena.Step(context.Background())
		}
		encoded, err := json.Marshal(arena.EventLog())
		if err != nil {
			t.Fatal(err)
		}
		return encoded
	}
	first := run()
	if second := run(); !reflect.DeepEqual(first, second) {
		t.Fatalf("protocol v3 event logs differ across same-seed runs:\n%s\n%s", first, second)
	}
	var events []Event
	if err := json.Unmarshal(first, &events); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, event := range events {
		seen[event.Type] = true
	}
	for _, kind := range []string{"dash", "mine_deployed", "mine_exploded", "scan_performed"} {
		if !seen[kind] {
			t.Fatalf("expected %s events in the v3 match log", kind)
		}
	}
}
