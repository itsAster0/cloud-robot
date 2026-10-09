// Package scripts holds the deployable demo robots offered in the box
// console. Templates are embedded so the API binary stays self-contained;
// examples/ mirrors two of them for manual SSH users.
package scripts

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed templates
var files embed.FS

// Template is a deployable strategy. Source is main.lua; Files holds extra
// modules for multi-file templates (templates/<name>/ directories).
type Template struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Source      string            `json:"source"`
	Files       map[string]string `json:"files,omitempty"`
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
	// Multi-file strategies: main.lua plus modules, to learn project layout.
	"zone-runner":   "Multi-file: a state machine (sense, plan, act) that rides the drifting zone, hunts bounties, and heals.",
	"bounty-hunter": "Multi-file: remembers every robot on a kill streak, predicts where it went, and snipes it with fire discipline.",
}

func List() []Template {
	names := make([]string, 0, len(descriptions))
	for name := range descriptions {
		names = append(names, name)
	}
	sort.Strings(names)
	templates := make([]Template, 0, len(names))
	for _, name := range names {
		if template, err := GetTemplate(name); err == nil {
			templates = append(templates, template)
		}
	}
	return templates
}

// Get returns a template's main.lua.
func Get(name string) (string, error) {
	template, err := GetTemplate(name)
	return template.Source, err
}

// GetTemplate loads templates/<name>.lua, or templates/<name>/main.lua with
// every other .lua file in that directory as a module.
func GetTemplate(name string) (Template, error) {
	if descriptions[name] == "" {
		return Template{}, fmt.Errorf("unknown script template %q", name)
	}
	template := Template{Name: name, Description: descriptions[name]}
	if source, err := files.ReadFile("templates/" + name + ".lua"); err == nil {
		template.Source = string(source)
		return template, nil
	}
	root := "templates/" + name
	main, err := files.ReadFile(root + "/main.lua")
	if err != nil {
		return Template{}, fmt.Errorf("script template %q is missing", name)
	}
	template.Source, template.Files = string(main), map[string]string{}
	err = fs.WalkDir(files, root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(path, ".lua") {
			return walkErr
		}
		rel := strings.TrimPrefix(path, root+"/")
		if rel == "main.lua" {
			return nil
		}
		data, readErr := files.ReadFile(path)
		template.Files[rel] = string(data)
		return readErr
	})
	if err != nil {
		return Template{}, err
	}
	return template, nil
}
