package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/kryxen/cloud-robot/internal/boxes"
	"github.com/kryxen/cloud-robot/internal/model"
)

type server struct {
	token, image, sshHost string
	portStart, portEnd    int
	limits                model.BoxLimits
	storageBytes          int64
}

func main() {
	portRange, err := boxes.ResolveSSHPortRange(os.Getenv)
	if err != nil {
		panic(err)
	}
	s := &server{
		token:        requiredEnv("PROVISIONER_TOKEN"),
		image:        envOr("ROBOT_BOX_IMAGE", "cloud-robot-box:local"),
		sshHost:      envOr("SSH_PUBLIC_HOST", "localhost"),
		portStart:    portRange.Start,
		portEnd:      portRange.End,
		limits:       model.BoxLimits{CPUs: envFloat("BOX_CPUS", 1), MemoryMB: int64(envInt("BOX_MEMORY_MB", 512)), PIDs: int64(envInt("BOX_PIDS", 128)), StorageBytes: boxes.DefaultStorageBytes},
		storageBytes: envInt64("BOX_STORAGE_BYTES", boxes.DefaultStorageBytes),
	}
	s.limits.StorageBytes = s.storageBytes
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.Handle("/v1/", s.authorize(http.HandlerFunc(s.handle)))
	address := envOr("PROVISIONER_ADDR", ":8090")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	httpServer := &http.Server{Addr: address, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdown)
		s.stopAll(shutdown)
	}()
	slog.Info("box provisioner ready", "address", address)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		panic(err)
	}
}

// adminList reports every robot box container, for the admin console.
func (s *server) adminList(w http.ResponseWriter, r *http.Request) {
	lines, err := output(r.Context(), "docker", "ps", "-a", "--filter", "label=robot-arena.box=true",
		"--format", "{{.Names}}|{{.State}}|{{.Status}}|{{.CreatedAt}}")
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	type summary struct {
		BoxID     string `json:"boxId"`
		State     string `json:"state"`
		Status    string `json:"status"`
		CreatedAt string `json:"createdAt"`
	}
	boxes := []summary{}
	for _, line := range strings.Split(strings.TrimSpace(lines), "\n") {
		parts := strings.SplitN(line, "|", 4)
		if len(parts) == 4 {
			boxes = append(boxes, summary{parts[0], parts[1], parts[2], parts[3]})
		}
	}
	writeJSON(w, 200, map[string]any{"boxes": boxes})
}

func (s *server) handle(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/v1/boxes" && r.Method == http.MethodGet {
		s.adminList(w, r)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/v1/boxes/")
	parts := strings.Split(path, "/")
	boxID := parts[0]
	if !regexp.MustCompile(`^robot-box-[a-f0-9]{16}$`).MatchString(boxID) {
		writeError(w, 400, "invalid box id")
		return
	}
	var result model.BoxRecord
	var err error
	switch {
	case r.Method == http.MethodPut && len(parts) == 1:
		result, err = s.ensure(r.Context(), boxID)
	case r.Method == http.MethodGet && len(parts) == 1:
		result, err = s.status(r.Context(), boxID)
	case r.Method == http.MethodPut && len(parts) == 2 && parts[1] == "ssh-key":
		var input struct {
			PublicKey string `json:"publicKey"`
		}
		if err = decode(r.Body, &input); err == nil {
			if _, _, validationErr := boxes.ValidatePublicKey(input.PublicKey); validationErr != nil {
				err = validationErr
			} else {
				err = s.execInput(r.Context(), boxID, input.PublicKey, "set-key")
			}
		}
		if err == nil {
			result, err = s.status(r.Context(), boxID)
		}
	case r.Method == http.MethodPut && len(parts) == 2 && parts[1] == "agent":
		var input boxes.AgentConfig
		if err = decode(r.Body, &input); err == nil {
			var payload []byte
			payload, err = json.Marshal(input)
			if err == nil {
				err = s.execInput(r.Context(), boxID, string(payload), "configure-agent")
			}
		}
		if err == nil {
			result, err = s.status(r.Context(), boxID)
		}
	case r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "restart":
		err = run(r.Context(), "docker", "restart", boxID)
		if err == nil {
			result, err = s.status(r.Context(), boxID)
		}
	case r.Method == http.MethodGet && len(parts) == 2 && parts[1] == "logs":
		// Agent and supervisor output is the container's stdout/stderr.
		tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
		if tail <= 0 || tail > 2000 {
			tail = 300
		}
		logs, logErr := output(r.Context(), "docker", "logs", "--timestamps", "--tail", strconv.Itoa(tail), boxID)
		if logErr != nil {
			writeError(w, 502, logErr.Error())
			return
		}
		writeJSON(w, 200, map[string]string{"logs": logs})
		return
	case r.Method == http.MethodGet && len(parts) == 2 && parts[1] == "main.lua":
		output, readErr := output(r.Context(), "docker", "exec", boxID, "/usr/local/bin/robot-box-supervisor", "read-main")
		if readErr != nil {
			writeError(w, 502, readErr.Error())
			return
		}
		writeJSON(w, 200, map[string]string{"source": output})
		return
	case r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "validate-main":
		var input struct {
			Source string `json:"source"`
		}
		if err = decode(r.Body, &input); err != nil || len(input.Source) > 16*1024 {
			writeError(w, 400, "invalid source")
			return
		}
		if err = s.execInput(r.Context(), boxID, input.Source, "validate-main"); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, map[string]bool{"valid": true})
		return
	case r.Method == http.MethodPut && len(parts) == 2 && parts[1] == "main.lua":
		var input struct {
			Source   string `json:"source"`
			Revision string `json:"revision"`
		}
		if err = decode(r.Body, &input); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		source := strings.TrimSpace(input.Source)
		if source == "" || len(source) > 16*1024 {
			writeError(w, 400, "main.lua source must be 1 byte to 16 KiB")
			return
		}
		action := "write-main"
		payload := source
		if input.Revision != "" {
			action = "write-main-if-match"
			raw, _ := json.Marshal(map[string]string{"source": input.Source, "revision": input.Revision})
			payload = string(raw)
		}
		if err = s.execInput(r.Context(), boxID, payload, action); err != nil {
			writeError(w, 502, err.Error())
			return
		}
		written, readErr := output(r.Context(), "docker", "exec", boxID, "/usr/local/bin/robot-box-supervisor", "read-main")
		if readErr != nil {
			writeError(w, 502, readErr.Error())
			return
		}
		writeJSON(w, 200, map[string]string{"source": written})
		return
	default:
		writeError(w, 404, "not found")
		return
	}
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, result)
}

func (s *server) ensure(ctx context.Context, boxID string) (model.BoxRecord, error) {
	if err := run(ctx, "docker", "inspect", boxID); err == nil {
		_ = run(ctx, "docker", "start", boxID)
		return s.status(ctx, boxID)
	}
	port, err := s.availablePort(ctx)
	if err != nil {
		return model.BoxRecord{}, err
	}
	err = run(ctx, "docker", "run", "-d", "--name", boxID,
		"--label", "robot-arena.box=true", "--label", "robot-arena.box-id="+boxID,
		"--cpus", strconv.FormatFloat(s.limits.CPUs, 'f', -1, 64),
		"--memory", strconv.FormatInt(s.limits.MemoryMB, 10)+"m", "--pids-limit", strconv.FormatInt(s.limits.PIDs, 10),
		"--restart", "unless-stopped", "--add-host", "host.docker.internal:host-gateway",
		"-e", "BOX_STORAGE_BYTES="+strconv.FormatInt(s.storageBytes, 10),
		"-p", "127.0.0.1:"+strconv.Itoa(port)+":22",
		"-v", boxID+"-workspace:/workspace", "-v", boxID+"-control:/var/lib/robot-box", s.image)
	if err != nil {
		return model.BoxRecord{}, err
	}
	return s.status(ctx, boxID)
}

func (s *server) status(ctx context.Context, boxID string) (model.BoxRecord, error) {
	line, err := output(ctx, "docker", "inspect", "--format", `{{.State.Status}}|{{(index (index .NetworkSettings.Ports "22/tcp") 0).HostPort}}`, boxID)
	if err != nil {
		return model.BoxRecord{}, err
	}
	parts := strings.Split(strings.TrimSpace(line), "|")
	if len(parts) != 2 {
		return model.BoxRecord{}, errors.New("invalid Docker box status")
	}
	port, _ := strconv.Atoi(parts[1])
	record := model.BoxRecord{BoxID: boxID, Status: parts[0], SSHHost: s.sshHost, SSHPort: port, SSHUser: "developer", AgentStatus: "offline", Limits: s.limits, UpdatedAt: time.Now().UTC()}
	if parts[0] == "running" {
		state, stateErr := output(ctx, "docker", "exec", boxID, "/usr/local/bin/robot-box-supervisor", "status")
		if stateErr == nil {
			_ = json.Unmarshal([]byte(state), &record)
			record.BoxID, record.Status, record.SSHHost, record.SSHPort, record.SSHUser = boxID, parts[0], s.sshHost, port, "developer"
			record.Limits = s.limits
		}
	}
	return record, nil
}

func (s *server) execInput(ctx context.Context, boxID, input, action string) error {
	command := exec.CommandContext(ctx, "docker", "exec", "-i", boxID, "/usr/local/bin/robot-box-supervisor", action)
	command.Stdin = strings.NewReader(input)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("docker exec: %s", strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (s *server) availablePort(ctx context.Context) (int, error) {
	listing, err := output(ctx, "docker", "ps", "-a", "--format", "{{.Ports}}")
	if err != nil {
		return 0, err
	}
	used := boxes.ParseSSHHostPorts(listing)
	capacity := s.portEnd - s.portStart + 1
	usedInRange := 0
	for port := range used {
		if port >= s.portStart && port <= s.portEnd {
			usedInRange++
		}
	}
	remaining := capacity - usedInRange
	if remaining <= max(1, capacity/10) {
		slog.Warn("SSH box ports running low", "remaining", remaining, "capacity", capacity, "start", s.portStart, "end", s.portEnd)
	}
	return boxes.NextSSHPort(used, s.portStart, s.portEnd)
}

func (s *server) stopAll(ctx context.Context) {
	ids, err := output(ctx, "docker", "ps", "-q", "--filter", "label=robot-arena.box=true")
	if err != nil || strings.TrimSpace(ids) == "" {
		return
	}
	args := []string{"stop", "--time", "2"}
	args = append(args, strings.Fields(ids)...)
	_ = run(ctx, "docker", args...)
}

func (s *server) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+s.token {
			writeError(w, 401, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func run(ctx context.Context, name string, args ...string) error {
	_, err := output(ctx, name, args...)
	return err
}
func output(ctx context.Context, name string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, name, args...)
	data, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %s", name, strings.TrimSpace(string(data)))
	}
	return string(data), nil
}
func decode(reader io.Reader, target any) error {
	decoder := json.NewDecoder(io.LimitReader(reader, 20*1024))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(key))
	if err != nil {
		return fallback
	}
	return value
}
func envInt64(key string, fallback int64) int64 {
	value, err := strconv.ParseInt(os.Getenv(key), 10, 64)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
func envFloat(key string, fallback float64) float64 {
	value, err := strconv.ParseFloat(os.Getenv(key), 64)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
func requiredEnv(key string) string {
	value := os.Getenv(key)
	if value == "" {
		panic(key + " is required")
	}
	return value
}
