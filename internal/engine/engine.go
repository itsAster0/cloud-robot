package engine

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"math/rand/v2"
	"sort"
	"strings"
	"sync"
)

const (
	ArenaWidth       = 1200.0
	ArenaHeight      = 750.0
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
	// Protocol additions: dash, deployable mines, and area scans. Mines are
	// proximity bombs with an arming delay; dashes and scans run on charge or
	// cooldown gates so agents cannot spam them every tick.
	MineDamage        = 50
	MineBlastRadius   = 70.0
	MineTriggerRadius = RobotRadius + 10
	MineArmTicks      = 30
	MineLifetimeTicks = 600
	MaxMines          = 20
	DashDistance      = 40.0
	DashCooldownTicks = 40
	ScanCooldownTicks = 60
	MinScanRadius     = 40.0
	MaxScanRadius     = 400.0
	MaxRecentEvents   = 8
	MaxTeamMessages   = 5
	MaxMessageBytes   = 128
	// DefaultVisionRange is the base radius (units) at which a robot perceives
	// items, projectiles, mines, and living enemies in its controller view.
	// Optics/radar effects multiply it; the result clamps to [60, 1000].
	DefaultVisionRange = 320.0
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
	DashCharges    int            `json:"dashCharges,omitempty"`
	MineCharges    int            `json:"mineCharges,omitempty"`
	DashReadyTick  int            `json:"-"`
	ScanReadyTick  int            `json:"-"`
	ScanResult     *ScanReport    `json:"scanResult,omitempty"`
	Messages       []string       `json:"messages,omitempty"`
	RecentEvents   []Event        `json:"events,omitempty"`
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
	// VisionRange is the per-tick perception radius backing WorldState
	// filtering; zero means unlimited for direct helper callers.
	VisionRange float64 `json:"visionRange,omitempty"`
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
	Dash        bool
	Deploy      string
	Scan        *ScanRequest
	Message     string
}
type Controller interface {
	Tick(context.Context, RobotState, []RobotState) (Intent, error)
	Close()
}
type WorldState struct {
	Tick        int
	MapID       string
	Width       float64
	Height      float64
	Obstacles   []Obstacle
	Items       []Item
	Projectiles []Projectile
	Mines       []MineState
	Hazards     []Hazard
	Zone        *ZoneState
	Overtime    bool
	// VisionRange echoes the observer's perception radius so controllers can
	// reason about what the filter already removed.
	VisionRange float64
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

// MineState is a deployable proximity bomb. It arms MineArmTicks after
// SpawnTick, explodes once when a non-owner robot comes close, and is
// removed entirely once its lifetime runs out; expired entries never appear
// in snapshots or observations.
type MineState struct {
	MineID    string  `json:"mineId"`
	OwnerID   string  `json:"ownerId"`
	Team      string  `json:"team"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	SpawnTick int     `json:"spawnTick"`
	ArmTick   int     `json:"armTick"`
	Active    bool    `json:"active"`
}

// TurretRadius is the hit radius robot projectiles use when checking turret
// hits; turrets themselves are not movement obstacles (robots drive through
// them) to keep demos free of permanent traffic jams.
const TurretRadius = 16.0

// TurretProjectileSpeed mirrors the turret firing profile baked into maps.
const TurretProjectileSpeed = 20.0

// TurretState is the shared snapshot/agent view of one neutral map turret.
// Range/Damage/CooldownTicks/NextFireTick are runtime-only (json "-"): the
// browser renders position, HP, and alive state.
type TurretState struct {
	TurretID      string  `json:"turretId"`
	X             float64 `json:"x"`
	Y             float64 `json:"y"`
	HP            int     `json:"hp"`
	MaxHP         int     `json:"maxHp"`
	Alive         bool    `json:"alive"`
	Range         float64 `json:"-"`
	Damage        int     `json:"-"`
	CooldownTicks int     `json:"-"`
	NextFireTick  int     `json:"-"`
}

// ScanRequest is the agent intent payload for an area scan centered on X/Y.
type ScanRequest struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Radius float64 `json:"radius"`
}

// ScanReport is the server-built result of one scan. It persists on the
// scanning robot (snapshot robots[] and agent observations) until replaced.
type ScanReport struct {
	X       float64         `json:"x"`
	Y       float64         `json:"y"`
	Radius  float64         `json:"radius"`
	Items   []ScannedItem   `json:"items"`
	Enemies []ScannedRobot  `json:"enemies"`
	Mines   []ScannedMine   `json:"mines"`
	Hazards []ScannedHazard `json:"hazards"`
}

type ScannedItem struct {
	ItemID   string  `json:"itemId"`
	Type     string  `json:"type"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Distance float64 `json:"distance"`
}

type ScannedRobot struct {
	RobotID  string  `json:"robotId"`
	Team     string  `json:"team"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Heading  float64 `json:"heading"`
	HP       int     `json:"hp"`
	Distance float64 `json:"distance"`
	Cloaked  bool    `json:"cloaked"`
}

type ScannedMine struct {
	MineID   string  `json:"mineId"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Distance float64 `json:"distance"`
	Armed    bool    `json:"armed"`
}

type ScannedHazard struct {
	ID       string  `json:"id"`
	Type     string  `json:"type"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Width    float64 `json:"width"`
	Height   float64 `json:"height"`
	Distance float64 `json:"distance"`
}

type Snapshot struct {
	Type          string        `json:"type"`
	Version       int           `json:"version"`
	MatchID       string        `json:"matchId"`
	Sequence      int           `json:"sequence"`
	Tick          int           `json:"tick"`
	Status        string        `json:"status"`
	WinnerTeam    string        `json:"winnerTeam,omitempty"`
	MapID         string        `json:"mapId"`
	Width         float64       `json:"width"`
	Height        float64       `json:"height"`
	Robots        []RobotState  `json:"robots"`
	Projectiles   []Projectile  `json:"projectiles"`
	Items         []Item        `json:"items,omitempty"`
	Mines         []MineState   `json:"mines,omitempty"`
	Turrets       []TurretState `json:"turrets,omitempty"`
	Obstacles     []Obstacle    `json:"obstacles,omitempty"`
	Events        []Event       `json:"events,omitempty"`
	Zone          *ZoneState    `json:"zone,omitempty"`
	Announcements []string      `json:"announcements,omitempty"`
	Overtime      bool          `json:"overtime"`
}

type Arena struct {
	MatchID     string
	TickNumber  int
	MaxTicks    int
	Robots      []RobotState
	Projectiles []Projectile
	Items       []Item
	Mines       []MineState
	Turrets     []TurretState
	Config      Config
	// singleTeam is set when the roster fielded exactly one team (solo lobby
	// without bots). Such matches must not end at tick 0 through the
	// last-team-standing rule; they end only when everyone dies or ticks out.
	singleTeam    string
	controllers   map[string]Controller
	withdrawals   chan string
	winner        string
	finished      bool
	rng           *rand.Rand
	nextItem      int
	nextMine      int
	lastSpawnTick int
	overtimeEnd   int
	events        []Event
	pairRam       map[string]int
	pickupPrefs   map[string]Intent
	itemsOrdered  bool
	// TeamMessages queues chat lines keyed by sender robot ID; they flush to
	// the sender's teammates at the start of the next tick.
	TeamMessages map[string][]string
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
	// Turrets mirror the map's TurretSpec list with live runtime state; the
	// json-"-" fields carry the firing profile so no second lookup is needed.
	var turrets []TurretState
	for _, spec := range c.Map.Turrets {
		turrets = append(turrets, TurretState{TurretID: spec.TurretID, X: spec.X, Y: spec.Y, HP: spec.HP, MaxHP: spec.HP, Alive: true, Range: spec.Range, Damage: spec.Damage, CooldownTicks: spec.CooldownTicks})
	}
	return &Arena{MatchID: id, Robots: rs, controllers: controllers, withdrawals: make(chan string, 8), MaxTicks: c.MaxTicks, Config: c, singleTeam: singleTeam, rng: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)), pairRam: map[string]int{}, pickupPrefs: map[string]Intent{}, TeamMessages: map[string][]string{}, Turrets: turrets}
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
	a.deliverTeamMessages()
	// Vision is recomputed before decisions so controllers see both their own
	// range and a consistently filtered world this tick.
	for i := range a.Robots {
		a.Robots[i].VisionRange = clamp(a.Config.VisionRange*visionMult(a.Robots[i]), 60, 1000)
	}
	before := cloneRobots(a.Robots)
	intents := make(map[string]Intent, len(before))
	type decision struct {
		active bool
		index  int
		intent Intent
		err    error
	}
	ordered := make([]decision, len(before))
	var wg sync.WaitGroup
	for i := range before {
		if !before[i].Alive {
			continue
		}
		c := a.controllers[before[i].RobotID]
		if c == nil {
			ordered[i] = decision{active: true, index: i, err: errors.New("controller missing")}
			continue
		}
		if aware, ok := c.(WorldAwareController); ok {
			aware.SetWorld(a.worldFor(before[i]))
		}
		view := a.visibleRobots(before[i])
		wg.Add(1)
		go func(n int, c Controller, view []RobotState) {
			defer wg.Done()
			v, e := c.Tick(ctx, before[n], view)
			ordered[n] = decision{active: true, index: n, intent: v, err: e}
		}(i, c, view)
	}
	wg.Wait()
	for _, d := range ordered {
		if !d.active {
			continue
		}
		r := &a.Robots[d.index]
		if d.err != nil {
			r.Alive, r.Failed, r.HP = false, true, 0
			r.Deaths++
			events = append(events, a.event(Event{Type: "robot_failed", RobotID: r.RobotID, Message: d.err.Error()}))
			continue
		}
		intents[r.RobotID] = d.intent
		a.pickupPrefs[r.RobotID] = d.intent
		if message := strings.TrimSpace(d.intent.Message); message != "" {
			if len(message) > MaxMessageBytes {
				message = message[:MaxMessageBytes]
			}
			a.TeamMessages[r.RobotID] = append(a.TeamMessages[r.RobotID], message)
		}
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
		// Dash rides the same slide pattern as a normal move but covers a
		// flat burst distance, ignoring slow and overdrive. Gated on charge
		// and cooldown so agents cannot chain bursts every tick.
		if in.Dash && r.DashCharges > 0 && a.TickNumber >= r.DashReadyTick {
			r.DashCharges--
			r.DashReadyTick = a.TickNumber + DashCooldownTicks
			rad := r.Heading * math.Pi / 180
			dx, dy := clamp(r.X+math.Cos(rad)*DashDistance, RobotRadius, a.Config.Width-RobotRadius), clamp(r.Y+math.Sin(rad)*DashDistance, RobotRadius, a.Config.Height-RobotRadius)
			if !collidesRobot(a.Config.Map.Obstacles, dx, dy) {
				r.X, r.Y = dx, dy
			} else if !collidesRobot(a.Config.Map.Obstacles, dx, r.Y) {
				r.X = dx
			} else if !collidesRobot(a.Config.Map.Obstacles, r.X, dy) {
				r.Y = dy
			}
			events = append(events, a.event(Event{Type: "dash", RobotID: r.RobotID, X: r.X, Y: r.Y}))
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
	events = append(events, a.advanceMines()...)
	events = append(events, a.advanceTurrets()...)
	events = append(events, a.advanceProjectiles()...)
	for i := range a.Robots {
		r := &a.Robots[i]
		in := intents[r.RobotID]
		if r.Alive && in.Fire && r.Cooldown == 0 && !effectActive(*r, "emp") {
			events = append(events, a.fire(r)...)
		}
	}
	events = append(events, a.applyDeploys(intents)...)
	events = append(events, a.applyScans(intents)...)
	events = append(events, a.powerSurge()...)
	events = append(events, a.spawnItems()...)
	events = append(events, a.pickupItems()...)
	events = append(events, a.applyZone()...)
	events = append(events, a.applyHazards()...)
	a.TickNumber++
	a.checkFinished()
	a.events = append(a.events, events...)
	a.attachRecentEvents(events)
	return a.snapshot(events)
}

func (a *Arena) fire(r *RobotState) []Event {
	if r.Weapon == "mine_layer" {
		// Mines deploy through the deploy intent, not the fire phase; the
		// cooldown keeps a held trigger from retrying every tick.
		r.Cooldown = 20
		return nil
	}
	w := WeaponByName(r.Weapon)
	// Firing breaks cloak before the shot resolves.
	breakCloak(r)
	cd := w.Cooldown
	if effectActive(*r, "rapid_fire") {
		cd = max(1, cd/2)
	}
	r.Cooldown = cd
	damage := w.Damage
	if a.overtime() {
		damage *= 2
	}
	if effectActive(*r, "berserk") {
		damage = int(math.Round(float64(damage) * 1.5))
	}
	angle := r.Heading
	rad := angle * math.Pi / 180
	e := []Event{a.event(Event{Type: "shot_fired", RobotID: r.RobotID, Message: w.Name})}
	if w.Pellets > 1 {
		for range w.Pellets {
			pellet := angle + (a.rng.Float64()*2-1)*w.Spread
			a.Projectiles = append(a.Projectiles, a.projectile(r, w, pellet*math.Pi/180, damage))
		}
		return e
	}
	if w.Spread > 0 {
		angle += (a.rng.Float64()*2 - 1) * w.Spread
		rad = angle * math.Pi / 180
	}
	if w.Hitscan {
		target, d := a.firstTargetOnRay(*r, rad, w.Range)
		if target != nil {
			e = append(e, a.damage(r, target, damage, w)...)
			e = append(e, a.event(Event{Type: "railgun_hit", RobotID: r.RobotID, TargetID: target.RobotID, X: r.X + math.Cos(rad)*d, Y: r.Y + math.Sin(rad)*d}))
		}
		return e
	}
	a.Projectiles = append(a.Projectiles, a.projectile(r, w, rad, damage))
	return e
}

// projectile builds one fired projectile. The ID embeds the live projectile
// count so multi-pellet shots in the same tick stay unique.
func (a *Arena) projectile(r *RobotState, w Weapon, rad float64, damage int) Projectile {
	return Projectile{ProjectileID: fmt.Sprintf("%s-%d-%d", r.RobotID, a.TickNumber, len(a.Projectiles)), OwnerID: r.RobotID, Team: r.Team, Kind: w.Name, X: r.X + math.Cos(rad)*(RobotRadius+ProjectileRadius+1), Y: r.Y + math.Sin(rad)*(RobotRadius+ProjectileRadius+1), VX: math.Cos(rad) * w.ProjectileSpeed, VY: math.Sin(rad) * w.ProjectileSpeed, Damage: damage, TTL: max(1, int(w.Range/w.ProjectileSpeed)), Knockback: w.Knockback, BurnTicks: w.BurnTicks, SlowTicks: w.SlowTicks, EMPTicks: w.EMPTicks}
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
			e = append(e, a.explode(p)...)
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
			if w.BlastRadius > 0 {
				// Blast weapons trade the direct hit for the explosion, which
				// already covers the struck robot.
				e = append(e, a.explode(p)...)
			} else {
				w.Knockback, w.BurnTicks, w.SlowTicks, w.EMPTicks = p.Knockback, p.BurnTicks, p.SlowTicks, p.EMPTicks
				e = append(e, a.damage(a.robot(p.OwnerID), t, p.Damage, w)...)
			}
			hit = true
			break
		}
		// Robots can shoot neutral turrets (turret projectiles skip this:
		// their owners are "turret-*", never robots). Any robot can damage a
		// turret regardless of team or friendly-fire settings; destroying one
		// drops a shield as the reward.
		if !hit && !strings.HasPrefix(p.OwnerID, "turret-") {
			for i := range a.Turrets {
				turret := &a.Turrets[i]
				if !turret.Alive || distancePointSegment(turret.X, turret.Y, ox, oy, p.X, p.Y) > TurretRadius {
					continue
				}
				turret.HP -= p.Damage
				e = append(e, a.event(Event{Type: "turret_damaged", RobotID: p.OwnerID, TargetID: turret.TurretID, Damage: p.Damage}))
				if turret.HP <= 0 {
					turret.Alive = false
					e = append(e, a.event(Event{Type: "turret_destroyed", RobotID: p.OwnerID, TargetID: turret.TurretID, X: turret.X, Y: turret.Y}))
					a.dropItem("shield", turret.X, turret.Y)
				}
				hit = true
				break
			}
		}
		if !hit {
			active = append(active, p)
		}
	}
	a.Projectiles = active
	return e
}

// explode applies a blast weapon's area damage at the projectile's final
// position: full BlastDamage inside half the radius, half (min 1) out to the
// edge. The owner is immune and friendly-fire rules are honored.
func (a *Arena) explode(p Projectile) []Event {
	w := WeaponByName(p.Kind)
	if w.BlastRadius <= 0 {
		return nil
	}
	e := []Event{a.event(Event{Type: "explosion", RobotID: p.OwnerID, X: p.X, Y: p.Y, Value: w.BlastDamage})}
	owner := a.robot(p.OwnerID)
	for i := range a.Robots {
		t := &a.Robots[i]
		if !t.Alive || t.RobotID == p.OwnerID || (!a.Config.FriendlyFire && t.Team == p.Team) {
			continue
		}
		distance := math.Hypot(t.X-p.X, t.Y-p.Y)
		if distance > w.BlastRadius {
			continue
		}
		amount := w.BlastDamage
		if distance > w.BlastRadius*0.5 {
			amount = max(1, amount/2)
		}
		e = append(e, a.damage(owner, t, amount, w)...)
	}
	return e
}

// advanceMines runs after movement so freshly-armed mines catch robots that
// just drove over them. Mines resolve in slice order; the first non-owner
// robot inside the trigger radius detonates the mine, which damages everyone
// in the blast (owner always immune, teammates per friendly-fire), then the
// loop continues with the next mine. Expired mines are dropped and the slice
// is compacted preserving order.
func (a *Arena) advanceMines() []Event {
	events := []Event{}
	for i := range a.Mines {
		mine := &a.Mines[i]
		if !mine.Active || a.TickNumber < mine.ArmTick {
			continue
		}
		if victim := a.firstMineVictim(*mine); victim != nil {
			events = append(events, a.explodeMine(mine, victim)...)
			continue
		}
		if a.TickNumber >= mine.SpawnTick+MineLifetimeTicks {
			mine.Active = false
			events = append(events, a.event(Event{Type: "mine_expired", RobotID: mine.OwnerID, X: mine.X, Y: mine.Y}))
		}
	}
	kept := a.Mines[:0]
	for _, mine := range a.Mines {
		if mine.Active {
			kept = append(kept, mine)
		}
	}
	a.Mines = kept
	return events
}

// firstMineVictim finds the first alive robot (roster order) close enough to
// set the mine off. The owner never triggers their own mine, even standing
// on it; teammates can, though friendly-fire rules still guard the blast.
func (a *Arena) firstMineVictim(mine MineState) *RobotState {
	for i := range a.Robots {
		r := &a.Robots[i]
		if !r.Alive || r.RobotID == mine.OwnerID {
			continue
		}
		if math.Hypot(r.X-mine.X, r.Y-mine.Y) <= MineTriggerRadius {
			return r
		}
	}
	return nil
}

// explodeMine deals flat MineDamage to every living robot in the blast radius
// through the normal damage pipeline (shields and armor still apply). The
// owner is exempt even under friendly fire; teammates follow the arena's
// FriendlyFire setting.
func (a *Arena) explodeMine(mine *MineState, victim *RobotState) []Event {
	mine.Active = false
	owner := a.robot(mine.OwnerID)
	events := []Event{a.event(Event{Type: "mine_exploded", RobotID: mine.OwnerID, TargetID: victim.RobotID, X: mine.X, Y: mine.Y, Value: MineDamage})}
	for i := range a.Robots {
		r := &a.Robots[i]
		if !r.Alive || r.RobotID == mine.OwnerID || (!a.Config.FriendlyFire && r.Team == mine.Team) {
			continue
		}
		if math.Hypot(r.X-mine.X, r.Y-mine.Y) > MineBlastRadius {
			continue
		}
		events = append(events, a.damage(owner, r, MineDamage, Weapon{})...)
	}
	return events
}

// advanceTurrets lets neutral map turrets engage. Turrets run after mines and
// before projectiles so their shots travel the same tick they are fired. Each
// turret scans the roster in slice order and engages the first living robot
// that is visible (cloak hides), inside Range, and has line of sight; the
// cooldown stamp is set only on an actual shot, so an idle turret fires the
// moment a valid target appears. Turrets attack every team and hold position
// forever — they are scenery with a grudge, not participants.
func (a *Arena) advanceTurrets() []Event {
	events := []Event{}
	for i := range a.Turrets {
		turret := &a.Turrets[i]
		if !turret.Alive || a.TickNumber < turret.NextFireTick {
			continue
		}
		target := a.firstTurretTarget(*turret)
		if target == nil {
			continue
		}
		dx, dy := target.X-turret.X, target.Y-turret.Y
		distance := math.Hypot(dx, dy)
		if distance <= 0 {
			continue
		}
		a.Projectiles = append(a.Projectiles, Projectile{ProjectileID: fmt.Sprintf("turret-%s-%d", turret.TurretID, a.TickNumber), OwnerID: turret.TurretID, Team: "turret", Kind: "plasma", X: turret.X, Y: turret.Y, VX: dx / distance * TurretProjectileSpeed, VY: dy / distance * TurretProjectileSpeed, Damage: turret.Damage, TTL: max(1, int(distance/TurretProjectileSpeed)+2)})
		turret.NextFireTick = a.TickNumber + turret.CooldownTicks
		events = append(events, a.event(Event{Type: "turret_shot", TargetID: target.RobotID, X: turret.X, Y: turret.Y}))
	}
	return events
}

// firstTurretTarget returns the first living, uncloaked, in-range robot with
// clear line of sight, in roster order.
func (a *Arena) firstTurretTarget(turret TurretState) *RobotState {
	for i := range a.Robots {
		r := &a.Robots[i]
		if !r.Alive || isCloaked(*r) || math.Hypot(r.X-turret.X, r.Y-turret.Y) > turret.Range {
			continue
		}
		if a.LineOfSight(turret.X, turret.Y, r.X, r.Y) {
			return r
		}
	}
	return nil
}

// applyDeploys places mines requested by the deploy intent, in roster order.
// Charges gate the deploy, the arena-wide cap refuses silently when full, and
// the landing spot is nudged out of obstacles so mines never spawn buried.
func (a *Arena) applyDeploys(intents map[string]Intent) []Event {
	events := []Event{}
	for i := range a.Robots {
		r := &a.Robots[i]
		in := intents[r.RobotID]
		if !r.Alive || in.Deploy != "mine" || r.MineCharges <= 0 {
			continue
		}
		if a.activeMineCount() >= MaxMines {
			continue
		}
		r.MineCharges--
		a.nextMine++
		x, y := a.safeDrop(r.X, r.Y)
		a.Mines = append(a.Mines, MineState{MineID: fmt.Sprintf("mine-%s-%d", r.RobotID, a.nextMine), OwnerID: r.RobotID, Team: r.Team, X: x, Y: y, SpawnTick: a.TickNumber, ArmTick: a.TickNumber + MineArmTicks, Active: true})
		events = append(events, a.event(Event{Type: "mine_deployed", RobotID: r.RobotID, X: x, Y: y, Message: "mine"}))
	}
	return events
}

func (a *Arena) activeMineCount() int {
	n := 0
	for _, mine := range a.Mines {
		if mine.Active {
			n++
		}
	}
	return n
}

// applyScans resolves scan intents in roster order. The result persists on
// the robot until the next scan; a scan on cooldown is ignored silently.
func (a *Arena) applyScans(intents map[string]Intent) []Event {
	events := []Event{}
	for i := range a.Robots {
		r := &a.Robots[i]
		in := intents[r.RobotID]
		if !r.Alive || in.Scan == nil || a.TickNumber < r.ScanReadyTick {
			continue
		}
		r.ScanReadyTick = a.TickNumber + ScanCooldownTicks
		r.ScanResult = a.buildScanReport(*r, *in.Scan)
		events = append(events, a.event(Event{Type: "scan_performed", RobotID: r.RobotID, X: in.Scan.X, Y: in.Scan.Y, Value: int(r.ScanResult.Radius)}))
	}
	return events
}

// buildScanReport assembles everything inside the scan circle: active items,
// living enemies (cloaked ones only for radar holders), active mines, and
// hazards whose rectangle center falls in range. Every list is sorted by ID
// so report bytes stay stable.
func (a *Arena) buildScanReport(r RobotState, req ScanRequest) *ScanReport {
	radius := clamp(req.Radius, MinScanRadius, MaxScanRadius)
	report := &ScanReport{X: req.X, Y: req.Y, Radius: radius, Items: []ScannedItem{}, Enemies: []ScannedRobot{}, Mines: []ScannedMine{}, Hazards: []ScannedHazard{}}
	radar := effectActive(r, "radar")
	for i := range a.Items {
		item := &a.Items[i]
		if !item.Active {
			continue
		}
		distance := math.Hypot(item.X-req.X, item.Y-req.Y)
		if distance > radius {
			continue
		}
		report.Items = append(report.Items, ScannedItem{ItemID: item.ItemID, Type: item.Type, X: item.X, Y: item.Y, Distance: distance})
	}
	sort.Slice(report.Items, func(i, j int) bool { return report.Items[i].ItemID < report.Items[j].ItemID })
	for i := range a.Robots {
		enemy := &a.Robots[i]
		if !enemy.Alive || enemy.Team == r.Team {
			continue
		}
		cloaked := isCloaked(*enemy)
		if cloaked && !radar {
			continue
		}
		distance := math.Hypot(enemy.X-req.X, enemy.Y-req.Y)
		if distance > radius {
			continue
		}
		report.Enemies = append(report.Enemies, ScannedRobot{RobotID: enemy.RobotID, Team: enemy.Team, X: enemy.X, Y: enemy.Y, Heading: enemy.Heading, HP: enemy.HP, Distance: distance, Cloaked: cloaked})
	}
	sort.Slice(report.Enemies, func(i, j int) bool { return report.Enemies[i].RobotID < report.Enemies[j].RobotID })
	for i := range a.Mines {
		mine := &a.Mines[i]
		if !mine.Active {
			continue
		}
		distance := math.Hypot(mine.X-req.X, mine.Y-req.Y)
		if distance > radius {
			continue
		}
		report.Mines = append(report.Mines, ScannedMine{MineID: mine.MineID, X: mine.X, Y: mine.Y, Distance: distance, Armed: a.TickNumber >= mine.ArmTick})
	}
	sort.Slice(report.Mines, func(i, j int) bool { return report.Mines[i].MineID < report.Mines[j].MineID })
	for _, hazard := range a.Config.Map.Hazards {
		distance := math.Hypot(hazard.X+hazard.Width/2-req.X, hazard.Y+hazard.Height/2-req.Y)
		if distance > radius {
			continue
		}
		report.Hazards = append(report.Hazards, ScannedHazard{ID: hazard.ID, Type: hazard.Type, X: hazard.X, Y: hazard.Y, Width: hazard.Width, Height: hazard.Height, Distance: distance})
	}
	sort.Slice(report.Hazards, func(i, j int) bool { return report.Hazards[i].ID < report.Hazards[j].ID })
	return report
}

// worldFor builds the per-robot WorldState handed to WorldAwareControllers
// (and mirrored into agent observations). Items, projectiles, mines, and
// living enemies are filtered to the observer's vision range; obstacles,
// hazards, and the zone stay full so navigation keeps working. BROWSER
// SNAPSHOTS INTENTIONALLY STAY FULL-VISIBILITY — only controller/agent views
// are filtered here.
func (a *Arena) worldFor(observer RobotState) WorldState {
	a.ensureItemOrder()
	within := func(x, y float64) bool {
		dx, dy := x-observer.X, y-observer.Y
		return dx*dx+dy*dy <= observer.VisionRange*observer.VisionRange
	}
	items := []Item{}
	for i := range a.Items {
		if item := a.Items[i]; item.Active && within(item.X, item.Y) {
			items = append(items, item)
		}
	}
	// Filtering preserves the arena's stable item order. Projectiles and mines
	// keep engine slice order as before.
	projectiles := []Projectile{}
	for _, p := range a.Projectiles {
		if within(p.X, p.Y) {
			projectiles = append(projectiles, p)
		}
	}
	mines := []MineState{}
	for _, mine := range a.Mines {
		if mine.Active && within(mine.X, mine.Y) {
			mines = append(mines, mine)
		}
	}
	return WorldState{Tick: a.TickNumber, MapID: a.Config.Map.ID, Width: a.Config.Width, Height: a.Config.Height, Obstacles: a.Config.Map.Obstacles, Items: items, Projectiles: projectiles, Mines: mines, Hazards: a.Config.Map.Hazards, Zone: a.zoneState(), Overtime: a.overtime(), VisionRange: observer.VisionRange}
}

// visibleRobots is the per-robot view handed to controllers: teammates and the
// observer are always visible; living enemies are dropped when beyond the
// observer's vision range (a zero range means unlimited), and cloaked enemies
// additionally stay hidden unless the observer carries radar. Range and cloak
// both apply: radar reveals a cloaked enemy only inside vision range.
func (a *Arena) visibleRobots(observer RobotState) []RobotState {
	view := make([]RobotState, 0, len(a.Robots))
	radar := effectActive(observer, "radar")
	for _, r := range a.Robots {
		if r.Alive && r.RobotID != observer.RobotID && r.Team != observer.Team {
			if observer.VisionRange > 0 && math.Hypot(r.X-observer.X, r.Y-observer.Y) > observer.VisionRange {
				continue
			}
			if isCloaked(r) && !radar {
				continue
			}
		}
		view = append(view, r)
	}
	return view
}

// deliverTeamMessages flushes chat queued during the previous tick onto the
// sender's teammates, in roster order of the senders, at most
// MaxTeamMessages lines per robot. Senders never receive their own lines.
func (a *Arena) deliverTeamMessages() {
	pending := a.TeamMessages
	a.TeamMessages = map[string][]string{}
	for i := range a.Robots {
		a.Robots[i].Messages = nil
	}
	if len(pending) == 0 {
		return
	}
	for i := range a.Robots {
		receiver := &a.Robots[i]
		for j := range a.Robots {
			sender := &a.Robots[j]
			if sender.RobotID == receiver.RobotID || sender.Team != receiver.Team {
				continue
			}
			for _, message := range pending[sender.RobotID] {
				if len(receiver.Messages) >= MaxTeamMessages {
					break
				}
				receiver.Messages = append(receiver.Messages, message)
			}
			if len(receiver.Messages) >= MaxTeamMessages {
				break
			}
		}
	}
}

// attachRecentEvents gives each robot the events of the just-completed tick
// where it was actor or target, capped at the most recent MaxRecentEvents,
// oldest first. Cleared every tick by assigning nil first.
func (a *Arena) attachRecentEvents(events []Event) {
	for i := range a.Robots {
		r := &a.Robots[i]
		r.RecentEvents = nil
		for _, e := range events {
			if e.RobotID == r.RobotID || e.TargetID == r.RobotID {
				r.RecentEvents = append(r.RecentEvents, e)
			}
		}
		if len(r.RecentEvents) > MaxRecentEvents {
			r.RecentEvents = append([]Event(nil), r.RecentEvents[len(r.RecentEvents)-MaxRecentEvents:]...)
		}
	}
}

func (a *Arena) damage(source, target *RobotState, amount int, w Weapon) []Event {
	if target == nil || !target.Alive {
		return nil
	}
	breakCloak(target)
	// Criticals apply to weapon hits only. Burn ticks, ramming, and hazards
	// pass a zero-value Weapon, so their steady pressure never spikes.
	crit := false
	if w.Damage > 0 && a.Config.CriticalChance > 0 && a.rng.IntN(100) < a.Config.CriticalChance {
		crit = true
		amount = amount * 3 / 2
	}
	absorbed := min(target.Shield, amount)
	target.Shield -= absorbed
	actual := amount - absorbed
	if actual > 0 {
		// Armor scales the post-shield remainder but never fully negates a
		// landed hit; berserk robots take 25% extra on top of that.
		actual = max(1, int(float64(actual)*effectMultiplier(*target, "armor", 1)))
		if effectActive(*target, "berserk") {
			actual = max(1, int(float64(actual)*1.25))
		}
	}
	target.HP -= actual
	target.DamageTaken += actual
	target.LastDamageTick = a.TickNumber
	if source != nil {
		source.DamageDealt += actual
	}
	e := []Event{a.event(Event{Type: "hit", RobotID: robotID(source), TargetID: target.RobotID, Damage: actual, Value: absorbed})}
	if crit {
		e = append(e, a.event(Event{Type: "critical_hit", RobotID: robotID(source), TargetID: target.RobotID, Damage: actual, Message: "critical hit"}))
	}
	if source != nil && source.Alive && actual > 0 && effectActive(*source, "vampiric") {
		if healed := min(source.MaxHP-source.HP, actual/4); healed > 0 {
			source.HP += healed
			e = append(e, a.event(Event{Type: "vampiric_heal", RobotID: source.RobotID, Value: healed}))
		}
	}
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
		// Streak rewards fire on exact counts and never touch the RNG: 3 kills
		// grant a decaying shield, 5 grant the frenzy overdrive burst.
		if killer.KillStreak == 3 {
			killer.Shield = min(50, killer.Shield+50)
			upsertEffect(killer, "shield_decay", 200, 0.25, "")
			e = append(e, a.event(Event{Type: "streak_reward", RobotID: killer.RobotID, Value: 3, Message: "shield"}))
		}
		if killer.KillStreak == 5 {
			upsertEffect(killer, "overdrive", 60, 1.5, "")
			upsertEffect(killer, "rapid_fire", 60, 0.5, "")
			e = append(e, a.event(Event{Type: "streak_reward", RobotID: killer.RobotID, Value: 5, Message: "frenzy"}))
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
		// Regen is healing, not damage: it bypasses the damage pipeline
		// (no shields, crits, or counters) and tops up every 10 ticks.
		if effect.Type == "regen" && effect.Ticks%10 == 0 && r.HP < r.MaxHP {
			healed := min(r.MaxHP-r.HP, int(math.Round(effect.Magnitude)))
			if healed > 0 {
				r.HP += healed
				*events = append(*events, a.event(Event{Type: "regen_tick", TargetID: r.RobotID, Value: healed}))
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
	// Solo matches name the human robot explicitly: the moment it stops being
	// alive — by shot, withdrawal, or controller failure — the match ends and
	// the surviving teams are ranked by HP. Nobody left alive is a draw. The
	// worker only sets SoloRobotID for solo mode, so other modes are unaffected.
	if a.Config.SoloRobotID != "" {
		if solo := a.robot(a.Config.SoloRobotID); solo != nil && !solo.Alive {
			alive := []RobotState{}
			for _, r := range a.Robots {
				if r.Alive {
					alive = append(alive, r)
				}
			}
			a.finished, a.winner = true, winnerByHP(alive)
			return
		}
	}
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
		// Multi-stage zones call out the last stage explicitly; single-stage
		// zones keep the plain closing banner.
		if a.Config.Zone.StageCount > 1 && zone.Stage >= a.Config.Zone.StageCount {
			ann = append(ann, "FINAL ZONE")
		} else {
			ann = append(ann, "ZONE CLOSING")
		}
	}
	// The surge event is stamped on tick T but the snapshot covers T+1, so
	// announcements key off the just-completed tick.
	if !a.finished && a.Config.PowerSurgeEveryTicks > 0 && a.TickNumber > 1 && (a.TickNumber-1)%a.Config.PowerSurgeEveryTicks == 0 {
		ann = append(ann, "POWER SURGE")
	}
	return Snapshot{Type: "snapshot", Version: 3, MatchID: a.MatchID, Sequence: a.TickNumber, Tick: a.TickNumber, Status: status, WinnerTeam: a.winner, MapID: a.Config.Map.ID, Width: a.Config.Width, Height: a.Config.Height, Robots: cloneRobots(a.Robots), Projectiles: append([]Projectile(nil), a.Projectiles...), Items: append([]Item(nil), a.Items...), Mines: append([]MineState(nil), a.Mines...), Turrets: append([]TurretState(nil), a.Turrets...), Obstacles: a.Config.Map.Obstacles, Events: e, Zone: zone, Announcements: ann, Overtime: a.overtime()}
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
		out[i].Messages = append([]string(nil), out[i].Messages...)
		out[i].RecentEvents = append([]Event(nil), out[i].RecentEvents...)
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
