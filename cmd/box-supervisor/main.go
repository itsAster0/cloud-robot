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
		} else {
			s.record.AgentStatus = "stopped"
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
	data, err := io.ReadAll(io.LimitReader(os.Stdin, 20*1024))
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
	if err := writeAtomic("/workspace/main.lua", data, 0644); err != nil {
		fatal(err)
	}
	fatal(os.Chown("/workspace/main.lua", 1000, 1000))
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

const defaultScript = `local arena = require "arena"
arena.run({
  url = assert(os.getenv("ROBOT_ARENA_URL")),
  token = assert(os.getenv("ROBOT_TOKEN")),
  decide = function(observation)
    local enemy = arena.nearest_enemy(observation)
    if enemy then return arena.approach(observation, enemy, 8) end
    return arena.action({ turn = 12 })
  end,
})
`
