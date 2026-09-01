package engine

import (
	"math"
	"reflect"
	"testing"
)

func TestGenerateMapDeterministicPerSeedAndStyle(t *testing.T) {
	for _, style := range []string{"maze", "rooms", "bunkers"} {
		first, err := GenerateMap(987654321, style, 900, 600)
		if err != nil {
			t.Fatalf("style %s: %v", style, err)
		}
		second, err := GenerateMap(987654321, style, 900, 600)
		if err != nil {
			t.Fatalf("style %s: %v", style, err)
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("style %s: same seed must regenerate a byte-identical map", style)
		}
	}
	other, err := GenerateMap(987654322, "maze", 900, 600)
	if err != nil {
		t.Fatalf("second seed: %v", err)
	}
	if reflect.DeepEqual(other.Obstacles, mustGenerate(t, 987654321, "maze").Obstacles) {
		t.Fatal("different seeds must produce different obstacle layouts")
	}
}

func mustGenerate(t *testing.T, seed uint64, style string) MapDefinition {
	t.Helper()
	m, err := GenerateMap(seed, style, 900, 600)
	if err != nil {
		t.Fatalf("seed %d style %s: %v", seed, style, err)
	}
	return m
}

func TestGenerateMapUnknownStyle(t *testing.T) {
	if _, err := GenerateMap(1, "swiss-cheese", 900, 600); err == nil {
		t.Fatal("unknown style must return an error")
	}
}

func TestGenerateMapStylesValidForManySeeds(t *testing.T) {
	for _, style := range []string{"maze", "rooms", "bunkers"} {
		for seed := uint64(1); seed <= 12; seed++ {
			m, err := GenerateMap(seed, style, 900, 600)
			if err != nil {
				t.Fatalf("seed %d style %s: %v", seed, style, err)
			}
			if m.ID != "random-"+style || m.Name == "" || m.Width != 900 || m.Height != 600 {
				t.Fatalf("seed %d style %s: bad header %+v", seed, style, m)
			}
			if !mapConnectivity(m) {
				t.Fatalf("seed %d style %s: spawns and center must be mutually reachable", seed, style)
			}
			assertSpawnsValid(t, m)
			assertZonesValid(t, m)
			assertObstaclesInBounds(t, m)
			assertHazardsValid(t, m)
			if len(m.Obstacles) < 4 {
				t.Fatalf("seed %d style %s: arena too empty (%d obstacles)", seed, style, len(m.Obstacles))
			}
		}
	}
}

func TestGenerateMapScalesToCustomDimensions(t *testing.T) {
	for _, dims := range [][2]float64{{1200, 800}, {2000, 1400}, {400, 300}} {
		for _, style := range []string{"maze", "rooms", "bunkers"} {
			m, err := GenerateMap(7, style, dims[0], dims[1])
			if err != nil {
				t.Fatalf("dims %v style %s: %v", dims, style, err)
			}
			if m.Width != dims[0] || m.Height != dims[1] {
				t.Fatalf("dims %v: generated map kept %vx%v", dims, m.Width, m.Height)
			}
			if !mapConnectivity(m) {
				t.Fatalf("dims %v style %s: map not connected", dims, style)
			}
			assertObstaclesInBounds(t, m)
		}
	}
}

func assertSpawnsValid(t *testing.T, m MapDefinition) {
	t.Helper()
	red, blue := m.SpawnPoints["red"], m.SpawnPoints["blue"]
	if len(red) != 2 || len(blue) != 2 {
		t.Fatalf("%s: expected 2 spawn points per team, got red=%d blue=%d", m.ID, len(red), len(blue))
	}
	if red[0].Y >= red[1].Y {
		t.Fatalf("%s: red spawns must be y-spread: %v", m.ID, red)
	}
	for team, points := range map[string][]Point{"red": red, "blue": blue} {
		for i, p := range points {
			if p.X < RobotRadius || p.Y < RobotRadius || p.X > m.Width-RobotRadius || p.Y > m.Height-RobotRadius {
				t.Fatalf("%s: %s spawn %d out of bounds: %v", m.ID, team, i, p)
			}
			if !pointClear(m.Obstacles, p.X, p.Y, spawnClearance) {
				t.Fatalf("%s: %s spawn %d not 30 units clear of obstacles: %v", m.ID, team, i, p)
			}
		}
	}
	for i, p := range red {
		if p.X > m.Width/2 {
			t.Fatalf("%s: red spawn %d must sit in the left half: %v", m.ID, i, p)
		}
		mirror := blue[i]
		if math.Abs(mirror.X-(m.Width-p.X)) > 0.01 || math.Abs(mirror.Y-p.Y) > 0.01 {
			t.Fatalf("%s: blue spawn %d must mirror red: red=%v blue=%v", m.ID, i, p, mirror)
		}
	}
}

func assertZonesValid(t *testing.T, m MapDefinition) {
	t.Helper()
	if len(m.ItemSpawnZones) < 3 || len(m.ItemSpawnZones) > 5 {
		t.Fatalf("%s: expected 3-5 item spawn zones, got %d", m.ID, len(m.ItemSpawnZones))
	}
	for i, zone := range m.ItemSpawnZones {
		if len(zone.Types) == 0 {
			t.Fatalf("%s: zone %d has no Types filter", m.ID, i)
		}
		if zone.Width <= 0 || zone.Height <= 0 || zone.X < 0 || zone.Y < 0 || zone.X+zone.Width > m.Width || zone.Y+zone.Height > m.Height {
			t.Fatalf("%s: zone %d out of bounds: %+v", m.ID, i, zone)
		}
	}
}

func assertObstaclesInBounds(t *testing.T, m MapDefinition) {
	t.Helper()
	seen := map[string]bool{}
	for _, o := range m.Obstacles {
		if o.ID == "" || seen[o.ID] {
			t.Fatalf("%s: obstacle id %q empty or duplicated", m.ID, o.ID)
		}
		seen[o.ID] = true
		switch o.Shape {
		case "aabb":
			if o.Width <= 0 || o.Height <= 0 || o.X < -0.01 || o.Y < -0.01 || o.X+o.Width > m.Width+0.01 || o.Y+o.Height > m.Height+0.01 {
				t.Fatalf("%s: aabb %q out of bounds: %+v", m.ID, o.ID, o)
			}
		case "circle":
			if o.Radius <= 0 || o.X-o.Radius < -0.01 || o.X+o.Radius > m.Width+0.01 || o.Y-o.Radius < -0.01 || o.Y+o.Radius > m.Height+0.01 {
				t.Fatalf("%s: circle %q out of bounds: %+v", m.ID, o.ID, o)
			}
		default:
			t.Fatalf("%s: obstacle %q has unknown shape %q", m.ID, o.ID, o.Shape)
		}
	}
}

func assertHazardsValid(t *testing.T, m MapDefinition) {
	t.Helper()
	if len(m.Hazards) < 1 || len(m.Hazards) > 3 {
		t.Fatalf("%s: expected 1-3 hazards, got %d", m.ID, len(m.Hazards))
	}
	for _, hazard := range m.Hazards {
		switch hazard.Type {
		case "slow-field":
			if hazard.Damage != 0 {
				t.Fatalf("%s: slow-field %q must have zero damage", m.ID, hazard.ID)
			}
		case "spike":
			if hazard.Damage <= 0 {
				t.Fatalf("%s: spike %q must deal damage", m.ID, hazard.ID)
			}
		default:
			t.Fatalf("%s: hazard %q has unexpected type %q", m.ID, hazard.ID, hazard.Type)
		}
		if hazard.X < 0 || hazard.Y < 0 || hazard.X+hazard.Width > m.Width || hazard.Y+hazard.Height > m.Height {
			t.Fatalf("%s: hazard %q out of bounds: %+v", m.ID, hazard.ID, hazard)
		}
	}
}

func TestSpawnZoneAccepts(t *testing.T) {
	filtered := SpawnZone{Types: []string{"heal", "cloak"}}
	open := SpawnZone{}
	cases := []struct {
		zone SpawnZone
		kind string
		want bool
	}{
		{filtered, "heal", true},
		{filtered, "cloak", true},
		{filtered, "battery", false},
		{filtered, "", false},
		{open, "heal", true},
		{open, "weapon_railgun", true},
		{open, "", true},
	}
	for _, c := range cases {
		if got := c.zone.Accepts(c.kind); got != c.want {
			t.Fatalf("zone types %v accepts(%q) = %v, want %v", c.zone.Types, c.kind, got, c.want)
		}
	}
}

func TestStarterMapsKeepLegacyEntries(t *testing.T) {
	maps := StarterMaps()
	for _, id := range []string{"open-field", "four-corners", "corridors", "pillars", "crater", "bunker-line", "crossing-fire", "vault"} {
		if _, ok := maps[id]; !ok {
			t.Fatalf("starter map %q missing", id)
		}
	}
	// Old maps must stay byte-stable for existing replays.
	if got := maps["four-corners"].Obstacles[0]; got != (Obstacle{ID: "center", Shape: "aabb", X: 350, Y: 200, Width: 100, Height: 100}) {
		t.Fatalf("four-corners changed: %+v", got)
	}
}
