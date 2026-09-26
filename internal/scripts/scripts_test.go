package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListReturnsCuratedTemplates(t *testing.T) {
	templates := List()
	if len(templates) != 7 {
		t.Fatalf("expected 7 templates, got %d: %+v", len(templates), templates)
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
		if template.Name == "v4" {
			examplePath = filepath.Join("..", "..", "examples", "lua-v4", "main.lua")
		} else {
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
