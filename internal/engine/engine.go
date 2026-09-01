package engine

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
)

const (
	ArenaWidth       = 800.0
	ArenaHeight      = 500.0
	RobotRadius      = 14.0
	MaxMovePerTick   = 8.0
	MaxTurnPerTick   = 18.0
	FireDamage       = 25
	FireCooldown     = 8
	ProjectileSpeed  = 24.0
	ProjectileTTL    = 36
	ProjectileRadius = 4.0
	DefaultMaxTicks  = 1800
)

type RobotState struct {
	RobotID        string   `json:"robotId"`
	Name           string   `json:"name"`
	Team           string   `json:"team"`
	X              float64  `json:"x"`
	Y              float64  `json:"y"`
	Heading        float64  `json:"heading"`
	HP             int      `json:"hp"`
	Cooldown       int      `json:"cooldown"`
	Alive          bool     `json:"alive"`
	Failed         bool     `json:"failed"`
	Connected      bool     `json:"connected"`
	AvgResponseMS  float64  `json:"avgResponseMs"`
	LastResponseMS float64  `json:"lastResponseMs"`
	ComputeMS      float64  `json:"computeMs"`
	MemoryMB       float64  `json:"memoryMb"`
	Equipment      []string `json:"equipment,omitempty"`
	LastAction     string   `json:"lastAction,omitempty"`
	Logs           []string `json:"logs,omitempty"`
}

type Intent struct {
	Move       float64
	Turn       float64
	Fire       bool
	TargetX    *float64
	TargetY    *float64
	Logs       []string
	ResponseMS float64
	ComputeMS  float64
	MemoryMB   float64
	Equipment  []string
}

type Controller interface {
	Tick(ctx context.Context, self RobotState, robots []RobotState) (Intent, error)
	Close()
}

type Event struct {
	Type     string `json:"type"`
	RobotID  string `json:"robotId,omitempty"`
	TargetID string `json:"targetId,omitempty"`
	Damage   int    `json:"damage,omitempty"`
	Message  string `json:"message,omitempty"`
}

type Projectile struct {
	ProjectileID string  `json:"projectileId"`
	OwnerID      string  `json:"ownerId"`
	Team         string  `json:"team"`
	Kind         string  `json:"kind"`
	X            float64 `json:"x"`
	Y            float64 `json:"y"`
	VX           float64 `json:"vx"`
	VY           float64 `json:"vy"`
	Damage       int     `json:"damage"`
	TTL          int     `json:"ttl"`
}

type Snapshot struct {
	Type        string       `json:"type"`
	Version     int          `json:"version"`
	MatchID     string       `json:"matchId"`
	Sequence    int          `json:"sequence"`
	Tick        int          `json:"tick"`
	Status      string       `json:"status"`
	WinnerTeam  string       `json:"winnerTeam,omitempty"`
	Robots      []RobotState `json:"robots"`
	Projectiles []Projectile `json:"projectiles"`
	Events      []Event      `json:"events,omitempty"`
}

type Arena struct {
	MatchID     string
	TickNumber  int
	MaxTicks    int
	Robots      []RobotState
	Projectiles []Projectile
	controllers map[string]Controller
	winner      string
	finished    bool
}

func New(matchID string, robots []RobotState, controllers map[string]Controller) *Arena {
	copyOfRobots := append([]RobotState(nil), robots...)
	sort.Slice(copyOfRobots, func(i, j int) bool { return copyOfRobots[i].RobotID < copyOfRobots[j].RobotID })
	return &Arena{MatchID: matchID, Robots: copyOfRobots, controllers: controllers, MaxTicks: DefaultMaxTicks}
}

func (a *Arena) Close() {
	for _, controller := range a.controllers {
		controller.Close()
	}
}

func (a *Arena) Step(ctx context.Context) Snapshot {
	if a.finished {
		return a.snapshot(nil)
	}

	before := cloneRobots(a.Robots)
	intents := make(map[string]Intent, len(a.Robots))
	events := make([]Event, 0)

	type decision struct {
		index  int
		intent Intent
		err    error
	}
	decisions := make(chan decision, len(a.Robots))
	var wait sync.WaitGroup
	for i := range before {
		if !before[i].Alive {
			continue
		}
		controller := a.controllers[before[i].RobotID]
		if controller == nil {
			decisions <- decision{index: i, err: errors.New("controller missing")}
			continue
		}
		wait.Add(1)
		go func(index int, controller Controller) {
			defer wait.Done()
			intent, err := controller.Tick(ctx, before[index], before)
			decisions <- decision{index: index, intent: intent, err: err}
		}(i, controller)
	}
	wait.Wait()
	close(decisions)
	for decision := range decisions {
		robot := &a.Robots[decision.index]
		if decision.err != nil {
			robot.Alive, robot.Failed, robot.HP = false, true, 0
			robot.LastAction = "controller failed"
			events = append(events, Event{Type: "robot_failed", RobotID: robot.RobotID, Message: decision.err.Error()})
			continue
		}
		intents[robot.RobotID] = decision.intent
	}

	for i := range a.Robots {
		robot := &a.Robots[i]
		if !robot.Alive {
			continue
		}
		intent := intents[robot.RobotID]
		turn := clamp(intent.Turn, -MaxTurnPerTick, MaxTurnPerTick)
		if intent.TargetX != nil && intent.TargetY != nil {
			desired := normalizeDegrees(math.Atan2(*intent.TargetY-robot.Y, *intent.TargetX-robot.X) * 180 / math.Pi)
			turn = clamp(shortestTurn(robot.Heading, desired), -MaxTurnPerTick, MaxTurnPerTick)
		}
		robot.Heading = normalizeDegrees(robot.Heading + turn)
		move := clamp(intent.Move, -MaxMovePerTick/2, MaxMovePerTick)
		radians := robot.Heading * math.Pi / 180
		robot.X = clamp(robot.X+math.Cos(radians)*move, RobotRadius, ArenaWidth-RobotRadius)
		robot.Y = clamp(robot.Y+math.Sin(radians)*move, RobotRadius, ArenaHeight-RobotRadius)
		robot.Logs = appendBounded(robot.Logs, intent.Logs, 100)
		robot.Connected = true
		robot.LastResponseMS = intent.ResponseMS
		if intent.ResponseMS > 0 {
			if robot.AvgResponseMS == 0 {
				robot.AvgResponseMS = intent.ResponseMS
			} else {
				robot.AvgResponseMS = robot.AvgResponseMS*0.85 + intent.ResponseMS*0.15
			}
		}
		robot.ComputeMS, robot.MemoryMB = intent.ComputeMS, intent.MemoryMB
		if len(intent.Equipment) > 0 {
			robot.Equipment = append([]string(nil), intent.Equipment...)
		}
		robot.LastAction = describeIntent(intent)
		if robot.Cooldown > 0 {
			robot.Cooldown--
		}
	}

	resolveOverlaps(a.Robots)
	events = append(events, a.advanceProjectiles()...)

	for i := range a.Robots {
		shooter := &a.Robots[i]
		intent := intents[shooter.RobotID]
		if !shooter.Alive || !intent.Fire || shooter.Cooldown > 0 {
			continue
		}
		shooter.Cooldown = FireCooldown
		radians := shooter.Heading * math.Pi / 180
		a.Projectiles = append(a.Projectiles, Projectile{
			ProjectileID: fmt.Sprintf("%s-%d", shooter.RobotID, a.TickNumber),
			OwnerID:      shooter.RobotID, Team: shooter.Team, Kind: "plasma",
			X:  shooter.X + math.Cos(radians)*(RobotRadius+ProjectileRadius+1),
			Y:  shooter.Y + math.Sin(radians)*(RobotRadius+ProjectileRadius+1),
			VX: math.Cos(radians) * ProjectileSpeed, VY: math.Sin(radians) * ProjectileSpeed,
			Damage: FireDamage, TTL: ProjectileTTL,
		})
		events = append(events, Event{Type: "shot_fired", RobotID: shooter.RobotID})
	}

	a.TickNumber++
	a.checkFinished()
	return a.snapshot(events)
}

func (a *Arena) Finished() bool { return a.finished }

func (a *Arena) Winner() string { return a.winner }

func (a *Arena) snapshot(events []Event) Snapshot {
	status := "running"
	if a.finished {
		status = "finished"
	}
	return Snapshot{
		Type: "snapshot", Version: 1, MatchID: a.MatchID, Sequence: a.TickNumber,
		Tick: a.TickNumber, Status: status, WinnerTeam: a.winner,
		Robots: cloneRobots(a.Robots), Projectiles: append([]Projectile(nil), a.Projectiles...), Events: events,
	}
}

func (a *Arena) advanceProjectiles() []Event {
	events := make([]Event, 0)
	active := a.Projectiles[:0]
	for _, projectile := range a.Projectiles {
		projectile.X += projectile.VX
		projectile.Y += projectile.VY
		projectile.TTL--
		if projectile.TTL <= 0 || projectile.X < 0 || projectile.X > ArenaWidth || projectile.Y < 0 || projectile.Y > ArenaHeight {
			continue
		}
		hit := false
		for index := range a.Robots {
			target := &a.Robots[index]
			if !target.Alive || target.Team == projectile.Team || target.RobotID == projectile.OwnerID {
				continue
			}
			if math.Hypot(target.X-projectile.X, target.Y-projectile.Y) > RobotRadius+ProjectileRadius {
				continue
			}
			target.HP -= projectile.Damage
			events = append(events, Event{Type: "hit", RobotID: projectile.OwnerID, TargetID: target.RobotID, Damage: projectile.Damage})
			if target.HP <= 0 {
				target.HP, target.Alive = 0, false
				events = append(events, Event{Type: "robot_destroyed", RobotID: target.RobotID, TargetID: projectile.OwnerID})
			}
			hit = true
			break
		}
		if !hit {
			active = append(active, projectile)
		}
	}
	a.Projectiles = active
	return events
}

func (a *Arena) checkFinished() {
	teams := map[string]bool{}
	for _, robot := range a.Robots {
		if robot.Alive {
			teams[robot.Team] = true
		}
	}
	if len(teams) <= 1 {
		a.finished = true
		for team := range teams {
			a.winner = team
		}
		if len(teams) == 0 {
			a.winner = "draw"
		}
		return
	}
	if a.TickNumber >= a.MaxTicks {
		a.finished = true
		a.winner = winnerByHP(a.Robots)
	}
}

func SpawnPositions(robots []RobotState) []RobotState {
	redIndex, blueIndex := 0, 0
	for i := range robots {
		robots[i].HP, robots[i].Alive = 100, true
		if robots[i].Team == "red" {
			robots[i].X = 100
			robots[i].Y = 120 + float64(redIndex*90)
			robots[i].Heading = 0
			redIndex++
		} else {
			robots[i].X = ArenaWidth - 100
			robots[i].Y = 120 + float64(blueIndex*90)
			robots[i].Heading = 180
			blueIndex++
		}
	}
	return robots
}

func ValidateTeams(robots []RobotState) error {
	teams := map[string]int{}
	for _, robot := range robots {
		if robot.Team != "red" && robot.Team != "blue" {
			return errors.New("team must be red or blue")
		}
		teams[robot.Team]++
	}
	if teams["red"] == 0 || teams["blue"] == 0 {
		return errors.New("match requires at least one robot on each team")
	}
	return nil
}

func resolveOverlaps(robots []RobotState) {
	for i := 0; i < len(robots); i++ {
		if !robots[i].Alive {
			continue
		}
		for j := i + 1; j < len(robots); j++ {
			if !robots[j].Alive {
				continue
			}
			dx, dy := robots[j].X-robots[i].X, robots[j].Y-robots[i].Y
			distance := math.Hypot(dx, dy)
			minimum := RobotRadius * 2
			if distance >= minimum {
				continue
			}
			if distance == 0 {
				dx, dy, distance = 1, 0, 1
			}
			push := (minimum - distance) / 2
			nx, ny := dx/distance, dy/distance
			robots[i].X = clamp(robots[i].X-nx*push, RobotRadius, ArenaWidth-RobotRadius)
			robots[i].Y = clamp(robots[i].Y-ny*push, RobotRadius, ArenaHeight-RobotRadius)
			robots[j].X = clamp(robots[j].X+nx*push, RobotRadius, ArenaWidth-RobotRadius)
			robots[j].Y = clamp(robots[j].Y+ny*push, RobotRadius, ArenaHeight-RobotRadius)
		}
	}
}

func winnerByHP(robots []RobotState) string {
	hp := map[string]int{}
	for _, robot := range robots {
		hp[robot.Team] += robot.HP
	}
	if hp["red"] > hp["blue"] {
		return "red"
	}
	if hp["blue"] > hp["red"] {
		return "blue"
	}
	return "draw"
}

func cloneRobots(robots []RobotState) []RobotState {
	result := append([]RobotState(nil), robots...)
	for i := range result {
		result[i].Logs = append([]string(nil), result[i].Logs...)
	}
	return result
}

func describeIntent(intent Intent) string {
	switch {
	case intent.Fire:
		return "fire"
	case intent.Move != 0:
		return "move"
	case intent.Turn != 0 || intent.TargetX != nil:
		return "turn"
	default:
		return "idle"
	}
}

func appendBounded(existing, additions []string, limit int) []string {
	existing = append(existing, additions...)
	if len(existing) > limit {
		existing = existing[len(existing)-limit:]
	}
	return existing
}

func clamp(value, low, high float64) float64 {
	return math.Max(low, math.Min(high, value))
}

func normalizeDegrees(value float64) float64 {
	for value < 0 {
		value += 360
	}
	for value >= 360 {
		value -= 360
	}
	return value
}

func shortestTurn(from, to float64) float64 {
	difference := normalizeDegrees(to) - normalizeDegrees(from)
	if difference > 180 {
		difference -= 360
	}
	if difference < -180 {
		difference += 360
	}
	return difference
}
