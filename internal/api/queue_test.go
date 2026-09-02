package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kryxen/cloud-robot/internal/model"
)

// newQueueServer wires a server against fakes and stretches the queue timers
// so background goroutines spawned by requeueing stay dormant for the test run.
func newQueueServer(t *testing.T) (*Server, *fakeStore) {
	t.Helper()
	t.Setenv("QUEUE_CONNECT_GRACE_SECONDS", "3600")
	t.Setenv("QUEUE_BOT_FILL_SECONDS", "3600")
	store := newFakeStore()
	app := NewServer(store, &fakeProvisioner{})
	app.auth = &fakeAuth{}
	return app, store
}

// seedQueuedPair stores a lobby match with two human robots, their script
// snapshots, box records, and matched queue entries, as the duel queue leaves
// them right after pairing.
func seedQueuedPair(t *testing.T, app *Server, store *fakeStore) model.Match {
	t.Helper()
	match := model.Match{
		MatchID: "qm1", OwnerID: "alice", Status: model.MatchLobby, Mode: "duel",
		MapID: "open-field", ArenaWidth: 800, ArenaHeight: 500, TickRate: 10,
		Robots: []model.RobotSubmission{
			{RobotID: "qr1", PlayerID: "alice", OwnerBoxID: "box-alice", DisplayName: "Alice", Team: "red", ScriptObjectKey: "scripts/box-alice/qm1/qr1/main.lua", StartCommand: "lua main.lua", Runtime: "lua5.4"},
			{RobotID: "qr2", PlayerID: "bob", OwnerBoxID: "box-bob", DisplayName: "Bob", Team: "blue", ScriptObjectKey: "scripts/box-bob/qm1/qr2/main.lua", StartCommand: "lua main.lua", Runtime: "lua5.4"},
		},
	}
	if err := store.PutMatch(context.Background(), match); err != nil {
		t.Fatal(err)
	}
	store.scripts[match.Robots[0].ScriptObjectKey] = "alice script"
	store.scripts[match.Robots[1].ScriptObjectKey] = "bob script"
	aliceBox := model.BoxRecord{BoxID: "box-alice", Status: "running", KeyFingerprint: "SHA256:a", ActiveRobotID: "qr1", ActiveMatchID: "qm1"}
	bobBox := model.BoxRecord{BoxID: "box-bob", Status: "running", KeyFingerprint: "SHA256:b", ActiveRobotID: "qr2", ActiveMatchID: "qm1"}
	store.boxes["alice"] = aliceBox
	store.boxes["bob"] = bobBox
	app.queue.entries["alice"] = &queueEntry{UserID: "alice", Box: aliceBox, Source: "alice script", Request: queueRequest{DisplayName: "Alice", Mode: "duel", Runtime: "lua5.4", StartCommand: "lua main.lua"}, Status: "matched", MatchID: "qm1"}
	app.queue.entries["bob"] = &queueEntry{UserID: "bob", Box: bobBox, Source: "bob script", Request: queueRequest{DisplayName: "Bob", Mode: "duel", Runtime: "lua5.4", StartCommand: "lua main.lua"}, Status: "matched", MatchID: "qm1"}
	return match
}

func TestCancelledQueuedMatchRequeuesPlayers(t *testing.T) {
	app, store := newQueueServer(t)
	seedQueuedPair(t, app, store)

	app.cancelQueuedMatch(context.Background(), "qm1", "agent connection grace window expired")

	if store.matches["qm1"].Status != model.MatchFailed || store.matches["qm1"].Error != "agent connection grace window expired" {
		t.Fatalf("match not failed with reason: %+v", store.matches["qm1"])
	}
	alice, bob := app.queue.entries["alice"], app.queue.entries["bob"]
	if alice == nil || bob == nil {
		t.Fatalf("players not requeued: %+v", app.queue.entries)
	}
	if alice.Status != "matched" || bob.Status != "matched" || alice.MatchID == "" || alice.MatchID != bob.MatchID {
		t.Fatalf("requeued players did not re-pair FIFO: alice=%+v bob=%+v", alice, bob)
	}
	if alice.MatchID == "qm1" {
		t.Fatal("requeue reused the cancelled match")
	}
	fresh, err := store.GetMatch(context.Background(), alice.MatchID)
	if err != nil || fresh.Status != model.MatchLobby {
		t.Fatalf("fresh match missing or not in lobby: %+v %v", fresh, err)
	}
	if len(fresh.Robots) != 2 {
		t.Fatalf("fresh match roster wrong: %+v", fresh.Robots)
	}
	// Robot selections survive the round trip.
	if alice.Request.DisplayName != "Alice" || alice.Request.StartCommand != "lua main.lua" || alice.Source != "alice script" {
		t.Fatalf("alice selection not preserved: %+v", alice)
	}
	if bob.Request.DisplayName != "Bob" || bob.Source != "bob script" {
		t.Fatalf("bob selection not preserved: %+v", bob)
	}
	expectedScript := map[string]string{"alice": "alice script", "bob": "bob script"}
	for _, robot := range fresh.Robots {
		script := store.scripts[robot.ScriptObjectKey]
		if script != expectedScript[robot.PlayerID] {
			t.Fatalf("robot %s script snapshot not carried over: %q", robot.DisplayName, script)
		}
	}
}

func TestLeaveQueueRequeuesPartnerButNotLeaver(t *testing.T) {
	app, store := newQueueServer(t)
	seedQueuedPair(t, app, store)

	request := httptest.NewRequest(http.MethodDelete, "/api/queue", nil)
	request.Header.Set("Authorization", "Bearer alice-token")
	recorder := httptest.NewRecorder()
	app.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("leave queue failed: %d %s", recorder.Code, recorder.Body.String())
	}

	if app.queue.entries["alice"] != nil {
		t.Fatalf("leaver was requeued: %+v", app.queue.entries["alice"])
	}
	bob := app.queue.entries["bob"]
	if bob == nil || bob.Status != "waiting" || bob.MatchID != "" {
		t.Fatalf("abandoned partner not waiting after leave: %+v", bob)
	}
	if bob.Request.DisplayName != "Bob" || bob.Request.StartCommand != "lua main.lua" || bob.Source != "bob script" {
		t.Fatalf("partner selection not preserved: %+v", bob)
	}
	if store.matches["qm1"].Status != model.MatchFailed {
		t.Fatalf("match not cancelled: %+v", store.matches["qm1"])
	}
	if store.boxes["bob"].ActiveMatchID != "" {
		t.Fatalf("partner box markers not released: %+v", store.boxes["bob"])
	}
}

func TestGraceExpiryRequeuesSoloPlayer(t *testing.T) {
	app, store := newQueueServer(t)
	t.Setenv("QUEUE_CONNECT_GRACE_SECONDS", "1")
	match := model.Match{
		MatchID: "qm1", OwnerID: "alice", Status: model.MatchLobby, Mode: "duel",
		Robots: []model.RobotSubmission{
			{RobotID: "qr1", PlayerID: "alice", OwnerBoxID: "box-alice", DisplayName: "Alice", Team: "red", ScriptObjectKey: "scripts/box-alice/qm1/qr1/main.lua", StartCommand: "lua main.lua", Runtime: "lua5.4"},
		},
	}
	if err := store.PutMatch(context.Background(), match); err != nil {
		t.Fatal(err)
	}
	store.scripts[match.Robots[0].ScriptObjectKey] = "alice script"
	store.boxes["alice"] = model.BoxRecord{BoxID: "box-alice", Status: "running", KeyFingerprint: "SHA256:a", ActiveRobotID: "qr1", ActiveMatchID: "qm1"}
	app.queue.entries["alice"] = &queueEntry{UserID: "alice", Box: store.boxes["alice"], Source: "alice script", Request: queueRequest{DisplayName: "Alice", Mode: "duel", Runtime: "lua5.4", StartCommand: "lua main.lua"}, Status: "matched", MatchID: "qm1"}

	app.enforceConnectGrace("qm1", []string{"alice"})

	if store.matches["qm1"].Status != model.MatchFailed || store.matches["qm1"].Error != "agent connection grace window expired" {
		t.Fatalf("grace expiry did not fail the match: %+v", store.matches["qm1"])
	}
	entry := app.queue.entries["alice"]
	if entry == nil || entry.Status != "waiting" || entry.MatchID != "" {
		t.Fatalf("player not requeued after grace expiry: %+v", entry)
	}
	if entry.Request.DisplayName != "Alice" || entry.Source != "alice script" || entry.Request.Runtime != "lua5.4" {
		t.Fatalf("requeued selection not preserved: %+v", entry)
	}
}

func TestCancelledSoloQueuedMatchRequeuesAsWaiting(t *testing.T) {
	app, store := newQueueServer(t)
	// Bot-fill failure shape: a lobby match with one human robot and no live
	// queue entry (the pairing entry was already dropped).
	match := model.Match{
		MatchID: "qm1", OwnerID: "alice", Status: model.MatchLobby, Mode: "duel",
		Robots: []model.RobotSubmission{
			{RobotID: "qr1", PlayerID: "alice", OwnerBoxID: "box-alice", DisplayName: "Alice", Team: "red", ScriptObjectKey: "scripts/box-alice/qm1/qr1/main.lua", StartCommand: "lua main.lua", Runtime: "lua5.4"},
		},
	}
	if err := store.PutMatch(context.Background(), match); err != nil {
		t.Fatal(err)
	}
	store.scripts[match.Robots[0].ScriptObjectKey] = "alice script"
	store.boxes["alice"] = model.BoxRecord{BoxID: "box-alice", Status: "running", KeyFingerprint: "SHA256:a", ActiveRobotID: "qr1", ActiveMatchID: "qm1"}

	app.cancelQueuedMatch(context.Background(), "qm1", "bot fill failed")

	entry := app.queue.entries["alice"]
	if entry == nil || entry.Status != "waiting" || entry.MatchID != "" {
		t.Fatalf("solo player not requeued as waiting: %+v", entry)
	}
	if entry.Request.DisplayName != "Alice" || entry.Source != "alice script" || entry.Request.StartCommand != "lua main.lua" {
		t.Fatalf("solo player selection not preserved: %+v", entry)
	}
	if store.boxes["alice"].ActiveMatchID != "" {
		t.Fatalf("box markers not released before requeue: %+v", store.boxes["alice"])
	}
}

func TestFailMatchRequeuesOnlyDuelQueueMatches(t *testing.T) {
	app, store := newQueueServer(t)
	// A manual match has no duel-queue provenance: failing it must not
	// create any queue entries.
	manual := model.Match{
		MatchID: "m-manual", Status: model.MatchQueued, Mode: "duel",
		Robots: []model.RobotSubmission{
			{RobotID: "mr1", PlayerID: "alice", DisplayName: "Alice", Team: "red", ScriptObjectKey: "scripts/box-alice/m-manual/mr1/main.lua", StartCommand: "lua main.lua", Runtime: "lua5.4"},
			{RobotID: "mr2", PlayerID: "bob", DisplayName: "Bob", Team: "blue", ScriptObjectKey: "scripts/box-bob/m-manual/mr2/main.lua", StartCommand: "lua main.lua", Runtime: "lua5.4"},
		},
	}
	if err := store.PutMatch(context.Background(), manual); err != nil {
		t.Fatal(err)
	}
	if err := app.failMatch(context.Background(), manual, errors.New("robot agent mr1 disconnected")); err == nil {
		t.Fatal("failMatch should report the failure")
	}
	if len(app.queue.entries) != 0 {
		t.Fatalf("manual match requeued players: %+v", app.queue.entries)
	}

	// A duel-queue match with live queue entries requeues its players.
	match := seedQueuedPair(t, app, store)
	match.Status = model.MatchQueued
	if err := store.PutMatch(context.Background(), match); err != nil {
		t.Fatal(err)
	}
	app.failMatch(context.Background(), match, errors.New("robot agent qr1 disconnected"))
	alice, bob := app.queue.entries["alice"], app.queue.entries["bob"]
	if alice == nil || bob == nil || alice.MatchID == "" || alice.MatchID == "qm1" || alice.MatchID != bob.MatchID {
		t.Fatalf("queue match players not requeued: alice=%+v bob=%+v", alice, bob)
	}
}

func TestRequeueSkipsBoxRegisteredInAnotherLiveMatch(t *testing.T) {
	app, store := newQueueServer(t)
	seedQueuedPair(t, app, store)
	// Alice registered her box into another live match while qm1 sat unstarted.
	if err := store.PutMatch(context.Background(), model.Match{MatchID: "m2", Status: model.MatchRunning}); err != nil {
		t.Fatal(err)
	}
	aliceBox := store.boxes["alice"]
	aliceBox.ActiveRobotID, aliceBox.ActiveMatchID = "qr-other", "m2"
	store.boxes["alice"] = aliceBox

	app.cancelQueuedMatch(context.Background(), "qm1", "agent connection grace window expired")

	if app.queue.entries["alice"] != nil {
		t.Fatalf("box in another live match was requeued: %+v", app.queue.entries["alice"])
	}
	bob := app.queue.entries["bob"]
	if bob == nil || bob.Status != "waiting" || bob.MatchID != "" {
		t.Fatalf("unaffected player not requeued: %+v", bob)
	}
	if store.boxes["alice"].ActiveMatchID != "m2" {
		t.Fatalf("live match binding must be preserved: %+v", store.boxes["alice"])
	}
}

// A queue entry must not outlive its match. The web client auto-redirects a
// "matched" player from /play to the entry's match, so an entry left behind
// after the match started bounces the player back to the finished match page
// forever — the stuck-after-duel bug.
func TestEnforceConnectGraceClearsEntriesAfterMatchStarts(t *testing.T) {
	app, store := newQueueServer(t)
	t.Setenv("QUEUE_CONNECT_GRACE_SECONDS", "1")
	match := seedQueuedPair(t, app, store)
	match.Status = model.MatchRunning
	if err := store.PutMatch(context.Background(), match); err != nil {
		t.Fatal(err)
	}

	app.enforceConnectGrace("qm1", []string{"alice", "bob"})

	if app.queue.entries["alice"] != nil || app.queue.entries["bob"] != nil {
		t.Fatalf("started match left queue entries behind: %+v", app.queue.entries)
	}
	if store.matches["qm1"].Status != model.MatchRunning {
		t.Fatalf("grace timer must not touch a started match: %+v", store.matches["qm1"])
	}
}

// The worker owns the normal start path, so it must drop the queue entries
// when the match flips from queued to running.
func TestRunMatchClearsQueueEntries(t *testing.T) {
	app, store := newQueueServer(t)
	match := seedQueuedPair(t, app, store)
	// Swap the humans for dummy bots so the worker can simulate the duel
	// without agent connections; the queue entries still bind to the match.
	match.Status = model.MatchQueued
	match.TickRate = 2000
	for i := range match.Robots {
		match.Robots[i].Bot = true
		match.Robots[i].StartCommand = "bot:dummy"
		match.Robots[i].PlayerID = ""
		match.Robots[i].OwnerBoxID = ""
	}
	if err := store.PutMatch(context.Background(), match); err != nil {
		t.Fatal(err)
	}

	if err := app.runMatch(context.Background(), "qm1"); err != nil {
		t.Fatalf("run match failed: %v", err)
	}

	if app.queue.entries["alice"] != nil || app.queue.entries["bob"] != nil {
		t.Fatalf("match start left queue entries behind: %+v", app.queue.entries)
	}
	if store.matches["qm1"].Status != model.MatchFinished {
		t.Fatalf("match did not finish: %+v", store.matches["qm1"])
	}
}

// A cancelled match rebuilds fresh entries pointing at a new match id; the
// grace timer of the old match must leave those alone.
func TestGraceCleanupSkipsRepairedEntries(t *testing.T) {
	app, store := newQueueServer(t)
	seedQueuedPair(t, app, store)
	// Alice and Bob were requeued into a different match after a cancel.
	app.queue.entries["alice"].MatchID = "qm2"
	app.queue.entries["bob"].MatchID = "qm2"

	app.clearQueueEntriesForMatch("qm1")

	if app.queue.entries["alice"] == nil || app.queue.entries["bob"] == nil {
		t.Fatalf("re-paired entries were dropped: %+v", app.queue.entries)
	}
	app.clearQueueEntriesForMatch("qm2")
	if app.queue.entries["alice"] != nil || app.queue.entries["bob"] != nil {
		t.Fatalf("entries for the cleared match survived: %+v", app.queue.entries)
	}
}
