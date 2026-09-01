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
	Enabled        bool    `json:"enabled"`
	StartTick      int     `json:"startTick"`
	EndTick        int     `json:"endTick"`
	EndRadius      float64 `json:"endRadius"`
	Damage         int     `json:"damage"`
	DamageInterval int     `json:"damageInterval"`
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
}

func DefaultConfig() Config {
	return Config{Width: ArenaWidth, Height: ArenaHeight, Map: DefaultMap(ArenaWidth, ArenaHeight), MaxTicks: DefaultMaxTicks, Items: ItemConfig{SpawnMinTicks: 100, SpawnMaxTicks: 200, RespawnMinTicks: 100, RespawnMaxTicks: 150, MaxConcurrent: 3, PickupRadius: 20}, DropOnDeath: true, DropWeapons: true, Zone: ZoneConfig{StartTick: 1200, EndTick: 1800, EndRadius: 80, Damage: 2, DamageInterval: 10}, OvertimeTicks: 100}
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
	if c.RegenDelayTicks <= 0 {
		c.RegenDelayTicks = 50
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
	kind := "heal"
	roll := a.rng.IntN(100)
	if roll >= 75 && roll < 88 {
		kind = "shield"
	} else if roll >= 88 && roll < 95 {
		kind = "overdrive"
	} else if roll >= 95 {
		kind = "rapid_fire"
	}
	x := zone.X + a.rng.Float64()*zone.Width
	y := zone.Y + a.rng.Float64()*zone.Height
	a.addItem(kind, x, y, "spawner")
	a.lastSpawnTick = a.TickNumber
	item := a.Items[len(a.Items)-1]
	return []Event{a.event(Event{Type: "item_spawned", ItemID: item.ItemID, Message: item.Type, X: item.X, Y: item.Y})}
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
}
func (a *Arena) dropItem(kind string, x, y float64) { a.addItem(kind, x, y, "drop") }
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
	sort.SliceStable(a.Items, func(i, j int) bool { return a.Items[i].ItemID < a.Items[j].ItemID })
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
			value := applyItem(robot, item.Type)
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
			break
		}
	}
	return events
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
		return 1
	}
	return -1
}
func itemRarity(kind string) string {
	switch kind {
	case "shield", "overdrive", "rapid_fire":
		return "rare"
	case "weapon_railgun":
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
}

var Weapons = map[string]Weapon{"plasma": {Name: "plasma", Damage: 25, Cooldown: 8, ProjectileSpeed: 24, Range: 864}, "cannon": {Name: "cannon", Damage: 60, Cooldown: 20, ProjectileSpeed: 14, Range: 700, Knockback: 28}, "machine_gun": {Name: "machine_gun", Damage: 8, Cooldown: 2, ProjectileSpeed: 32, Range: 550, Spread: 3}, "railgun": {Name: "railgun", Damage: 45, Cooldown: 25, Range: 1000, Hitscan: true}, "incendiary": {Name: "incendiary", Damage: 15, Cooldown: 12, ProjectileSpeed: 20, Range: 600, BurnTicks: 50}, "cryo": {Name: "cryo", Damage: 12, Cooldown: 12, ProjectileSpeed: 20, Range: 600, SlowTicks: 50}, "emp": {Name: "emp", Damage: 5, Cooldown: 15, ProjectileSpeed: 18, Range: 500, EMPTicks: 30}}

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

type ZoneState struct {
	Active bool    `json:"active"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Radius float64 `json:"radius"`
	Damage int     `json:"damage"`
}

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
	active := a.TickNumber >= start
	if active {
		progress := clamp(float64(a.TickNumber-start)/float64(max(1, end-start)), 0, 1)
		radius = initial + (z.EndRadius-initial)*progress
	}
	return &ZoneState{Active: active, X: a.Config.Width / 2, Y: a.Config.Height / 2, Radius: radius, Damage: z.Damage}
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
		if (hazard.Type != "damage" && hazard.Type != "damage-edge") || hazard.Damage <= 0 || a.TickNumber%10 != 0 {
			continue
		}
		for i := range a.Robots {
			r := &a.Robots[i]
			if r.Alive && r.X >= hazard.X && r.X <= hazard.X+hazard.Width && r.Y >= hazard.Y && r.Y <= hazard.Y+hazard.Height {
				events = append(events, a.damage(nil, r, hazard.Damage, Weapon{})...)
				events = append(events, a.event(Event{Type: "hazard_damage", TargetID: r.RobotID, Damage: hazard.Damage, Message: hazard.ID}))
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
