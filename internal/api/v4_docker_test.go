package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	robotauth "github.com/kryxen/cloud-robot/internal/auth"
	"github.com/kryxen/cloud-robot/internal/boxes"
	"github.com/kryxen/cloud-robot/internal/cloud"
	"github.com/kryxen/cloud-robot/internal/model"
)

type smokeIdentity struct {
	token string
	users map[string]string
}

func (s smokeIdentity) Verify(ctx context.Context, header string) (context.Context, error) {
	for token, user := range s.users {
		if header == "Bearer "+s.token+token {
			return robotauth.WithUserID(ctx, user), nil
		}
	}
	return ctx, fmt.Errorf("smoke credential required")
}

// Uses real Floci, provisioner, SSH, Lua 5.4 and WebSocket agents. Only account
// sign-in is replaced inside this loopback-only test process; production
// authentication is never changed. All Docker boxes belong to this test run.
func TestV4DockerSSHSmoke(t *testing.T) {
	if os.Getenv("ARENA_DOCKER_SMOKE") != "1" {
		t.Skip("set ARENA_DOCKER_SMOKE=1 with the Compose stack running")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	store, err := cloud.New(ctx, cloud.Config{Endpoint: "http://localhost:4566", Region: "us-east-1", Bucket: "robot-arena-v4-smoke", Table: "robot-arena-v4-smoke", Queue: "robot-arena-v4-smoke"})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	provisioner := boxes.NewClient("http://localhost:8090", os.Getenv("PROVISIONER_TOKEN"))
	app := NewServer(store, provisioner)
	run := uuid.NewString()
	identity := smokeIdentity{token: uuid.NewString(), users: map[string]string{"a": "smoke-" + run + "-a", "b": "smoke-" + run + "-b"}}
	app.auth = identity
	server := httptest.NewServer(app.Handler())
	defer server.Close()
	port := strings.Split(server.URL, ":")[2]
	t.Setenv("ROBOT_AGENT_BASE_URL", "ws://host.docker.internal:"+port)
	worker, _ := filepath.Abs("../../crates/arena-engine/target/release/arena-engine")
	t.Setenv("ARENA_ENGINE_PATH", worker)
	call := func(user, method, path string, body any) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(body)
		r, err := http.NewRequestWithContext(ctx, method, server.URL+path, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Authorization", "Bearer "+identity.token+user)
		r.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var result map[string]any
		if err = json.NewDecoder(response.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		if response.StatusCode >= 300 {
			t.Fatalf("%s %s: %d %v", method, path, response.StatusCode, result)
		}
		return result
	}
	key := filepath.Join(t.TempDir(), "key")
	if output, err := exec.CommandContext(ctx, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", key).CombinedOutput(); err != nil {
		t.Fatalf("key generation: %s %v", output, err)
	}
	public, err := os.ReadFile(key + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	source := `local arena=require "arena"
arena.run({url=assert(os.getenv("ROBOT_ARENA_URL")),token=assert(os.getenv("ROBOT_TOKEN")),decide=function(o)
return arena.control({throttle=0.8,turn=0.1,fire=true,label="SSH_SMOKE"}) end})
`
	// ARENA_SMOKE_SCRIPT runs a real strategy file instead and additionally
	// checks that its robots travel rather than circling in place.
	behaviour := os.Getenv("ARENA_SMOKE_SCRIPT")
	duration := 12
	if behaviour != "" {
		raw, readErr := os.ReadFile(filepath.Join("..", "..", behaviour))
		if readErr != nil {
			t.Fatal(readErr)
		}
		// Zone phases scale with match length; 90 s keeps them realistic.
		source, duration = string(raw), 90
	}
	for _, user := range []string{"a", "b"} {
		boxID := boxes.IDForUser(identity.users[user])
		// Never remove anything not created for this uniquely named smoke run.
		t.Cleanup(func() {
			_ = exec.Command("docker", "rm", "-f", boxID).Run()
			_ = exec.Command("docker", "volume", "rm", boxID+"-workspace", boxID+"-control").Run()
		})
		record := call(user, "POST", "/api/me/box", nil)
		call(user, "PUT", "/api/me/box/ssh-key", map[string]string{"publicKey": string(public)})
		sshPort := int(record["sshPort"].(float64))
		var output []byte
		for attempt := 0; attempt < 20; attempt++ {
			command := exec.CommandContext(ctx, "ssh", "-i", key, "-p", strconv.Itoa(sshPort), "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=accept-new", "-o", "UserKnownHostsFile="+key+".known_hosts", "-o", "ConnectTimeout=2", "developer@127.0.0.1", "cat > /workspace/main.lua")
			command.Stdin = strings.NewReader(source)
			output, err = command.CombinedOutput()
			if err == nil {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		if err != nil {
			t.Fatalf("SSH write: %s %v", output, err)
		}
		if _, err = provisioner.Restart(ctx, boxID); err != nil {
			t.Fatal(err)
		}
		var saved string
		for attempt := 0; attempt < 20; attempt++ {
			saved, err = provisioner.ReadMain(ctx, boxID)
			if err == nil {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		if err != nil || saved != source {
			t.Fatalf("workspace lost after restart: %v", err)
		}
	}
	match := call("a", "POST", "/api/v4/matches", map[string]any{"mode": "quick-duel", "capacity": 2, "width": 2400, "height": 1500, "durationSeconds": duration, "seed": 42})
	id := match["matchId"].(string)
	for _, user := range []string{"a", "b"} {
		call(user, "POST", "/api/matches/"+id+"/robots", map[string]any{"displayName": "Smoke " + user, "runtime": "lua5.4", "startCommand": "lua main.lua", "sdkVersion": "0.4.0", "loadout": map[string]any{"chassis": "generalist", "weapon": "plasma"}})
	}
	for attempt := 0; attempt < 100 && app.agents.Count() < 2; attempt++ {
		time.Sleep(200 * time.Millisecond)
	}
	if app.agents.Count() != 2 {
		t.Fatal("Lua agents did not connect")
	}
	call("a", "POST", "/api/matches/"+id+"/start", nil)
	// Poll the owner live view while the match runs: scripts built on
	// arena.tactics draw debug marks, which must arrive there.
	var sawMarks atomic.Bool
	polling, stopPolling := context.WithCancel(ctx)
	defer stopPolling()
	if behaviour != "" {
		go func() {
			for polling.Err() == nil {
				r, _ := http.NewRequestWithContext(polling, http.MethodGet, server.URL+"/api/v4/matches/"+id+"/view", nil)
				r.Header.Set("Authorization", "Bearer "+identity.token+"a")
				if response, err := http.DefaultClient.Do(r); err == nil {
					body, _ := io.ReadAll(response.Body)
					response.Body.Close()
					if strings.Contains(string(body), `"debug":[`) {
						sawMarks.Store(true)
					}
				}
				time.Sleep(500 * time.Millisecond)
			}
		}()
	}
	if err = app.runMatch(ctx, id); err != nil {
		t.Fatal(err)
	}
	stopPolling()
	if behaviour != "" && strings.Contains(source, "arena.tactics(") && !sawMarks.Load() {
		t.Error("owner live view never carried script debug marks")
	}
	completed, err := store.GetMatch(ctx, id)
	if err != nil || completed.Status != model.MatchFinished {
		t.Fatalf("unfinished match: %v", err)
	}
	for _, robot := range completed.Robots {
		if robot.ScriptObjectKey == "" {
			t.Fatal("registration did not snapshot source")
		}
		saved, err := store.GetScript(ctx, robot.ScriptObjectKey)
		if err != nil || saved != source {
			t.Fatal("immutable source mismatch")
		}
	}
	inputs, err := store.GetReplayObject(ctx, "replays/"+id+"/v4/inputs-000000.json")
	if err != nil || (behaviour == "" && !strings.Contains(inputs, "SSH_SMOKE")) || !strings.Contains(inputs, "label") {
		for _, user := range []string{"a", "b"} {
			output, _ := exec.Command("docker", "logs", "--tail", "12", boxes.IDForUser(identity.users[user])).CombinedOutput()
			t.Logf("box %s: %s", user, output)
		}
		if len(inputs) > 1000 {
			inputs = inputs[:1000]
		}
		t.Logf("input read error: %v; sample: %s", err, inputs)
		t.Fatal("no accepted Lua decisions in durable replay")
	}
	frames, err := store.GetReplayObject(ctx, "replays/"+id+"/v4/frames-000000.gz.b64")
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := unpackFrames(frames)
	if err != nil {
		t.Fatal(err)
	}
	fired := false
	for _, frame := range decoded {
		var s struct {
			Projectiles []json.RawMessage `json:"projectiles"`
		}
		_ = json.Unmarshal(frame, &s)
		fired = fired || len(s.Projectiles) > 0
	}
	if behaviour != "" {
		fired = checkLuaMovement(t, ctx, store, id, completed.Robots) || fired
		if t.Failed() {
			for _, user := range []string{"a", "b"} {
				output, _ := exec.Command("docker", "logs", "--tail", "15", boxes.IDForUser(identity.users[user])).CombinedOutput()
				t.Logf("box %s log tail:\n%s", user, output)
			}
		}
	}
	if !fired {
		t.Fatal("Lua agents never fired")
	}
	t.Logf("two SSH workspaces survived restart; two Lua agents fired; match %s and replay persisted in Floci", id)
}

// Rechecks an existing smoke result after a separate Compose stop/start.
func TestV4StoredSmoke(t *testing.T) {
	id := os.Getenv("ARENA_SMOKE_MATCH_ID")
	if id == "" {
		t.Skip("set ARENA_SMOKE_MATCH_ID after a successful Docker smoke")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	store, err := cloud.New(ctx, cloud.Config{Endpoint: "http://localhost:4566", Region: "us-east-1", Bucket: "robot-arena-v4-smoke", Table: "robot-arena-v4-smoke", Queue: "robot-arena-v4-smoke"})
	if err != nil {
		t.Fatal(err)
	}
	m, err := store.GetMatch(ctx, id)
	if err != nil || m.Status != model.MatchFinished {
		t.Fatalf("stored match: %v", err)
	}
	if len(m.Robots) != 2 {
		t.Fatal("stored roster changed")
	}
	for _, robot := range m.Robots {
		source, err := store.GetScript(ctx, robot.ScriptObjectKey)
		if err != nil || !strings.Contains(source, "SSH_SMOKE") {
			t.Fatalf("stored source: %v", err)
		}
	}
	inputs, err := store.GetReplayObject(ctx, "replays/"+id+"/v4/inputs-000000.json")
	if err != nil || !strings.Contains(inputs, "SSH_SMOKE") {
		t.Fatalf("stored inputs: %v", err)
	}
	frames, err := store.GetReplayObject(ctx, "replays/"+id+"/v4/frames-000000.gz.b64")
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := unpackFrames(frames)
	if err != nil || len(decoded) == 0 {
		t.Fatalf("stored frames: %v", err)
	}
}

// checkLuaMovement reads every replay page and asserts each human robot
// covered real ground and left its spawn area, then logs decision labels.
func checkLuaMovement(t *testing.T, ctx context.Context, store *cloud.Store, id string, robots []model.RobotSubmission) (fired bool) {
	t.Helper()
	type point struct{ x, y float64 }
	start, last := map[string]point{}, map[string]point{}
	path, reach := map[string]float64{}, map[string]float64{}
	trail := map[string]string{}
	for page := 0; ; page++ {
		raw, err := store.GetReplayObject(ctx, fmt.Sprintf("replays/%s/v4/frames-%06d.gz.b64", id, page))
		if err != nil {
			break
		}
		frames, err := unpackFrames(raw)
		if err != nil {
			t.Fatal(err)
		}
		for _, frame := range frames {
			var s struct {
				Robots []struct {
					RobotID string  `json:"robotId"`
					X       float64 `json:"x"`
					Y       float64 `json:"y"`
					Alive   bool    `json:"alive"`
					HP      float64 `json:"hp"`
					Heading float64 `json:"heading"`
				} `json:"robots"`
				Tick        uint32            `json:"tick"`
				Projectiles []json.RawMessage `json:"projectiles"`
				Zone        *struct {
					X, Y, Radius float64
				} `json:"zone"`
			}
			_ = json.Unmarshal(frame, &s)
			fired = fired || len(s.Projectiles) > 0
			for _, r := range s.Robots {
				if s.Tick%100 == 0 {
					zone := ""
					if s.Zone != nil {
						zone = fmt.Sprintf(" zone(%.0f,%.0f r%.0f)", s.Zone.X, s.Zone.Y, s.Zone.Radius)
					}
					trail[r.RobotID] += fmt.Sprintf(" t%d(%.0f,%.0f h%.0f hp%.0f%s)%s", s.Tick, r.X, r.Y, r.Heading, r.HP, map[bool]string{true: "", false: " DEAD"}[r.Alive], zone)
				}
				now := point{r.X, r.Y}
				if _, ok := start[r.RobotID]; !ok {
					start[r.RobotID] = now
				}
				if prev, ok := last[r.RobotID]; ok && r.Alive {
					path[r.RobotID] += math.Hypot(now.x-prev.x, now.y-prev.y)
				}
				last[r.RobotID] = now
				reach[r.RobotID] = math.Max(reach[r.RobotID], math.Hypot(now.x-start[r.RobotID].x, now.y-start[r.RobotID].y))
			}
		}
	}
	labels := map[string]int{}
	for page := 0; ; page++ {
		raw, err := store.GetReplayObject(ctx, fmt.Sprintf("replays/%s/v4/inputs-%06d.json", id, page))
		if err != nil {
			break
		}
		if at := strings.Index(raw, "script error"); at >= 0 {
			t.Logf("script error in inputs: %s", raw[at:min(len(raw), at+400)])
		}
		for _, part := range strings.Split(raw, `"label":"`)[1:] {
			labels[part[:strings.IndexByte(part, '"')]]++
		}
	}
	t.Logf("labels: %v", labels)
	for _, robot := range robots {
		t.Logf("%s travelled %.0f units, reached %.0f from spawn", robot.DisplayName, path[robot.RobotID], reach[robot.RobotID])
		if path[robot.RobotID] < 300 || reach[robot.RobotID] < 150 {
			t.Errorf("%s barely moved: path %.0f, reach %.0f; trail:%s", robot.DisplayName, path[robot.RobotID], reach[robot.RobotID], trail[robot.RobotID])
		}
	}
	return fired
}
