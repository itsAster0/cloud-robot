package cloud

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/kryxen/cloud-robot/internal/model"
)

func TestStoredMatchKeepsPrivatePlayerMapping(t *testing.T) {
	original := model.Match{MatchID: "match-1", Robots: []model.RobotSubmission{{RobotID: "robot-1", PlayerID: "user-secret", DisplayName: "Public name"}}}
	payload, err := encodeStoredMatch(original)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(payload, []byte(`"playerId"`)) {
		t.Fatalf("public playerId field leaked into stored match: %s", payload)
	}
	restored, err := decodeStoredMatch(payload)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Robots[0].PlayerID != "user-secret" {
		t.Fatalf("player mapping lost: %+v", restored.Robots[0])
	}
	public, err := json.Marshal(restored)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(public, []byte("user-secret")) {
		t.Fatalf("private player ID leaked through model JSON: %s", public)
	}
}
