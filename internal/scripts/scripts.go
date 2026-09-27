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
	"v4":        "Balanced: fights at weapon range, takes cover when hurt, loots, hunts remembered enemies, and patrols the zone.",
	"assault":   "Close-range brawler: engages early, dashes to close the gap, drops mines while backing off.",
	"scavenger": "Loot first: long detours for weapons and supplies, rides transit, then fights with what it found.",
	"scout":     "Fast recon: scans constantly, rides transit, reports contacts to its squad, breaks off early.",
	"sentinel":  "Zone denial: walks a beat around the post nearest the zone centre and mines approaches.",
	"sniper":    "Long range: holds 600-950 units for steady led shots, re-ranges when rushed, cloaks when hurt.",
	"support":   "Team player: trails allies, drops repair fields, smokes retreats, relays squad reports.",
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
