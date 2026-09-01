package scripts

import (
	"strings"
	"testing"
)

func TestListReturnsCuratedTemplates(t *testing.T) {
	templates := List()
	if len(templates) != 4 {
		t.Fatalf("expected 4 templates, got %d: %+v", len(templates), templates)
	}
	for _, template := range templates {
		if template.Description == "" {
			t.Fatalf("template %q missing description", template.Name)
		}
		if !strings.Contains(template.Source, "arena.run(") {
			t.Fatalf("template %q does not use the SDK runner", template.Name)
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
