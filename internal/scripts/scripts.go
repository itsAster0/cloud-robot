// Package scripts holds the deployable demo robots offered in the box
// console. Templates are embedded so the API binary stays self-contained;
// examples/ mirrors two of them for manual SSH users.
package scripts

import (
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed templates/*.lua
var files embed.FS

type Template struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
}

// Descriptions double as the allow-list: a template without an entry here is
// not deployable even if a .lua file somehow lands in the directory.
// Rust-only v4 strategies; legacy Go-engine scripts were removed.
var descriptions = map[string]string{
	"v4":       "Balanced SDK 0.4 strategy for Rust arenas: combat, recovery, loot, and bounded navigation.",
	"scout":    "Fast recon: cruises transit sites, pulses scans, relays positions, breaks contact early.",
	"assault":  "Balanced brawler: engages nearest, dashes out at low HP, channels heals, scavenges loot.",
	"sniper":   "Railgun control: holds 600-900 range, brakes to aim, cloaks to re-range, seeks optics.",
	"support":  "Squad medic: trails allies, drops repair fields, screens with smoke, relays state.",
	"sentinel": "Area denial: holds the safe zone, prioritizes shields, lays mines, sweeps scans.",
	"scavenger": "Loot runner: prioritizes upgrades, equips weapons explicitly, rides transit, fights close only.",
}

func List() []Template {
	entries, err := files.ReadDir("templates")
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := strings.TrimSuffix(entry.Name(), ".lua")
		if descriptions[name] != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	templates := make([]Template, 0, len(names))
	for _, name := range names {
		source, readErr := files.ReadFile("templates/" + name + ".lua")
		if readErr != nil {
			continue
		}
		templates = append(templates, Template{Name: name, Description: descriptions[name], Source: string(source)})
	}
	return templates
}

func Get(name string) (string, error) {
	if descriptions[name] == "" {
		return "", fmt.Errorf("unknown script template %q", name)
	}
	source, err := files.ReadFile("templates/" + name + ".lua")
	if err != nil {
		return "", fmt.Errorf("script template %q is missing", name)
	}
	return string(source), nil
}
