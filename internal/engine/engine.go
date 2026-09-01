package engine

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"math/rand/v2"
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
	MaxHP            = 100
)

type RobotState struct {
	RobotID        string         `json:"robotId"`
	Name           string         `json:"name"`
	Team           string         `json:"team"`
	X              float64        `json:"x"`
	Y              float64        `json:"y"`
	Heading        float64        `json:"heading"`
	HP             int            `json:"hp"`
	MaxHP          int            `json:"maxHp"`
	Cooldown       int            `json:"cooldown"`
	Alive          bool           `json:"alive"`
	Failed         bool           `json:"failed"`
	Connected      bool           `json:"connected"`
	AvgResponseMS  float64        `json:"avgResponseMs"`
	LastResponseMS float64        `json:"lastResponseMs"`
	ComputeMS      float64        `json:"computeMs"`
	MemoryMB       float64        `json:"memoryMb"`
	Equipment      []string       `json:"equipment,omitempty"`
	Weapon         string         `json:"weapon,omitempty"`
	Effects        []StatusEffect `json:"effects,omitempty"`
	Shield         int            `json:"shield,omitempty"`
	Kills          int            `json:"kills"`
	Deaths         int            `json:"deaths"`
	DamageDealt    int            `json:"damageDealt"`
	DamageTaken    int            `json:"damageTaken"`
	ItemsPickedUp  int            `json:"itemsPickedUp"`
	KillStreak     int            `json:"killStreak"`
	StreakName     string         `json:"streakName,omitempty"`
	LastDamageTick int            `json:"lastDamageTick,omitempty"`
	LastAction     string         `json:"lastAction,omitempty"`
	Logs           []string       `json:"logs,omitempty"`
}
type Intent struct {
	Move        float64
	Turn        float64
	Fire        bool
	TargetX     *float64
	TargetY     *float64
	Logs        []string
	ResponseMS  float64
	ComputeMS   float64
	MemoryMB    float64
	Equipment   []string
	AutoPickup  *bool
	PickupTypes []string
}
type Controller interface {
	Tick(context.Context, RobotState, []RobotState) (Intent, error)
	Close()
}
type WorldState struct {
	Tick      int
	MapID     string
	Width     float64
	Height    float64
	Obstacles []Obstacle
	Items     []Item
	Hazards   []Hazard
	Zone      *ZoneState
	Overtime  bool
}
type WorldAwareController interface{ SetWorld(WorldState) }
type Event struct {
	Tick     int     `json:"tick"`
	Type     string  `json:"type"`
	RobotID  string  `json:"robotId,omitempty"`
	TargetID string  `json:"targetId,omitempty"`
	ItemID   string  `json:"itemId,omitempty"`
	Damage   int     `json:"damage,omitempty"`
	Value    int     `json:"value,omitempty"`
	X        float64 `json:"x,omitempty"`
	Y        float64 `json:"y,omitempty"`
	Message  string  `json:"message,omitempty"`
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
	Knockback    float64 `json:"knockback,omitempty"`
	BurnTicks    int     `json:"burnTicks,omitempty"`
	SlowTicks    int     `json:"slowTicks,omitempty"`
	EMPTicks     int     `json:"empTicks,omitempty"`
}
type StatusEffect struct {
	Type      string  `json:"type"`
	Ticks     int     `json:"ticks"`
	Magnitude float64 `json:"magnitude,omitempty"`
	SourceID  string  `json:"sourceId,omitempty"`
}
type Snapshot struct {
	Type          string       `json:"type"`
	Version       int          `json:"version"`
	MatchID       string       `json:"matchId"`
	Sequence      int          `json:"sequence"`
	Tick          int          `json:"tick"`
	Status        string       `json:"status"`
	WinnerTeam    string       `json:"winnerTeam,omitempty"`
	MapID         string       `json:"mapId"`
	Width         float64      `json:"width"`
	Height        float64      `json:"height"`
	Robots        []RobotState `json:"robots"`
	Projectiles   []Projectile `json:"projectiles"`
	Items         []Item       `json:"items,omitempty"`
	Obstacles     []Obstacle   `json:"obstacles,omitempty"`
	Events        []Event      `json:"events,omitempty"`
	Zone          *ZoneState   `json:"zone,omitempty"`
	Announcements []string     `json:"announcements,omitempty"`
	Overtime      bool         `json:"overtime"`
}

type Arena struct {
	MatchID       string
	TickNumber    int
	MaxTicks      int
	Robots        []RobotState
	Projectiles   []Projectile
	Items         []Item
	Config        Config
	// singleTeam is set when the roster fielded exactly one team (solo lobby
	// without bots). Such matches must not end at tick 0 through the
	// last-team-standing rule; they end only when everyone dies or ticks out.
	singleTeam  string
	controllers map[string]Controller
	withdrawals   chan string
	winner        string
	finished      bool
	rng           *rand.Rand
	nextItem      int
	lastSpawnTick int
	overtimeEnd   int
	events        []Event
	pairRam       map[string]int
	pickupPrefs   map[string]Intent
}

func New(id string, robots []RobotState, controllers map[string]Controller) *Arena {
	return NewWithConfig(id, robots, controllers, DefaultConfig())
}
func NewWithConfig(id string, robots []RobotState, controllers map[string]Controller, c Config) *Arena {
	c.normalize()
	rs := cloneRobots(robots)
	sort.Slice(rs, func(i, j int) bool { return rs[i].RobotID < rs[j].RobotID })
	for i := range rs {
		if rs[i].MaxHP <= 0 {
			rs[i].MaxHP = MaxHP
		}
		if rs[i].HP <= 0 && !rs[i].Alive {
			rs[i].HP, rs[i].Alive = rs[i].MaxHP, true
		}
		if rs[i].Weapon == "" {
			rs[i].Weapon = "plasma"
		}
	}
	seed := c.Seed
	if seed == 0 {
		h := fnv.New64a()
		_, _ = h.Write([]byte(id))
		seed = h.Sum64()
	}
	singleTeam := ""
	distinct := map[string]bool{}
	for _, r := range rs {
		distinct[r.Team] = true
	}
	if len(distinct) == 1 {
		singleTeam = rs[0].Team
	}
	return &Arena{MatchID: id, Robots: rs, controllers: controllers, withdrawals: make(chan string, 8), MaxTicks: c.MaxTicks, Config: c, singleTeam: singleTeam, rng: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)), pairRam: map[string]int{}, pickupPrefs: map[string]Intent{}}
}
func (a *Arena) Close() {
	for _, c := range a.controllers {
		c.Close()
	}
}
func (a *Arena) Finished() bool    { return a.finished }
func (a *Arena) Winner() string    { return a.winner }
func (a *Arena) EventLog() []Event { return append([]Event(nil), a.events...) }

// RequestWithdraw queues a mid-match concession for the robot. It is safe to
// call from HTTP handlers while the worker steps the arena; the elimination
// applies at the start of the next tick so tick-loop state stays single-owner.
// It returns false when the pending queue is full, which means the match is
// finishing or many withdrawals raced; the caller reports that as a conflict.
func (a *Arena) RequestWithdraw(robotID string) bool {
	select {
	case a.withdrawals <- robotID:
		return true
	default:
		return false
	}
}

// applyWithdrawals destroys robots that conceded since the last tick. No kill
// or item-drop credit: the opponent wins by elimination rules, not by a shot.
func (a *Arena) applyWithdrawals(events *[]Event) {
	for {
		select {
		case robotID := <-a.withdrawals:
			r := a.robot(robotID)
			if r == nil || !r.Alive {
				continue
			}
			r.HP, r.Alive, r.KillStreak, r.StreakName = 0, false, 0, ""
			r.Deaths++
			*events = append(*events, a.event(Event{Type: "robot_withdrawn", RobotID: r.RobotID, X: r.X, Y: r.Y, Message: "withdrew from the match"}))
		default:
			return
		}
	}
}
func (a *Arena) LineOfSight(x1, y1, x2, y2 float64) bool {
	return LineOfSight(a.Config.Map.Obstacles, x1, y1, x2, y2)
}

func (a *Arena) Step(ctx context.Context) Snapshot {
	if a.finished {
		return a.snapshot(nil)
	}
	events := []Event{}
	a.applyWithdrawals(&events)
	before := cloneRobots(a.Robots)
	intents := map[string]Intent{}
	type decision struct {
		index  int
		intent Intent
		err    error
	}
	ch := make(chan decision, len(before))
	var wg sync.WaitGroup
	for i := range before {
		if !before[i].Alive {
			continue
		}
		c := a.controllers[before[i].RobotID]
		if c == nil {
			ch <- decision{i, Intent{}, errors.New("controller missing")}
			continue
		}
		if aware, ok := c.(WorldAwareController); ok {
			aware.SetWorld(WorldState{Tick: a.TickNumber, MapID: a.Config.Map.ID, Width: a.Config.Width, Height: a.Config.Height, Obstacles: append([]Obstacle(nil), a.Config.Map.Obstacles...), Items: append([]Item(nil), a.Items...), Hazards: append([]Hazard(nil), a.Config.Map.Hazards...), Zone: a.zoneState(), Overtime: a.overtime()})
		}
		wg.Add(1)
		go func(n int, c Controller) {
			defer wg.Done()
			v, e := c.Tick(ctx, before[n], before)
			ch <- decision{n, v, e}
		}(i, c)
	}
	wg.Wait()
	close(ch)
	ordered := make([]decision, 0, len(before))
	for d := range ch {
		ordered = append(ordered, d)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].index < ordered[j].index })
	for _, d := range ordered {
		r := &a.Robots[d.index]
		if d.err != nil {
			r.Alive, r.Failed, r.HP = false, true, 0
			r.Deaths++
			events = append(events, a.event(Event{Type: "robot_failed", RobotID: r.RobotID, Message: d.err.Error()}))
			continue
		}
		intents[r.RobotID] = d.intent
		a.pickupPrefs[r.RobotID] = d.intent
	}
	for i := range a.Robots {
		r := &a.Robots[i]
		if !r.Alive {
			continue
		}
		in := intents[r.RobotID]
		a.applyEffects(r, &events)
		if !r.Alive {
			continue
		}
		turn := clamp(in.Turn, -MaxTurnPerTick, MaxTurnPerTick)
		if in.TargetX != nil && in.TargetY != nil {
			wanted := normalizeDegrees(math.Atan2(*in.TargetY-r.Y, *in.TargetX-r.X) * 180 / math.Pi)
			turn = clamp(shortestTurn(r.Heading, wanted), -MaxTurnPerTick, MaxTurnPerTick)
		}
		r.Heading = normalizeDegrees(r.Heading + turn)
		limit := MaxMovePerTick * effectMultiplier(*r, "overdrive", 1)
		if effectActive(*r, "slow") {
			limit *= effectMultiplier(*r, "slow", .5)
		}
		move := clamp(in.Move, -limit/2, limit)
		rad := r.Heading * math.Pi / 180
		nx := clamp(r.X+math.Cos(rad)*move, RobotRadius, a.Config.Width-RobotRadius)
		ny := clamp(r.Y+math.Sin(rad)*move, RobotRadius, a.Config.Height-RobotRadius)
		if !collidesRobot(a.Config.Map.Obstacles, nx, ny) {
			r.X, r.Y = nx, ny
		} else if !collidesRobot(a.Config.Map.Obstacles, nx, r.Y) {
			// Slide along the blocking obstacle instead of stopping dead so
			// diagonal approaches never leave a robot grinding a wall.
			r.X = nx
		} else if !collidesRobot(a.Config.Map.Obstacles, r.X, ny) {
			r.Y = ny
		}
		updateTelemetry(r, in)
		if r.Cooldown > 0 {
			r.Cooldown--
		}
		if a.Config.RegenPerTick > 0 && a.TickNumber-r.LastDamageTick >= a.Config.RegenDelayTicks && r.HP < r.MaxHP {
			r.HP = min(r.MaxHP, r.HP+a.Config.RegenPerTick)
		}
	}
	resolveOverlaps(a.Robots, a.Config.Width, a.Config.Height, a.Config.Map.Obstacles)
	if a.Config.RammingDamage {
		events = append(events, a.resolveRamming(before)...)
	}
	events = append(events, a.advanceProjectiles()...)
	for i := range a.Robots {
		r := &a.Robots[i]
		in := intents[r.RobotID]
		if r.Alive && in.Fire && r.Cooldown == 0 && !effectActive(*r, "emp") {
			events = append(events, a.fire(r)...)
		}
	}
	events = append(events, a.spawnItems()...)
	events = append(events, a.pickupItems()...)
	events = append(events, a.applyZone()...)
	events = append(events, a.applyHazards()...)
	a.TickNumber++
	a.checkFinished()
	a.events = append(a.events, events...)
	return a.snapshot(events)
}

func (a *Arena) fire(r *RobotState) []Event {
	w := WeaponByName(r.Weapon)
	cd := w.Cooldown
	if effectActive(*r, "rapid_fire") {
		cd = max(1, cd/2)
	}
	r.Cooldown = cd
	damage := w.Damage
	if a.overtime() {
		damage *= 2
	}
	angle := r.Heading
	if w.Spread > 0 {
		angle += (a.rng.Float64()*2 - 1) * w.Spread
	}
	rad := angle * math.Pi / 180
	e := []Event{a.event(Event{Type: "shot_fired", RobotID: r.RobotID, Message: w.Name})}
	if w.Hitscan {
		target, d := a.firstTargetOnRay(*r, rad, w.Range)
		if target != nil {
			e = append(e, a.damage(r, target, damage, w)...)
			e = append(e, a.event(Event{Type: "railgun_hit", RobotID: r.RobotID, TargetID: target.RobotID, X: r.X + math.Cos(rad)*d, Y: r.Y + math.Sin(rad)*d}))
		}
		return e
	}
	a.Projectiles = append(a.Projectiles, Projectile{ProjectileID: fmt.Sprintf("%s-%d-%d", r.RobotID, a.TickNumber, len(a.Projectiles)), OwnerID: r.RobotID, Team: r.Team, Kind: w.Name, X: r.X + math.Cos(rad)*(RobotRadius+ProjectileRadius+1), Y: r.Y + math.Sin(rad)*(RobotRadius+ProjectileRadius+1), VX: math.Cos(rad) * w.ProjectileSpeed, VY: math.Sin(rad) * w.ProjectileSpeed, Damage: damage, TTL: max(1, int(w.Range/w.ProjectileSpeed)), Knockback: w.Knockback, BurnTicks: w.BurnTicks, SlowTicks: w.SlowTicks, EMPTicks: w.EMPTicks})
	return e
}
func (a *Arena) advanceProjectiles() []Event {
	e := []Event{}
	active := a.Projectiles[:0]
	for _, p := range a.Projectiles {
		ox, oy := p.X, p.Y
		p.X += p.VX
		p.Y += p.VY
		p.TTL--
		blocked := segmentBlocked(a.Config.Map.Obstacles, ox, oy, p.X, p.Y)
		if p.TTL <= 0 || p.X < 0 || p.X > a.Config.Width || p.Y < 0 || p.Y > a.Config.Height || blocked {
			if blocked {
				e = append(e, a.event(Event{Type: "projectile_blocked", RobotID: p.OwnerID, X: p.X, Y: p.Y}))
			}
			continue
		}
		hit := false
		for i := range a.Robots {
			t := &a.Robots[i]
			if !t.Alive || t.RobotID == p.OwnerID || (!a.Config.FriendlyFire && t.Team == p.Team) {
				continue
			}
			if distancePointSegment(t.X, t.Y, ox, oy, p.X, p.Y) > RobotRadius+ProjectileRadius {
				continue
			}
			w := WeaponByName(p.Kind)
			w.Knockback, w.BurnTicks, w.SlowTicks, w.EMPTicks = p.Knockback, p.BurnTicks, p.SlowTicks, p.EMPTicks
			e = append(e, a.damage(a.robot(p.OwnerID), t, p.Damage, w)...)
			hit = true
			break
		}
		if !hit {
			active = append(active, p)
		}
	}
	a.Projectiles = active
	return e
}
func (a *Arena) damage(source, target *RobotState, amount int, w Weapon) []Event {
	if target == nil || !target.Alive {
		return nil
	}
	absorbed := min(target.Shield, amount)
	target.Shield -= absorbed
	actual := amount - absorbed
	target.HP -= actual
	target.DamageTaken += actual
	target.LastDamageTick = a.TickNumber
	if source != nil {
		source.DamageDealt += actual
	}
	e := []Event{a.event(Event{Type: "hit", RobotID: robotID(source), TargetID: target.RobotID, Damage: actual, Value: absorbed})}
	if w.Knockback > 0 && source != nil {
		dx, dy := target.X-source.X, target.Y-source.Y
		d := math.Hypot(dx, dy)
		if d > 0 {
			nx := clamp(target.X+dx/d*w.Knockback, RobotRadius, a.Config.Width-RobotRadius)
			ny := clamp(target.Y+dy/d*w.Knockback, RobotRadius, a.Config.Height-RobotRadius)
			if !collidesRobot(a.Config.Map.Obstacles, nx, ny) {
				target.X, target.Y = nx, ny
			}
		}
	}
	addWeaponEffects(target, w, robotID(source))
	if target.HP <= 0 {
		e = append(e, a.destroy(target, source)...)
	}
	return e
}
func (a *Arena) destroy(target, killer *RobotState) []Event {
	if !target.Alive {
		return nil
	}
	oldStreak := target.KillStreak
	target.HP, target.Alive, target.KillStreak, target.StreakName = 0, false, 0, ""
	target.Deaths++
	e := []Event{a.event(Event{Type: "robot_destroyed", RobotID: target.RobotID, TargetID: robotID(killer), X: target.X, Y: target.Y})}
	if killer != nil && killer != target {
		killer.Kills++
		killer.KillStreak++
		killer.StreakName = streakName(killer.KillStreak)
		if killer.StreakName != "" {
			e = append(e, a.event(Event{Type: "kill_streak", RobotID: killer.RobotID, Value: killer.KillStreak, Message: killer.StreakName}))
		}
		if oldStreak >= 3 {
			a.dropItem("shield", target.X, target.Y)
			e = append(e, a.event(Event{Type: "bounty_claimed", RobotID: killer.RobotID, TargetID: target.RobotID}))
		}
	}
	if a.Config.DropOnDeath {
		a.dropItem("repair-core", target.X, target.Y)
	}
	if a.Config.DropWeapons && target.Weapon != "" && target.Weapon != "plasma" {
		a.dropItem("weapon_"+target.Weapon, target.X, target.Y)
		target.Weapon = "plasma"
	}
	return e
}
func (a *Arena) applyEffects(r *RobotState, events *[]Event) {
	next := r.Effects[:0]
	for _, effect := range r.Effects {
		if effect.Type == "shield_decay" && r.Shield > 0 && effect.Ticks%4 == 0 {
			r.Shield--
		}
		if effect.Type == "burn" && effect.Ticks%10 == 0 {
			*events = append(*events, a.damage(a.robot(effect.SourceID), r, max(1, int(effect.Magnitude)), Weapon{})...)
			if !r.Alive {
				break
			}
		}
		effect.Ticks--
		if effect.Ticks > 0 {
			next = append(next, effect)
		}
	}
	r.Effects = next
}
func (a *Arena) checkFinished() {
	teams := map[string]bool{}
	for _, r := range a.Robots {
		if r.Alive {
			teams[r.Team] = true
		}
	}
	if a.singleTeam != "" {
		// A roster with one team from the start (solo sandbox) only ends when
		// every robot is destroyed or the tick budget is spent; otherwise the
		// last-team-standing rule would finish it at tick 0.
		if len(teams) == 0 {
			a.finished, a.winner = true, "draw"
		}
	} else if len(teams) <= 1 {
		a.finished = true
		for t := range teams {
			a.winner = t
		}
		if len(teams) == 0 {
			a.winner = "draw"
		}
		return
	}
	if a.TickNumber >= a.MaxTicks {
		if a.Config.OvertimeTicks > 0 && a.overtimeEnd == 0 {
			a.overtimeEnd = a.MaxTicks + a.Config.OvertimeTicks
			return
		}
		if a.overtimeEnd == 0 || a.TickNumber >= a.overtimeEnd {
			a.finished = true
			a.winner = winnerByHP(a.Robots)
		}
	}
}
func (a *Arena) overtime() bool { return a.overtimeEnd > 0 && a.TickNumber < a.overtimeEnd }
func (a *Arena) snapshot(e []Event) Snapshot {
	status := "running"
	if a.finished {
		status = "finished"
	}
	ann := []string{}
	if a.overtime() {
		ann = append(ann, "OVERTIME")
	}
	zone := a.zoneState()
	if zone != nil && zone.Active {
		ann = append(ann, "ZONE CLOSING")
	}
	return Snapshot{Type: "snapshot", Version: 2, MatchID: a.MatchID, Sequence: a.TickNumber, Tick: a.TickNumber, Status: status, WinnerTeam: a.winner, MapID: a.Config.Map.ID, Width: a.Config.Width, Height: a.Config.Height, Robots: cloneRobots(a.Robots), Projectiles: append([]Projectile(nil), a.Projectiles...), Items: append([]Item(nil), a.Items...), Obstacles: append([]Obstacle(nil), a.Config.Map.Obstacles...), Events: e, Zone: zone, Announcements: ann, Overtime: a.overtime()}
}
func (a *Arena) event(e Event) Event { e.Tick = a.TickNumber; return e }
func (a *Arena) robot(id string) *RobotState {
	for i := range a.Robots {
		if a.Robots[i].RobotID == id {
			return &a.Robots[i]
		}
	}
	return nil
}
func robotID(r *RobotState) string {
	if r == nil {
		return ""
	}
	return r.RobotID
}

func ValidateTeams(rs []RobotState) error {
	return ValidateTeamsForMode("duel", rs)
}

// ValidateTeamsForMode checks a roster's teams per mode. Solo free-for-all
// lobbies may field one team per robot, including the single-robot sandbox
// with no bots at all; every other mode needs at least two teams.
func ValidateTeamsForMode(mode string, rs []RobotState) error {
	teams := map[string]bool{}
	for _, r := range rs {
		if r.Team == "" {
			return errors.New("team is required")
		}
		teams[r.Team] = true
	}
	if mode != "solo" && len(teams) < 2 {
		return errors.New("match requires at least two teams")
	}
	return nil
}
func SpawnPositions(rs []RobotState) []RobotState {
	return SpawnPositionsForMap(rs, DefaultMap(ArenaWidth, ArenaHeight), ArenaWidth, ArenaHeight)
}
func SpawnPositionsForMap(rs []RobotState, m MapDefinition, w, h float64) []RobotState {
	counts := map[string]int{}
	teams := []string{}
	seen := map[string]bool{}
	for _, r := range rs {
		if !seen[r.Team] {
			seen[r.Team] = true
			teams = append(teams, r.Team)
		}
	}
	sort.Strings(teams)
	for i := range rs {
		r := &rs[i]
		r.MaxHP, r.HP, r.Alive = MaxHP, MaxHP, true
		points := m.SpawnPoints[r.Team]
		n := counts[r.Team]
		counts[r.Team]++
		if len(points) > 0 {
			p := points[n%len(points)]
			r.X, r.Y = p.X, p.Y
		} else {
			side := float64(sort.SearchStrings(teams, r.Team)+1) / float64(len(teams)+1)
			r.X = w * side
			r.Y = h * float64(n+1) / float64(countTeam(rs, r.Team)+1)
			// Teams without map spawn points (solo free-for-all) may compute a
			// spot inside an obstacle. Nudge the row deterministically until
			// the spot is clear so no robot is born wedged.
			for step := 0; step < 40 && collidesRobot(m.Obstacles, r.X, r.Y); step++ {
				r.Y += 30
				if r.Y > h-RobotRadius {
					r.Y = RobotRadius
				}
			}
		}
		r.Heading = normalizeDegrees(math.Atan2(h/2-r.Y, w/2-r.X) * 180 / math.Pi)
		r.Weapon = "plasma"
	}
	return rs
}
func countTeam(rs []RobotState, t string) int {
	n := 0
	for _, r := range rs {
		if r.Team == t {
			n++
		}
	}
	return n
}
func winnerByHP(rs []RobotState) string {
	hp, total := map[string]int{}, map[string]int{}
	for _, r := range rs {
		hp[r.Team] += r.HP
		total[r.Team] += max(1, r.MaxHP)
	}
	winner := "draw"
	best := -1.0
	tie := false
	for team, n := range hp {
		score := float64(n) / float64(total[team])
		if score > best {
			best, winner, tie = score, team, false
		} else if score == best {
			tie = true
		}
	}
	if tie {
		return "draw"
	}
	return winner
}
func cloneRobots(rs []RobotState) []RobotState {
	out := append([]RobotState(nil), rs...)
	for i := range out {
		out[i].Logs = append([]string(nil), out[i].Logs...)
		out[i].Equipment = append([]string(nil), out[i].Equipment...)
		out[i].Effects = append([]StatusEffect(nil), out[i].Effects...)
	}
	return out
}
func updateTelemetry(r *RobotState, i Intent) {
	r.Logs = appendBounded(r.Logs, i.Logs, 100)
	r.Connected = true
	r.LastResponseMS = i.ResponseMS
	if i.ResponseMS > 0 {
		if r.AvgResponseMS == 0 {
			r.AvgResponseMS = i.ResponseMS
		} else {
			r.AvgResponseMS = r.AvgResponseMS*.85 + i.ResponseMS*.15
		}
	}
	r.ComputeMS, r.MemoryMB = i.ComputeMS, i.MemoryMB
	if len(i.Equipment) > 0 {
		r.Equipment = append([]string(nil), i.Equipment...)
		for _, equipment := range i.Equipment {
			if _, ok := Weapons[equipment]; ok {
				r.Weapon = equipment
				break
			}
		}
	}
	r.LastAction = describeIntent(i)
}
func describeIntent(i Intent) string {
	if i.Fire {
		return "fire"
	}
	if i.Move != 0 {
		return "move"
	}
	if i.Turn != 0 || i.TargetX != nil {
		return "turn"
	}
	return "idle"
}
func appendBounded(v, a []string, n int) []string {
	v = append(v, a...)
	if len(v) > n {
		v = v[len(v)-n:]
	}
	return v
}
func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }
func normalizeDegrees(v float64) float64 {
	for v < 0 {
		v += 360
	}
	for v >= 360 {
		v -= 360
	}
	return v
}
func shortestTurn(a, b float64) float64 {
	d := normalizeDegrees(b) - normalizeDegrees(a)
	if d > 180 {
		d -= 360
	}
	if d < -180 {
		d += 360
	}
	return d
}
func streakName(n int) string {
	if n >= 7 {
		return "LEGENDARY"
	}
	if n >= 5 {
		return "UNSTOPPABLE"
	}
	if n >= 3 {
		return "RAMPAGE"
	}
	return ""
}
