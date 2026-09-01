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
