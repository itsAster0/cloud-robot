package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	robotauth "github.com/kryxen/cloud-robot/internal/auth"
	"github.com/kryxen/cloud-robot/internal/boxes"
	"github.com/kryxen/cloud-robot/internal/cloud"
	"github.com/kryxen/cloud-robot/internal/engine"
	"github.com/kryxen/cloud-robot/internal/model"
	"github.com/kryxen/cloud-robot/internal/scripts"
)

const testUserID = "user_reviewer"

var testBoxID = boxes.IDForUser(testUserID)

type fakeStore struct {
	matches     map[string]model.Match
	boxes       map[string]model.BoxRecord
	credentials map[string]cloud.AgentCredential
	scripts     map[string]string
	queue       []string
	readyErr    error
	putMatchErr error
	players     map[string]model.PlayerStats
	replays     map[string][]model.MatchEvent
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		matches:     map[string]model.Match{},
		boxes:       map[string]model.BoxRecord{},
		credentials: map[string]cloud.AgentCredential{},
		scripts:     map[string]string{},
		players:     map[string]model.PlayerStats{},
		replays:     map[string][]model.MatchEvent{},
	}
}

func (f *fakeStore) Ready(context.Context) error { return f.readyErr }
func (f *fakeStore) Status() map[string]string   { return map[string]string{"provider": "fake"} }
func (f *fakeStore) PutMatch(_ context.Context, match model.Match) error {
	if f.putMatchErr != nil {
		return f.putMatchErr
	}
	f.matches[match.MatchID] = match
	return nil
}
func (f *fakeStore) GetMatch(_ context.Context, matchID string) (model.Match, error) {
	match, ok := f.matches[matchID]
	if !ok {
		return model.Match{}, errors.New("match not found")
	}
	return match, nil
}
func (f *fakeStore) ListMatches(_ context.Context, status model.MatchStatus, limit int) ([]model.Match, error) {
	result := make([]model.Match, 0, len(f.matches))
	for _, match := range f.matches {
		if status == "" || match.Status == status {
			result = append(result, match)
		}
	}
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}
func (f *fakeStore) PutPlayerStats(_ context.Context, stats model.PlayerStats) error {
	f.players[stats.PlayerID] = stats
	return nil
}
func (f *fakeStore) GetPlayerStats(_ context.Context, playerID string) (model.PlayerStats, error) {
	stats, ok := f.players[playerID]
	if !ok {
		return model.PlayerStats{}, errors.New("player not found")
	}
	return stats, nil
}
func (f *fakeStore) ListPlayerStats(_ context.Context, limit int) ([]model.PlayerStats, error) {
	result := make([]model.PlayerStats, 0, len(f.players))
	for _, player := range f.players {
		result = append(result, player)
	}
	return result, nil
}
func (f *fakeStore) PutReplay(_ context.Context, key string, events []model.MatchEvent) error {
	f.replays[key] = append([]model.MatchEvent(nil), events...)
	return nil
}
func (f *fakeStore) GetReplay(_ context.Context, key string) ([]model.MatchEvent, error) {
	events, ok := f.replays[key]
	if !ok {
		return nil, errors.New("replay not found")
	}
	return events, nil
}
func (f *fakeStore) PutBox(_ context.Context, userID string, box model.BoxRecord) error {
	f.boxes[userID] = box
	return nil
}
func (f *fakeStore) GetBox(_ context.Context, userID string) (model.BoxRecord, error) {
	box, ok := f.boxes[userID]
	if !ok {
		return model.BoxRecord{}, errors.New("box not found")
	}
	return box, nil
}
func (f *fakeStore) PutScript(_ context.Context, key, source string) error {
	f.scripts[key] = source
	return nil
}
func (f *fakeStore) GetScript(_ context.Context, key string) (string, error) {
	source, ok := f.scripts[key]
	if !ok {
		return "", errors.New("script not found")
	}
	return source, nil
}
func (f *fakeStore) ListScriptVersions(_ context.Context, boxID string, limit int) ([]model.ScriptVersion, error) {
	prefix := "versions/" + boxID + "/"
	result := []model.ScriptVersion{}
	for key := range f.scripts {
		if strings.HasPrefix(key, prefix) {
			parts := strings.Split(strings.TrimPrefix(key, prefix), "/")
			if len(parts) == 2 {
				result = append(result, model.ScriptVersion{VersionID: parts[0], ObjectKey: key})
			}
		}
	}
	return result, nil
}
func (f *fakeStore) PutAgentCredential(_ context.Context, credential cloud.AgentCredential) error {
	f.credentials[credential.RobotID] = credential
	return nil
}
func (f *fakeStore) GetAgentCredential(_ context.Context, robotID string) (cloud.AgentCredential, error) {
	credential, ok := f.credentials[robotID]
	if !ok {
		return cloud.AgentCredential{}, errors.New("agent credential not found")
	}
	return credential, nil
}
func (f *fakeStore) EnqueueMatch(_ context.Context, matchID string) error {
	f.queue = append(f.queue, matchID)
	return nil
}
func (f *fakeStore) ReceiveJob(context.Context) (cloud.Job, bool, error) {
	return cloud.Job{}, false, nil
}
func (f *fakeStore) DeleteJob(context.Context, string) error { return nil }

type fakeProvisioner struct {
	box          model.BoxRecord
	ensureErr    error
	statusErr    error
	setKeyErr    error
	configErr    error
	restartErr   error
	readMain     string
	writeMainErr error
	wroteMain    []string
	configured   []boxes.AgentConfig
	keys         []string
}

func (f *fakeProvisioner) Ensure(context.Context, string) (model.BoxRecord, error) {
	if f.ensureErr != nil {
		return model.BoxRecord{}, f.ensureErr
	}
	return f.box, nil
}
func (f *fakeProvisioner) Status(context.Context, string) (model.BoxRecord, error) {
	if f.statusErr != nil {
		return model.BoxRecord{}, f.statusErr
	}
	return f.box, nil
}
func (f *fakeProvisioner) SetKey(_ context.Context, _, key string) (model.BoxRecord, error) {
	if f.setKeyErr != nil {
		return model.BoxRecord{}, f.setKeyErr
	}
	f.keys = append(f.keys, key)
	f.box.KeyFingerprint = "SHA256:test"
	return f.box, nil
}
func (f *fakeProvisioner) ConfigureAgent(_ context.Context, _ string, config boxes.AgentConfig) (model.BoxRecord, error) {
	if f.configErr != nil {
		return model.BoxRecord{}, f.configErr
	}
	f.configured = append(f.configured, config)
	f.box.AgentStatus = "starting"
	// Mirror box-supervisor: configuring an agent sets the active markers.
	f.box.ActiveRobotID, f.box.ActiveMatchID = config.RobotID, config.MatchID
	return f.box, nil
}
func (f *fakeProvisioner) Restart(context.Context, string) (model.BoxRecord, error) {
	if f.restartErr != nil {
		return model.BoxRecord{}, f.restartErr
	}
	return f.box, nil
}
func (f *fakeProvisioner) ReadMain(context.Context, string) (string, error) {
	if f.readMain == "" {
		return "", errors.New("main.lua missing")
	}
	return f.readMain, nil
}

func (f *fakeProvisioner) WriteMain(_ context.Context, _, source string) (string, error) {
	if f.writeMainErr != nil {
		return "", f.writeMainErr
	}
	f.wroteMain = append(f.wroteMain, source)
	return source, nil
}

type fakeAuth struct{ required bool }

func (f *fakeAuth) Verify(ctx context.Context, authorization string) (context.Context, error) {
	token := strings.TrimPrefix(authorization, "Bearer ")
	if token == "" {
		if f.required {
			return ctx, errors.New("sign in required")
		}
		return robotauth.WithUserID(ctx, "guest"), nil
	}
	if token == "alice-token" {
		return robotauth.WithUserID(ctx, "alice"), nil
	}
	return robotauth.WithUserID(ctx, testUserID), nil
}

type harness struct {
	store       *fakeStore
	provisioner *fakeProvisioner
	app         *Server
	server      *httptest.Server
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{store: newFakeStore(), provisioner: &fakeProvisioner{}}
	h.app = NewServer(h.store, h.provisioner)
	h.app.auth = &fakeAuth{}
	h.server = httptest.NewServer(h.app.Handler())
	t.Cleanup(h.server.Close)
	return h
}

func (h *harness) request(t *testing.T, method, path, body string) (*http.Response, map[string]any) {
	t.Helper()
	return h.requestAs(t, method, path, body, true)
}

func (h *harness) requestAs(t *testing.T, method, path, body string, authenticated bool) (*http.Response, map[string]any) {
	t.Helper()
	request, err := http.NewRequest(method, h.server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if authenticated {
		request.Header.Set("Authorization", "Bearer user-token")
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	return response, payload
}

func readyBox() model.BoxRecord {
	return model.BoxRecord{
		BoxID: testBoxID, Status: "running", SSHHost: "localhost", SSHPort: 22000, SSHUser: "developer",
		KeyFingerprint: "SHA256:test", AgentStatus: "idle",
		Limits: model.BoxLimits{CPUs: 1, MemoryMB: 512, StorageBytes: boxes.DefaultStorageBytes, PIDs: 128},
	}
}

func TestEnsureBoxStoresRecord(t *testing.T) {
	h := newHarness(t)
	h.provisioner.box = readyBox()
	response, payload := h.request(t, http.MethodPost, "/api/me/box", "")
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("ensure box status %d payload %v", response.StatusCode, payload)
	}
	if h.store.boxes[testUserID].BoxID != testBoxID {
		t.Fatalf("box record not persisted: %+v", h.store.boxes)
	}
}

func TestEnsureBoxReportsProvisionerFailure(t *testing.T) {
	h := newHarness(t)
	h.provisioner.ensureErr = errors.New("docker unavailable")
	response, payload := h.request(t, http.MethodPost, "/api/me/box", "")
	if response.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d %v", response.StatusCode, payload)
	}
	if payload["error"] != "docker unavailable" {
		t.Fatalf("unexpected error message %v", payload["error"])
	}
}

func TestGetBoxWithoutProvisioning(t *testing.T) {
	h := newHarness(t)
	h.provisioner.statusErr = errors.New("No such object")
	response, _ := h.request(t, http.MethodGet, "/api/me/box", "")
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", response.StatusCode)
	}
}

// Box markers must self-heal: a binding to a match the store no longer has (or
// that already ended) would otherwise block queueing forever and offer a
// withdraw button that always fails with "match not found".
func TestGetBoxClearsStaleMatchMarker(t *testing.T) {
	for name, status := range map[string]model.MatchStatus{"missing": "", "finished": "finished", "failed": "failed"} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.provisioner.box = readyBox()
			stored := readyBox()
			stored.ActiveRobotID, stored.ActiveMatchID = "r-old", "m-gone"
			if err := h.store.PutBox(context.Background(), testUserID, stored); err != nil {
				t.Fatal(err)
			}
			if status != "" {
				if err := h.store.PutMatch(context.Background(), model.Match{MatchID: "m-gone", Status: status}); err != nil {
					t.Fatal(err)
				}
			}

			response, payload := h.request(t, http.MethodGet, "/api/me/box", "")
			if response.StatusCode != http.StatusOK {
				t.Fatalf("get box failed: %d %v", response.StatusCode, payload)
			}
			if payload["activeMatchId"] != nil || payload["activeRobotId"] != nil {
				t.Fatalf("stale marker not cleared: %v", payload)
			}
			cleared := h.store.boxes[testUserID]
			if cleared.ActiveMatchID != "" || cleared.ActiveRobotID != "" {
				t.Fatalf("stale marker not persisted: %+v", cleared)
			}
		})
	}
}

func TestGetBoxKeepsLiveMatchMarker(t *testing.T) {
	h := newHarness(t)
	h.provisioner.box = readyBox()
	stored := readyBox()
	stored.ActiveRobotID, stored.ActiveMatchID = "r1", "m1"
	if err := h.store.PutBox(context.Background(), testUserID, stored); err != nil {
		t.Fatal(err)
	}
	if err := h.store.PutMatch(context.Background(), model.Match{MatchID: "m1", Status: model.MatchRunning}); err != nil {
		t.Fatal(err)
	}

	response, payload := h.request(t, http.MethodGet, "/api/me/box", "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("get box failed: %d %v", response.StatusCode, payload)
	}
	if payload["activeMatchId"] != "m1" || payload["activeRobotId"] != "r1" {
		t.Fatalf("live marker was cleared: %v", payload)
	}
	if h.store.boxes[testUserID].ActiveMatchID != "m1" {
		t.Fatalf("live marker not persisted: %+v", h.store.boxes[testUserID])
	}
}

// After emulator state loss the supervisor still reports markers from
// agent.json while the store has no box record. Those markers must not be
// resurrected into the store or the browser, or the box stays "in a match"
// that no longer exists.
func TestGetBoxDropsSupervisorMarkersWithoutStoreRecord(t *testing.T) {
	h := newHarness(t)
	supervisor := readyBox()
	supervisor.ActiveRobotID, supervisor.ActiveMatchID = "r-old", "m-gone"
	h.provisioner.box = supervisor

	response, payload := h.request(t, http.MethodGet, "/api/me/box", "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("get box failed: %d %v", response.StatusCode, payload)
	}
	if payload["activeMatchId"] != nil || payload["activeRobotId"] != nil {
		t.Fatalf("supervisor marker leaked without a store record: %v", payload)
	}
	if stored := h.store.boxes[testUserID]; stored.ActiveMatchID != "" || stored.ActiveRobotID != "" {
		t.Fatalf("stale marker persisted: %+v", stored)
	}
}

func TestRecoverMatchesFailsRunningAndRequeuesQueued(t *testing.T) {
	h := newHarness(t)
	running := model.Match{MatchID: "m-running", Status: model.MatchRunning, Robots: []model.RobotSubmission{{RobotID: "r1", PlayerID: testUserID}}}
	queued := model.Match{MatchID: "m-queued", Status: model.MatchQueued}
	if err := h.store.PutMatch(context.Background(), running); err != nil {
		t.Fatal(err)
	}
	if err := h.store.PutMatch(context.Background(), queued); err != nil {
		t.Fatal(err)
	}
	box := readyBox()
	box.ActiveRobotID, box.ActiveMatchID = "r1", "m-running"
	if err := h.store.PutBox(context.Background(), testUserID, box); err != nil {
		t.Fatal(err)
	}

	h.app.RecoverMatches(context.Background())

	failed := h.store.matches["m-running"]
	if failed.Status != model.MatchFailed || failed.Error == "" {
		t.Fatalf("running match not failed after restart: %+v", failed)
	}
	if released := h.store.boxes[testUserID]; released.ActiveMatchID != "" {
		t.Fatalf("box not released after restart: %+v", released)
	}
	if len(h.store.queue) != 1 || h.store.queue[0] != "m-queued" {
		t.Fatalf("queued match not re-enqueued: %v", h.store.queue)
	}
}

func TestGetBoxMainReturnsWorkspaceSource(t *testing.T) {
	h := newHarness(t)
	h.provisioner.readMain = "-- demo robot script"
	response, payload := h.request(t, http.MethodGet, "/api/me/box/main.lua", "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("read main failed: %d %v", response.StatusCode, payload)
	}
	if payload["source"] != h.provisioner.readMain {
		t.Fatalf("unexpected source %v", payload["source"])
	}
}

func TestGetBoxMainReportsProvisionerFailure(t *testing.T) {
	h := newHarness(t)
	response, payload := h.request(t, http.MethodGet, "/api/me/box/main.lua", "")
	if response.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d %v", response.StatusCode, payload)
	}
	if payload["error"] != "read /workspace/main.lua: main.lua missing" {
		t.Fatalf("unexpected error message %v", payload["error"])
	}
}

func TestListScriptsReturnsTemplates(t *testing.T) {
	h := newHarness(t)
	response, payload := h.request(t, http.MethodGet, "/api/scripts", "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("list scripts failed: %d %v", response.StatusCode, payload)
	}
	entries, ok := payload["scripts"].([]any)
	if !ok || len(entries) < 3 {
		t.Fatalf("expected embedded templates, got %v", payload["scripts"])
	}
}

func TestDeployScriptWritesTemplateToBox(t *testing.T) {
	h := newHarness(t)
	response, payload := h.request(t, http.MethodPut, "/api/me/box/main.lua", `{"template":"sniper"}`)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("deploy failed: %d %v", response.StatusCode, payload)
	}
	source, err := scripts.Get("sniper")
	if err != nil {
		t.Fatal(err)
	}
	if len(h.provisioner.wroteMain) != 1 || h.provisioner.wroteMain[0] != source {
		t.Fatalf("template not written to box: %v", h.provisioner.wroteMain)
	}
	if payload["source"] != source {
		t.Fatalf("response source mismatch: %v", payload["source"])
	}
}

func TestDeployScriptRejectsUnknownTemplate(t *testing.T) {
	h := newHarness(t)
	response, payload := h.request(t, http.MethodPut, "/api/me/box/main.lua", `{"template":"rm-rf"}`)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d %v", response.StatusCode, payload)
	}
	if len(h.provisioner.wroteMain) != 0 {
		t.Fatalf("unknown template reached provisioner: %v", h.provisioner.wroteMain)
	}
}

func TestDeployScriptReportsProvisionerFailure(t *testing.T) {
	h := newHarness(t)
	h.provisioner.writeMainErr = errors.New("docker unavailable")
	response, payload := h.request(t, http.MethodPut, "/api/me/box/main.lua", `{"template":"evasive"}`)
	if response.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d %v", response.StatusCode, payload)
	}
	if payload["error"] != "write /workspace/main.lua: docker unavailable" {
		t.Fatalf("unexpected error message %v", payload["error"])
	}
}

func TestSetBoxKeyValidatesInput(t *testing.T) {
	h := newHarness(t)
	h.provisioner.box = readyBox()
	for _, key := range []string{"", "line one\nline two", "not-a-key"} {
		response, _ := h.request(t, http.MethodPut, "/api/me/box/ssh-key", fmt.Sprintf(`{"publicKey":%q}`, key))
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("key %q accepted with status %d", key, response.StatusCode)
		}
	}
	if len(h.provisioner.keys) != 0 {
		t.Fatalf("invalid keys reached provisioner: %v", h.provisioner.keys)
	}
}

func TestSetBoxKeyStoresFingerprint(t *testing.T) {
	h := newHarness(t)
	key, fingerprint, err := boxes.ValidatePublicKey("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZha2UtYnV0LWxvbmctZW5vdWdoLWtleQ== reviewer@example")
	if err != nil {
		t.Fatal(err)
	}
	response, payload := h.request(t, http.MethodPut, "/api/me/box/ssh-key", fmt.Sprintf(`{"publicKey":%q}`, key))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("set key failed: %d %v", response.StatusCode, payload)
	}
	if h.provisioner.box.KeyFingerprint != fingerprint && payload["keyFingerprint"] != fingerprint {
		t.Fatalf("fingerprint mismatch: box=%q response=%v", h.provisioner.box.KeyFingerprint, payload["keyFingerprint"])
	}
}

func TestSubmitRobotRequiresRunningBoxWithKey(t *testing.T) {
	h := newHarness(t)
	match := model.Match{MatchID: "m1", OwnerID: testUserID, Status: model.MatchLobby}
	if err := h.store.PutMatch(context.Background(), match); err != nil {
		t.Fatal(err)
	}
	h.provisioner.box = readyBox()
	h.provisioner.box.Status = "exited"
	h.provisioner.box.KeyFingerprint = ""
	h.provisioner.readMain = "function tick(robot) end"
	response, payload := h.request(t, http.MethodPost, "/api/matches/m1/robots", `{"displayName":"Ada","team":"red","startCommand":"lua main.lua"}`)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("expected conflict for stopped box, got %d %v", response.StatusCode, payload)
	}
}

func TestSubmitRobotRegistersAndNeverReturnsToken(t *testing.T) {
	h := newHarness(t)
	match := model.Match{MatchID: "m1", OwnerID: testUserID, Status: model.MatchLobby}
	if err := h.store.PutMatch(context.Background(), match); err != nil {
		t.Fatal(err)
	}
	h.provisioner.box = readyBox()
	h.provisioner.readMain = "-- player main.lua"
	body := `{"displayName":"Ada","team":"red","startCommand":"lua main.lua"}`
	response, payload := h.request(t, http.MethodPost, "/api/matches/m1/robots", body)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("register failed: %d %v", response.StatusCode, payload)
	}
	raw, _ := json.Marshal(payload)
	if bytes.Contains(raw, []byte("token")) {
		t.Fatalf("token leaked in response: %s", raw)
	}
	if len(h.store.matches["m1"].Robots) != 1 {
		t.Fatalf("robot not appended: %+v", h.store.matches["m1"])
	}
	stored := h.store.matches["m1"].Robots[0]
	if stored.OwnerBoxID != testBoxID || stored.ScriptObjectKey == "" {
		t.Fatalf("submission missing box binding: %+v", stored)
	}
	script := h.store.scripts[stored.ScriptObjectKey]
	if script != h.provisioner.readMain {
		t.Fatalf("script snapshot mismatch: %q", script)
	}
	if len(h.provisioner.configured) != 1 {
		t.Fatalf("agent not configured: %v", h.provisioner.configured)
	}
	config := h.provisioner.configured[0]
	if config.MatchID != "m1" || config.StartCommand != "lua main.lua" || config.Token == "" {
		t.Fatalf("agent config incomplete: %+v", config)
	}
	credential, ok := h.store.credentials[stored.RobotID]
	if !ok || credential.MatchID != "m1" || credential.TokenHash == "" {
		t.Fatalf("credential not stored: %+v", h.store.credentials)
	}
}

func TestSubmitRobotBlocksDuplicateUserInMatch(t *testing.T) {
	h := newHarness(t)
	match := model.Match{MatchID: "m1", OwnerID: testUserID, Status: model.MatchLobby}
	match.Robots = append(match.Robots, model.RobotSubmission{RobotID: "r1", OwnerBoxID: testBoxID})
	if err := h.store.PutMatch(context.Background(), match); err != nil {
		t.Fatal(err)
	}
	h.provisioner.box = readyBox()
	h.provisioner.readMain = "x"
	response, payload := h.request(t, http.MethodPost, "/api/matches/m1/robots", `{"displayName":"Ada","team":"blue","startCommand":"lua main.lua"}`)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("expected conflict, got %d %v", response.StatusCode, payload)
	}
}

func TestSubmitRobotBlocksBoxWithActiveMatch(t *testing.T) {
	h := newHarness(t)
	if err := h.store.PutMatch(context.Background(), model.Match{MatchID: "m1", Status: model.MatchLobby}); err != nil {
		t.Fatal(err)
	}
	if err := h.store.PutMatch(context.Background(), model.Match{MatchID: "m2", Status: model.MatchRunning, Robots: []model.RobotSubmission{
		{RobotID: "r2", PlayerID: testUserID, OwnerBoxID: testBoxID},
	}}); err != nil {
		t.Fatal(err)
	}
	box := readyBox()
	box.ActiveRobotID, box.ActiveMatchID = "r2", "m2"
	if err := h.store.PutBox(context.Background(), testUserID, box); err != nil {
		t.Fatal(err)
	}
	h.provisioner.box = box
	h.provisioner.readMain = "x"
	response, payload := h.request(t, http.MethodPost, "/api/matches/m1/robots", `{"displayName":"Ada","team":"red","startCommand":"lua main.lua"}`)
	if response.StatusCode != http.StatusConflict || payload["error"] != "box already has an active robot" {
		t.Fatalf("expected conflict, got %d %v", response.StatusCode, payload)
	}
}

func TestSubmitRobotIgnoresStaleSupervisorMarker(t *testing.T) {
	// The supervisor keeps the agent.json marker after a withdrawal, but the
	// store no longer binds the box; registration into a fresh lobby must
	// proceed instead of failing with "box already has an active robot".
	h := newHarness(t)
	if err := h.store.PutMatch(context.Background(), model.Match{MatchID: "m1", OwnerID: testUserID, Status: model.MatchLobby}); err != nil {
		t.Fatal(err)
	}
	if err := h.store.PutMatch(context.Background(), model.Match{MatchID: "m2", Status: model.MatchLobby, Robots: []model.RobotSubmission{
		{RobotID: "bot-1", Bot: true},
	}}); err != nil {
		t.Fatal(err)
	}
	box := readyBox()
	box.ActiveRobotID, box.ActiveMatchID = "r-old", "m2"
	h.provisioner.box = box
	h.provisioner.readMain = "x"
	response, payload := h.request(t, http.MethodPost, "/api/matches/m1/robots", `{"displayName":"Ada","team":"red","startCommand":"lua main.lua"}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("expected registration despite supervisor marker, got %d %v", response.StatusCode, payload)
	}
	storedBox := h.store.boxes[testUserID]
	if storedBox.ActiveMatchID != "m1" {
		t.Fatalf("binding not moved to the new match: %+v", storedBox)
	}
}

func TestSubmitRobotAllowsBoxAfterMatchFinishes(t *testing.T) {
	h := newHarness(t)
	if err := h.store.PutMatch(context.Background(), model.Match{MatchID: "m1", Status: model.MatchLobby}); err != nil {
		t.Fatal(err)
	}
	if err := h.store.PutMatch(context.Background(), model.Match{MatchID: "m2", Status: model.MatchFinished}); err != nil {
		t.Fatal(err)
	}
	h.provisioner.box = readyBox()
	h.provisioner.box.ActiveMatchID = "m2"
	h.provisioner.readMain = "x"
	response, payload := h.request(t, http.MethodPost, "/api/matches/m1/robots", `{"displayName":"Ada","team":"red","startCommand":"lua main.lua"}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("expected reuse after finish, got %d %v", response.StatusCode, payload)
	}
	storedBox := h.store.boxes[testUserID]
	if storedBox.ActiveMatchID != "m1" || storedBox.ActiveRobotID == "" {
		t.Fatalf("stale markers not replaced with new registration: %+v", storedBox)
	}
}

func TestSubmitRobotRevertsOnConfigureFailure(t *testing.T) {
	h := newHarness(t)
	if err := h.store.PutMatch(context.Background(), model.Match{MatchID: "m1", Status: model.MatchLobby}); err != nil {
		t.Fatal(err)
	}
	h.provisioner.box = readyBox()
	h.provisioner.readMain = "x"
	h.provisioner.configErr = errors.New("supervisor unreachable")
	response, payload := h.request(t, http.MethodPost, "/api/matches/m1/robots", `{"displayName":"Ada","team":"red","startCommand":"lua main.lua"}`)
	if response.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d %v", response.StatusCode, payload)
	}
	if len(h.store.matches["m1"].Robots) != 0 {
		t.Fatalf("robot not reverted: %+v", h.store.matches["m1"].Robots)
	}
}

func TestStartMatchRequiresOwner(t *testing.T) {
	h := newHarness(t)
	match := model.Match{MatchID: "m1", OwnerID: "someone-else", Status: model.MatchLobby}
	match.Robots = []model.RobotSubmission{{RobotID: "r1", Team: "red"}, {RobotID: "r2", Team: "blue"}}
	if err := h.store.PutMatch(context.Background(), match); err != nil {
		t.Fatal(err)
	}
	app := NewServer(h.store, h.provisioner)
	app.auth = &fakeAuth{}
	server := httptest.NewServer(app.Handler())
	defer server.Close()
	response, _ := h.request(t, http.MethodPost, "/api/matches/m1/start", "")
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", response.StatusCode)
	}
}

func TestStartMatchRequiresBothTeamsAndConnectedAgents(t *testing.T) {
	h := newHarness(t)
	match := model.Match{MatchID: "m1", OwnerID: testUserID, Status: model.MatchLobby}
	match.Robots = []model.RobotSubmission{{RobotID: "r1", Team: "red"}, {RobotID: "r2", Team: "blue"}}
	if err := h.store.PutMatch(context.Background(), match); err != nil {
		t.Fatal(err)
	}
	response, payload := h.request(t, http.MethodPost, "/api/matches/m1/start", "")
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("expected conflict without agents, got %d %v", response.StatusCode, payload)
	}
	if h.store.matches["m1"].Status != model.MatchLobby {
		t.Fatalf("match should stay in lobby: %+v", h.store.matches["m1"])
	}
}

func TestStartMatchEnqueuesJob(t *testing.T) {
	h := newHarness(t)
	match := model.Match{MatchID: "m1", OwnerID: testUserID, Status: model.MatchLobby}
	match.Robots = []model.RobotSubmission{{RobotID: "r1", Team: "red"}, {RobotID: "r2", Team: "blue"}}
	if err := h.store.PutMatch(context.Background(), match); err != nil {
		t.Fatal(err)
	}
	app := NewServer(h.store, h.provisioner)
	app.auth = &fakeAuth{}
	for _, robotID := range []string{"r1", "r2"} {
		app.agents.Attach(robotID, &AgentSession{closed: make(chan struct{})})
	}
	server := httptest.NewServer(app.Handler())
	defer server.Close()
	h.server = server
	response, payload := h.request(t, http.MethodPost, "/api/matches/m1/start", "")
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("start failed: %d %v", response.StatusCode, payload)
	}
	if len(h.store.queue) != 1 || h.store.queue[0] != "m1" {
		t.Fatalf("match not enqueued: %v", h.store.queue)
	}
}

func attachAgents(app *Server, robotIDs ...string) {
	for _, robotID := range robotIDs {
		app.agents.Attach(robotID, &AgentSession{closed: make(chan struct{})})
	}
}

func TestAutoStartWhenAllAgentsConnect(t *testing.T) {
	h := newHarness(t)
	match := model.Match{MatchID: "m1", OwnerID: testUserID, Status: model.MatchLobby}
	match.Robots = []model.RobotSubmission{{RobotID: "r1", Team: "red"}, {RobotID: "r2", Team: "blue"}}
	if err := h.store.PutMatch(context.Background(), match); err != nil {
		t.Fatal(err)
	}
	app := NewServer(h.store, h.provisioner)
	attachAgents(app, "r1", "r2")
	app.autoStartIfReady("m1")
	if h.store.matches["m1"].Status != model.MatchQueued {
		t.Fatalf("ready match did not auto-start: %+v", h.store.matches["m1"])
	}
	if len(h.store.queue) != 1 || h.store.queue[0] != "m1" {
		t.Fatalf("auto start did not enqueue: %v", h.store.queue)
	}
}

func TestAutoStartSkipsIncompleteRosters(t *testing.T) {
	h := newHarness(t)
	if err := h.store.PutMatch(context.Background(), model.Match{MatchID: "m1", Status: model.MatchLobby}); err != nil {
		t.Fatal(err)
	}
	app := NewServer(h.store, h.provisioner)
	// Only one team registered: no start.
	attachAgents(app, "r1")
	app.autoStartIfReady("m1")
	if h.store.matches["m1"].Status != model.MatchLobby {
		t.Fatalf("one-team roster should not start: %+v", h.store.matches["m1"])
	}
	// Both teams but one agent missing: no start.
	match := h.store.matches["m1"]
	match.Robots = []model.RobotSubmission{{RobotID: "r1", Team: "red"}, {RobotID: "r2", Team: "blue"}}
	if err := h.store.PutMatch(context.Background(), match); err != nil {
		t.Fatal(err)
	}
	app.autoStartIfReady("m1")
	if h.store.matches["m1"].Status != model.MatchLobby {
		t.Fatalf("missing agent should not start: %+v", h.store.matches["m1"])
	}
}

func TestWithdrawRobot(t *testing.T) {
	h := newHarness(t)
	match := model.Match{MatchID: "m1", Status: model.MatchLobby}
	match.Robots = []model.RobotSubmission{{RobotID: "r1", Team: "red", OwnerBoxID: testBoxID}}
	if err := h.store.PutMatch(context.Background(), match); err != nil {
		t.Fatal(err)
	}
	h.provisioner.box = readyBox()
	h.provisioner.box.ActiveRobotID, h.provisioner.box.ActiveMatchID = "r1", "m1"
	response, payload := h.request(t, http.MethodDelete, "/api/matches/m1/robots", "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("withdraw failed: %d %v", response.StatusCode, payload)
	}
	if len(h.store.matches["m1"].Robots) != 0 {
		t.Fatalf("robot not removed: %+v", h.store.matches["m1"].Robots)
	}
	storedBox, ok := h.store.boxes[testUserID]
	if !ok || storedBox.ActiveMatchID != "" || storedBox.ActiveRobotID != "" {
		t.Fatalf("box binding not cleared: %+v", storedBox)
	}
}

func TestWithdrawRobotRejectsRunningMatch(t *testing.T) {
	h := newHarness(t)
	match := model.Match{MatchID: "m1", Status: model.MatchRunning}
	match.Robots = []model.RobotSubmission{{RobotID: "r1", Team: "red", OwnerBoxID: testBoxID}}
	if err := h.store.PutMatch(context.Background(), match); err != nil {
		t.Fatal(err)
	}
	response, _ := h.request(t, http.MethodDelete, "/api/matches/m1/robots", "")
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("expected conflict, got %d", response.StatusCode)
	}
	if len(h.store.matches["m1"].Robots) != 1 {
		t.Fatal("running match lost a robot")
	}
}

func TestWithdrawFromMatchConcedesRobot(t *testing.T) {
	h := newHarness(t)
	match := model.Match{MatchID: "m1", OwnerID: testUserID, Status: model.MatchRunning}
	match.Robots = []model.RobotSubmission{
		{RobotID: "r1", Team: "red", OwnerBoxID: testBoxID, PlayerID: testUserID},
		{RobotID: "r2", Team: "blue", Bot: true},
	}
	if err := h.store.PutMatch(context.Background(), match); err != nil {
		t.Fatal(err)
	}
	controller := func(id string) engine.Controller {
		return engine.NewBotController(id, engine.BotDummy, engine.PersonalityAggressive, engine.DefaultConfig().Map)
	}
	states := engine.SpawnPositions([]engine.RobotState{
		{RobotID: "r1", Name: "Player", Team: "red"},
		{RobotID: "r2", Name: "Bot", Team: "blue"},
	})
	arena := engine.New("m1", states, map[string]engine.Controller{"r1": controller("r1"), "r2": controller("r2")})
	h.app.registerActiveArena("m1", arena)
	defer h.app.unregisterActiveArena("m1")

	response, payload := h.request(t, http.MethodPost, "/api/matches/m1/withdraw", "")
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("withdraw failed: %d %v", response.StatusCode, payload)
	}
	if payload["status"] != "withdrawing" || payload["robotId"] != "r1" {
		t.Fatalf("unexpected response: %v", payload)
	}

	// The next tick concedes the robot and resolves the match by elimination.
	snapshot := arena.Step(context.Background())
	if !arena.Finished() || arena.Winner() != "blue" {
		t.Fatalf("opponent should win: finished=%v winner=%q", arena.Finished(), arena.Winner())
	}
	for _, robot := range snapshot.Robots {
		if robot.RobotID != "r1" {
			continue
		}
		if robot.Alive || robot.Failed || robot.HP != 0 {
			t.Fatalf("withdrawn robot not eliminated: %+v", robot)
		}
	}
}

func TestWithdrawFromMatchRejectsLobby(t *testing.T) {
	h := newHarness(t)
	match := model.Match{MatchID: "m1", Status: model.MatchLobby}
	match.Robots = []model.RobotSubmission{{RobotID: "r1", Team: "red", OwnerBoxID: testBoxID}}
	if err := h.store.PutMatch(context.Background(), match); err != nil {
		t.Fatal(err)
	}
	response, _ := h.request(t, http.MethodPost, "/api/matches/m1/withdraw", "")
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("expected conflict, got %d", response.StatusCode)
	}
}

func TestWithdrawFromMatchWithoutRegistration(t *testing.T) {
	h := newHarness(t)
	match := model.Match{MatchID: "m1", Status: model.MatchRunning}
	match.Robots = []model.RobotSubmission{{RobotID: "r1", Team: "red", OwnerBoxID: "someone-else"}}
	if err := h.store.PutMatch(context.Background(), match); err != nil {
		t.Fatal(err)
	}
	response, _ := h.request(t, http.MethodPost, "/api/matches/m1/withdraw", "")
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("expected not found, got %d", response.StatusCode)
	}
}

func TestWithdrawRobotWithoutRegistration(t *testing.T) {
	h := newHarness(t)
	if err := h.store.PutMatch(context.Background(), model.Match{MatchID: "m1", Status: model.MatchLobby}); err != nil {
		t.Fatal(err)
	}
	response, _ := h.request(t, http.MethodDelete, "/api/matches/m1/robots", "")
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", response.StatusCode)
	}
}

func TestAutoStartIgnoresRunningMatch(t *testing.T) {
	h := newHarness(t)
	match := model.Match{MatchID: "m1", Status: model.MatchRunning}
	match.Robots = []model.RobotSubmission{{RobotID: "r1", Team: "red"}, {RobotID: "r2", Team: "blue"}}
	if err := h.store.PutMatch(context.Background(), match); err != nil {
		t.Fatal(err)
	}
	app := NewServer(h.store, h.provisioner)
	attachAgents(app, "r1", "r2")
	app.autoStartIfReady("m1")
	if h.store.matches["m1"].Status != model.MatchRunning {
		t.Fatalf("running match must stay running: %+v", h.store.matches["m1"])
	}
	if len(h.store.queue) != 0 {
		t.Fatalf("running match enqueued again: %v", h.store.queue)
	}
}

func TestAutoStartEnqueuesExactlyOnce(t *testing.T) {
	h := newHarness(t)
	match := model.Match{MatchID: "m1", Status: model.MatchLobby}
	match.Robots = []model.RobotSubmission{{RobotID: "r1", Team: "red"}, {RobotID: "r2", Team: "blue"}}
	if err := h.store.PutMatch(context.Background(), match); err != nil {
		t.Fatal(err)
	}
	app := NewServer(h.store, h.provisioner)
	attachAgents(app, "r1", "r2")
	app.autoStartIfReady("m1")
	// A second agent reconnecting must not double-queue.
	previous := app.agents.sessions["r2"]
	if previous != nil {
		app.agents.Detach("r2", previous)
	}
	attachAgents(app, "r2")
	app.autoStartIfReady("m1")
	if len(h.store.queue) != 1 {
		t.Fatalf("match enqueued %d times: %v", len(h.store.queue), h.store.queue)
	}
}

func TestAuthRequiredBlocksAnonymousBoxOperations(t *testing.T) {
	h := newHarness(t)
	app := NewServer(h.store, h.provisioner)
	app.auth = &fakeAuth{required: true}
	server := httptest.NewServer(app.Handler())
	defer server.Close()
	previous := h.server
	h.server = server
	defer func() { h.server = previous }()
	response, payload := h.requestAs(t, http.MethodGet, "/api/me/box", "", false)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d %v", response.StatusCode, payload)
	}
	response, payload = h.requestAs(t, http.MethodPost, "/api/me/box/release", "", false)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for box release, got %d %v", response.StatusCode, payload)
	}
}

func TestConnectAgentRejectsBadCredential(t *testing.T) {
	h := newHarness(t)
	response, _ := h.request(t, http.MethodGet, "/agent/connect/missing", "")
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", response.StatusCode)
	}
}

func TestReadyReportsCloudFailure(t *testing.T) {
	h := newHarness(t)
	h.store.readyErr = errors.New("floci down")
	response, payload := h.request(t, http.MethodGet, "/readyz", "")
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d %v", response.StatusCode, payload)
	}
}

func TestPublicMatchListAndProfile(t *testing.T) {
	h := newHarness(t)
	h.store.matches["m1"] = model.Match{MatchID: "m1", Status: model.MatchFinished}
	h.store.players["reviewer"] = model.PlayerStats{PlayerID: "reviewer", Handle: "reviewer", Ratings: map[string]int{"duel": 1510}}
	response, payload := h.requestAs(t, http.MethodGet, "/api/matches?status=finished", "", false)
	if response.StatusCode != http.StatusOK || len(payload["matches"].([]any)) != 1 {
		t.Fatalf("public matches failed: %d %v", response.StatusCode, payload)
	}
	response, payload = h.requestAs(t, http.MethodGet, "/api/profiles/reviewer", "", false)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("public profile failed: %d %v", response.StatusCode, payload)
	}
}

func TestQueueJoinStatusAndLeave(t *testing.T) {
	h := newHarness(t)
	h.provisioner.box = readyBox()
	h.provisioner.readMain = "return arena.action({ move = 1 })"
	body := `{"displayName":"Queue Bot","mode":"duel","runtime":"lua5.4","startCommand":"lua main.lua"}`
	response, payload := h.request(t, http.MethodPost, "/api/queue", body)
	if response.StatusCode != http.StatusAccepted || payload["status"] != "waiting" {
		t.Fatalf("queue join failed: %d %v", response.StatusCode, payload)
	}
	response, payload = h.request(t, http.MethodGet, "/api/queue", "")
	if response.StatusCode != http.StatusOK || payload["status"] != "waiting" {
		t.Fatalf("queue status failed: %d %v", response.StatusCode, payload)
	}
	response, payload = h.request(t, http.MethodDelete, "/api/queue", "")
	if response.StatusCode != http.StatusOK || payload["status"] != "idle" {
		t.Fatalf("queue leave failed: %d %v", response.StatusCode, payload)
	}
}

func TestQueueJoinBlocksBoxWithActiveMatch(t *testing.T) {
	h := newHarness(t)
	if err := h.store.PutMatch(context.Background(), model.Match{MatchID: "m2", Status: model.MatchRunning, Robots: []model.RobotSubmission{
		{RobotID: "r2", PlayerID: testUserID, OwnerBoxID: testBoxID},
	}}); err != nil {
		t.Fatal(err)
	}
	box := readyBox()
	box.ActiveRobotID, box.ActiveMatchID = "r2", "m2"
	if err := h.store.PutBox(context.Background(), testUserID, box); err != nil {
		t.Fatal(err)
	}
	h.provisioner.box = box
	h.provisioner.readMain = "return 1"
	body := `{"displayName":"Ada","mode":"duel","runtime":"lua5.4","startCommand":"lua main.lua"}`
	response, payload := h.request(t, http.MethodPost, "/api/queue", body)
	if response.StatusCode != http.StatusConflict || payload["error"] != "box already has an active robot" {
		t.Fatalf("expected conflict, got %d %v", response.StatusCode, payload)
	}
}

func TestQueueJoinIgnoresStaleSupervisorMarker(t *testing.T) {
	// Same store-authority rule as registration: a supervisor marker pointing
	// at a live match that no longer holds this box's robot never blocks the
	// duel queue.
	h := newHarness(t)
	if err := h.store.PutMatch(context.Background(), model.Match{MatchID: "m2", Status: model.MatchRunning, Robots: []model.RobotSubmission{
		{RobotID: "bot-1", Bot: true},
	}}); err != nil {
		t.Fatal(err)
	}
	box := readyBox()
	box.ActiveRobotID, box.ActiveMatchID = "r-old", "m2"
	h.provisioner.box = box
	h.provisioner.readMain = "return 1"
	body := `{"displayName":"Ada","mode":"duel","runtime":"lua5.4","startCommand":"lua main.lua"}`
	response, payload := h.request(t, http.MethodPost, "/api/queue", body)
	if response.StatusCode != http.StatusAccepted || payload["status"] != "waiting" {
		t.Fatalf("expected queue join despite supervisor marker, got %d %v", response.StatusCode, payload)
	}
	stored := h.store.boxes[testUserID]
	if stored.ActiveRobotID != "" || stored.ActiveMatchID != "" {
		t.Fatalf("stale markers not cleared: %+v", stored)
	}
}

func TestQueueJoinClearsStaleActiveMatch(t *testing.T) {
	h := newHarness(t)
	if err := h.store.PutMatch(context.Background(), model.Match{MatchID: "m2", Status: model.MatchFinished}); err != nil {
		t.Fatal(err)
	}
	h.provisioner.box = readyBox()
	h.provisioner.box.ActiveRobotID, h.provisioner.box.ActiveMatchID = "r-old", "m2"
	h.provisioner.readMain = "return 1"
	body := `{"displayName":"Ada","mode":"duel","runtime":"lua5.4","startCommand":"lua main.lua"}`
	response, payload := h.request(t, http.MethodPost, "/api/queue", body)
	if response.StatusCode != http.StatusAccepted || payload["status"] != "waiting" {
		t.Fatalf("expected queue join after stale release, got %d %v", response.StatusCode, payload)
	}
	stored := h.store.boxes[testUserID]
	if stored.ActiveRobotID != "" || stored.ActiveMatchID != "" {
		t.Fatalf("stale markers not cleared: %+v", stored)
	}
}

func TestReleaseBoxDropsLobbyRegistration(t *testing.T) {
	h := newHarness(t)
	h.provisioner.box = readyBox()
	if err := h.store.PutMatch(context.Background(), model.Match{MatchID: "m9", Status: model.MatchLobby, Robots: []model.RobotSubmission{
		{RobotID: "r9", PlayerID: testUserID, OwnerBoxID: testBoxID},
	}}); err != nil {
		t.Fatal(err)
	}
	box := readyBox()
	box.ActiveRobotID, box.ActiveMatchID = "r9", "m9"
	if err := h.store.PutBox(context.Background(), testUserID, box); err != nil {
		t.Fatal(err)
	}
	response, payload := h.request(t, http.MethodPost, "/api/me/box/release", "")
	if response.StatusCode != http.StatusOK || payload["status"] != "released" {
		t.Fatalf("lobby release failed: %d %v", response.StatusCode, payload)
	}
	stored := h.store.boxes[testUserID]
	if stored.ActiveRobotID != "" || stored.ActiveMatchID != "" {
		t.Fatalf("markers not cleared: %+v", stored)
	}
	if robots := h.store.matches["m9"].Robots; len(robots) != 0 {
		t.Fatalf("robot not removed from lobby: %+v", robots)
	}
	// The unblocked box can queue again immediately.
	h.provisioner.readMain = "return 1"
	body := `{"displayName":"Ada","mode":"duel","runtime":"lua5.4","startCommand":"lua main.lua"}`
	response, payload = h.request(t, http.MethodPost, "/api/queue", body)
	if response.StatusCode != http.StatusAccepted || payload["status"] != "waiting" {
		t.Fatalf("queue join after release failed: %d %v", response.StatusCode, payload)
	}
}

func TestReleaseBoxClearsStaleMarkerWithoutMatch(t *testing.T) {
	h := newHarness(t)
	h.provisioner.box = readyBox()
	box := readyBox()
	box.ActiveRobotID, box.ActiveMatchID = "r-gone", "m-gone"
	if err := h.store.PutBox(context.Background(), testUserID, box); err != nil {
		t.Fatal(err)
	}
	// overlayBoxMatchState clears the vanished-match marker during the read,
	// so the release itself reports idle.
	response, payload := h.request(t, http.MethodPost, "/api/me/box/release", "")
	if response.StatusCode != http.StatusOK || (payload["status"] != "released" && payload["status"] != "idle") {
		t.Fatalf("stale release failed: %d %v", response.StatusCode, payload)
	}
	stored := h.store.boxes[testUserID]
	if stored.ActiveRobotID != "" || stored.ActiveMatchID != "" {
		t.Fatalf("stale markers not cleared: %+v", stored)
	}
}

func TestReleaseBoxWithoutActiveMatchIsIdle(t *testing.T) {
	h := newHarness(t)
	h.provisioner.box = readyBox()
	response, payload := h.request(t, http.MethodPost, "/api/me/box/release", "")
	if response.StatusCode != http.StatusOK || payload["status"] != "idle" {
		t.Fatalf("expected idle release, got %d %v", response.StatusCode, payload)
	}
}

func TestReleaseBoxFailsStuckRunningMatchWithoutWorker(t *testing.T) {
	// A running match with no registered arena can never resolve (stack
	// restart); releasing the box must fail the match instead of blocking.
	h := newHarness(t)
	h.provisioner.box = readyBox()
	if err := h.store.PutMatch(context.Background(), model.Match{MatchID: "m7", Status: model.MatchRunning, Robots: []model.RobotSubmission{
		{RobotID: "r7", PlayerID: testUserID, OwnerBoxID: testBoxID},
	}}); err != nil {
		t.Fatal(err)
	}
	box := readyBox()
	box.ActiveRobotID, box.ActiveMatchID = "r7", "m7"
	if err := h.store.PutBox(context.Background(), testUserID, box); err != nil {
		t.Fatal(err)
	}
	response, payload := h.request(t, http.MethodPost, "/api/me/box/release", "")
	if response.StatusCode != http.StatusOK || payload["status"] != "released" {
		t.Fatalf("release failed: %d %v", response.StatusCode, payload)
	}
	if stored := h.store.matches["m7"]; stored.Status != model.MatchFailed {
		t.Fatalf("stuck match not failed: %+v", stored)
	}
	storedBox := h.store.boxes[testUserID]
	if storedBox.ActiveRobotID != "" || storedBox.ActiveMatchID != "" {
		t.Fatalf("markers not cleared: %+v", storedBox)
	}
}

func TestReleaseBoxesClearsActiveMarkersAfterMatchEnds(t *testing.T) {
	store := newFakeStore()
	app := NewServer(store, &fakeProvisioner{})
	match := model.Match{MatchID: "m1", Status: model.MatchFinished, Robots: []model.RobotSubmission{
		{RobotID: "r1", PlayerID: testUserID, OwnerBoxID: testBoxID},
		{RobotID: "bot-1", Bot: true},
	}}
	box := readyBox()
	box.ActiveRobotID, box.ActiveMatchID = "r1", "m1"
	if err := store.PutBox(context.Background(), testUserID, box); err != nil {
		t.Fatal(err)
	}
	app.releaseBoxes(context.Background(), match)
	stored, err := store.GetBox(context.Background(), testUserID)
	if err != nil || stored.ActiveRobotID != "" || stored.ActiveMatchID != "" {
		t.Fatalf("box markers not released: %+v %v", stored, err)
	}
}

func TestReplayFallsBackToEventSummary(t *testing.T) {
	h := newHarness(t)
	h.store.matches["m1"] = model.Match{MatchID: "m1", Status: model.MatchFinished, EventSummary: []model.MatchEvent{{Type: "hit", Damage: 25}}}
	response, payload := h.requestAs(t, http.MethodGet, "/api/matches/m1/replay", "", false)
	if response.StatusCode != http.StatusOK || payload["complete"] != false {
		t.Fatalf("replay fallback failed: %d %v", response.StatusCode, payload)
	}
}

func TestUpdatePlayerStatsStoresResultAndDamage(t *testing.T) {
	store := newFakeStore()
	app := NewServer(store, &fakeProvisioner{})
	match := model.Match{Mode: "duel", WinnerTeam: "red", Robots: []model.RobotSubmission{{RobotID: "r1", PlayerID: "alice", Team: "red"}, {RobotID: "r2", PlayerID: "bob", Team: "blue"}}, RobotSummaries: []model.RobotSummary{{RobotID: "r1", DamageDealt: 75, DamageTaken: 25}, {RobotID: "r2", DamageDealt: 25, DamageTaken: 75}}}
	app.updatePlayerStats(context.Background(), match)
	if store.players["alice"].Wins != 1 || store.players["alice"].Ratings["duel"] != 1516 || store.players["alice"].DamageDealt != 75 {
		t.Fatalf("winner stats wrong: %+v", store.players["alice"])
	}
	if store.players["bob"].Losses != 1 || store.players["bob"].Ratings["duel"] != 1484 {
		t.Fatalf("loser stats wrong: %+v", store.players["bob"])
	}
}

func TestReconnectGraceReusesLastIntent(t *testing.T) {
	manager := NewAgentManager()
	manager.last["r1"] = engine.Intent{Move: 3}
	manager.disconnectedAt["r1"] = time.Now()
	intent, err := manager.Controller("r1").Tick(context.Background(), engine.RobotState{}, nil)
	if err != nil || intent.Move != 3 {
		t.Fatalf("reconnect fallback failed: %+v %v", intent, err)
	}
}

func TestSDKVersionComparison(t *testing.T) {
	if !sdkVersionOlder("0.1.9", "0.2.0") || sdkVersionOlder("0.2.0", "0.2.0") || sdkVersionOlder("1.0.0", "0.2.0") {
		t.Fatal("SDK version comparison wrong")
	}
}

func onlyStoredMatch(t *testing.T, store *fakeStore) model.Match {
	t.Helper()
	if len(store.matches) != 1 {
		t.Fatalf("expected exactly one stored match, got %d", len(store.matches))
	}
	for _, match := range store.matches {
		return match
	}
	return model.Match{}
}

func TestCreateMatchPersistsCombatOptionsAndPersonality(t *testing.T) {
	h := newHarness(t)
	body := `{"friendlyFire":true,"regenPerTick":2,"regenDelayTicks":120,"rammingDamage":true,"botPersonality":"camper"}`
	response, payload := h.request(t, http.MethodPost, "/api/matches", body)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create match failed: %d %v", response.StatusCode, payload)
	}
	if payload["friendlyFire"] != true || payload["regenPerTick"] != float64(2) || payload["regenDelayTicks"] != float64(120) || payload["rammingDamage"] != true {
		t.Fatalf("combat options missing from response: %v", payload)
	}
	if payload["botPersonality"] != "camper" {
		t.Fatalf("botPersonality missing from response: %v", payload)
	}
	stored := onlyStoredMatch(t, h.store)
	if !stored.FriendlyFire || stored.RegenPerTick != 2 || stored.RegenDelayTicks != 120 || !stored.RammingDamage || stored.BotPersonality != "camper" {
		t.Fatalf("combat options not persisted: %+v", stored)
	}
}

func TestCreateMatchDefaultsKeepStockBehavior(t *testing.T) {
	h := newHarness(t)
	response, payload := h.request(t, http.MethodPost, "/api/matches", `{}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create match failed: %d", response.StatusCode)
	}
	stored := onlyStoredMatch(t, h.store)
	if stored.FriendlyFire || stored.RegenPerTick != 0 || stored.RegenDelayTicks != 0 || stored.RammingDamage {
		t.Fatalf("defaults must keep stock combat behavior: %+v", stored)
	}
	if stored.BotPersonality != "aggressive" {
		t.Fatalf("default bot personality must be aggressive: %+v", stored)
	}
	raw, _ := json.Marshal(payload)
	for _, key := range []string{"friendlyFire", "regenPerTick", "regenDelayTicks", "rammingDamage"} {
		if bytes.Contains(raw, []byte(key)) {
			t.Fatalf("unset option %s should be omitted from response: %s", key, raw)
		}
	}
}

func TestCreateMatchAcceptsMixedBotPersonality(t *testing.T) {
	h := newHarness(t)
	response, payload := h.request(t, http.MethodPost, "/api/matches", `{"botPersonality":"mixed"}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("mixed personality rejected: %d %v", response.StatusCode, payload)
	}
	if payload["botPersonality"] != "mixed" {
		t.Fatalf("mixed personality missing from response: %v", payload)
	}
	if stored := onlyStoredMatch(t, h.store); stored.BotPersonality != "mixed" {
		t.Fatalf("mixed personality not persisted: %+v", stored)
	}
}

func TestCreateMatchRejectsInvalidCombatOptions(t *testing.T) {
	for _, body := range []string{
		`{"regenPerTick":-1}`,
		`{"regenPerTick":11}`,
		`{"regenDelayTicks":-5}`,
		`{"regenDelayTicks":601}`,
		`{"botPersonality":"sneaky"}`,
	} {
		h := newHarness(t)
		response, payload := h.request(t, http.MethodPost, "/api/matches", body)
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("body %s accepted with status %d: %v", body, response.StatusCode, payload)
		}
		if payload["error"] == "" {
			t.Fatalf("body %s rejected without useful error: %v", body, payload)
		}
	}
}

func TestWorkerCopiesMatchOptionsIntoEngineConfig(t *testing.T) {
	match := model.Match{MapID: "open-field", ArenaWidth: 800, ArenaHeight: 500, Seed: 42, FriendlyFire: true, RegenPerTick: 3, RegenDelayTicks: 90, RammingDamage: true, BotPersonality: "evasive"}
	config := engineConfigFor(match)
	if !config.FriendlyFire || config.RegenPerTick != 3 || config.RegenDelayTicks != 90 || !config.RammingDamage {
		t.Fatalf("combat options not copied into engine config: %+v", config)
	}
	if config.Width != 800 || config.Height != 500 || config.Seed != 42 {
		t.Fatalf("arena config wrong: %+v", config)
	}
	if botPersonalityFor(match) != engine.PersonalityEvasive {
		t.Fatalf("evasive personality not mapped: %s", botPersonalityFor(match))
	}
	plain := engineConfigFor(model.Match{MapID: "open-field", ArenaWidth: 800, ArenaHeight: 500, Seed: 7})
	if plain.FriendlyFire || plain.RegenPerTick != 0 || plain.RegenDelayTicks != 0 || plain.RammingDamage {
		t.Fatalf("zero-value match must keep stock engine config: %+v", plain)
	}
	if botPersonalityFor(model.Match{}) != engine.PersonalityAggressive {
		t.Fatal("empty personality must default to aggressive")
	}
	if botPersonalityFor(model.Match{BotPersonality: "camper"}) != engine.PersonalityCamper {
		t.Fatal("camper personality not mapped")
	}
	if botPersonalityFor(model.Match{BotPersonality: "mixed"}) != engine.PersonalityMixed {
		t.Fatal("mixed personality not mapped")
	}
	random := engineConfigFor(model.Match{MapID: "random-maze", ArenaWidth: 1200, ArenaHeight: 700, Seed: 4242})
	again := engineConfigFor(model.Match{MapID: "random-maze", ArenaWidth: 1200, ArenaHeight: 700, Seed: 4242})
	if random.Map.ID != "random-maze" || len(random.Map.Obstacles) == 0 {
		t.Fatalf("procedural map not generated into engine config: %+v", random.Map)
	}
	if random.Map.Width != 1200 || random.Map.Height != 700 {
		t.Fatalf("procedural map must use the match arena dims: %+v", random.Map)
	}
	if !reflect.DeepEqual(random.Map, again.Map) {
		t.Fatal("same match seed must rebuild an identical procedural map")
	}
	if handcrafted := engineConfigFor(model.Match{MapID: "crossing-fire", ArenaWidth: 1000, ArenaHeight: 600, Seed: 5}); handcrafted.Map.ID != "crossing-fire" {
		t.Fatalf("handcrafted starter map not resolved: %+v", handcrafted.Map)
	}
}

func TestCreateMatchAcceptsProceduralMapIDs(t *testing.T) {
	for _, mapID := range []string{"random-maze", "random-rooms", "random-bunkers"} {
		h := newHarness(t)
		response, payload := h.request(t, http.MethodPost, "/api/matches", `{"mapId":"`+mapID+`"}`)
		if response.StatusCode != http.StatusCreated {
			t.Fatalf("%s rejected with %d: %v", mapID, response.StatusCode, payload)
		}
		stored := onlyStoredMatch(t, h.store)
		if stored.MapID != mapID {
			t.Fatalf("map id not stored as given: %+v", stored)
		}
		if stored.ArenaWidth != 1200 || stored.ArenaHeight != 750 {
			t.Fatalf("procedural map defaults wrong: %+v", stored)
		}
	}
}

func TestCreateMatchRejectsUnknownMapID(t *testing.T) {
	for _, body := range []string{`{"mapId":"random-nope"}`, `{"mapId":"super-fort"}`} {
		h := newHarness(t)
		response, payload := h.request(t, http.MethodPost, "/api/matches", body)
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("body %s accepted with status %d: %v", body, response.StatusCode, payload)
		}
		if payload["error"] == "" {
			t.Fatalf("body %s rejected without useful error: %v", body, payload)
		}
	}
}

func TestCreateSquadSeedsFiveBotsPerSide(t *testing.T) {
	h := newHarness(t)
	response, payload := h.request(t, http.MethodPost, "/api/matches", `{"mode":"squad","botDifficulty":"fighter"}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create squad failed: %d %v", response.StatusCode, payload)
	}
	robots := payload["robots"].([]any)
	if len(robots) != 10 {
		t.Fatalf("squad lobby must seed 10 bots, got %d", len(robots))
	}
	red, blue := 0, 0
	for _, raw := range robots {
		robot := raw.(map[string]any)
		switch robot["team"] {
		case "red":
			red++
		case "blue":
			blue++
		}
		if robot["bot"] != true {
			t.Fatalf("squad seed must be bots: %v", robot)
		}
	}
	if red != 5 || blue != 5 {
		t.Fatalf("squad must open 5v5: red=%d blue=%d", red, blue)
	}
	if payload["mode"] != "squad" || payload["mapId"] != "corridors" || payload["practice"] != true {
		t.Fatalf("squad defaults wrong: mode=%v mapId=%v practice=%v", payload["mode"], payload["mapId"], payload["practice"])
	}
}

func TestSquadRegistrationDisplacesBotOnChosenTeam(t *testing.T) {
	h := newHarness(t)
	h.provisioner.box = readyBox()
	h.provisioner.readMain = "-- squad robot"
	_, payload := h.request(t, http.MethodPost, "/api/matches", `{"mode":"squad"}`)
	matchID := payload["matchId"].(string)
	response, payload := h.request(t, http.MethodPost, "/api/matches/"+matchID+"/robots", `{"displayName":"Ada","team":"blue","startCommand":"lua main.lua"}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("squad registration failed: %d %v", response.StatusCode, payload)
	}
	robots := h.store.matches[matchID].Robots
	if len(robots) != 10 {
		t.Fatalf("squad roster must stay at 10 robots: %d", len(robots))
	}
	redBots, blueBots, bluePlayers := 0, 0, 0
	for _, robot := range robots {
		switch {
		case robot.Bot && robot.Team == "red":
			redBots++
		case robot.Bot && robot.Team == "blue":
			blueBots++
		case !robot.Bot && robot.Team == "blue":
			bluePlayers++
		default:
			t.Fatalf("unexpected roster entry: %+v", robot)
		}
	}
	if redBots != 5 || blueBots != 4 || bluePlayers != 1 {
		t.Fatalf("blue bot not displaced: redBots=%d blueBots=%d bluePlayers=%d", redBots, blueBots, bluePlayers)
	}
}

func TestSquadRegistrationRejectsFullTeam(t *testing.T) {
	h := newHarness(t)
	h.provisioner.box = readyBox()
	h.provisioner.readMain = "-- squad robot"
	match := model.Match{MatchID: "m1", OwnerID: testUserID, Status: model.MatchLobby, Mode: "squad"}
	for i := 0; i < 5; i++ {
		match.Robots = append(match.Robots, model.RobotSubmission{RobotID: fmt.Sprintf("r%d", i), Team: "red", PlayerID: fmt.Sprintf("p%d", i), OwnerBoxID: fmt.Sprintf("box-%d", i)})
	}
	if err := h.store.PutMatch(context.Background(), match); err != nil {
		t.Fatal(err)
	}
	response, payload := h.request(t, http.MethodPost, "/api/matches/m1/robots", `{"displayName":"Ada","team":"red","startCommand":"lua main.lua"}`)
	if response.StatusCode != http.StatusConflict || payload["error"] != "team is full: five robots per side" {
		t.Fatalf("full team must be rejected: %d %v", response.StatusCode, payload)
	}
}

func TestSquadAutoStartWaitsForHumanOnEachSide(t *testing.T) {
	h := newHarness(t)
	match := model.Match{MatchID: "m1", OwnerID: testUserID, Status: model.MatchLobby, Mode: "squad"}
	match.Robots = []model.RobotSubmission{
		{RobotID: "bot-r1", Team: "red", Bot: true},
		{RobotID: "bot-b1", Team: "blue", Bot: true},
		{RobotID: "r1", Team: "red", PlayerID: testUserID, OwnerBoxID: testBoxID},
	}
	if err := h.store.PutMatch(context.Background(), match); err != nil {
		t.Fatal(err)
	}
	attachAgents(h.app, "r1")
	h.app.autoStartIfReady("m1")
	if h.store.matches["m1"].Status != model.MatchLobby {
		t.Fatalf("one-sided squad lobby must not auto-start: %+v", h.store.matches["m1"])
	}
	match.Robots = append(match.Robots, model.RobotSubmission{RobotID: "r2", Team: "blue", PlayerID: "alice", OwnerBoxID: "box-alice"})
	if err := h.store.PutMatch(context.Background(), match); err != nil {
		t.Fatal(err)
	}
	attachAgents(h.app, "r2")
	h.app.autoStartIfReady("m1")
	if h.store.matches["m1"].Status != model.MatchQueued {
		t.Fatalf("squad lobby with humans on both sides should auto-start: %+v", h.store.matches["m1"])
	}
}

func TestSoloModeAssignsUniqueTeamsPerRobot(t *testing.T) {
	h := newHarness(t)
	h.provisioner.box = readyBox()
	h.provisioner.readMain = "-- solo robot"
	_, payload := h.request(t, http.MethodPost, "/api/matches", `{"mode":"solo","bots":2,"botDifficulty":"fighter"}`)
	matchID := payload["matchId"].(string)
	response, payload := h.request(t, http.MethodPost, "/api/matches/"+matchID+"/robots", `{"displayName":"Ada","team":"red","startCommand":"lua main.lua"}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("solo registration failed: %d %v", response.StatusCode, payload)
	}
	stored := h.store.matches[matchID]
	if len(stored.Robots) != 3 {
		t.Fatalf("solo roster wrong: %+v", stored.Robots)
	}
	teams := map[string]int{}
	for _, robot := range stored.Robots {
		teams[robot.Team]++
	}
	for team, count := range teams {
		if count != 1 {
			t.Fatalf("solo teams must be unique per robot: %q appears %d times", team, count)
		}
	}
	player := payload["match"].(map[string]any)["robots"].([]any)
	var playerTeam any
	for _, raw := range player {
		robot := raw.(map[string]any)
		if robot["ownerBoxId"] != nil {
			playerTeam = robot["team"]
		}
	}
	if playerTeam != "solo-03" {
		t.Fatalf("player should be assigned solo-03 after two bots, got %v", playerTeam)
	}
	if stored.Practice != true {
		t.Fatalf("solo matches must be unranked practice: %+v", stored)
	}
}

func TestSoloWithoutBotsStartsSingleTeamSandbox(t *testing.T) {
	h := newHarness(t)
	h.provisioner.box = readyBox()
	h.provisioner.readMain = "-- sandbox robot"
	_, payload := h.request(t, http.MethodPost, "/api/matches", `{"mode":"solo","bots":0}`)
	matchID := payload["matchId"].(string)
	_, payload = h.request(t, http.MethodPost, "/api/matches/"+matchID+"/robots", `{"displayName":"Ada","team":"red","startCommand":"lua main.lua"}`)
	if response := payload; response == nil {
		t.Fatal("missing registration payload")
	}
	robotID := payload["agent"].(map[string]any)["robotId"].(string)
	attachAgents(h.app, robotID)
	response, payload := h.request(t, http.MethodPost, "/api/matches/"+matchID+"/start", "")
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("single-robot sandbox must be startable: %d %v", response.StatusCode, payload)
	}
	if h.store.matches[matchID].Status != model.MatchQueued {
		t.Fatalf("sandbox not queued: %+v", h.store.matches[matchID])
	}
}

func TestHubSeparatesArenaLayoutFromSnapshot(t *testing.T) {
	hub := NewHub()
	channel, unsubscribe := hub.Subscribe("m1")
	defer unsubscribe()
	hub.Publish("m1", engine.Snapshot{Type: "snapshot", MatchID: "m1", MapID: "map-1", Width: 800, Height: 500, Obstacles: []engine.Obstacle{{ID: "wall", Shape: "rect", Width: 40, Height: 20}}})
	message := <-channel
	if !bytes.Contains(message.layout, []byte(`"type":"arena_layout"`)) || !bytes.Contains(message.layout, []byte(`"id":"wall"`)) {
		t.Fatalf("layout payload missing geometry: %s", message.layout)
	}
	if bytes.Contains(message.payload, []byte(`"obstacles"`)) || !bytes.Contains(message.payload, []byte(`"type":"snapshot"`)) {
		t.Fatalf("dynamic snapshot contains layout or is malformed: %s", message.payload)
	}

	hub.Publish("m1", map[string]any{"type": "agent_status"})
	status := <-channel
	if !bytes.Contains(status.payload, []byte(`"type":"agent_status"`)) || len(status.layout) == 0 {
		t.Fatalf("cached layout was not retained with later events: payload=%s layout=%s", status.payload, status.layout)
	}
}

func TestHubReleasesEmptySubscriptionsAndForgottenEvents(t *testing.T) {
	hub := NewHub()
	channel, unsubscribe := hub.Subscribe("m1")
	hub.Publish("m1", "snapshot")
	if got := <-channel; string(got.payload) != `"snapshot"` {
		t.Fatalf("subscriber received %s, want snapshot", got.payload)
	}
	unsubscribe()
	if counts := hub.ViewerCounts(); len(counts) != 0 {
		t.Fatalf("empty subscriber map retained: %#v", counts)
	}

	hub.Forget("m1")
	late, lateUnsubscribe := hub.Subscribe("m1")
	defer lateUnsubscribe()
	select {
	case event := <-late:
		t.Fatalf("forgotten event was retained: %s", event.payload)
	default:
	}
}
