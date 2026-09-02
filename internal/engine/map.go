package engine

import "math"

type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}
type Obstacle struct {
	ID     string  `json:"id"`
	Shape  string  `json:"shape"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width,omitempty"`
	Height float64 `json:"height,omitempty"`
	Radius float64 `json:"radius,omitempty"`
}
type SpawnZone struct {
	X      float64  `json:"x"`
	Y      float64  `json:"y"`
	Width  float64  `json:"width"`
	Height float64  `json:"height"`
	Types  []string `json:"types,omitempty"`
}
type Hazard struct {
	ID     string  `json:"id"`
	Type   string  `json:"type"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	Damage int     `json:"damage,omitempty"`
}

// TurretSpec defines a neutral auto-turret baked into a handcrafted map.
// Turrets attack every robot regardless of team and can be destroyed for a
// shield drop; procedural maps carry none this pass.
type TurretSpec struct {
	TurretID      string  `json:"turretId"`
	X             float64 `json:"x"`
	Y             float64 `json:"y"`
	HP            int     `json:"hp"`
	Range         float64 `json:"range"`
	Damage        int     `json:"damage"`
	CooldownTicks int     `json:"cooldownTicks"`
}
type MapDefinition struct {
	ID             string             `json:"id"`
	Name           string             `json:"name"`
	Width          float64            `json:"width"`
	Height         float64            `json:"height"`
	Obstacles      []Obstacle         `json:"obstacles,omitempty"`
	SpawnPoints    map[string][]Point `json:"spawnPoints,omitempty"`
	ItemSpawnZones []SpawnZone        `json:"itemSpawnZones,omitempty"`
	Hazards        []Hazard           `json:"hazards,omitempty"`
	Turrets        []TurretSpec       `json:"turrets,omitempty"`
}

// Accepts reports whether an item kind may spawn inside the zone. Zones
// without a Types filter accept every kind; otherwise the kind must be listed.
func (z SpawnZone) Accepts(kind string) bool {
	if len(z.Types) == 0 {
		return true
	}
	for _, kind2 := range z.Types {
		if kind2 == kind {
			return true
		}
	}
	return false
}

func DefaultMap(w, h float64) MapDefinition {
	return MapDefinition{ID: "open-field", Name: "Open Field", Width: w, Height: h, SpawnPoints: map[string][]Point{"red": {{100, h / 2}}, "blue": {{w - 100, h / 2}}}, ItemSpawnZones: []SpawnZone{{X: w * .3, Y: h * .25, Width: w * .4, Height: h * .5}}}
}

func ScaleMap(source MapDefinition, width, height float64) MapDefinition {
	if source.Width <= 0 || source.Height <= 0 || width <= 0 || height <= 0 {
		return source
	}
	scaled := source
	scaleX, scaleY := width/source.Width, height/source.Height
	radiusScale := math.Min(scaleX, scaleY)
	scaled.Width, scaled.Height = width, height
	scaled.Obstacles = append([]Obstacle(nil), source.Obstacles...)
	for index := range scaled.Obstacles {
		obstacle := &scaled.Obstacles[index]
		obstacle.X, obstacle.Y = obstacle.X*scaleX, obstacle.Y*scaleY
		obstacle.Width, obstacle.Height = obstacle.Width*scaleX, obstacle.Height*scaleY
		obstacle.Radius *= radiusScale
	}
	scaled.SpawnPoints = make(map[string][]Point, len(source.SpawnPoints))
	for team, points := range source.SpawnPoints {
		scaled.SpawnPoints[team] = append([]Point(nil), points...)
		for index := range scaled.SpawnPoints[team] {
			scaled.SpawnPoints[team][index].X *= scaleX
			scaled.SpawnPoints[team][index].Y *= scaleY
		}
	}
	scaled.ItemSpawnZones = append([]SpawnZone(nil), source.ItemSpawnZones...)
	for index := range scaled.ItemSpawnZones {
		zone := &scaled.ItemSpawnZones[index]
		zone.X, zone.Y = zone.X*scaleX, zone.Y*scaleY
		zone.Width, zone.Height = zone.Width*scaleX, zone.Height*scaleY
		zone.Types = append([]string(nil), zone.Types...)
	}
	scaled.Hazards = append([]Hazard(nil), source.Hazards...)
	for index := range scaled.Hazards {
		hazard := &scaled.Hazards[index]
		hazard.X, hazard.Y = hazard.X*scaleX, hazard.Y*scaleY
		hazard.Width, hazard.Height = hazard.Width*scaleX, hazard.Height*scaleY
	}
	scaled.Turrets = append([]TurretSpec(nil), source.Turrets...)
	for index := range scaled.Turrets {
		turret := &scaled.Turrets[index]
		turret.X, turret.Y = turret.X*scaleX, turret.Y*scaleY
		turret.Range *= radiusScale
	}
	return scaled
}
func StarterMaps() map[string]MapDefinition {
	return map[string]MapDefinition{
		// Open field stays relatively open: scattered cover walls and rocks
		// give dash lanes without blocking the classic center rush.
		"open-field": {ID: "open-field", Name: "Open Field", Width: 1200, Height: 750, Obstacles: []Obstacle{
			{ID: "cover-nw", Shape: "aabb", X: 300, Y: 180, Width: 120, Height: 24}, {ID: "cover-ne", Shape: "aabb", X: 780, Y: 180, Width: 120, Height: 24},
			{ID: "cover-sw", Shape: "aabb", X: 300, Y: 546, Width: 120, Height: 24}, {ID: "cover-se", Shape: "aabb", X: 780, Y: 546, Width: 120, Height: 24},
			{ID: "rock-nw", Shape: "circle", X: 480, Y: 300, Radius: 30}, {ID: "rock-ne", Shape: "circle", X: 720, Y: 300, Radius: 30},
			{ID: "rock-sw", Shape: "circle", X: 480, Y: 450, Radius: 30}, {ID: "rock-se", Shape: "circle", X: 720, Y: 450, Radius: 30},
		}, SpawnPoints: map[string][]Point{"red": {{100, 375}}, "blue": {{1100, 375}}}, ItemSpawnZones: []SpawnZone{{X: 360, Y: 187.5, Width: 480, Height: 375}}},
		// Corner bunkers now connect through broken walls with gaps to slip
		// through instead of one lonely center block.
		"four-corners": {ID: "four-corners", Name: "Four Corners", Width: 1200, Height: 750, Obstacles: []Obstacle{
			{ID: "center", Shape: "aabb", X: 550, Y: 325, Width: 100, Height: 100},
			{ID: "wall-top-l", Shape: "aabb", X: 400, Y: 60, Width: 170, Height: 24}, {ID: "wall-top-r", Shape: "aabb", X: 630, Y: 60, Width: 170, Height: 24},
			{ID: "wall-bottom-l", Shape: "aabb", X: 400, Y: 666, Width: 170, Height: 24}, {ID: "wall-bottom-r", Shape: "aabb", X: 630, Y: 666, Width: 170, Height: 24},
			{ID: "wall-left", Shape: "aabb", X: 60, Y: 300, Width: 24, Height: 150}, {ID: "wall-right", Shape: "aabb", X: 1116, Y: 300, Width: 24, Height: 150},
			{ID: "crate-tl", Shape: "aabb", X: 180, Y: 180, Width: 40, Height: 40}, {ID: "crate-tr", Shape: "aabb", X: 980, Y: 180, Width: 40, Height: 40},
			{ID: "crate-bl", Shape: "aabb", X: 180, Y: 530, Width: 40, Height: 40}, {ID: "crate-br", Shape: "aabb", X: 980, Y: 530, Width: 40, Height: 40},
		}, SpawnPoints: map[string][]Point{"red": {{100, 100}, {100, 650}}, "blue": {{1100, 100}, {1100, 650}}}, ItemSpawnZones: []SpawnZone{{X: 350, Y: 187, Width: 500, Height: 375}}},
		// The two long corridors gain cross walls that form four chokepoints
		// plus a mid-block and side crates for junction cover.
		"corridors": {ID: "corridors", Name: "The Corridors", Width: 1200, Height: 750, Obstacles: []Obstacle{
			{ID: "top", Shape: "aabb", X: 300, Y: 200, Width: 600, Height: 30}, {ID: "bottom", Shape: "aabb", X: 300, Y: 520, Width: 600, Height: 30},
			{ID: "cross-nw", Shape: "aabb", X: 480, Y: 90, Width: 24, Height: 110}, {ID: "cross-ne", Shape: "aabb", X: 696, Y: 90, Width: 24, Height: 110},
			{ID: "cross-sw", Shape: "aabb", X: 480, Y: 550, Width: 24, Height: 110}, {ID: "cross-se", Shape: "aabb", X: 696, Y: 550, Width: 24, Height: 110},
			{ID: "mid-block", Shape: "aabb", X: 560, Y: 355, Width: 80, Height: 40},
			{ID: "crate-w", Shape: "aabb", X: 200, Y: 350, Width: 40, Height: 50}, {ID: "crate-e", Shape: "aabb", X: 960, Y: 350, Width: 40, Height: 50},
		}, SpawnPoints: map[string][]Point{"red": {{100, 130}, {100, 375}, {100, 620}}, "blue": {{1100, 130}, {1100, 375}, {1100, 620}}}, ItemSpawnZones: []SpawnZone{{X: 350, Y: 260, Width: 500, Height: 230}}},
		// Pillar clusters get linking walls with wide gaps plus corner crates
		// and a center rock for cover between the columns.
		"pillars": {ID: "pillars", Name: "Pillars", Width: 1200, Height: 750, Obstacles: []Obstacle{
			{ID: "p1", Shape: "circle", X: 420, Y: 240, Radius: 40}, {ID: "p2", Shape: "circle", X: 780, Y: 240, Radius: 40},
			{ID: "p3", Shape: "circle", X: 420, Y: 510, Radius: 40}, {ID: "p4", Shape: "circle", X: 780, Y: 510, Radius: 40},
			{ID: "link-top", Shape: "aabb", X: 520, Y: 228, Width: 160, Height: 24}, {ID: "link-bottom", Shape: "aabb", X: 520, Y: 498, Width: 160, Height: 24},
			{ID: "crate-nw", Shape: "aabb", X: 250, Y: 120, Width: 40, Height: 40}, {ID: "crate-ne", Shape: "aabb", X: 910, Y: 120, Width: 40, Height: 40},
			{ID: "crate-sw", Shape: "aabb", X: 250, Y: 590, Width: 40, Height: 40}, {ID: "crate-se", Shape: "aabb", X: 910, Y: 590, Width: 40, Height: 40},
			{ID: "center-rock", Shape: "circle", X: 600, Y: 375, Radius: 30},
		}, SpawnPoints: map[string][]Point{"red": {{120, 255}, {120, 495}}, "blue": {{1080, 255}, {1080, 495}}}, ItemSpawnZones: []SpawnZone{{X: 480, Y: 300, Width: 240, Height: 150}}},
		// Crater rims now hide inner rocks: the bowl fights in stages instead
		// of one open charge across the rim line.
		"crater": {ID: "crater", Name: "Crater", Width: 1200, Height: 800, Obstacles: []Obstacle{
			{ID: "rim-top", Shape: "aabb", X: 350, Y: 215, Width: 500, Height: 28}, {ID: "rim-bottom", Shape: "aabb", X: 350, Y: 557, Width: 500, Height: 28},
			{ID: "rock-inner-nw", Shape: "circle", X: 520, Y: 330, Radius: 26}, {ID: "rock-inner-ne", Shape: "circle", X: 680, Y: 330, Radius: 26},
			{ID: "rock-inner-sw", Shape: "circle", X: 520, Y: 470, Radius: 26}, {ID: "rock-inner-se", Shape: "circle", X: 680, Y: 470, Radius: 26},
			{ID: "rock-center", Shape: "circle", X: 600, Y: 400, Radius: 32},
		}, Hazards: []Hazard{{ID: "edge", Type: "damage-edge", X: 0, Y: 0, Width: 1200, Height: 800, Damage: 2}}, SpawnPoints: map[string][]Point{"red": {{120, 400}}, "blue": {{1080, 400}}}, ItemSpawnZones: []SpawnZone{{X: 450, Y: 300, Width: 300, Height: 200}}},
		// Bunker line gains layered side walls (west/east flank lanes), choke
		// crates squeezing both center gaps, and symmetric flank routes.
		"bunker-line": {ID: "bunker-line", Name: "Bunker Line", Width: 1200, Height: 800, Obstacles: []Obstacle{
			{ID: "line-top-l", Shape: "aabb", X: 265, Y: 225, Width: 265, Height: 26}, {ID: "line-top-r", Shape: "aabb", X: 670, Y: 225, Width: 265, Height: 26},
			{ID: "line-mid-l", Shape: "aabb", X: 200, Y: 385, Width: 240, Height: 26}, {ID: "line-mid-r", Shape: "aabb", X: 760, Y: 385, Width: 240, Height: 26},
			{ID: "line-bottom-l", Shape: "aabb", X: 265, Y: 549, Width: 265, Height: 26}, {ID: "line-bottom-r", Shape: "aabb", X: 670, Y: 549, Width: 265, Height: 26},
			{ID: "crate-top-l", Shape: "aabb", X: 520, Y: 80, Width: 54, Height: 54}, {ID: "crate-top-r", Shape: "aabb", X: 626, Y: 80, Width: 54, Height: 54},
			{ID: "crate-bottom-l", Shape: "aabb", X: 520, Y: 666, Width: 54, Height: 54}, {ID: "crate-bottom-r", Shape: "aabb", X: 626, Y: 666, Width: 54, Height: 54},
			{ID: "choke-nw", Shape: "aabb", X: 500, Y: 160, Width: 70, Height: 26}, {ID: "choke-ne", Shape: "aabb", X: 630, Y: 160, Width: 70, Height: 26},
			{ID: "choke-sw", Shape: "aabb", X: 500, Y: 614, Width: 70, Height: 26}, {ID: "choke-se", Shape: "aabb", X: 630, Y: 614, Width: 70, Height: 26},
			{ID: "flank-wall-w", Shape: "aabb", X: 220, Y: 300, Width: 24, Height: 200}, {ID: "flank-wall-e", Shape: "aabb", X: 956, Y: 300, Width: 24, Height: 200},
		}, Hazards: []Hazard{{ID: "hazard-slow-1", Type: "slow-field", X: 550, Y: 320, Width: 100, Height: 160, Damage: 0}}, SpawnPoints: map[string][]Point{"red": {{120, 240}, {120, 560}}, "blue": {{1080, 240}, {1080, 560}}}, ItemSpawnZones: []SpawnZone{
			{X: 550, Y: 240, Width: 100, Height: 320, Types: []string{"heal", "battery", "overdrive", "rapid_fire", "dash_cell"}},
			{X: 160, Y: 334, Width: 160, Height: 132, Types: []string{"medkit", "nano_repair", "armor_plate", "shield"}},
			{X: 880, Y: 334, Width: 160, Height: 132, Types: []string{"medkit", "nano_repair", "armor_plate", "shield"}},
		}, Turrets: []TurretSpec{
			// Both guard the open center corridor between the bunker wall halves.
			{TurretID: "turret-north", X: 600, Y: 190, HP: 60, Range: 220, Damage: 8, CooldownTicks: 12},
			{TurretID: "turret-south", X: 600, Y: 610, HP: 60, Range: 220, Damage: 8, CooldownTicks: 12},
		}},
		// Crossing fire layers side flank walls behind the crates and a second
		// turret-backed chokepoint at the north/south heals.
		"crossing-fire": {ID: "crossing-fire", Name: "Crossing Fire", Width: 1200, Height: 750, Obstacles: []Obstacle{
			{ID: "cross-top-l", Shape: "aabb", X: 515, Y: 100, Width: 26, Height: 190}, {ID: "cross-top-r", Shape: "aabb", X: 659, Y: 100, Width: 26, Height: 190},
			{ID: "cross-bottom-l", Shape: "aabb", X: 515, Y: 460, Width: 26, Height: 190}, {ID: "cross-bottom-r", Shape: "aabb", X: 659, Y: 460, Width: 26, Height: 190},
			{ID: "cross-left", Shape: "aabb", X: 335, Y: 360, Width: 180, Height: 26}, {ID: "cross-right", Shape: "aabb", X: 685, Y: 360, Width: 180, Height: 26},
			{ID: "crate-tl", Shape: "aabb", X: 240, Y: 150, Width: 60, Height: 60}, {ID: "crate-tr", Shape: "aabb", X: 900, Y: 150, Width: 60, Height: 60},
			{ID: "crate-bl", Shape: "aabb", X: 240, Y: 540, Width: 60, Height: 60}, {ID: "crate-br", Shape: "aabb", X: 900, Y: 540, Width: 60, Height: 60},
			{ID: "pillar-tl", Shape: "circle", X: 420, Y: 190, Radius: 30}, {ID: "pillar-tr", Shape: "circle", X: 780, Y: 190, Radius: 30},
			{ID: "pillar-bl", Shape: "circle", X: 420, Y: 560, Radius: 30}, {ID: "pillar-br", Shape: "circle", X: 780, Y: 560, Radius: 30},
			{ID: "flank-nw", Shape: "aabb", X: 250, Y: 200, Width: 24, Height: 120}, {ID: "flank-ne", Shape: "aabb", X: 926, Y: 200, Width: 24, Height: 120},
			{ID: "flank-sw", Shape: "aabb", X: 250, Y: 430, Width: 24, Height: 120}, {ID: "flank-se", Shape: "aabb", X: 926, Y: 430, Width: 24, Height: 120},
			{ID: "choke-nw", Shape: "aabb", X: 520, Y: 50, Width: 60, Height: 24}, {ID: "choke-ne", Shape: "aabb", X: 620, Y: 50, Width: 60, Height: 24},
			{ID: "choke-sw", Shape: "aabb", X: 520, Y: 676, Width: 60, Height: 24}, {ID: "choke-se", Shape: "aabb", X: 620, Y: 676, Width: 60, Height: 24},
		}, Hazards: []Hazard{
			{ID: "hazard-spike-1", Type: "spike", X: 537, Y: 362, Width: 126, Height: 26, Damage: 8},
			{ID: "hazard-spike-2", Type: "spike", X: 170, Y: 363, Width: 70, Height: 24, Damage: 8},
			{ID: "hazard-spike-3", Type: "spike", X: 960, Y: 363, Width: 70, Height: 24, Damage: 8},
		}, SpawnPoints: map[string][]Point{"red": {{110, 190}, {110, 560}}, "blue": {{1090, 190}, {1090, 560}}}, ItemSpawnZones: []SpawnZone{
			{X: 535, Y: 290, Width: 130, Height: 170, Types: []string{"cloak", "berserker_charm", "vampiric_fang", "teleport_beacon", "frenzy", "weapon_grenade", "weapon_railgun"}},
			{X: 540, Y: 90, Width: 120, Height: 160, Types: []string{"heal", "battery", "overdrive", "rapid_fire", "dash_cell"}},
			{X: 540, Y: 500, Width: 120, Height: 160, Types: []string{"heal", "battery", "overdrive", "rapid_fire", "dash_cell"}},
			{X: 220, Y: 302, Width: 130, Height: 146, Types: []string{"medkit", "nano_repair", "armor_plate", "shield"}},
			{X: 850, Y: 302, Width: 130, Height: 146, Types: []string{"medkit", "nano_repair", "armor_plate", "shield"}},
		}, Turrets: []TurretSpec{
			// Overlook the narrow center gap between the crossing walls.
			{TurretID: "turret-north", X: 600, Y: 60, HP: 60, Range: 220, Damage: 8, CooldownTicks: 12},
			{TurretID: "turret-south", X: 600, Y: 690, HP: 60, Range: 220, Damage: 8, CooldownTicks: 12},
		}},
		// The vault keeps its armored box and gains outer flank walls, a second
		// chokepoint squeezing the west/east vault mouths, and taller crates.
		"vault": {ID: "vault", Name: "The Vault", Width: 1200, Height: 800, Obstacles: []Obstacle{
			{ID: "vault-top-l", Shape: "aabb", X: 495, Y: 295, Width: 72, Height: 24}, {ID: "vault-top-r", Shape: "aabb", X: 633, Y: 295, Width: 72, Height: 24},
			{ID: "vault-bottom-l", Shape: "aabb", X: 495, Y: 481, Width: 72, Height: 24}, {ID: "vault-bottom-r", Shape: "aabb", X: 633, Y: 481, Width: 72, Height: 24},
			{ID: "vault-left", Shape: "aabb", X: 495, Y: 295, Width: 24, Height: 210}, {ID: "vault-right", Shape: "aabb", X: 681, Y: 295, Width: 24, Height: 210},
			{ID: "crate-tl", Shape: "aabb", X: 330, Y: 160, Width: 80, Height: 54}, {ID: "crate-tr", Shape: "aabb", X: 790, Y: 160, Width: 80, Height: 54},
			{ID: "crate-bl", Shape: "aabb", X: 330, Y: 586, Width: 80, Height: 54}, {ID: "crate-br", Shape: "aabb", X: 790, Y: 586, Width: 80, Height: 54},
			{ID: "crate-top-l", Shape: "aabb", X: 465, Y: 105, Width: 54, Height: 54}, {ID: "crate-top-r", Shape: "aabb", X: 681, Y: 105, Width: 54, Height: 54},
			{ID: "crate-bottom-l", Shape: "aabb", X: 465, Y: 641, Width: 54, Height: 54}, {ID: "crate-bottom-r", Shape: "aabb", X: 681, Y: 641, Width: 54, Height: 54},
			{ID: "pillar-l", Shape: "circle", X: 240, Y: 400, Radius: 38}, {ID: "pillar-r", Shape: "circle", X: 960, Y: 400, Radius: 38},
			{ID: "flank-nw", Shape: "aabb", X: 300, Y: 260, Width: 24, Height: 110}, {ID: "flank-ne", Shape: "aabb", X: 876, Y: 260, Width: 24, Height: 110},
			{ID: "flank-sw", Shape: "aabb", X: 300, Y: 430, Width: 24, Height: 110}, {ID: "flank-se", Shape: "aabb", X: 876, Y: 430, Width: 24, Height: 110},
			{ID: "choke-west", Shape: "aabb", X: 430, Y: 388, Width: 40, Height: 24}, {ID: "choke-east", Shape: "aabb", X: 730, Y: 388, Width: 40, Height: 24},
		}, Hazards: []Hazard{
			{ID: "hazard-slow-1", Type: "slow-field", X: 547, Y: 160, Width: 106, Height: 106, Damage: 0},
			{ID: "hazard-spike-1", Type: "spike", X: 320, Y: 387, Width: 80, Height: 26, Damage: 8},
			{ID: "hazard-spike-2", Type: "spike", X: 800, Y: 387, Width: 80, Height: 26, Damage: 8},
		}, SpawnPoints: map[string][]Point{"red": {{120, 265}, {120, 535}}, "blue": {{1080, 265}, {1080, 535}}}, ItemSpawnZones: []SpawnZone{
			{X: 550, Y: 350, Width: 100, Height: 100, Types: []string{"cloak", "berserker_charm", "vampiric_fang", "teleport_beacon", "frenzy", "weapon_grenade", "weapon_railgun"}},
			{X: 200, Y: 187, Width: 147, Height: 120, Types: []string{"medkit", "nano_repair", "armor_plate", "shield"}},
			{X: 853, Y: 187, Width: 147, Height: 120, Types: []string{"medkit", "nano_repair", "armor_plate", "shield"}},
		}, Turrets: []TurretSpec{
			// Cover the vault entrances through the gaps in its walls.
			{TurretID: "turret-north", X: 600, Y: 200, HP: 60, Range: 220, Damage: 8, CooldownTicks: 12},
			{TurretID: "turret-south", X: 600, Y: 600, HP: 60, Range: 220, Damage: 8, CooldownTicks: 12},
		}},
	}
}

func LineOfSight(obstacles []Obstacle, x1, y1, x2, y2 float64) bool {
	return !segmentBlocked(obstacles, x1, y1, x2, y2)
}
func segmentBlocked(obstacles []Obstacle, x1, y1, x2, y2 float64) bool {
	for _, o := range obstacles {
		if o.Shape == "circle" {
			if distancePointSegment(o.X, o.Y, x1, y1, x2, y2) <= o.Radius {
				return true
			}
		} else if segmentAABB(x1, y1, x2, y2, o.X, o.Y, o.X+o.Width, o.Y+o.Height) {
			return true
		}
	}
	return false
}
func collidesRobot(obstacles []Obstacle, x, y float64) bool {
	for _, o := range obstacles {
		if o.Shape == "circle" {
			if math.Hypot(x-o.X, y-o.Y) < RobotRadius+o.Radius {
				return true
			}
		} else if x+RobotRadius > o.X && x-RobotRadius < o.X+o.Width && y+RobotRadius > o.Y && y-RobotRadius < o.Y+o.Height {
			return true
		}
	}
	return false
}
func segmentAABB(x1, y1, x2, y2, minX, minY, maxX, maxY float64) bool {
	dx, dy := x2-x1, y2-y1
	t0, t1 := 0.0, 1.0
	for _, v := range [][2]float64{{-dx, x1 - minX}, {dx, maxX - x1}, {-dy, y1 - minY}, {dy, maxY - y1}} {
		p, q := v[0], v[1]
		if p == 0 {
			if q < 0 {
				return false
			}
			continue
		}
		r := q / p
		if p < 0 {
			if r > t1 {
				return false
			}
			if r > t0 {
				t0 = r
			}
		} else {
			if r < t0 {
				return false
			}
			if r < t1 {
				t1 = r
			}
		}
	}
	return true
}
func distancePointSegment(px, py, x1, y1, x2, y2 float64) float64 {
	dx, dy := x2-x1, y2-y1
	if dx == 0 && dy == 0 {
		return math.Hypot(px-x1, py-y1)
	}
	t := clamp(((px-x1)*dx+(py-y1)*dy)/(dx*dx+dy*dy), 0, 1)
	return math.Hypot(px-(x1+t*dx), py-(y1+t*dy))
}
func resolveOverlaps(rs []RobotState, w, h float64, obstacles []Obstacle) {
	for i := 0; i < len(rs); i++ {
		if !rs[i].Alive {
			continue
		}
		for j := i + 1; j < len(rs); j++ {
			if !rs[j].Alive {
				continue
			}
			dx, dy := rs[j].X-rs[i].X, rs[j].Y-rs[i].Y
			d := math.Hypot(dx, dy)
			if d >= RobotRadius*2 {
				continue
			}
			if d == 0 {
				dx, d = 1, 1
			}
			push := (RobotRadius*2 - d) / 2
			nx, ny := dx/d, dy/d
			ax, ay := clamp(rs[i].X-nx*push, RobotRadius, w-RobotRadius), clamp(rs[i].Y-ny*push, RobotRadius, h-RobotRadius)
			bx, by := clamp(rs[j].X+nx*push, RobotRadius, w-RobotRadius), clamp(rs[j].Y+ny*push, RobotRadius, h-RobotRadius)
			if !collidesRobot(obstacles, ax, ay) {
				rs[i].X, rs[i].Y = ax, ay
			}
			if !collidesRobot(obstacles, bx, by) {
				rs[j].X, rs[j].Y = bx, by
			}
		}
	}
}
