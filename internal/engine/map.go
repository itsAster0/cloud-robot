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
type MapDefinition struct {
	ID             string             `json:"id"`
	Name           string             `json:"name"`
	Width          float64            `json:"width"`
	Height         float64            `json:"height"`
	Obstacles      []Obstacle         `json:"obstacles,omitempty"`
	SpawnPoints    map[string][]Point `json:"spawnPoints,omitempty"`
	ItemSpawnZones []SpawnZone        `json:"itemSpawnZones,omitempty"`
	Hazards        []Hazard           `json:"hazards,omitempty"`
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
	return scaled
}
func StarterMaps() map[string]MapDefinition {
	return map[string]MapDefinition{
		"open-field":   DefaultMap(800, 500),
		"four-corners": {ID: "four-corners", Name: "Four Corners", Width: 800, Height: 500, Obstacles: []Obstacle{{ID: "center", Shape: "aabb", X: 350, Y: 200, Width: 100, Height: 100}}, SpawnPoints: map[string][]Point{"red": {{80, 80}, {80, 420}}, "blue": {{720, 80}, {720, 420}}}, ItemSpawnZones: []SpawnZone{{X: 250, Y: 125, Width: 300, Height: 250}}},
		"corridors":    {ID: "corridors", Name: "The Corridors", Width: 1000, Height: 600, Obstacles: []Obstacle{{ID: "top", Shape: "aabb", X: 250, Y: 160, Width: 500, Height: 30}, {ID: "bottom", Shape: "aabb", X: 250, Y: 410, Width: 500, Height: 30}}, SpawnPoints: map[string][]Point{"red": {{80, 130}, {80, 300}, {80, 470}}, "blue": {{920, 130}, {920, 300}, {920, 470}}}, ItemSpawnZones: []SpawnZone{{X: 300, Y: 210, Width: 400, Height: 180}}},
		"pillars":      {ID: "pillars", Name: "Pillars", Width: 800, Height: 500, Obstacles: []Obstacle{{ID: "p1", Shape: "circle", X: 300, Y: 170, Radius: 35}, {ID: "p2", Shape: "circle", X: 500, Y: 170, Radius: 35}, {ID: "p3", Shape: "circle", X: 300, Y: 330, Radius: 35}, {ID: "p4", Shape: "circle", X: 500, Y: 330, Radius: 35}}, SpawnPoints: map[string][]Point{"red": {{80, 170}, {80, 330}}, "blue": {{720, 170}, {720, 330}}}, ItemSpawnZones: []SpawnZone{{X: 330, Y: 190, Width: 140, Height: 120}}},
		"crater":       {ID: "crater", Name: "Crater", Width: 900, Height: 600, Obstacles: []Obstacle{{ID: "rim-top", Shape: "aabb", X: 260, Y: 160, Width: 380, Height: 25}, {ID: "rim-bottom", Shape: "aabb", X: 260, Y: 415, Width: 380, Height: 25}}, Hazards: []Hazard{{ID: "edge", Type: "damage-edge", X: 0, Y: 0, Width: 900, Height: 600, Damage: 2}}, SpawnPoints: map[string][]Point{"red": {{90, 300}}, "blue": {{810, 300}}}, ItemSpawnZones: []SpawnZone{{X: 350, Y: 220, Width: 200, Height: 160}}},
		"bunker-line": {ID: "bunker-line", Name: "Bunker Line", Width: 900, Height: 600, Obstacles: []Obstacle{
			{ID: "line-top-l", Shape: "aabb", X: 200, Y: 170, Width: 200, Height: 22}, {ID: "line-top-r", Shape: "aabb", X: 500, Y: 170, Width: 200, Height: 22},
			{ID: "line-mid-l", Shape: "aabb", X: 150, Y: 289, Width: 180, Height: 22}, {ID: "line-mid-r", Shape: "aabb", X: 570, Y: 289, Width: 180, Height: 22},
			{ID: "line-bottom-l", Shape: "aabb", X: 200, Y: 408, Width: 200, Height: 22}, {ID: "line-bottom-r", Shape: "aabb", X: 500, Y: 408, Width: 200, Height: 22},
			{ID: "crate-top-l", Shape: "aabb", X: 390, Y: 60, Width: 40, Height: 40}, {ID: "crate-top-r", Shape: "aabb", X: 470, Y: 60, Width: 40, Height: 40},
			{ID: "crate-bottom-l", Shape: "aabb", X: 390, Y: 500, Width: 40, Height: 40}, {ID: "crate-bottom-r", Shape: "aabb", X: 470, Y: 500, Width: 40, Height: 40},
		}, Hazards: []Hazard{{ID: "hazard-slow-1", Type: "slow-field", X: 410, Y: 240, Width: 80, Height: 120, Damage: 0}}, SpawnPoints: map[string][]Point{"red": {{90, 180}, {90, 420}}, "blue": {{810, 180}, {810, 420}}}, ItemSpawnZones: []SpawnZone{
			{X: 400, Y: 180, Width: 100, Height: 240, Types: []string{"heal", "battery", "overdrive", "rapid_fire", "dash_cell"}},
			{X: 120, Y: 250, Width: 120, Height: 100, Types: []string{"medkit", "nano_repair", "armor_plate", "shield"}},
			{X: 660, Y: 250, Width: 120, Height: 100, Types: []string{"medkit", "nano_repair", "armor_plate", "shield"}},
		}},
		"crossing-fire": {ID: "crossing-fire", Name: "Crossing Fire", Width: 1000, Height: 600, Obstacles: []Obstacle{
			{ID: "cross-top-l", Shape: "aabb", X: 430, Y: 80, Width: 24, Height: 150}, {ID: "cross-top-r", Shape: "aabb", X: 546, Y: 80, Width: 24, Height: 150},
			{ID: "cross-bottom-l", Shape: "aabb", X: 430, Y: 370, Width: 24, Height: 150}, {ID: "cross-bottom-r", Shape: "aabb", X: 546, Y: 370, Width: 24, Height: 150},
			{ID: "cross-left", Shape: "aabb", X: 280, Y: 288, Width: 150, Height: 24}, {ID: "cross-right", Shape: "aabb", X: 570, Y: 288, Width: 150, Height: 24},
			{ID: "crate-tl", Shape: "aabb", X: 200, Y: 120, Width: 50, Height: 50}, {ID: "crate-tr", Shape: "aabb", X: 750, Y: 120, Width: 50, Height: 50},
			{ID: "crate-bl", Shape: "aabb", X: 200, Y: 430, Width: 50, Height: 50}, {ID: "crate-br", Shape: "aabb", X: 750, Y: 430, Width: 50, Height: 50},
			{ID: "pillar-tl", Shape: "circle", X: 350, Y: 150, Radius: 25}, {ID: "pillar-tr", Shape: "circle", X: 650, Y: 150, Radius: 25},
			{ID: "pillar-bl", Shape: "circle", X: 350, Y: 450, Radius: 25}, {ID: "pillar-br", Shape: "circle", X: 650, Y: 450, Radius: 25},
		}, Hazards: []Hazard{
			{ID: "hazard-spike-1", Type: "spike", X: 455, Y: 288, Width: 90, Height: 24, Damage: 8},
			{ID: "hazard-spike-2", Type: "spike", X: 140, Y: 290, Width: 60, Height: 20, Damage: 8},
			{ID: "hazard-spike-3", Type: "spike", X: 800, Y: 290, Width: 60, Height: 20, Damage: 8},
		}, SpawnPoints: map[string][]Point{"red": {{90, 150}, {90, 450}}, "blue": {{910, 150}, {910, 450}}}, ItemSpawnZones: []SpawnZone{
			{X: 440, Y: 230, Width: 120, Height: 140, Types: []string{"cloak", "berserker_charm", "vampiric_fang", "teleport_beacon", "frenzy", "weapon_grenade", "weapon_railgun"}},
			{X: 452, Y: 70, Width: 96, Height: 130, Types: []string{"heal", "battery", "overdrive", "rapid_fire", "dash_cell"}},
			{X: 180, Y: 240, Width: 110, Height: 120, Types: []string{"medkit", "nano_repair", "armor_plate", "shield"}},
			{X: 710, Y: 240, Width: 110, Height: 120, Types: []string{"medkit", "nano_repair", "armor_plate", "shield"}},
		}},
		"vault": {ID: "vault", Name: "The Vault", Width: 900, Height: 600, Obstacles: []Obstacle{
			{ID: "vault-top-l", Shape: "aabb", X: 370, Y: 220, Width: 55, Height: 18}, {ID: "vault-top-r", Shape: "aabb", X: 475, Y: 220, Width: 55, Height: 18},
			{ID: "vault-bottom-l", Shape: "aabb", X: 370, Y: 362, Width: 55, Height: 18}, {ID: "vault-bottom-r", Shape: "aabb", X: 475, Y: 362, Width: 55, Height: 18},
			{ID: "vault-left", Shape: "aabb", X: 370, Y: 220, Width: 18, Height: 160}, {ID: "vault-right", Shape: "aabb", X: 512, Y: 220, Width: 18, Height: 160},
			{ID: "crate-tl", Shape: "aabb", X: 250, Y: 120, Width: 60, Height: 40}, {ID: "crate-tr", Shape: "aabb", X: 590, Y: 120, Width: 60, Height: 40},
			{ID: "crate-bl", Shape: "aabb", X: 250, Y: 440, Width: 60, Height: 40}, {ID: "crate-br", Shape: "aabb", X: 590, Y: 440, Width: 60, Height: 40},
			{ID: "crate-top-l", Shape: "aabb", X: 350, Y: 80, Width: 40, Height: 40}, {ID: "crate-top-r", Shape: "aabb", X: 510, Y: 80, Width: 40, Height: 40},
			{ID: "crate-bottom-l", Shape: "aabb", X: 350, Y: 480, Width: 40, Height: 40}, {ID: "crate-bottom-r", Shape: "aabb", X: 510, Y: 480, Width: 40, Height: 40},
			{ID: "pillar-l", Shape: "circle", X: 180, Y: 300, Radius: 28}, {ID: "pillar-r", Shape: "circle", X: 720, Y: 300, Radius: 28},
		}, Hazards: []Hazard{
			{ID: "hazard-slow-1", Type: "slow-field", X: 410, Y: 120, Width: 80, Height: 80, Damage: 0},
			{ID: "hazard-spike-1", Type: "spike", X: 240, Y: 290, Width: 60, Height: 20, Damage: 8},
			{ID: "hazard-spike-2", Type: "spike", X: 600, Y: 290, Width: 60, Height: 20, Damage: 8},
		}, SpawnPoints: map[string][]Point{"red": {{90, 200}, {90, 400}}, "blue": {{810, 200}, {810, 400}}}, ItemSpawnZones: []SpawnZone{
			{X: 410, Y: 260, Width: 80, Height: 80, Types: []string{"cloak", "berserker_charm", "vampiric_fang", "teleport_beacon", "frenzy", "weapon_grenade", "weapon_railgun"}},
			{X: 150, Y: 140, Width: 110, Height: 90, Types: []string{"medkit", "nano_repair", "armor_plate", "shield"}},
			{X: 640, Y: 140, Width: 110, Height: 90, Types: []string{"medkit", "nano_repair", "armor_plate", "shield"}},
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
