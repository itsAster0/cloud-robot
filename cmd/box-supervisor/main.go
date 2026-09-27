package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/kryxen/cloud-robot/internal/boxes"
	"github.com/kryxen/cloud-robot/internal/model"
)

const controlDir = "/var/lib/robot-box"

type supervisor struct {
	mu         sync.Mutex
	process    *exec.Cmd
	configHash [32]byte
	record     model.BoxRecord
}

func main() {
	action := "daemon"
	if len(os.Args) > 1 {
		action = os.Args[1]
	}
	switch action {
	case "daemon":
		runDaemon()
	case "set-key":
		setKey()
	case "configure-agent":
		configureAgent()
	case "validate-main":
		data, err := io.ReadAll(io.LimitReader(os.Stdin, 16*1024+1))
		fatal(err)
		fatal(validateSource(data))
	case "write-main-if-match":
		var input struct {
			Source   string `json:"source"`
			Revision string `json:"revision"`
		}
		fatal(json.NewDecoder(io.LimitReader(os.Stdin, 128*1024)).Decode(&input))
		fatal(writeMainRevision("/workspace/main.lua", input.Source, input.Revision))
	case "read-main":
		readMain()
	case "write-main":
		writeMain()
	case "status":
		status()
	default:
		fatal(errors.New("unknown supervisor action"))
	}
}

func runDaemon() {
	_ = os.MkdirAll(controlDir, 0700)
	if _, err := os.Stat("/workspace/main.lua"); errors.Is(err, os.ErrNotExist) {
		_ = os.WriteFile("/workspace/main.lua", []byte(defaultScript), 0644)
		_ = os.Chown("/workspace/main.lua", 1000, 1000)
	}
	s := &supervisor{record: model.BoxRecord{AgentStatus: "idle"}}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	s.sync(ctx)
	for {
		select {
		case <-ctx.Done():
			s.stop()
			return
		case <-ticker.C:
			s.sync(ctx)
		}
	}
}

func (s *supervisor) sync(ctx context.Context) {
	usage, err := boxes.WorkspaceUsage("/workspace")
	if err != nil {
		s.record.Error = err.Error()
	}
	s.record.UsageBytes, s.record.UpdatedAt = usage, time.Now().UTC()
	key, _, _ := boxes.ValidatePublicKey(readFile(filepath.Join(controlDir, "authorized_keys")))
	if key != "" {
		_, s.record.KeyFingerprint, _ = boxes.ValidatePublicKey(key)
	}
	quota := int64(boxes.DefaultStorageBytes)
	if value, parseErr := strconv.ParseInt(os.Getenv("BOX_STORAGE_BYTES"), 10, 64); parseErr == nil && value > 0 {
		quota = value
	}
	configData, configErr := os.ReadFile(filepath.Join(controlDir, "agent.json"))
	if boxes.QuotaBreached(usage, quota) {
		if s.record.AgentStatus != "quota_exceeded" {
			logf("workspace quota exceeded: %d of %d bytes; agent stopped", usage, quota)
		}
		s.stop()
		s.record.AgentStatus = "quota_exceeded"
		s.record.Error = fmt.Sprintf("workspace usage %d bytes exceeds %d byte quota; delete files over SSH to resume", usage, quota)
		s.writeStatus()
		return
	}
	if configErr != nil {
		s.record.AgentStatus = "idle"
		s.writeStatus()
		return
	}
	hash := sha256.Sum256(configData)
	s.mu.Lock()
	changed := hash != s.configHash
	running := s.process != nil
	s.mu.Unlock()
	if running {
		s.record.AgentStatus = "running"
	}
	if changed || !running {
		var config boxes.AgentConfig
		if err := json.Unmarshal(configData, &config); err != nil {
			s.record.AgentStatus = "failed"
			s.record.Error = "invalid agent configuration"
			s.writeStatus()
			return
		}
		s.record.ActiveRobotID, s.record.ActiveMatchID = config.RobotID, config.MatchID
		logf("starting agent robot=%s match=%s command=%q", config.RobotID, config.MatchID, config.StartCommand)
		s.stop()
		s.configHash = hash
		command := exec.CommandContext(ctx, "su", "-s", "/bin/sh", "developer", "-c", "cd /workspace && exec "+config.StartCommand)
		command.Env = append(os.Environ(), "ROBOT_ARENA_URL="+config.URL, "ROBOT_TOKEN="+config.Token, "ROBOT_ID="+config.RobotID)
		command.Stdout, command.Stderr = os.Stdout, os.Stderr
		if err := command.Start(); err != nil {
			s.record.AgentStatus = "failed"
			s.record.Error = err.Error()
		} else {
			s.mu.Lock()
			s.process = command
			s.mu.Unlock()
			s.record.AgentStatus = "starting"
			s.record.Error = ""
			go s.wait(command)
		}
	}
	s.writeStatus()
}

func (s *supervisor) wait(command *exec.Cmd) {
	err := command.Wait()
	s.mu.Lock()
	if s.process == command {
		s.process = nil
		if err != nil {
			s.record.AgentStatus = "failed"
			s.record.Error = err.Error()
			logf("agent exited: %v", err)
		} else {
			s.record.AgentStatus = "stopped"
			logf("agent exited cleanly")
		}
	}
	s.mu.Unlock()
	s.writeStatus()
}
func (s *supervisor) stop() {
	s.mu.Lock()
	process := s.process
	s.process = nil
	s.mu.Unlock()
	if process != nil && process.Process != nil {
		_ = process.Process.Signal(syscall.SIGTERM)
		time.Sleep(250 * time.Millisecond)
		_ = process.Process.Kill()
	}
}
func (s *supervisor) writeStatus() {
	s.mu.Lock()
	record := s.record
	s.mu.Unlock()
	data, _ := json.Marshal(record)
	_ = writeAtomic(filepath.Join(controlDir, "status.json"), data, 0600)
}

func setKey() {
	data, err := io.ReadAll(io.LimitReader(os.Stdin, boxes.MaxPublicKeyBytes+1))
	if err != nil {
		fatal(err)
	}
	key, _, err := boxes.ValidatePublicKey(string(data))
	if err != nil {
		fatal(err)
	}
	// 0644 root-owned: sshd reads authorized_keys as the developer user, while
	// the 0711 control dir keeps agent credentials unreadable and unlistable.
	fatal(writeAtomic(filepath.Join(controlDir, "authorized_keys"), []byte(key+"\n"), 0644))
}
func configureAgent() {
	data, err := io.ReadAll(io.LimitReader(os.Stdin, 128*1024))
	if err != nil {
		fatal(err)
	}
	var config boxes.AgentConfig
	if err = json.Unmarshal(data, &config); err != nil {
		fatal(err)
	}
	if err := boxes.ValidateAgentConfig(config); err != nil {
		fatal(err)
	}
	if err := validateLuaSyntax("/workspace/main.lua"); err != nil {
		fatal(err)
	}
	if config.ImmutableSource != "" {
		fatal(validateSource([]byte(config.ImmutableSource)))
		fatal(os.MkdirAll("/opt/robot-arena-run", 0755))
		fatal(writeAtomic("/opt/robot-arena-run/main.lua", []byte(config.ImmutableSource), 0644))
		config.StartCommand = "lua /opt/robot-arena-run/main.lua"
		data, err = json.Marshal(config)
		fatal(err)
	}
	fatal(writeAtomic(filepath.Join(controlDir, "agent.json"), data, 0600))
}
func readMain() {
	data, err := os.ReadFile("/workspace/main.lua")
	if err != nil {
		fatal(err)
	}
	if len(data) > 16*1024 {
		fatal(errors.New("main.lua exceeds 16 KiB"))
	}
	_, _ = os.Stdout.Write(data)
}

// writeMain replaces /workspace/main.lua from stdin. Same 16 KiB cap as
// read-main so a deployed script can always be snapshotted back.
func writeMain() {
	data, err := io.ReadAll(io.LimitReader(os.Stdin, 16*1024+1))
	if err != nil {
		fatal(err)
	}
	if len(data) > 16*1024 {
		fatal(errors.New("main.lua exceeds 16 KiB"))
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		fatal(errors.New("main.lua must not be empty"))
	}
	if err := writeValidatedLua("/workspace/main.lua", data); err != nil {
		fatal(err)
	}
}

func writeValidatedLua(path string, data []byte) error {
	temporary := path + ".validate"
	if err := os.WriteFile(temporary, data, 0644); err != nil {
		return err
	}
	defer os.Remove(temporary)
	if err := validateLuaSyntax(temporary); err != nil {
		return err
	}
	if err := os.Chown(temporary, 1000, 1000); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

// luaInterpreter is a variable so tests can fall back to another Lua binary.
var luaInterpreter = "lua5.4"

// validateLuaSyntax parses the chunk without executing it. The path must be
// embedded in the -e chunk: passing it as a trailing argument makes the
// standalone interpreter run the file as its main chunk after loading, which
// executed bot code (as root, without ROBOT_* env) during validation.
func validateLuaSyntax(path string) error {
	command := exec.Command(luaInterpreter, "-e", "assert(loadfile("+strconv.Quote(path)+"))")
	output, err := command.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("Lua syntax error: %s", message)
	}
	return nil
}
func status() {
	data, err := os.ReadFile(filepath.Join(controlDir, "status.json"))
	if err != nil {
		data = []byte(`{"agentStatus":"idle"}`)
	}
	_, _ = os.Stdout.Write(data)
}
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, mode); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
func readFile(path string) string { data, _ := os.ReadFile(path); return string(data) }
func fatal(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// defaultScript is written to /workspace/main.lua on first boot: a compact
// all-rounder that still exercises items, zone, and randomized dodging so a
// fresh box is immediately competitive without any edits.
const defaultScript = `local arena = require "arena"

-- Vanguard: engages the nearest enemy with lead shots, grabs heals and
-- upgrades, respects the collapsing zone, and dodges on random intervals.
-- Randomness is seeded from the robot ID so boxes diverge without wall clock.

local seed = 0
local robot_id = os.getenv("ROBOT_ID") or "vanguard"
for index = 1, #robot_id do
  seed = (seed * 131 + robot_id:byte(index)) % 2147483647
end
math.randomseed(seed)

local PROJECTILE_SPEED = { plasma = 24, cannon = 14, machine_gun = 32, incendiary = 20, cryo = 20, emp = 18, railgun = 0 }
local ITEM_SCORE = {
  heal = 90, ["repair-core"] = 60, shield = 70, overdrive = 65, rapid_fire = 65,
  weapon_railgun = 100, weapon_cannon = 80, weapon_incendiary = 55,
  weapon_cryo = 50, weapon_emp = 45, weapon_machine_gun = 35, weapon_plasma = 30,
}

local state = { strafe = 1, flip_at = 0, last_x = nil, last_y = nil, stuck = 0, escape = 0, escape_turn = 0, wander = nil }

local function unstick(obs, action)
  local self = obs.self
  if state.last_x then
    local moved = arena.distance({ x = state.last_x, y = state.last_y }, self)
    if action.moving and moved < 1.5 then state.stuck = state.stuck + 1 else state.stuck = 0 end
  end
  state.last_x, state.last_y = self.x, self.y
  if state.stuck > 6 then
    state.stuck, state.escape = 0, 8
    state.escape_turn = (math.random() < 0.5 and -1 or 1) * (10 + math.random() * 8)
  end
  if state.escape > 0 then
    state.escape = state.escape - 1
    return arena.action({ move = -4, turn = state.escape_turn, logs = { "backing off obstacle" } })
  end
  return nil
end

local function navigate(obs, x, y, speed, log)
  local self = obs.self
  local goal = { x = x, y = y }
  if not arena.line_of_sight(self.x, self.y, x, y, obs.obstacles) then
    local detour = arena.path_to(x, y, obs)[1]
    if detour then goal = detour end
  end
  local turn = ((arena.bearing(self, goal) - self.heading + 540) % 360) - 180
  return arena.action({ move = speed, turn = turn, logs = { log or "moving" } })
end

local function item_score(obs, item)
  local self, base = obs.self, ITEM_SCORE[item.type]
  if not base then return 0 end
  if item.type == "heal" or item.type == "repair-core" then
    if obs.overtime or self.hp >= self.maxHp then return 0 end
    if self.hp < self.maxHp * 0.5 then base = base * 2 end
  end
  if item.type == "shield" and self.shield >= 50 then return 0 end
  if self.weapon ~= "" and item.type == "weapon_" .. self.weapon then return 0 end
  return base
end

local function best_item(obs, max_distance)
  local best, best_value
  for _, item in ipairs(obs.items or {}) do
    if item.active ~= false then
      local score = item_score(obs, item)
      local distance = arena.distance(obs.self, item)
      if score > 0 and distance <= (max_distance or math.huge) then
        local value = score / (distance + 20)
        if not best_value or value > best_value then best, best_value = item, value end
      end
    end
  end
  return best
end

local function lead_target(obs, enemy)
  local speed = PROJECTILE_SPEED[obs.self.weapon] or 14
  if speed <= 0 then return enemy.x, enemy.y end
  local flight = math.min(arena.distance(obs.self, enemy) / speed, 8)
  local radians = enemy.heading * math.pi / 180
  return enemy.x + math.cos(radians) * flight * 8 * 0.7, enemy.y + math.sin(radians) * flight * 8 * 0.7
end

local function zone_safe(obs)
  local zone = obs.zone
  if not zone or not zone.active then return true end
  return arena.distance(obs.self, zone) < zone.radius - 40
end

local function decide(observation)
  local self = observation.self

  if not zone_safe(observation) then
    local held = unstick(observation, { moving = true })
    if held then return held end
    return navigate(observation, observation.zone.x, observation.zone.y, 7, "returning to zone")
  end

  local enemy, distance, visible
  for _, robot in ipairs(observation.robots or {}) do
    if robot.alive and robot.team ~= self.team then
      local candidate = arena.distance(self, robot)
      if not distance or candidate < distance then
        enemy, distance = robot, candidate
        visible = arena.line_of_sight(self.x, self.y, robot.x, robot.y, observation.obstacles)
      end
    end
  end

  -- Hurt with no immediate threat: sprint for the nearest heal.
  if self.hp < self.maxHp * 0.5 and (not enemy or not visible or distance > 60) and not observation.overtime then
    local heal = arena.nearest_item(observation, "heal") or arena.nearest_item(observation, "repair-core")
    if heal then
      local held = unstick(observation, { moving = true })
      if held then return held end
      return navigate(observation, heal.x, heal.y, 8, "grabbing " .. heal.type)
    end
  end

  if enemy then
    if distance > 30 and visible then
      local held = unstick(observation, { moving = true })
      if held then return held end
      return arena.approach(observation, enemy, 8)
    end
    if observation.tick >= state.flip_at then
      state.strafe = math.random() < 0.5 and 1 or -1
      state.flip_at = observation.tick + 20 + math.random(30)
    end
    local x, y = lead_target(observation, enemy)
    local held = unstick(observation, { moving = true })
    if held then return held end
    return arena.action({ move = 6, turn = 14 * state.strafe, fire = visible, target_x = x, target_y = y, logs = { "engaging " .. enemy.name } })
  end

  local item = best_item(observation)
  if item then
    local held = unstick(observation, { moving = true })
    if held then return held end
    return navigate(observation, item.x, item.y, 7, "looting " .. item.type)
  end
  if not state.wander or arena.distance(self, state.wander) < 40 then
    state.wander = { x = 60 + math.random() * (observation.arenaWidth - 120), y = 60 + math.random() * (observation.arenaHeight - 120) }
  end
  local held = unstick(observation, { moving = true })
  if held then return held end
  return navigate(observation, state.wander.x, state.wander.y, 6, "hunting")
end

arena.run({
  url = assert(os.getenv("ROBOT_ARENA_URL")),
  token = assert(os.getenv("ROBOT_TOKEN")),
  decide = decide,
})
`

func validateSource(data []byte) error {
	if len(data) == 0 || len(data) > 16*1024 {
		return errors.New("source must be 1..16384 bytes")
	}
	file, err := os.CreateTemp("", "arena-check-*.lua")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return validateLuaSyntax(file.Name())
}
func writeMainRevision(path, source, expected string) error {
	if err := validateSource([]byte(source)); err != nil {
		return err
	}
	current, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(current)
	if fmt.Sprintf("%x", hash) != expected {
		return errors.New("workspace revision changed; reload before saving")
	}
	if err = writeAtomic(path, []byte(source), 0644); err != nil {
		return err
	}
	return os.Chown(path, 1000, 1000)
}

// logf writes a timestamp-free supervisor line to stdout; Docker timestamps
// container output and the admin console reads it through the provisioner.
func logf(format string, args ...any) {
	fmt.Fprintf(os.Stdout, "[supervisor] "+format+"\n", args...)
}
