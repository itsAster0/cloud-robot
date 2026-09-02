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
	ensureSpawnCover(&m)
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

// proceduralMazeWalls builds the left half of a braided recursive-division
// maze: the cell grid is divided with door-bearing walls, seeded avenues open
// long lanes, and every dead-end cell gets a second exit so robots keep loops
// to escape through instead of single-lane culs. Pillar circles at cell
// centers add anchor cover away from the walls.
func proceduralMazeWalls(rng *rand.Rand, half, height float64) []Obstacle {
	cols := clampInt(int(math.Round(half/90)), 3, 9)
	rows := clampInt(int(math.Round(height/90)), 3, 10)
	cw, ch := half/float64(cols), height/float64(rows)
	// vOpen[c][r] marks the wall between cells (c,r) and (c+1,r) as open;
	// hOpen[r][c] the wall between (c,r) and (c,r+1).
	vOpen := make([][]bool, cols-1)
	for c := range vOpen {
		vOpen[c] = make([]bool, rows)
	}
	hOpen := make([][]bool, rows-1)
	for r := range hOpen {
		hOpen[r] = make([]bool, cols)
	}
	var divide func(x0, y0, x1, y1 int)
	divide = func(x0, y0, x1, y1 int) {
		w, h := x1-x0+1, y1-y0+1
		if w < 2 && h < 2 {
			return
		}
		if (w > h || (w == h && rng.IntN(2) == 0)) && w >= 2 {
			c := x0 + rng.IntN(w-1)
			door := y0 + rng.IntN(h)
			for r := y0; r <= y1; r++ {
				vOpen[c][r] = r == door
			}
			divide(x0, y0, c, y1)
			divide(c+1, y0, x1, y1)
			return
		}
		if h >= 2 {
			r := y0 + rng.IntN(h-1)
			door := x0 + rng.IntN(w)
			for c := x0; c <= x1; c++ {
				hOpen[r][c] = c == door
			}
			divide(x0, y0, x1, r)
			divide(x0, r+1, x1, y1)
		}
	}
	divide(0, 0, cols-1, rows-1)
	// Avenues: fully open rows (and sometimes a column) give the maze long
	// highways that double as escape and flank routes.
	for _, r := range rng.Perm(rows)[:1+rng.IntN(2)] {
		for c := range vOpen {
			vOpen[c][r] = true
		}
	}
	if rng.IntN(2) == 0 {
		c := rng.IntN(cols - 1)
		for r := range hOpen {
			hOpen[r][c] = true
		}
	}
	// Braid: open one extra wall per dead-end cell so every pocket keeps a
	// second exit; the scan order is fixed for determinism.
	openEdges := func(c, r int) int {
		n := 0
		if c > 0 && vOpen[c-1][r] {
			n++
		}
		if c < cols-2 && vOpen[c][r] {
			n++
		}
		if r > 0 && hOpen[r-1][c] {
			n++
		}
		if r < rows-2 && hOpen[r][c] {
			n++
		}
		return n
	}
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			if openEdges(c, r) != 1 {
				continue
			}
			candidates := [][2]int{}
			if c > 0 && !vOpen[c-1][r] {
				candidates = append(candidates, [2]int{c - 1, r})
			}
			if c < cols-2 && !vOpen[c][r] {
				candidates = append(candidates, [2]int{c + 1, r})
			}
			if r > 0 && !hOpen[r-1][c] {
				candidates = append(candidates, [2]int{c, r - 1})
			}
			if r < rows-2 && !hOpen[r][c] {
				candidates = append(candidates, [2]int{c, r + 1})
			}
			if len(candidates) == 0 {
				continue
			}
			pick := candidates[rng.IntN(len(candidates))]
			if pick[0] != c {
				vOpen[min(pick[0], c)][r] = true
			} else {
				hOpen[min(pick[1], r)][c] = true
			}
		}
	}
	b := &obstacleBuilder{prefix: "maze"}
	for c := 0; c < cols-1; c++ {
		thickness := 16 + rng.Float64()*6
		fixed := float64(c+1)*cw - thickness/2
		r := 0
		for r < rows {
			if vOpen[c][r] {
				r++
				continue
			}
			start := r
			for r < rows && !vOpen[c][r] {
				r++
			}
			b.aabb(fixed, float64(start)*ch, thickness, float64(r-start)*ch)
		}
	}
	for r := 0; r < rows-1; r++ {
		thickness := 16 + rng.Float64()*6
		fixed := float64(r+1)*ch - thickness/2
		c := 0
		for c < cols {
			if hOpen[r][c] {
				c++
				continue
			}
			start := c
			for c < cols && !hOpen[r][c] {
				c++
			}
			b.aabb(float64(start)*cw, fixed, float64(c-start)*cw, thickness)
		}
	}
	for i, n := 0, 4+rng.IntN(5); i < n; i++ {
		cx := (float64(rng.IntN(cols)) + .5) * cw
		cy := (float64(rng.IntN(rows)) + .5) * ch
		radius := 12 + rng.Float64()*6
		if pointClear(b.obstacles, cx, cy, radius+8) {
			b.circle(cx, cy, radius)
		}
	}
	return b.obstacles
}

type bspLeaf struct{ x0, y0, x1, y1 float64 }

// proceduralRooms BSP-splits the left half into 4+ rooms with doored wall
// rings, links them with a minimum spanning tree of carved corridors, and
// drops interior cover into the larger rooms. Corridor carving only removes
// geometry inside the two rooms it joins, so rooms keep their shape.
func proceduralRooms(rng *rand.Rand, half, height float64) []Obstacle {
	b := &obstacleBuilder{prefix: "rooms"}
	const minLeafSide = 150.0
	target := 8 + rng.IntN(7)
	leaves := []bspLeaf{{0, 0, half, height}}
	for len(leaves) < target {
		bestIndex, bestLongest := -1, 0.0
		for i, leaf := range leaves {
			w, h := leaf.x1-leaf.x0, leaf.y1-leaf.y0
			longest := math.Max(w, h)
			if math.Min(w, h) < minLeafSide*2-40 || longest <= bestLongest {
				continue
			}
			bestIndex, bestLongest = i, longest
		}
		if bestIndex < 0 {
			break
		}
		leaf := leaves[bestIndex]
		w, h := leaf.x1-leaf.x0, leaf.y1-leaf.y0
		var a, c bspLeaf
		if w >= h {
			split := clamp(leaf.x0+w*(0.35+rng.Float64()*0.3), leaf.x0+minLeafSide, leaf.x1-minLeafSide)
			a, c = bspLeaf{leaf.x0, leaf.y0, split, leaf.y1}, bspLeaf{split, leaf.y0, leaf.x1, leaf.y1}
		} else {
			split := clamp(leaf.y0+h*(0.35+rng.Float64()*0.3), leaf.y0+minLeafSide, leaf.y1-minLeafSide)
			a, c = bspLeaf{leaf.x0, leaf.y0, leaf.x1, split}, bspLeaf{leaf.x0, split, leaf.x1, leaf.y1}
		}
		leaves[bestIndex] = a
		leaves = append(leaves, c)
	}
	roomRects := make([]bspLeaf, 0, len(leaves))
	for _, leaf := range leaves {
		room := bspLeaf{leaf.x0 + 12, leaf.y0 + 12, leaf.x1 - 12, leaf.y1 - 12}
		if room.x1-room.x0 < 90 || room.y1-room.y0 < 90 {
			continue
		}
		rb := &obstacleBuilder{prefix: fmt.Sprintf("rooms-r%d", len(roomRects))}
		doors := map[int]bool{}
		for _, wall := range rng.Perm(4)[:1+rng.IntN(2)] {
			doors[wall] = true
		}
		thickness := 18.0
		gapHalf := 28.0 + rng.Float64()*6
		midX, midY := (room.x0+room.x1)/2, (room.y0+room.y1)/2
		for wall := 0; wall < 4; wall++ {
			var horizontal bool
			var fixed, start, end float64
			switch wall {
			case 0: // top
				horizontal, fixed, start, end = true, room.y0, room.x0, room.x1
			case 1: // right
				fixed, start, end = room.x1-thickness, room.y0, room.y1
			case 2: // bottom
				horizontal, fixed, start, end = true, room.y1-thickness, room.x0, room.x1
			default: // left
				fixed, start, end = room.x0, room.y0, room.y1
			}
			gaps := [][2]float64{}
			if doors[wall] {
				center := midX
				if !horizontal {
					center = midY
				}
				gaps = append(gaps, [2]float64{center - gapHalf, center + gapHalf})
			}
			rb.segmentedWall(horizontal, fixed, start, end, thickness, gaps)
		}
		w, h := room.x1-room.x0, room.y1-room.y0
		if math.Min(w, h) >= 150 {
			for p, pieces := 0, 1+rng.IntN(2); p < pieces; p++ {
				if rng.IntN(2) == 0 {
					cw, ch := 40+rng.Float64()*20, 40+rng.Float64()*20
					x := room.x0 + 26 + rng.Float64()*math.Max(1, w-52-cw)
					y := room.y0 + 26 + rng.Float64()*math.Max(1, h-52-ch)
					if pointClear(append([]Obstacle(nil), rb.obstacles...), x+cw/2, y+ch/2, 26) {
						rb.aabb(x, y, cw, ch)
					}
				} else {
					cx := room.x0 + 34 + rng.Float64()*math.Max(1, w-68)
					cy := room.y0 + 34 + rng.Float64()*math.Max(1, h-68)
					if pointClear(append([]Obstacle(nil), rb.obstacles...), cx, cy, 30) {
						rb.circle(cx, cy, 14+rng.Float64()*5)
					}
				}
			}
		}
		b.obstacles = append(b.obstacles, rb.obstacles...)
		roomRects = append(roomRects, room)
	}
	// Minimum spanning tree over room centers (deterministic Prim) — every
	// room is reachable through carved corridors.
	if len(roomRects) > 1 {
		connected := []int{0}
		remaining := make([]int, 0, len(roomRects)-1)
		for i := 1; i < len(roomRects); i++ {
			remaining = append(remaining, i)
		}
		for len(remaining) > 0 {
			bestDist, bestA, bestB := math.MaxFloat64, 0, 0
			for _, a := range connected {
				for _, c := range remaining {
					ac, cc := roomRects[a], roomRects[c]
					d := math.Hypot((ac.x0+ac.x1)/2-(cc.x0+cc.x1)/2, (ac.y0+ac.y1)/2-(cc.y0+cc.y1)/2)
					if d < bestDist {
						bestDist, bestA, bestB = d, a, c
					}
				}
			}
			carveRoomCorridor(&b.obstacles, roomRects[bestA], roomRects[bestB])
			for i, c := range remaining {
				if c == bestB {
					remaining = append(remaining[:i], remaining[i+1:]...)
					break
				}
			}
			connected = append(connected, bestB)
		}
	}
	return b.obstacles
}

// carveRoomCorridor opens a 70-unit L-shaped strip between two room centers,
// clamped to the union of the two room rects so unrelated rooms keep their
// walls. Cutting only removes geometry, so it can never seal a room.
func carveRoomCorridor(obstacles *[]Obstacle, a, c bspLeaf) {
	const half = 35.0
	ax, ay := (a.x0+a.x1)/2, (a.y0+a.y1)/2
	cx, cy := (c.x0+c.x1)/2, (c.y0+c.y1)/2
	ux0, uy0 := math.Min(a.x0, c.x0), math.Min(a.y0, c.y0)
	ux1, uy1 := math.Max(a.x1, c.x1), math.Max(a.y1, c.y1)
	hx0, hx1 := clamp(math.Min(ax, cx)-half, ux0, ux1), clamp(math.Max(ax, cx)+half, ux0, ux1)
	hy0, hy1 := clamp(ay-half, uy0, uy1), clamp(ay+half, uy0, uy1)
	carveRectList(obstacles, hx0, hy0, hx1-hx0, hy1-hy0)
	vx0, vx1 := clamp(cx-half, ux0, ux1), clamp(cx+half, ux0, ux1)
	vy0, vy1 := clamp(math.Min(ay, cy)-half, uy0, uy1), clamp(math.Max(ay, cy)+half, uy0, uy1)
	carveRectList(obstacles, vx0, vy0, vx1-vx0, vy1-vy0)
}

func carveRectList(obstacles *[]Obstacle, x, y, w, h float64) {
	kept := (*obstacles)[:0]
	for _, o := range *obstacles {
		if !obstacleIntersectsRect(o, x, y, w, h) {
			kept = append(kept, o)
		}
	}
	*obstacles = kept
}

// proceduralBunkers scatters crates, L-shaped cover corners, broken trench
// lines, and side pockets over the left half, kept clear of the center vault
// ring added after mirroring.
func proceduralBunkers(rng *rand.Rand, half, height float64) []Obstacle {
	b := &obstacleBuilder{prefix: "bunkers"}
	margin := 40.0
	vaultKeepOut := 100.0
	for i, crates := 0, 6+rng.IntN(5); i < crates; i++ {
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
	// Trench lines: broken horizontal walls with staggered gaps give mid-field
	// cover without sealing lanes.
	for i := 0; i < 2; i++ {
		y := height*(0.33+0.34*float64(i)) - 8
		gapA := half * (0.25 + rng.Float64()*0.2)
		gapB := half * (0.6 + rng.Float64()*0.2)
		b.segmentedWall(true, y, 40, half-110, 14+rng.Float64()*4, [][2]float64{{gapA, gapA + 70}, {gapB, gapB + 70}})
	}
	// L-shaped double crates read as cover corners to hide behind.
	for i, n := 0, 3+rng.IntN(2); i < n; i++ {
		w := 44 + rng.Float64()*18
		x := 60 + rng.Float64()*(half-260)
		y := 60 + rng.Float64()*(height-180)
		if bunkersOverlap(b.obstacles, x, y, w, w) {
			continue
		}
		b.aabb(x, y, w, w*0.5)
		b.aabb(x, y, w*0.5, w)
	}
	// Side pockets: three-wall alcoves open toward the center — deliberate
	// hiding spots near the flanks.
	for _, py := range []float64{height * 0.24, height * 0.76} {
		x, depth, thickness := 46.0, 80.0, 16.0
		if bunkersOverlap(b.obstacles, x, py-depth/2, depth+40, depth) {
			continue
		}
		b.aabb(x, py-depth/2, depth, thickness)
		b.aabb(x, py+depth/2-thickness, depth, thickness)
		b.aabb(x, py-depth/2, thickness, depth)
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

// nearestObstacleDistance reports the distance from a point to the closest
// obstacle surface (0 when the point is inside one).
func nearestObstacleDistance(obstacles []Obstacle, x, y float64) float64 {
	best := math.MaxFloat64
	for _, o := range obstacles {
		var d float64
		if o.Shape == "circle" {
			d = math.Abs(math.Hypot(x-o.X, y-o.Y) - o.Radius)
		} else {
			nx, ny := clamp(x, o.X, o.X+o.Width), clamp(y, o.Y, o.Y+o.Height)
			d = math.Hypot(x-nx, y-ny)
		}
		best = math.Min(best, d)
	}
	return best
}

// ensureSpawnCover guarantees every spawn point has cover within ~90 units so
// robots spawn next to a hiding spot instead of in the open. When the nudged
// spawn landed in open space, a small crate is placed at a fixed offset toward
// the arena center and mirrored for the opposing team. The crate is small on
// purpose: corridor-style spawns may only have a ~70-unit gap to fit it in.
func ensureSpawnCover(m *MapDefinition) {
	const coverReach = 90.0
	placed := 0
	for _, p := range m.SpawnPoints["red"] {
		if nearestObstacleDistance(m.Obstacles, p.X, p.Y) <= coverReach {
			continue
		}
		base := math.Atan2(m.Height/2-p.Y, m.Width/2-p.X)
		for attempt := 0; attempt < 16; attempt++ {
			angle := base + float64(attempt)*math.Pi/8
			distance := 55 + float64(attempt%2)*10
			crate := Obstacle{ID: fmt.Sprintf("cover-%d", placed+1), Shape: "aabb", X: p.X + math.Cos(angle)*distance - 18, Y: p.Y + math.Sin(angle)*distance - 14, Width: 36, Height: 28}
			crate.X = clamp(crate.X, 1, m.Width/2-37)
			crate.Y = clamp(crate.Y, 1, m.Height-29)
			if overlapsObstacleSolid(m.Obstacles, crate.X, crate.Y, crate.Width, crate.Height) {
				continue
			}
			candidate := append(append([]Obstacle(nil), m.Obstacles...), crate, Obstacle{ID: crate.ID + "-m", Shape: "aabb", X: m.Width - crate.X - crate.Width, Y: crate.Y, Width: crate.Width, Height: crate.Height})
			clear := true
			for _, points := range m.SpawnPoints {
				for _, sp := range points {
					if !pointClear(candidate, sp.X, sp.Y, spawnClearance) {
						clear = false
						break
					}
				}
				if !clear {
					break
				}
			}
			if !clear {
				continue
			}
			probe := *m
			probe.Obstacles = candidate
			if !mapConnectivity(probe) {
				continue
			}
			m.Obstacles = candidate
			placed++
			break
		}
	}
}

// overlapsObstacleSolid reports whether the box intersects any obstacle with
// no margin — cover crates are allowed to hug walls so they fit corridors.
func overlapsObstacleSolid(existing []Obstacle, x, y, w, h float64) bool {
	for _, o := range existing {
		ox, oy, ow, oh := o.X, o.Y, o.Width, o.Height
		if o.Shape == "circle" {
			ox, oy, ow, oh = o.X-o.Radius, o.Y-o.Radius, o.Radius*2, o.Radius*2
		}
		if x < ox+ow && x+w > ox && y < oy+oh && y+h > oy {
			return true
		}
	}
	return false
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
	carveRectList(&m.Obstacles, x, y, w, h)
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
