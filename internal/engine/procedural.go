package engine

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
)

// ProceduralMapStyles maps the public "random-*" map ids onto their generation
// styles. The API accepts these ids at match creation; the worker generates
// the actual geometry from the persisted match seed, so every worker and every
// replay rebuilds an identical arena.
var ProceduralMapStyles = map[string]string{
	"random-maze":    "maze",
	"random-rooms":   "rooms",
	"random-bunkers": "bunkers",
}

var proceduralMapNames = map[string]string{
	"maze":    "Procedural Maze",
	"rooms":   "Procedural Rooms",
	"bunkers": "Procedural Bunkers",
}

var (
	neutralZoneTypes = []string{"heal", "battery", "overdrive", "rapid_fire", "dash_cell"}
	flankZoneTypes   = []string{"medkit", "nano_repair", "armor_plate", "shield"}
	epicZoneTypes    = []string{"cloak", "berserker_charm", "vampiric_fang", "teleport_beacon", "frenzy", "weapon_grenade", "weapon_railgun"}
)

// spawnClearance keeps spawn points this far from every obstacle so robots
// never materialize wedged into cover.
const spawnClearance = 30.0

// GenerateMap builds a symmetric arena from a match seed. Deterministic: the
// same seed, style, and dimensions always produce a byte-identical definition.
// Only a PCG stream seeded by the match seed is used — never wall-clock time
// or shared global random state. Only an unknown style is an error.
func GenerateMap(seed uint64, style string, width, height float64) (MapDefinition, error) {
	name, ok := proceduralMapNames[style]
	if !ok {
		return MapDefinition{}, errors.New("unknown procedural map style: " + style)
	}
	if width <= 0 {
		width = 900
	}
	if height <= 0 {
		height = 600
	}
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	m := MapDefinition{ID: "random-" + style, Name: name, Width: width, Height: height}
	var left []Obstacle
	switch style {
	case "maze":
		left = proceduralMazeWalls(rng, width/2, height)
	case "rooms":
		left = proceduralRooms(rng, width/2, height)
	case "bunkers":
		left = proceduralBunkers(rng, width/2, height)
	}
	m.Obstacles = mirrorObstacles(left, width)
	if style == "bunkers" {
		m.Obstacles = append(m.Obstacles, centerVaultObstacles(width, height)...)
	}
	m.SpawnPoints = proceduralSpawnPoints(m.Obstacles, width, height)
	m.ItemSpawnZones = proceduralItemZones(width, height)
	m.Hazards = proceduralHazards(style, width, height)
	carveForConnectivity(&m)
	return m, nil
}

type obstacleBuilder struct {
	obstacles []Obstacle
	prefix    string
	next      int
}

func (b *obstacleBuilder) aabb(x, y, w, h float64) {
	if w <= 0 || h <= 0 {
		return
	}
	b.next++
	b.obstacles = append(b.obstacles, Obstacle{ID: fmt.Sprintf("%s-%d", b.prefix, b.next), Shape: "aabb", X: x, Y: y, Width: w, Height: h})
}

func (b *obstacleBuilder) circle(x, y, r float64) {
	b.next++
	b.obstacles = append(b.obstacles, Obstacle{ID: fmt.Sprintf("%s-%d", b.prefix, b.next), Shape: "circle", X: x, Y: y, Radius: r})
}

// segmentedWall appends the solid pieces of one wall span left over after the
// given door gaps are cut out.
func (b *obstacleBuilder) segmentedWall(horizontal bool, fixed, start, end, thickness float64, gaps [][2]float64) {
	sort.Slice(gaps, func(i, j int) bool { return gaps[i][0] < gaps[j][0] })
	cursor := start
	for _, gap := range gaps {
		if gap[0] > cursor {
			b.piece(horizontal, fixed, cursor, math.Min(gap[0], end), thickness)
		}
		cursor = math.Max(cursor, gap[1])
	}
	if cursor < end {
		b.piece(horizontal, fixed, cursor, end, thickness)
	}
}

func (b *obstacleBuilder) piece(horizontal bool, fixed, a, z, thickness float64) {
	if horizontal {
		b.aabb(a, fixed, z-a, thickness)
	} else {
		b.aabb(fixed, a, thickness, z-a)
	}
}

// proceduralMazeWalls builds the left half of a wall-segment maze: vertical
// dividers with door gaps plus horizontal cross walls. The last vertical line
// sits on the mirror axis and maps onto itself, acting as the team divider
// whose doors are the only crossings besides carved lanes.
func proceduralMazeWalls(rng *rand.Rand, half, height float64) []Obstacle {
	b := &obstacleBuilder{prefix: "maze"}
	cols := clampInt(int(math.Round(half/90)), 2, 7)
	rows := clampInt(int(math.Round(height/90)), 2, 7)
	cw, ch := half/float64(cols), height/float64(rows)
	thickness := func() float64 { return 16 + rng.Float64()*6 }
	for line := 1; line <= cols; line++ {
		// Skipping a few dividers leaves open lanes; the mirror-line divider is
		// always kept so the halves meet only through its doors.
		if line != cols && rng.Float64() < 0.25 {
			continue
		}
		perm := rng.Perm(rows)
		gaps := make([][2]float64, 0, 3)
		for _, row := range perm[:2+rng.IntN(2)] {
			gaps = append(gaps, [2]float64{float64(row)*ch + ch*0.15, float64(row+1)*ch - ch*0.15})
		}
		b.segmentedWall(false, float64(line)*cw-thickness()/2, 0, height, thickness(), gaps)
	}
	for line := 1; line < rows; line++ {
		if rng.Float64() < 0.45 {
			continue
		}
		perm := rng.Perm(cols)
		gaps := make([][2]float64, 0, 2)
		for _, col := range perm[:2] {
			gaps = append(gaps, [2]float64{float64(col)*cw + cw*0.15, float64(col+1)*cw - cw*0.15})
		}
		b.segmentedWall(true, float64(line)*ch-thickness()/2, 0, half, thickness(), gaps)
	}
	return b.obstacles
}

// proceduralRooms lays 4-8 rooms on a coarse grid over the left half. Each
// room is either a solid block or a wall ring with two door gaps; the gaps in
// the ring plus the open lanes between rooms form the corridors.
func proceduralRooms(rng *rand.Rand, half, height float64) []Obstacle {
	b := &obstacleBuilder{prefix: "rooms"}
	cols := clampInt(int(math.Round(half/110)), 2, 3)
	rows := clampInt(int(math.Round(height/110)), 2, 3)
	cw, ch := half/float64(cols), height/float64(rows)
	cells := make([][2]int, 0, cols*rows)
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			cells = append(cells, [2]int{col, row})
		}
	}
	rng.Shuffle(len(cells), func(i, j int) { cells[i], cells[j] = cells[j], cells[i] })
	count := clampInt(4+rng.IntN(5), 4, len(cells))
	thickness := 18.0
	for _, cell := range cells[:count] {
		side := math.Min(60+rng.Float64()*80, math.Min(cw*0.55, ch*0.55))
		side = math.Max(side, 48)
		x := float64(cell[0])*cw + (cw-side)/2
		y := float64(cell[1])*ch + (ch-side)/2
		if rng.Float64() < 0.3 {
			b.aabb(x, y, side, side)
			continue
		}
		// Two of the four ring walls get a centered door gap; the rest stay
		// solid so rooms read as rooms, not rubble.
		doors := map[int]bool{}
		for _, wall := range rng.Perm(4)[:2] {
			doors[wall] = true
		}
		midX, midY := x+side/2, y+side/2
		for wall := 0; wall < 4; wall++ {
			var horizontal bool
			var fixed, start, end float64
			switch wall {
			case 0: // top
				horizontal, fixed, start, end = true, y, x, x+side
			case 1: // right
				fixed, start, end = x+side-thickness, y, y+side
			case 2: // bottom
				horizontal, fixed, start, end = true, y+side-thickness, x, x+side
			default: // left
				fixed, start, end = x, y, y+side
			}
			gaps := [][2]float64{}
			if doors[wall] {
				center := midX
				if !horizontal {
					center = midY
				}
				gaps = append(gaps, [2]float64{center - 28, center + 28})
			}
			b.segmentedWall(horizontal, fixed, start, end, thickness, gaps)
		}
	}
	return b.obstacles
}

// proceduralBunkers scatters crates and pillars over the left half, kept clear
// of the center vault ring added after mirroring.
func proceduralBunkers(rng *rand.Rand, half, height float64) []Obstacle {
	b := &obstacleBuilder{prefix: "bunkers"}
	margin := 40.0
	vaultKeepOut := 100.0
	for i, crates := 0, 8+rng.IntN(7); i < crates; i++ {
		w, h := 40+rng.Float64()*50, 40+rng.Float64()*50
		for attempt := 0; attempt < 24; attempt++ {
			x := margin + rng.Float64()*(half-vaultKeepOut-margin-w)
			y := margin + rng.Float64()*(height-2*margin-h)
			if bunkersOverlap(b.obstacles, x, y, w, h) {
				continue
			}
			b.aabb(x, y, w, h)
			break
		}
	}
	for i, pillars := 0, 2+rng.IntN(3); i < pillars; i++ {
		r := 20 + rng.Float64()*15
		for attempt := 0; attempt < 24; attempt++ {
			x := margin + r + rng.Float64()*(half-vaultKeepOut-margin-2*r)
			y := margin + r + rng.Float64()*(height-2*margin-2*r)
			if bunkersOverlap(b.obstacles, x-r, y-r, 2*r, 2*r) {
				continue
			}
			b.circle(x, y, r)
			break
		}
	}
	return b.obstacles
}

// bunkersOverlap checks the candidate box against existing obstacles, treating
// circles as their bounding squares; a loose margin keeps crates readable.
func bunkersOverlap(existing []Obstacle, x, y, w, h float64) bool {
	const margin = 12
	for _, o := range existing {
		ox, oy, ow, oh := o.X, o.Y, o.Width, o.Height
		if o.Shape == "circle" {
			ox, oy, ow, oh = o.X-o.Radius, o.Y-o.Radius, o.Radius*2, o.Radius*2
		}
		if x-margin < ox+ow && x+w+margin > ox && y-margin < oy+oh && y+h+margin > oy {
			return true
		}
	}
	return false
}

// centerVaultObstacles rings the arena center with four walls: top and bottom
// are split around a centered door gap, sides are solid, so the vault has
// exactly two entrances on the mirror axis.
func centerVaultObstacles(width, height float64) []Obstacle {
	b := &obstacleBuilder{prefix: "vault"}
	cx, cy := width/2, height/2
	half, thickness, gap := 80.0, 18.0, 56.0
	segment := (2*half - gap) / 2
	b.aabb(cx-half, cy-half, segment, thickness)
	b.aabb(cx+gap/2, cy-half, segment, thickness)
	b.aabb(cx-half, cy+half-thickness, segment, thickness)
	b.aabb(cx+gap/2, cy+half-thickness, segment, thickness)
	b.aabb(cx-half, cy-half, thickness, 2*half)
	b.aabb(cx+half-thickness, cy-half, thickness, 2*half)
	return b.obstacles
}

// mirrorObstacles reflects the left-half geometry across the vertical center
// axis, which is what makes procedural maps fair for both teams.
func mirrorObstacles(left []Obstacle, width float64) []Obstacle {
	out := make([]Obstacle, 0, len(left)*2)
	for _, o := range left {
		out = append(out, o)
		mirror := o
		mirror.ID = o.ID + "-m"
		if o.Shape == "circle" {
			mirror.X = width - o.X
		} else {
			mirror.X = width - o.X - o.Width
		}
		// Obstacles already centered on the mirror axis are their own reflection.
		if math.Abs(mirror.X-o.X) < 0.01 {
			continue
		}
		out = append(out, mirror)
	}
	return out
}

func proceduralSpawnPoints(obstacles []Obstacle, width, height float64) map[string][]Point {
	quarter := width / 4
	red := make([]Point, 0, 2)
	for _, base := range []Point{{X: quarter * 0.6, Y: height * 0.3}, {X: quarter * 0.5, Y: height * 0.7}} {
		red = append(red, nudgeSpawnClear(obstacles, width, height, base))
	}
	blue := make([]Point, 0, 2)
	for _, p := range red {
		blue = append(blue, Point{X: width - p.X, Y: p.Y})
	}
	return map[string][]Point{"red": red, "blue": blue}
}

// nudgeSpawnClear spirals outward deterministically until the point keeps
// spawnClearance from every obstacle, clamped to the team's own half.
func nudgeSpawnClear(obstacles []Obstacle, width, height float64, p Point) Point {
	maxX := width/2 - 60
	clampPoint := func(q Point) Point {
		return Point{X: clamp(q.X, RobotRadius, maxX), Y: clamp(q.Y, RobotRadius, height-RobotRadius)}
	}
	start := clampPoint(p)
	if pointClear(obstacles, start.X, start.Y, spawnClearance) {
		return start
	}
	for radius := 12.0; radius <= 240; radius += 12 {
		for step := 0; step < 16; step++ {
			angle := float64(step) * math.Pi / 8
			candidate := clampPoint(Point{X: start.X + math.Cos(angle)*radius, Y: start.Y + math.Sin(angle)*radius})
			if pointClear(obstacles, candidate.X, candidate.Y, spawnClearance) {
				return candidate
			}
		}
	}
	return start
}

func pointClear(obstacles []Obstacle, x, y, clearance float64) bool {
	for _, o := range obstacles {
		if o.Shape == "circle" {
			if math.Hypot(x-o.X, y-o.Y) < clearance+o.Radius {
				return false
			}
			continue
		}
		nearestX, nearestY := clamp(x, o.X, o.X+o.Width), clamp(y, o.Y, o.Y+o.Height)
		if math.Hypot(x-nearestX, y-nearestY) < clearance {
			return false
		}
	}
	return true
}

func proceduralItemZones(width, height float64) []SpawnZone {
	cx, cy := width/2, height/2
	flankWidth, flankHeight := 130.0, 120.0
	return []SpawnZone{
		{X: cx - 70, Y: cy - 70, Width: 140, Height: 140, Types: append([]string(nil), epicZoneTypes...)},
		{X: cx - 45, Y: height * 0.22, Width: 90, Height: height * 0.28, Types: append([]string(nil), neutralZoneTypes...)},
		{X: width * 0.14, Y: cy - flankHeight/2, Width: flankWidth, Height: flankHeight, Types: append([]string(nil), flankZoneTypes...)},
		{X: width - width*0.14 - flankWidth, Y: cy - flankHeight/2, Width: flankWidth, Height: flankHeight, Types: append([]string(nil), flankZoneTypes...)},
	}
}

// proceduralHazards returns one or two hazard placements per style; hazards
// off the mirror axis are mirrored like obstacles for team fairness.
func proceduralHazards(style string, width, height float64) []Hazard {
	cx, cy := width/2, height/2
	switch style {
	case "maze":
		return []Hazard{{ID: "hazard-slow-1", Type: "slow-field", X: cx - 40, Y: cy - 75, Width: 80, Height: 150, Damage: 0}}
	case "rooms":
		return []Hazard{
			{ID: "hazard-spike-1", Type: "spike", X: cx - 30, Y: cy - 95, Width: 60, Height: 55, Damage: 8},
			{ID: "hazard-slow-1", Type: "slow-field", X: cx - 170, Y: cy - 55, Width: 110, Height: 110, Damage: 0},
			{ID: "hazard-slow-2", Type: "slow-field", X: cx + 60, Y: cy - 55, Width: 110, Height: 110, Damage: 0},
		}
	default:
		return []Hazard{
			{ID: "hazard-slow-1", Type: "slow-field", X: cx - 35, Y: cy - 105, Width: 70, Height: 70, Damage: 0},
			{ID: "hazard-spike-1", Type: "spike", X: cx - 160, Y: cy - 25, Width: 50, Height: 50, Damage: 8},
			{ID: "hazard-spike-2", Type: "spike", X: cx + 110, Y: cy - 25, Width: 50, Height: 50, Damage: 8},
		}
	}
}

const connectivityCell = 20.0

// mapConnectivity reports whether every red spawn reaches every blue spawn and
// the arena center on a coarse occupancy grid whose cells are blocked when a
// robot-sized circle collides with an obstacle there.
func mapConnectivity(m MapDefinition) bool {
	cols := int(m.Width / connectivityCell)
	rows := int(m.Height / connectivityCell)
	if cols < 1 || rows < 1 {
		return false
	}
	blocked := make([]bool, cols*rows)
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			blocked[row*cols+col] = collidesRobot(m.Obstacles, (float64(col)+.5)*connectivityCell, (float64(row)+.5)*connectivityCell)
		}
	}
	freeCellNear := func(p Point) (int, int, bool) {
		col := clampInt(int(p.X/connectivityCell), 0, cols-1)
		row := clampInt(int(p.Y/connectivityCell), 0, rows-1)
		if !blocked[row*cols+col] {
			return col, row, true
		}
		for radius := 1; radius <= 2; radius++ {
			for dr := -radius; dr <= radius; dr++ {
				for dc := -radius; dc <= radius; dc++ {
					r, c := row+dr, col+dc
					if r < 0 || c < 0 || r >= rows || c >= cols || blocked[r*cols+c] {
						continue
					}
					return c, r, true
				}
			}
		}
		return 0, 0, false
	}
	starts := [][2]int{}
	for _, p := range m.SpawnPoints["red"] {
		col, row, ok := freeCellNear(p)
		if !ok {
			return false
		}
		starts = append(starts, [2]int{col, row})
	}
	if len(starts) == 0 {
		return false
	}
	reached := floodFill(blocked, cols, rows, starts)
	reaches := func(p Point) bool {
		col, row, ok := freeCellNear(p)
		return ok && reached[row*cols+col]
	}
	for _, p := range m.SpawnPoints["blue"] {
		if !reaches(p) {
			return false
		}
	}
	return reaches(Point{X: m.Width / 2, Y: m.Height / 2})
}

func floodFill(blocked []bool, cols, rows int, starts [][2]int) []bool {
	reached := make([]bool, len(blocked))
	queue := make([]int, 0, len(blocked))
	push := func(col, row int) {
		index := row*cols + col
		if blocked[index] || reached[index] {
			return
		}
		reached[index] = true
		queue = append(queue, index)
	}
	for _, start := range starts {
		push(start[0], start[1])
	}
	for len(queue) > 0 {
		index := queue[0]
		queue = queue[1:]
		col, row := index%cols, index/cols
		if col > 0 {
			push(col-1, row)
		}
		if col < cols-1 {
			push(col+1, row)
		}
		if row > 0 {
			push(col, row-1)
		}
		if row < rows-1 {
			push(col, row+1)
		}
	}
	return reached
}

// carveForConnectivity guarantees spawns and the center are mutually
// reachable: if the flood fill says the arena is sealed, it deletes obstacles
// along deterministic L-shaped channels from each spawn to the center, and as
// a last resort clears a full-width corridor along the mid line. Carving only
// removes obstacles, so it can never block a previously open path.
func carveForConnectivity(m *MapDefinition) {
	if mapConnectivity(*m) {
		return
	}
	cx, cy, channel := m.Width/2, m.Height/2, 70.0
	for _, team := range []string{"red", "blue"} {
		for _, p := range m.SpawnPoints[team] {
			carveLPath(m, p, Point{X: cx, Y: cy}, channel)
		}
	}
	if mapConnectivity(*m) {
		return
	}
	carveRect(m, 0, cy-channel/2, m.Width, channel)
}

func carveLPath(m *MapDefinition, from, to Point, channel float64) {
	half := channel / 2
	if math.Abs(from.Y-to.Y) > 0.01 {
		top, bottom := math.Min(from.Y, to.Y)-half, math.Max(from.Y, to.Y)+half
		carveRect(m, to.X-half, top, channel, bottom-top)
	}
	left, right := math.Min(from.X, to.X)-half, math.Max(from.X, to.X)+half
	carveRect(m, left, from.Y-half, right-left, channel)
}

func carveRect(m *MapDefinition, x, y, w, h float64) {
	kept := m.Obstacles[:0]
	for _, o := range m.Obstacles {
		if !obstacleIntersectsRect(o, x, y, w, h) {
			kept = append(kept, o)
		}
	}
	m.Obstacles = kept
}

func obstacleIntersectsRect(o Obstacle, x, y, w, h float64) bool {
	if o.Shape == "circle" {
		nearestX, nearestY := clamp(o.X, x, x+w), clamp(o.Y, y, y+h)
		return math.Hypot(o.X-nearestX, o.Y-nearestY) <= o.Radius
	}
	return o.X < x+w && o.X+o.Width > x && o.Y < y+h && o.Y+o.Height > y
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
