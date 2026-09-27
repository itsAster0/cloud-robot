package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
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
	case r.Method == http.MethodDelete && len(parts) == 2 && parts[1] == "agent":
		err = run(r.Context(), "docker", "exec", boxID, "/usr/local/bin/robot-box-supervisor", "clear-agent")
		if err == nil {
			result, err = s.status(r.Context(), boxID)
		}
	case r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "restart":
		// A restart is an explicit request, so an outdated box is recreated
		// on the current image (volumes and SSH port are kept).
		if s.outdated(r.Context(), boxID) {
			var record model.BoxRecord
			record, err = s.status(r.Context(), boxID)
			if err == nil {
				log.Printf("upgrading %s on restart", boxID)
				if err = run(r.Context(), "docker", "rm", "-f", boxID); err == nil {
					err = s.create(r.Context(), boxID, record.SSHPort)
				}
			}
		} else {
			err = run(r.Context(), "docker", "restart", boxID)
		}
		if err == nil {
			result, err = s.status(r.Context(), boxID)
		}
	case r.Method == http.MethodGet && len(parts) == 2 && parts[1] == "stats":
		stats, statsErr := boxStats(r.Context(), boxID)
		if statsErr != nil {
			writeError(w, 502, statsErr.Error())
			return
		}
		writeJSON(w, 200, stats)
		return
	case r.Method == http.MethodGet && len(parts) == 2 && parts[1] == "processes":
		top, topErr := output(r.Context(), "docker", "top", boxID, "-eo", "pid,user,rss,etime,time,args")
		if topErr != nil {
			writeError(w, 502, topErr.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"processes": parseTop(top)})
		return
	case r.Method == http.MethodGet && len(parts) == 2 && parts[1] == "files":
		// Relative paths, sizes, and modification times under /workspace,
		// skipping hidden entries; capped so a huge tree stays cheap.
		listing, listErr := output(r.Context(), "docker", "exec", boxID, "find", "/workspace", "-maxdepth", "4", "-not", "-path", "*/.*", "-printf", "%y\t%s\t%T@\t%P\n")
		if listErr != nil {
			writeError(w, 502, listErr.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"files": parseFind(listing, 500)})
		return
	case r.Method == http.MethodGet && len(parts) == 2 && parts[1] == "file":
		path := r.URL.Query().Get("path")
		if !safeWorkspacePath(path) {
			writeError(w, 400, "invalid workspace path")
			return
		}
		content, readErr := output(r.Context(), "docker", "exec", boxID, "head", "-c", "262144", "/workspace/"+path)
		if readErr != nil {
			writeError(w, 404, "file not readable")
			return
		}
		writeJSON(w, 200, map[string]string{"path": path, "content": content})
		return
	case r.Method == http.MethodGet && len(parts) == 2 && parts[1] == "logs":
		// Agent and supervisor output is the container's stdout/stderr.
		tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
		if tail <= 0 || tail > 5000 {
			tail = 300
		}
		args := []string{"logs", "--timestamps", "--tail", strconv.Itoa(tail)}
		// "since" hides older output (the viewer's Clear); RFC 3339 only.
		if since := r.URL.Query().Get("since"); since != "" {
			if _, parseErr := time.Parse(time.RFC3339Nano, since); parseErr != nil {
				writeError(w, 400, "since must be RFC 3339")
				return
			}
			args = append(args, "--since", since)
		}
		logs, logErr := output(r.Context(), "docker", append(args, boxID)...)
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
		record, statusErr := s.status(ctx, boxID)
		if statusErr == nil && s.outdated(ctx, boxID) && record.AgentStatus != "running" {
			// Boxes built from an older image keep an old SDK and supervisor.
			// Recreate on the current image while no robot is playing; the
			// workspace and control volumes (code, SSH key, agent config)
			// and the SSH port are kept.
			log.Printf("upgrading %s to the current box image", boxID)
			if err := run(ctx, "docker", "rm", "-f", boxID); err != nil {
				return record, err
			}
			if err := s.create(ctx, boxID, record.SSHPort); err != nil {
				return model.BoxRecord{}, err
			}
		}
		return s.status(ctx, boxID)
	}
	port, err := s.availablePort(ctx)
	if err != nil {
		return model.BoxRecord{}, err
	}
	if err = s.create(ctx, boxID, port); err != nil {
		return model.BoxRecord{}, err
	}
	return s.status(ctx, boxID)
}

// outdated reports whether the box container runs an older image than the
// one the provisioner would use now.
func (s *server) outdated(ctx context.Context, boxID string) bool {
	running, err := output(ctx, "docker", "inspect", "--format", "{{.Image}}", boxID)
	if err != nil {
		return false
	}
	current, err := output(ctx, "docker", "image", "inspect", "--format", "{{.Id}}", s.image)
	if err != nil {
		return false
	}
	return strings.TrimSpace(running) != strings.TrimSpace(current)
}

func (s *server) create(ctx context.Context, boxID string, port int) error {
	return run(ctx, "docker", "run", "-d", "--name", boxID,
		"--label", "robot-arena.box=true", "--label", "robot-arena.box-id="+boxID,
		"--cpus", strconv.FormatFloat(s.limits.CPUs, 'f', -1, 64),
		"--memory", strconv.FormatInt(s.limits.MemoryMB, 10)+"m", "--pids-limit", strconv.FormatInt(s.limits.PIDs, 10),
		"--restart", "unless-stopped", "--add-host", "host.docker.internal:host-gateway",
		"-e", "BOX_STORAGE_BYTES="+strconv.FormatInt(s.storageBytes, 10),
		"-p", "127.0.0.1:"+strconv.Itoa(port)+":22",
		"-v", boxID+"-workspace:/workspace", "-v", boxID+"-control:/var/lib/robot-box", s.image)
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

var workspacePathPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._/-]{0,255}$`)

// safeWorkspacePath accepts relative paths under /workspace with no "..".
func safeWorkspacePath(path string) bool {
	return workspacePathPattern.MatchString(path) && !strings.Contains(path, "..") && !strings.Contains(path, "//")
}

type boxFile struct {
	Path     string  `json:"path"`
	Dir      bool    `json:"dir"`
	Size     int64   `json:"size"`
	Modified float64 `json:"modified"`
}

func parseFind(listing string, limit int) []boxFile {
	files := []boxFile{}
	for _, line := range strings.Split(strings.TrimSpace(listing), "\n") {
		fields := strings.SplitN(line, "\t", 4)
		if len(fields) != 4 || fields[3] == "" {
			continue
		}
		size, _ := strconv.ParseInt(fields[1], 10, 64)
		modified, _ := strconv.ParseFloat(fields[2], 64)
		files = append(files, boxFile{Path: fields[3], Dir: fields[0] == "d", Size: size, Modified: modified})
		if len(files) == limit {
			break
		}
	}
	return files
}

type boxProcess struct {
	PID     string `json:"pid"`
	User    string `json:"user"`
	RSSKB   string `json:"rssKb"`
	Elapsed string `json:"elapsed"`
	CPUTime string `json:"cpuTime"`
	Command string `json:"command"`
}

func parseTop(top string) []boxProcess {
	processes := []boxProcess{}
	lines := strings.Split(strings.TrimSpace(top), "\n")
	for _, line := range lines[min(1, len(lines)):] {
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		// Columns follow the busybox-compatible ps format used by docker top.
		processes = append(processes, boxProcess{PID: fields[0], User: fields[1], RSSKB: fields[2], Elapsed: fields[3], CPUTime: fields[4], Command: strings.Join(fields[5:], " ")})
	}
	return processes
}

// boxStats samples docker stats once and adds container metadata.
func boxStats(ctx context.Context, boxID string) (map[string]any, error) {
	raw, err := output(ctx, "docker", "stats", "--no-stream", "--format", "{{json .}}", boxID)
	if err != nil {
		return nil, err
	}
	var sample struct {
		CPUPerc, MemUsage, MemPerc, NetIO, BlockIO, PIDs string
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &sample); err != nil {
		return nil, fmt.Errorf("parse docker stats: %w", err)
	}
	memUsed, memLimit := splitPair(sample.MemUsage)
	netRx, netTx := splitPair(sample.NetIO)
	blockRead, blockWrite := splitPair(sample.BlockIO)
	pids, _ := strconv.Atoi(strings.TrimSpace(sample.PIDs))
	info, err := output(ctx, "docker", "inspect", "--format", "{{.Config.Image}}|{{.Created}}|{{.State.StartedAt}}|{{.RestartCount}}|{{.State.Status}}|{{.Id}}", boxID)
	if err != nil {
		return nil, err
	}
	meta := strings.Split(strings.TrimSpace(info), "|")
	for len(meta) < 6 {
		meta = append(meta, "")
	}
	restarts, _ := strconv.Atoi(meta[3])
	return map[string]any{
		"at": time.Now().UTC(), "cpuPercent": percent(sample.CPUPerc), "memoryPercent": percent(sample.MemPerc),
		"memoryBytes": memUsed, "memoryLimitBytes": memLimit, "netRxBytes": netRx, "netTxBytes": netTx,
		"blockReadBytes": blockRead, "blockWriteBytes": blockWrite, "pids": pids,
		"image": meta[0], "createdAt": meta[1], "startedAt": meta[2], "restarts": restarts, "state": meta[4], "containerId": shortID(meta[5]),
	}, nil
}

func shortID(id string) string { return id[:min(12, len(id))] }

func percent(value string) float64 {
	number, _ := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(value), "%"), 64)
	return number
}

func splitPair(value string) (int64, int64) {
	left, right, _ := strings.Cut(value, "/")
	return parseBytes(left), parseBytes(right)
}

// parseBytes reads docker's human sizes such as "12.5MiB" or "3kB".
func parseBytes(value string) int64 {
	value = strings.TrimSpace(value)
	units := []struct {
		suffix string
		scale  float64
	}{{"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10}, {"GB", 1e9}, {"MB", 1e6}, {"kB", 1e3}, {"KB", 1e3}, {"B", 1}}
	for _, unit := range units {
		if strings.HasSuffix(value, unit.suffix) {
			number, _ := strconv.ParseFloat(strings.TrimSuffix(value, unit.suffix), 64)
			return int64(number * unit.scale)
		}
	}
	number, _ := strconv.ParseFloat(value, 64)
	return int64(number)
}
