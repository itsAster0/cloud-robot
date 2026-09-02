package engine

import (
	"math"
	"sort"
)

type ItemConfig struct {
	SpawnMinTicks   int     `json:"spawnMinTicks"`
	SpawnMaxTicks   int     `json:"spawnMaxTicks"`
	RespawnMinTicks int     `json:"respawnMinTicks"`
	RespawnMaxTicks int     `json:"respawnMaxTicks"`
	MaxConcurrent   int     `json:"maxConcurrent"`
	PickupRadius    float64 `json:"pickupRadius"`
}
type ZoneConfig struct {
	Enabled   bool    `json:"enabled"`
	StartTick int     `json:"startTick"`
	EndTick   int     `json:"endTick"`
	EndRadius float64 `json:"endRadius"`
	Damage    int     `json:"damage"`
	// StageCount splits the shrink window into that many equal segments
	// separated by short hold pauses. Values <= 1 keep the legacy single
	// continuous shrink; normalize() defaults it to 3 when the zone is on.
	StageCount     int `json:"stageCount"`
	DamageInterval int `json:"damageInterval"`
}
type Config struct {
	Width           float64       `json:"width"`
	Height          float64       `json:"height"`
	Map             MapDefinition `json:"map"`
	Seed            uint64        `json:"seed"`
	MaxTicks        int           `json:"maxTicks"`
	FriendlyFire    bool          `json:"friendlyFire"`
	RegenPerTick    int           `json:"regenPerTick"`
	RegenDelayTicks int           `json:"regenDelayTicks"`
	Items           ItemConfig    `json:"items"`
	DropOnDeath     bool          `json:"dropOnDeath"`
	DropWeapons     bool          `json:"dropWeapons"`
	Zone            ZoneConfig    `json:"zone"`
	OvertimeTicks   int           `json:"overtimeTicks"`
	RammingDamage   bool          `json:"rammingDamage"`
	// CriticalChance is the percent of weapon hits that deal 1.5x damage.
	// Zero disables crits; burn, ramming, and hazard damage never crit.
	CriticalChance int `json:"criticalChance"`
	// PowerSurgeEveryTicks drops one epic item at the arena center every N
	// ticks; zero (or negative) disables the surge entirely.
	PowerSurgeEveryTicks int `json:"powerSurgeEveryTicks"`
	// VisionRange is the base perception radius for controller/agent world
	// views; zero normalizes to DefaultVisionRange.
	VisionRange float64 `json:"visionRange"`
	// SoloRobotID names the single human robot in solo matches; the engine
	// ends the match as soon as that robot stops being alive. Empty disables
	// the check (plain solo sandbox still runs to the tick limit).
	SoloRobotID string `json:"soloRobotId,omitempty"`
}

func DefaultConfig() Config {
	return Config{Width: ArenaWidth, Height: ArenaHeight, Map: DefaultMap(ArenaWidth, ArenaHeight), MaxTicks: DefaultMaxTicks, Items: ItemConfig{SpawnMinTicks: 100, SpawnMaxTicks: 200, RespawnMinTicks: 100, RespawnMaxTicks: 150, MaxConcurrent: 3, PickupRadius: 20}, DropOnDeath: true, DropWeapons: true, Zone: ZoneConfig{Enabled: true, StartTick: 1200, EndTick: 1800, EndRadius: 80, Damage: 2, DamageInterval: 10, StageCount: 3}, OvertimeTicks: 100, CriticalChance: 10, PowerSurgeEveryTicks: 300, VisionRange: DefaultVisionRange}
}
func (c *Config) normalize() {
	mapWidth, mapHeight := c.Map.Width, c.Map.Height
	if c.Width == 0 {
		c.Width = ArenaWidth
	}
	if c.Height == 0 {
		c.Height = ArenaHeight
	}
	if c.Map.Width > 0 && c.Width == ArenaWidth {
		c.Width = c.Map.Width
	}
	if c.Map.Height > 0 && c.Height == ArenaHeight {
		c.Height = c.Map.Height
	}
	c.Width = clamp(c.Width, 400, 2000)
	c.Height = clamp(c.Height, 300, 1400)
	if c.MaxTicks <= 0 {
		c.MaxTicks = DefaultMaxTicks
	}
	if c.Map.ID == "" {
		c.Map = DefaultMap(c.Width, c.Height)
		mapWidth, mapHeight = c.Width, c.Height
	}
	if mapWidth > 0 && mapHeight > 0 && (mapWidth != c.Width || mapHeight != c.Height) {
		c.Map = ScaleMap(c.Map, c.Width, c.Height)
	}
	c.Map.Width, c.Map.Height = c.Width, c.Height
	if c.Items.SpawnMinTicks <= 0 {
		c.Items.SpawnMinTicks = 100
	}
	if c.Items.SpawnMaxTicks < c.Items.SpawnMinTicks {
		c.Items.SpawnMaxTicks = c.Items.SpawnMinTicks
	}
	if c.Items.RespawnMinTicks <= 0 {
		c.Items.RespawnMinTicks = 100
	}
	if c.Items.RespawnMaxTicks < c.Items.RespawnMinTicks {
		c.Items.RespawnMaxTicks = c.Items.RespawnMinTicks
	}
	if c.Items.MaxConcurrent <= 0 {
		c.Items.MaxConcurrent = 3
	}
	if c.Items.PickupRadius <= 0 {
		c.Items.PickupRadius = 20
	}
	if c.Zone.DamageInterval <= 0 {
		c.Zone.DamageInterval = 10
	}
	if c.Zone.Damage <= 0 {
		c.Zone.Damage = 2
	}
	if c.Zone.Enabled && c.Zone.StageCount <= 0 {
		c.Zone.StageCount = 3
	}
	if c.RegenDelayTicks <= 0 {
		c.RegenDelayTicks = 50
	}
	if c.VisionRange <= 0 {
		c.VisionRange = DefaultVisionRange
	}
}

type Item struct {
	ItemID       string  `json:"itemId"`
	Type         string  `json:"type"`
	X            float64 `json:"x"`
	Y            float64 `json:"y"`
	SpawnTick    int     `json:"spawnTick"`
	Active       bool    `json:"active"`
	PickupRadius float64 `json:"pickupRadius"`
	Source       string  `json:"source,omitempty"`
	RespawnTick  int     `json:"respawnTick,omitempty"`
	Rarity       string  `json:"rarity,omitempty"`
}

// rollItemKind picks the next spawner drop from exactly one IntN(100) draw so
// the RNG call order stays fixed for byte-stable replays. Weapon kinds are
// sub-ranges of the same roll, never extra draws: common guns 12%, shotgun and
// grenade 3% each, cannon 2%, and the 1% railgun jackpot robots race for.
func (a *Arena) rollItemKind() string {
	switch roll := a.rng.IntN(100); {
	case roll < 28:
		return "heal"
	case roll < 30:
		return "scope"
	case roll < 40:
		return "shield"
	case roll < 46:
		return "medkit"
	case roll < 52:
		return "nano_repair"
	case roll < 56:
		return "armor_plate"
	case roll < 59:
		return "battery"
	case roll < 63:
		return "overdrive"
	case roll < 66:
		return "rapid_fire"
	case roll < 68:
		return "scanner"
	case roll < 70:
		return "dash_cell"
	case roll < 72:
		return "cloak"
	case roll < 74:
		return "frenzy"
	case roll < 76:
		return "teleport_beacon"
	case roll < 77:
		return "berserker_charm"
	case roll < 78:
		return "vampiric_fang"
	case roll < 81:
		return "weapon_machine_gun"
	case roll < 84:
		return "weapon_incendiary"
	case roll < 87:
		return "weapon_cryo"
	case roll < 90:
		return "weapon_emp"
	case roll < 93:
		return "weapon_shotgun"
	case roll < 96:
		return "weapon_grenade"
	case roll < 97:
		return "weapon_mine_layer"
	case roll < 99:
		return "weapon_cannon"
	default:
		return "weapon_railgun"
	}
}

func (a *Arena) spawnInZone(zone SpawnZone) Item {
	x := zone.X + a.rng.Float64()*zone.Width
	y := zone.Y + a.rng.Float64()*zone.Height
	kind := a.rollItemKind()
	if !zone.Accepts(kind) {
		// Keep one IntN(100) draw per spawn for byte-stable replays; typed
		// zones fall back to their first listed kind.
		kind = zone.Types[0]
	}
	a.addItem(kind, x, y, "spawner")
	return a.Items[len(a.Items)-1]
}

// supplyDropOdds is one in N spawn events that also drop a bonus item.
const supplyDropOdds = 8

func (a *Arena) spawnItems() []Event {
	activeCount := len(a.activeSpawnItems())
	for index := range a.Items {
		item := &a.Items[index]
		if activeCount < a.Config.Items.MaxConcurrent && item.Source == "spawner" && !item.Active && item.RespawnTick > 0 && item.RespawnTick <= a.TickNumber {
			item.Active = true
			item.SpawnTick = a.TickNumber
			item.RespawnTick = 0
			return []Event{a.event(Event{Type: "item_respawned", ItemID: item.ItemID, Message: item.Type, X: item.X, Y: item.Y})}
		}
	}
	if len(a.Config.Map.ItemSpawnZones) == 0 || activeCount >= a.Config.Items.MaxConcurrent {
		return nil
	}
	delay := a.Config.Items.SpawnMinTicks
	if d := a.Config.Items.SpawnMaxTicks - a.Config.Items.SpawnMinTicks; d > 0 {
		delay += a.rng.IntN(d + 1)
	}
	if a.TickNumber-a.lastSpawnTick < delay {
		return nil
	}
	zone := a.Config.Map.ItemSpawnZones[a.rng.IntN(len(a.Config.Map.ItemSpawnZones))]
	item := a.spawnInZone(zone)
	a.lastSpawnTick = a.TickNumber
	events := []Event{a.event(Event{Type: "item_spawned", ItemID: item.ItemID, Message: item.Type, X: item.X, Y: item.Y})}
	// Supply drop bursts: sometimes the drop plane releases two crates.
	if activeCount+1 < a.Config.Items.MaxConcurrent && a.rng.IntN(supplyDropOdds) == 0 {
		bonus := a.spawnInZone(zone)
		events = append(events, a.event(Event{Type: "supply_drop", ItemID: bonus.ItemID, Message: bonus.Type, X: bonus.X, Y: bonus.Y}))
	}
	return events
}
func (a *Arena) activeSpawnItems() []Item {
	out := []Item{}
	for _, i := range a.Items {
		if i.Active && i.Source == "spawner" {
			out = append(out, i)
		}
	}
	return out
}
func (a *Arena) addItem(kind string, x, y float64, source string) {
	a.nextItem++
	x, y = a.safeDrop(x, y)
	a.Items = append(a.Items, Item{ItemID: "item-" + itoa(a.nextItem), Type: kind, X: x, Y: y, SpawnTick: a.TickNumber, Active: true, PickupRadius: a.Config.Items.PickupRadius, Source: source, Rarity: itemRarity(kind)})
	a.itemsOrdered = false
}
func (a *Arena) dropItem(kind string, x, y float64) { a.addItem(kind, x, y, "drop") }

// powerSurgeSurgeKinds is the fixed epic rotation; the surge kind is derived
// from the tick counter, never the RNG, so repeated matches stay identical.
var powerSurgeKinds = []string{"cloak", "berserker_charm", "vampiric_fang", "teleport_beacon", "frenzy", "weapon_grenade", "weapon_railgun"}

// powerSurge drops one epic item at the arena center on every Nth tick
// (Config.PowerSurgeEveryTicks). The kind rotates through a fixed list keyed
// by the surge counter, making the whole schedule deterministic.
func (a *Arena) powerSurge() []Event {
	every := a.Config.PowerSurgeEveryTicks
	if every <= 0 || a.TickNumber <= 0 || a.TickNumber%every != 0 {
		return nil
	}
	kind := powerSurgeKinds[(a.TickNumber/every)%len(powerSurgeKinds)]
	x, y := a.safeDrop(a.Config.Width/2, a.Config.Height/2)
	a.addItem(kind, x, y, "surge")
	item := a.Items[len(a.Items)-1]
	return []Event{a.event(Event{Type: "power_surge", ItemID: item.ItemID, Message: kind, X: item.X, Y: item.Y})}
}
func (a *Arena) safeDrop(x, y float64) (float64, float64) {
	x = clamp(x, RobotRadius, a.Config.Width-RobotRadius)
	y = clamp(y, RobotRadius, a.Config.Height-RobotRadius)
	if !collidesRobot(a.Config.Map.Obstacles, x, y) {
		return x, y
	}
	for radius := RobotRadius * 2; radius < 200; radius += RobotRadius {
		for n := 0; n < 16; n++ {
			angle := float64(n) * math.Pi / 8
			nx, ny := clamp(x+math.Cos(angle)*radius, RobotRadius, a.Config.Width-RobotRadius), clamp(y+math.Sin(angle)*radius, RobotRadius, a.Config.Height-RobotRadius)
			if !collidesRobot(a.Config.Map.Obstacles, nx, ny) {
				return nx, ny
			}
		}
	}
	return x, y
}
func (a *Arena) pickupItems() []Event {
	events := []Event{}
	a.ensureItemOrder()
	for i := range a.Items {
		item := &a.Items[i]
		if !item.Active {
			continue
		}
		for r := range a.Robots {
			robot := &a.Robots[r]
			if !robot.Alive || math.Hypot(robot.X-item.X, robot.Y-item.Y) > RobotRadius+item.PickupRadius || !acceptsItem(a.pickupPrefs[robot.RobotID], item.Type) {
				continue
			}
			if a.overtime() && (item.Type == "heal" || item.Type == "repair-core") {
				continue
			}
			value, extra := a.applyItemArena(robot, item.Type)
			if value < 0 {
				continue
			}
			item.Active = false
			if item.Source == "spawner" {
				delay := a.Config.Items.RespawnMinTicks
				if difference := a.Config.Items.RespawnMaxTicks - a.Config.Items.RespawnMinTicks; difference > 0 {
					delay += a.rng.IntN(difference + 1)
				}
				item.RespawnTick = a.TickNumber + delay
			}
			robot.ItemsPickedUp++
			events = append(events, a.event(Event{Type: "item_picked_up", RobotID: robot.RobotID, ItemID: item.ItemID, Value: value, Message: item.Type, X: item.X, Y: item.Y}))
			events = append(events, extra...)
			break
		}
	}
	return events
}

func (a *Arena) ensureItemOrder() {
	if a.itemsOrdered {
		return
	}
	sort.SliceStable(a.Items, func(i, j int) bool { return a.Items[i].ItemID < a.Items[j].ItemID })
	a.itemsOrdered = true
}
func acceptsItem(in Intent, kind string) bool {
	if in.AutoPickup != nil && !*in.AutoPickup {
		return false
	}
	if len(in.PickupTypes) == 0 {
		return true
	}
	for _, v := range in.PickupTypes {
		if v == kind {
			return true
		}
	}
	return false
}

// applyItemArena applies a pickup that may need arena state: teleport_beacon
// rolls a seeded open spot with the arena RNG, everything else delegates to
// the pure applyItem. A negative value skips the pickup and leaves the item
// on the floor; extra events are appended after item_picked_up.
func (a *Arena) applyItemArena(r *RobotState, kind string) (int, []Event) {
	if kind == "teleport_beacon" {
		x, y := a.safeDrop(a.rng.Float64()*a.Config.Width, a.rng.Float64()*a.Config.Height)
		r.X, r.Y = x, y
		return 1, []Event{a.event(Event{Type: "teleport", RobotID: r.RobotID, X: x, Y: y})}
	}
	return applyItem(r, kind), nil
}
func applyItem(r *RobotState, kind string) int {
	switch kind {
	case "heal":
		before := r.HP
		r.HP = min(r.MaxHP, r.HP+25)
		return r.HP - before
	case "repair-core":
		before := r.HP
		r.HP = min(r.MaxHP, r.HP+15)
		return r.HP - before
	case "medkit":
		if r.HP >= r.MaxHP {
			return -1
		}
		before := r.HP
		r.HP = min(r.MaxHP, r.HP+60)
		return r.HP - before
	case "nano_repair":
		upsertEffect(r, "regen", 150, 2, "")
		return 150
	case "armor_plate":
		upsertEffect(r, "armor", 150, 0.6, "")
		return 150
	case "battery":
		r.Cooldown = 0
		kept := r.Effects[:0]
		for _, effect := range r.Effects {
			if effect.Type != "emp" && effect.Type != "slow" && effect.Type != "burn" {
				kept = append(kept, effect)
			}
		}
		r.Effects = kept
		return 1
	case "cloak":
		upsertEffect(r, "cloak", 90, 1, "")
		return 90
	case "scanner":
		upsertEffect(r, "radar", 150, 1, "")
		return 150
	case "scope":
		upsertEffect(r, "optics", 150, 1, "")
		return 150
	case "berserker_charm":
		upsertEffect(r, "berserk", 80, 1, "")
		return 80
	case "vampiric_fang":
		upsertEffect(r, "vampiric", 100, 1, "")
		return 100
	case "dash_cell":
		if r.DashCharges >= 2 {
			return -1
		}
		r.DashCharges = min(2, r.DashCharges+1)
		return r.DashCharges
	case "frenzy":
		upsertEffect(r, "overdrive", 60, 1.5, "")
		upsertEffect(r, "rapid_fire", 60, .5, "")
		return 60
	case "shield":
		r.Shield = min(50, r.Shield+50)
		upsertEffect(r, "shield_decay", 200, 0.25, "")
		return 50
	case "overdrive":
		upsertEffect(r, "overdrive", 80, 1.5, "")
		return 80
	case "rapid_fire":
		upsertEffect(r, "rapid_fire", 60, .5, "")
		return 60
	}
	const prefix = "weapon_"
	if len(kind) > len(prefix) && kind[:len(prefix)] == prefix {
		r.Weapon = kind[len(prefix):]
		if r.Weapon == "mine_layer" {
			r.MineCharges = min(3, r.MineCharges+3)
		}
		return 1
	}
	return -1
}
func itemRarity(kind string) string {
	switch kind {
	case "shield", "overdrive", "rapid_fire", "medkit", "nano_repair", "armor_plate", "scanner", "dash_cell", "scope", "weapon_shotgun":
		return "rare"
	case "weapon_railgun", "weapon_grenade", "weapon_mine_layer", "cloak", "teleport_beacon", "berserker_charm", "vampiric_fang", "frenzy":
		return "epic"
	}
	return "common"
}

type Weapon struct {
	Name            string
	Damage          int
	Cooldown        int
	ProjectileSpeed float64
	Range           float64
	Spread          float64
	Knockback       float64
	Hitscan         bool
	BurnTicks       int
	SlowTicks       int
	EMPTicks        int
	// Pellets spawns that many projectiles per shot, each independently
	// jittered by Spread; zero means a single projectile.
	Pellets int
	// BlastRadius replaces direct hits with an explosion: BlastDamage at the
	// core, half (min 1) beyond half the radius, owner and FF-protected
	// teammates excluded.
	BlastRadius float64
	BlastDamage int
}

var Weapons = map[string]Weapon{"plasma": {Name: "plasma", Damage: 25, Cooldown: 8, ProjectileSpeed: 24, Range: 864}, "cannon": {Name: "cannon", Damage: 60, Cooldown: 20, ProjectileSpeed: 14, Range: 700, Knockback: 28}, "machine_gun": {Name: "machine_gun", Damage: 8, Cooldown: 2, ProjectileSpeed: 32, Range: 550, Spread: 3}, "railgun": {Name: "railgun", Damage: 45, Cooldown: 25, Range: 1000, Hitscan: true}, "incendiary": {Name: "incendiary", Damage: 15, Cooldown: 12, ProjectileSpeed: 20, Range: 600, BurnTicks: 50}, "cryo": {Name: "cryo", Damage: 12, Cooldown: 12, ProjectileSpeed: 20, Range: 600, SlowTicks: 50}, "emp": {Name: "emp", Damage: 5, Cooldown: 15, ProjectileSpeed: 18, Range: 500, EMPTicks: 30}, "shotgun": {Name: "shotgun", Damage: 10, Cooldown: 14, ProjectileSpeed: 30, Range: 400, Spread: 12, Pellets: 5}, "grenade": {Name: "grenade", Damage: 60, Cooldown: 25, ProjectileSpeed: 10, Range: 500, BlastRadius: 80, BlastDamage: 60}, "mine_layer": {Name: "mine_layer", Damage: 0, Cooldown: 20}}

func WeaponByName(name string) Weapon {
	if w, ok := Weapons[name]; ok {
		return w
	}
	return Weapons["plasma"]
}
func addWeaponEffects(r *RobotState, w Weapon, source string) {
	if w.BurnTicks > 0 {
		upsertEffect(r, "burn", w.BurnTicks, 2, source)
	}
	if w.SlowTicks > 0 {
		upsertEffect(r, "slow", w.SlowTicks, .5, source)
	}
	if w.EMPTicks > 0 {
		upsertEffect(r, "emp", w.EMPTicks, 1, source)
	}
}
func upsertEffect(r *RobotState, kind string, ticks int, magnitude float64, source string) {
	for i := range r.Effects {
		if r.Effects[i].Type == kind {
			if ticks > r.Effects[i].Ticks {
				r.Effects[i].Ticks = ticks
			}
			r.Effects[i].Magnitude = magnitude
			r.Effects[i].SourceID = source
			return
		}
	}
	r.Effects = append(r.Effects, StatusEffect{Type: kind, Ticks: ticks, Magnitude: magnitude, SourceID: source})
}
func effectActive(r RobotState, kind string) bool {
	for _, e := range r.Effects {
		if e.Type == kind && e.Ticks > 0 {
			return true
		}
	}
	return false
}
func effectMultiplier(r RobotState, kind string, fallback float64) float64 {
	for _, e := range r.Effects {
		if e.Type == kind && e.Ticks > 0 {
			return e.Magnitude
		}
	}
	return fallback
}

// isCloaked reports whether the robot currently benefits from an active cloak.
// Observation filtering for cloaked robots lives with the vision pass.
func isCloaked(r RobotState) bool { return effectActive(r, "cloak") }

// visionMult is the per-tick vision multiplier granted by perception effects:
// optics (the scope pickup) beats radar, otherwise the base range applies.
func visionMult(r RobotState) float64 {
	if effectActive(r, "optics") {
		return 2.0
	}
	if effectActive(r, "radar") {
		return 1.75
	}
	return 1
}

// breakCloak removes an active cloak early: firing a shot or taking any hit
// reveals the robot.
func breakCloak(r *RobotState) {
	for i, effect := range r.Effects {
		if effect.Type == "cloak" {
			r.Effects = append(r.Effects[:i], r.Effects[i+1:]...)
			return
		}
	}
}

type ZoneState struct {
	Active bool    `json:"active"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Radius float64 `json:"radius"`
	Damage int     `json:"damage"`
	// Stage is 0 before the zone starts, 1..StageCount while collapsing
	// (StageCount = final ring), so browsers can render the current phase.
	Stage int `json:"stage,omitempty"`
}

// zoneState builds the shared zone view for the current tick. With
// StageCount <= 1 it reproduces the legacy single linear shrink exactly;
// otherwise the window splits into equal stages whose first 85% shrinks and
// last 15% holds, so each stage damage bump lands on a visible pause.
func (a *Arena) zoneState() *ZoneState {
	z := a.Config.Zone
	if !z.Enabled {
		return nil
	}
	start, end := z.StartTick, z.EndTick
	if end <= start {
		end = a.MaxTicks
	}
	initial := math.Hypot(a.Config.Width, a.Config.Height) / 2
	radius := initial
	stage := 0
	if a.TickNumber >= start {
		stage = 1
		if z.StageCount <= 1 {
			progress := clamp(float64(a.TickNumber-start)/float64(max(1, end-start)), 0, 1)
			radius = initial + (z.EndRadius-initial)*progress
		} else {
			stage, radius = zoneStage(z, initial, start, end, a.TickNumber)
		}
	}
	damage := z.Damage
	if stage > 1 {
		// Later stages squeeze harder: +1 damage per stage past the first.
		damage = z.Damage + stage - 1
	}
	return &ZoneState{Active: a.TickNumber >= start, X: a.Config.Width / 2, Y: a.Config.Height / 2, Radius: radius, Damage: damage, Stage: stage}
}

// zoneStage computes the active stage and piecewise-linear radius for a
// multi-stage collapse. Stage n lerps from radiusAt(n-1) to radiusAt(n)
// across the shrink slice, then holds until the next stage begins; at the
// final stage's end the radius rests exactly on EndRadius.
func zoneStage(z ZoneConfig, initial float64, start, end, tick int) (int, float64) {
	stages := z.StageCount
	segment := float64(end-start) / float64(stages)
	shrink := segment * 0.85
	progressed := float64(tick - start)
	stage := int(progressed/segment) + 1
	if stage > stages {
		stage = stages
	}
	radiusAt := func(n int) float64 { return initial + (z.EndRadius-initial)*float64(n)/float64(stages) }
	local := progressed - float64(stage-1)*segment
	radius := radiusAt(stage)
	if local < shrink {
		previous := radiusAt(stage - 1)
		radius = previous + (radiusAt(stage)-previous)*(local/shrink)
	}
	return stage, radius
}
func (a *Arena) applyZone() []Event {
	z := a.zoneState()
	if z == nil || !z.Active || a.TickNumber%a.Config.Zone.DamageInterval != 0 {
		return nil
	}
	events := []Event{}
	for i := range a.Robots {
		r := &a.Robots[i]
		if r.Alive && math.Hypot(r.X-z.X, r.Y-z.Y) > z.Radius {
			events = append(events, a.damage(nil, r, z.Damage, Weapon{})...)
			events = append(events, a.event(Event{Type: "zone_damage", TargetID: r.RobotID, Damage: z.Damage}))
		}
	}
	return events
}

func (a *Arena) applyHazards() []Event {
	events := []Event{}
	for _, hazard := range a.Config.Map.Hazards {
		inside := func(r *RobotState) bool {
			return r.Alive && r.X >= hazard.X && r.X <= hazard.X+hazard.Width && r.Y >= hazard.Y && r.Y <= hazard.Y+hazard.Height
		}
		switch hazard.Type {
		case "slow-field":
			for i := range a.Robots {
				if r := &a.Robots[i]; inside(r) {
					upsertEffect(r, "slow", 60, 0.5, "")
				}
			}
		case "spike":
			if a.TickNumber%6 != 0 {
				continue
			}
			for i := range a.Robots {
				if r := &a.Robots[i]; inside(r) {
					events = append(events, a.damage(nil, r, hazard.Damage, Weapon{})...)
					events = append(events, a.event(Event{Type: "hazard_damage", TargetID: r.RobotID, Damage: hazard.Damage, Message: hazard.ID}))
				}
			}
		case "damage", "damage-edge":
			if hazard.Damage <= 0 || a.TickNumber%10 != 0 {
				continue
			}
			for i := range a.Robots {
				if r := &a.Robots[i]; inside(r) {
					events = append(events, a.damage(nil, r, hazard.Damage, Weapon{})...)
					events = append(events, a.event(Event{Type: "hazard_damage", TargetID: r.RobotID, Damage: hazard.Damage, Message: hazard.ID}))
				}
			}
		}
	}
	return events
}
func (a *Arena) resolveRamming(before []RobotState) []Event {
	events := []Event{}
	for i := 0; i < len(a.Robots); i++ {
		for j := i + 1; j < len(a.Robots); j++ {
			x, y := &a.Robots[i], &a.Robots[j]
			if !x.Alive || !y.Alive || x.Team == y.Team || math.Hypot(x.X-y.X, x.Y-y.Y) > RobotRadius*2+.1 {
				continue
			}
			key := x.RobotID + "/" + y.RobotID
			if a.TickNumber-a.pairRam[key] < 10 {
				continue
			}
			speed := math.Hypot((x.X-before[i].X)-(y.X-before[j].X), (x.Y-before[i].Y)-(y.Y-before[j].Y))
			damage := int(speed / 2)
			if damage < 1 {
				continue
			}
			a.pairRam[key] = a.TickNumber
			events = append(events, a.damage(x, y, damage, Weapon{})...)
			events = append(events, a.damage(y, x, damage, Weapon{})...)
		}
	}
	return events
}
func (a *Arena) firstTargetOnRay(shooter RobotState, angle, maxRange float64) (*RobotState, float64) {
	ex, ey := shooter.X+math.Cos(angle)*maxRange, shooter.Y+math.Sin(angle)*maxRange
	if segmentBlocked(a.Config.Map.Obstacles, shooter.X, shooter.Y, ex, ey) {
		maxRange = firstObstacleDistance(a.Config.Map.Obstacles, shooter.X, shooter.Y, angle, maxRange)
	}
	var best *RobotState
	distance := maxRange + 1
	for i := range a.Robots {
		r := &a.Robots[i]
		if !r.Alive || r.RobotID == shooter.RobotID || (!a.Config.FriendlyFire && r.Team == shooter.Team) {
			continue
		}
		along := (r.X-shooter.X)*math.Cos(angle) + (r.Y-shooter.Y)*math.Sin(angle)
		if along < 0 || along > maxRange {
			continue
		}
		if distancePointSegment(r.X, r.Y, shooter.X, shooter.Y, ex, ey) <= RobotRadius && along < distance {
			best, distance = r, along
		}
	}
	return best, distance
}
func firstObstacleDistance(obs []Obstacle, x, y, angle, maxRange float64) float64 {
	step := RobotRadius / 2
	for d := step; d <= maxRange; d += step {
		px, py := x+math.Cos(angle)*d, y+math.Sin(angle)*d
		if collidesRobot(obs, px, py) {
			return d
		}
	}
	return maxRange
}
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	b := [20]byte{}
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
