package enginev4

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
)

type Loadout struct {
	Chassis   string   `json:"chassis"`
	Weapon    string   `json:"weapon"`
	Modules   []string `json:"modules"`
	Utilities []string `json:"utilities"`
}
type Registration struct {
	RobotID string  `json:"robotId"`
	Name    string  `json:"name"`
	Team    string  `json:"team"`
	Bot     bool    `json:"bot"`
	Loadout Loadout `json:"loadout"`
}
type Config struct {
	MatchID         string  `json:"matchId"`
	Mode            string  `json:"mode"`
	Width           float64 `json:"width"`
	Height          float64 `json:"height"`
	Capacity        int     `json:"capacity"`
	DurationSeconds int     `json:"durationSeconds"`
	Seed            uint64  `json:"seed"`
	FriendlyFire    bool    `json:"friendlyFire"`
	LiveEdit        bool    `json:"liveEdit"`
	SiteCount       int     `json:"siteCount"`
	CoverPerSite    int     `json:"coverPerSite"`
	LootPerSite     int     `json:"lootPerSite"`
	// TeamSize is robots per team in br-squad: 2 duo, 3 trio, 4 squad.
	TeamSize int `json:"teamSize,omitempty"`
	// Bots caps the bots added at start; nil fills every empty slot.
	Bots   *int           `json:"bots,omitempty"`
	Robots []Registration `json:"robots,omitempty"`
}

// SquadSize is the effective team size for br-squad (4 when unset).
func (c Config) SquadSize() int {
	if c.TeamSize <= 0 {
		return 4
	}
	return c.TeamSize
}

func IsMode(mode string) bool {
	return mode == "br-solo" || mode == "br-squad" || mode == "sandbox" || mode == "quick-duel"
}
func (c *Config) Defaults() {
	if c.Mode == "" {
		c.Mode = "br-solo"
	}
	if c.Width == 0 {
		c.Width = 42000
	}
	if c.Height == 0 {
		c.Height = 26250
	}
	if c.Capacity == 0 {
		c.Capacity = 256
	}
	if c.DurationSeconds == 0 {
		c.DurationSeconds = 1080
	}
}
func (c Config) Validate() error {
	if !IsMode(c.Mode) {
		return errors.New("unknown v4 mode")
	}
	if math.IsNaN(c.Width) || math.IsInf(c.Width, 0) || math.IsNaN(c.Height) || math.IsInf(c.Height, 0) || c.Width < 1200 || c.Width > 48000 || c.Height < 750 || c.Height > 30000 {
		return errors.New("dimensions must be 1200..48000 by 750..30000")
	}
	if c.Capacity < 1 || c.Capacity > 256 || len(c.Robots) > c.Capacity {
		return errors.New("capacity must be 1..256")
	}
	if c.TeamSize < 0 || c.TeamSize > 8 {
		return errors.New("team size must be 1..8")
	}
	if c.Mode == "br-squad" && c.Capacity%c.SquadSize() != 0 {
		return errors.New("capacity must be a multiple of the team size")
	}
	if c.Bots != nil && (*c.Bots < 0 || *c.Bots > c.Capacity) {
		return errors.New("bots must be 0..capacity")
	}
	if c.DurationSeconds < 10 || c.DurationSeconds > 2700 {
		return errors.New("duration must be 10..2700 seconds")
	}
	if c.SiteCount < 0 || c.SiteCount > 256 {
		return errors.New("site count must be 0..256")
	}
	if c.CoverPerSite < 0 || c.CoverPerSite > 12 {
		return errors.New("cover per site must be 0..12")
	}
	if c.LootPerSite < 0 || c.LootPerSite > 32 {
		return errors.New("loot per site must be 0..32")
	}
	return nil
}
func (l *Loadout) Defaults() {
	if l.Chassis == "" {
		l.Chassis = "generalist"
	}
	if l.Weapon == "" {
		l.Weapon = "plasma"
	}
	if l.Modules == nil {
		l.Modules = []string{}
	}
	if l.Utilities == nil {
		l.Utilities = []string{}
	}
}
func (l Loadout) Validate() error {
	c, ok := map[string]int{"scout": 10, "generalist": 15, "heavy": 25}[l.Chassis]
	if !ok {
		return errors.New("unknown chassis")
	}
	w, ok := map[string]int{"plasma": 10, "machine_gun": 15, "shotgun": 15, "cannon": 20, "railgun": 25, "grenade": 20, "incendiary": 15, "cryo": 15, "emp": 20}[l.Weapon]
	if !ok {
		return errors.New("unknown weapon")
	}
	c += w
	if len(l.Modules) > 2 || len(l.Utilities) > 2 {
		return errors.New("two module and utility slots")
	}
	seen := map[string]bool{}
	for _, m := range l.Modules {
		if !map[string]bool{"reinforced_plating": true, "optics": true, "capacitor": true, "cooling_system": true, "mobility_tuning": true, "shield_reservoir": true}[m] || seen[m] {
			return errors.New("unknown or duplicate module")
		}
		seen[m] = true
		c += 10
	}
	for _, u := range l.Utilities {
		v, ok := map[string]int{"cloak_emitter": 15, "mine_dispenser": 10, "smoke_projector": 10, "repair_field": 15}[u]
		if !ok || seen[u] {
			return errors.New("unknown or duplicate utility")
		}
		seen[u] = true
		c += v
	}
	if c > 60 {
		return fmt.Errorf("build costs %d; budget is 60", c)
	}
	return nil
}
func DecodeConfig(raw json.RawMessage) (Config, error) {
	var c Config
	err := json.Unmarshal(raw, &c)
	return c, err
}
