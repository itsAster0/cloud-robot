package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestV4MapPreviewRejectsBadConfig(t *testing.T) {
	h := newHarness(t)
	response, _ := h.request(t, http.MethodPost, "/api/v4/maps/preview", `{"mode":"br-solo","capacity":1,"width":100,"height":100,"durationSeconds":10}`)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", response.StatusCode)
	}
}

func TestV4MapPreviewRendersGeometry(t *testing.T) {
	path, err := filepath.Abs("../../crates/arena-engine/target/debug/arena-engine")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); err != nil {
		t.Skip("build Rust worker before integration tests")
	}
	t.Setenv("ARENA_ENGINE_PATH", path)
	h := newHarness(t)
	_, body := h.request(t, http.MethodPost, "/api/v4/maps/preview", `{"mode":"sandbox","capacity":8,"width":2400,"height":1500,"durationSeconds":180,"seed":7,"siteCount":4,"coverPerSite":6,"lootPerSite":12}`)
	raw, _ := json.Marshal(body)
	var preview struct {
		Sites     []any `json:"sites"`
		Obstacles []struct {
			ID string `json:"id"`
		} `json:"obstacles"`
		Containers []any `json:"containers"`
		Hazards    []any `json:"hazards"`
	}
	if err := json.Unmarshal(raw, &preview); err != nil {
		t.Fatal(err)
	}
	core, district := 0, 0
	for _, o := range preview.Obstacles {
		if strings.HasPrefix(o.ID, "district-") {
			district++
		} else {
			core++
		}
	}
	if len(preview.Sites) != 4 || core != 44 || district == 0 || len(preview.Containers) != 48 {
		t.Fatalf("unexpected preview geometry: %+v", body)
	}
	if len(preview.Hazards) == 0 {
		t.Fatal("preview missing hazard fields")
	}
}
