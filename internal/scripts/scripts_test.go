package scripts

import (
	"github.com/kryxen/cloud-robot/internal/boxes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListReturnsCuratedTemplates(t *testing.T) {
	templates := List()
	if len(templates) != 9 {
		t.Fatalf("expected 9 templates, got %d: %+v", len(templates), templates)
	}
	for _, template := range templates {
		if template.Description == "" {
			t.Fatalf("template %q missing description", template.Name)
		}
		if !strings.Contains(template.Source, "arena.run(") {
			t.Fatalf("template %q does not use the SDK runner", template.Name)
		}
		// Every Rust-only template must target the v4 protocol and use the
		// bounded navigation helpers so deployed scripts fit large maps and
		// the 16 KiB snapshot limit. arena.tactics asserts version 4 itself.
		if !strings.Contains(template.Source, "obs.version == 4") && !strings.Contains(template.Source, "arena.tactics(") {
			t.Fatalf("template %q does not target v4 arenas", template.Name)
		}
		if len(template.Source) > 16*1024 {
			t.Fatalf("template %q exceeds the 16 KiB snapshot cap", template.Name)
		}
	}
}

func TestExamplesMatchDeployableTemplates(t *testing.T) {
	for _, template := range List() {
		var examplePath string
		switch {
		case template.Name == "v4":
			examplePath = filepath.Join("..", "..", "examples", "lua-v4", "main.lua")
		case len(template.Files) > 0:
			examplePath = filepath.Join("..", "..", "examples", "lua-v4", template.Name, "main.lua")
			for rel, source := range template.Files {
				module, err := os.ReadFile(filepath.Join("..", "..", "examples", "lua-v4", template.Name, rel))
				if err != nil || string(module) != source {
					t.Fatalf("example module %s/%s drifted from its template", template.Name, rel)
				}
			}
		default:
			examplePath = filepath.Join("..", "..", "examples", "lua-v4", template.Name+".lua")
		}
		example, err := os.ReadFile(examplePath)
		if err != nil {
			t.Fatalf("read %s: %v", examplePath, err)
		}
		if string(example) != template.Source {
			t.Fatalf("example %q drifted from its deployable template", template.Name)
		}
	}
}

func TestGetTemplateAndUnknownRejection(t *testing.T) {
	source, err := Get("assault")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(source, "arena.tactics(") || !strings.Contains(source, "ASSAULT") {
		t.Fatalf("assault template lost its behavior: %q", source)
	}
	if _, err := Get("does-not-exist"); err == nil {
		t.Fatal("unknown template accepted")
	}
}

func TestMultiFileTemplatesShipTheirModules(t *testing.T) {
	for name, modules := range map[string][]string{
		"zone-runner":   {"brain/fsm.lua", "brain/sense.lua", "brain/plan.lua", "brain/act.lua"},
		"bounty-hunter": {"lib/memory.lua", "lib/pick.lua", "lib/aim.lua"},
	} {
		template, err := GetTemplate(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := boxes.ValidateModules(template.Files); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, module := range modules {
			if template.Files[module] == "" {
				t.Fatalf("%s is missing %s", name, module)
			}
			require := strings.TrimSuffix(strings.ReplaceAll(module, "/", "."), ".lua")
			if !strings.Contains(template.Source, `require "`+require+`"`) {
				t.Fatalf("%s main.lua does not require %s", name, require)
			}
		}
	}
}
