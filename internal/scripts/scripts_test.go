package scripts

import (
	"strings"
	"testing"
)

func TestListReturnsCuratedTemplates(t *testing.T) {
	templates := List()
	if len(templates) != 6 {
		t.Fatalf("expected 6 templates, got %d: %+v", len(templates), templates)
	}
	for _, template := range templates {
		if template.Description == "" {
			t.Fatalf("template %q missing description", template.Name)
		}
		if !strings.Contains(template.Source, "arena.run(") {
			t.Fatalf("template %q does not use the SDK runner", template.Name)
		}
		// Every template must be item-aware, zone-aware, and size-capped so a
		// deployed script still fits the 16 KiB snapshot limit.
		if !strings.Contains(template.Source, "obs.items") {
			t.Fatalf("template %q ignores items", template.Name)
		}
		if !strings.Contains(template.Source, "obs.zone") {
			t.Fatalf("template %q ignores the zone", template.Name)
		}
		if len(template.Source) > 16*1024 {
			t.Fatalf("template %q exceeds the 16 KiB snapshot cap", template.Name)
		}
	}
}

func TestGetTemplateAndUnknownRejection(t *testing.T) {
	source, err := Get("aggressive")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(source, "arena.approach") {
		t.Fatalf("aggressive template lost its behavior: %q", source)
	}
	if _, err := Get("does-not-exist"); err == nil {
		t.Fatal("unknown template accepted")
	}
}
