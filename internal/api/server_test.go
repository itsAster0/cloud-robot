package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	robotauth "github.com/kryxen/cloud-robot/internal/auth"
	"github.com/kryxen/cloud-robot/internal/boxes"
	"github.com/kryxen/cloud-robot/internal/cloud"
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
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		matches:     map[string]model.Match{},
		boxes:       map[string]model.BoxRecord{},
		credentials: map[string]cloud.AgentCredential{},
		scripts:     map[string]string{},
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
func (f *fakeStore) PutBox(_ context.Context, userID string, box model.BoxRecord) error {
	f.boxes[userID] = box
	return nil
}
func (f *fakeStore) PutScript(_ context.Context, key, source string) error {
	f.scripts[key] = source
	return nil
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
	if strings.TrimPrefix(authorization, "Bearer ") == "" {
		if f.required {
			return ctx, errors.New("sign in required")
		}
		return robotauth.WithUserID(ctx, "guest"), nil
	}
	return robotauth.WithUserID(ctx, testUserID), nil
}

type harness struct {
	store       *fakeStore
	provisioner *fakeProvisioner
	server      *httptest.Server
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{store: newFakeStore(), provisioner: &fakeProvisioner{}}
	app := NewServer(h.store, h.provisioner)
	app.auth = &fakeAuth{}
	h.server = httptest.NewServer(app.Handler())
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
	if err := h.store.PutMatch(context.Background(), model.Match{MatchID: "m2", Status: model.MatchRunning}); err != nil {
		t.Fatal(err)
	}
	h.provisioner.box = readyBox()
	h.provisioner.box.ActiveMatchID = "m2"
	h.provisioner.readMain = "x"
	response, payload := h.request(t, http.MethodPost, "/api/matches/m1/robots", `{"displayName":"Ada","team":"red","startCommand":"lua main.lua"}`)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("expected conflict, got %d %v", response.StatusCode, payload)
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
