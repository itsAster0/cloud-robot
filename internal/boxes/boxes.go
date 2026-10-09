package boxes

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/kryxen/cloud-robot/internal/model"
)

const (
	MaxPublicKeyBytes   = 8 * 1024
	MaxAgentCommandSize = 256
	DefaultStorageBytes = 1 << 30
	DefaultSSHPortMin   = 22000
	DefaultSSHPortCount = 1000
)

type SSHPortRange struct {
	Start int
	End   int
}

// ResolveSSHPortRange supports one-knob MIN+COUNT configuration while keeping
// START+END as explicit overrides for existing deployments.
func ResolveSSHPortRange(getenv func(string) string) (SSHPortRange, error) {
	minimum, err := optionalPositiveInt(getenv("SSH_PORT_MIN"), DefaultSSHPortMin)
	if err != nil {
		return SSHPortRange{}, fmt.Errorf("SSH_PORT_MIN: %w", err)
	}
	count, err := optionalPositiveInt(getenv("SSH_PORT_COUNT"), DefaultSSHPortCount)
	if err != nil {
		return SSHPortRange{}, fmt.Errorf("SSH_PORT_COUNT: %w", err)
	}
	start, err := optionalPositiveInt(getenv("SSH_PORT_START"), minimum)
	if err != nil {
		return SSHPortRange{}, fmt.Errorf("SSH_PORT_START: %w", err)
	}
	endDefault := start + count - 1
	end, err := optionalPositiveInt(getenv("SSH_PORT_END"), endDefault)
	if err != nil {
		return SSHPortRange{}, fmt.Errorf("SSH_PORT_END: %w", err)
	}
	expected, err := optionalPositiveInt(getenv("EXPECTED_BOX_COUNT"), 2)
	if err != nil {
		return SSHPortRange{}, fmt.Errorf("EXPECTED_BOX_COUNT: %w", err)
	}
	available := end - start + 1
	switch {
	case end < start:
		return SSHPortRange{}, fmt.Errorf("SSH port range is inverted: %d-%d", start, end)
	case start <= 22 && end >= 22:
		return SSHPortRange{}, errors.New("SSH box port range overlaps host SSH port 22")
	case start > 65535 || end > 65535:
		return SSHPortRange{}, errors.New("SSH box ports must be at most 65535")
	case available < expected:
		return SSHPortRange{}, fmt.Errorf("SSH port range has %d ports, fewer than EXPECTED_BOX_COUNT=%d", available, expected)
	}
	return SSHPortRange{Start: start, End: end}, nil
}

func optionalPositiveInt(value string, fallback int) (int, error) {
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, errors.New("must be a positive integer")
	}
	return parsed, nil
}

type AgentConfig struct {
	ImmutableSource string `json:"immutableSource,omitempty"`
	// ImmutableFiles are the workspace's other .lua modules, snapshotted at
	// registration next to main.lua so require() loads the registered code.
	ImmutableFiles map[string]string `json:"immutableFiles,omitempty"`
	RobotID        string            `json:"robotId"`
	MatchID        string            `json:"matchId"`
	URL            string            `json:"url"`
	Token          string            `json:"token"`
	StartCommand   string            `json:"startCommand"`
}

// ValidateAgentConfig rejects incomplete supervisor payloads before they reach
// a box. The provisioner validates once more, but the root-owned supervisor is
// the enforcement point.
func ValidateAgentConfig(config AgentConfig) error {
	if err := ValidateModules(config.ImmutableFiles); err != nil {
		return err
	}
	switch {
	case len(config.ImmutableSource) > 16*1024:
		return errors.New("immutable source exceeds 16 KiB")
	case config.RobotID == "":
		return errors.New("agent configuration requires robotId")
	case config.MatchID == "":
		return errors.New("agent configuration requires matchId")
	case config.URL == "":
		return errors.New("agent configuration requires url")
	case config.Token == "":
		return errors.New("agent configuration requires token")
	case config.StartCommand == "":
		return errors.New("agent configuration requires startCommand")
	case len(config.StartCommand) > MaxAgentCommandSize:
		return fmt.Errorf("agent startCommand exceeds %d characters", MaxAgentCommandSize)
	case strings.ContainsAny(config.StartCommand, "\n\r"):
		return errors.New("agent startCommand must be one line")
	}
	return nil
}

type Provisioner interface {
	Ensure(context.Context, string) (model.BoxRecord, error)
	Status(context.Context, string) (model.BoxRecord, error)
	SetKey(context.Context, string, string) (model.BoxRecord, error)
	ConfigureAgent(context.Context, string, AgentConfig) (model.BoxRecord, error)
	Restart(context.Context, string) (model.BoxRecord, error)
	ReadMain(context.Context, string) (string, error)
	WriteMain(context.Context, string, string) (string, error)
}

func IDForUser(userID string) string {
	sum := sha256.Sum256([]byte(userID))
	return "robot-box-" + hex.EncodeToString(sum[:8])
}

func ValidatePublicKey(value string) (string, string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > MaxPublicKeyBytes || strings.ContainsAny(value, "\r\n") {
		return "", "", errors.New("SSH public key must be one line and at most 8 KiB")
	}
	fields := strings.Fields(value)
	if len(fields) < 2 || (fields[0] != "ssh-ed25519" && fields[0] != "ssh-rsa" && !strings.HasPrefix(fields[0], "ecdsa-sha2-")) {
		return "", "", errors.New("SSH public key must use Ed25519, RSA, or ECDSA OpenSSH format")
	}
	decoded, err := base64.StdEncoding.DecodeString(fields[1])
	if err != nil || len(decoded) < 16 {
		return "", "", errors.New("SSH public key payload is invalid")
	}
	fingerprint := sha256.Sum256(decoded)
	return value, "SHA256:" + base64.RawStdEncoding.EncodeToString(fingerprint[:]), nil
}

// ParseSSHHostPorts extracts host ports mapped to container port 22 from a
// `docker ps --format '{{.Ports}}'` listing.
func ParseSSHHostPorts(listing string) map[int]bool {
	used := map[int]bool{}
	pattern := regexp.MustCompile(`(?:127\.0\.0\.1|0\.0\.0\.0|\[::\]):(\d+)->22/tcp`)
	for _, match := range pattern.FindAllStringSubmatch(listing, -1) {
		if port, err := strconv.Atoi(match[1]); err == nil {
			used[port] = true
		}
	}
	return used
}

// NextSSHPort returns the first free port inside the configured range. It is a
// pure function so the allocation policy stays testable without Docker.
func NextSSHPort(used map[int]bool, start, end int) (int, error) {
	if start <= 0 || end < start {
		return 0, fmt.Errorf("invalid SSH port range %d-%d", start, end)
	}
	for port := start; port <= end; port++ {
		if !used[port] {
			return port, nil
		}
	}
	return 0, fmt.Errorf("no SSH ports available in range %d-%d", start, end)
}

// WorkspaceUsage sums regular file sizes under root. Box quota checks use it;
// evaluation errors are reported so monitoring sees unreadable workspaces.
func WorkspaceUsage(root string) (int64, error) {
	var size int64
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			info, infoErr := entry.Info()
			if infoErr != nil {
				return infoErr
			}
			size += info.Size()
		}
		return nil
	})
	return size, err
}

// QuotaBreached reports whether usage crossed the configured workspace quota.
func QuotaBreached(usage, quota int64) bool { return quota > 0 && usage > quota }

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func NewClient(baseURL, token string) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, http: &http.Client{Timeout: 15 * time.Second}}
}

func (c *Client) Ensure(ctx context.Context, boxID string) (model.BoxRecord, error) {
	var result model.BoxRecord
	return result, c.do(ctx, http.MethodPut, "/v1/boxes/"+boxID, nil, &result)
}

func (c *Client) Status(ctx context.Context, boxID string) (model.BoxRecord, error) {
	var result model.BoxRecord
	return result, c.do(ctx, http.MethodGet, "/v1/boxes/"+boxID, nil, &result)
}

func (c *Client) SetKey(ctx context.Context, boxID, publicKey string) (model.BoxRecord, error) {
	var result model.BoxRecord
	return result, c.do(ctx, http.MethodPut, "/v1/boxes/"+boxID+"/ssh-key", map[string]string{"publicKey": publicKey}, &result)
}

func (c *Client) ConfigureAgent(ctx context.Context, boxID string, config AgentConfig) (model.BoxRecord, error) {
	var result model.BoxRecord
	return result, c.do(ctx, http.MethodPut, "/v1/boxes/"+boxID+"/agent", config, &result)
}

func (c *Client) Restart(ctx context.Context, boxID string) (model.BoxRecord, error) {
	var result model.BoxRecord
	return result, c.do(ctx, http.MethodPost, "/v1/boxes/"+boxID+"/restart", nil, &result)
}

func (c *Client) ReadMain(ctx context.Context, boxID string) (string, error) {
	var result struct {
		Source string `json:"source"`
	}
	err := c.do(ctx, http.MethodGet, "/v1/boxes/"+boxID+"/main.lua", nil, &result)
	return result.Source, err
}

// WriteMain deploys Lua source to a box and returns the file as read back,
// so callers can show exactly what persisted.
func (c *Client) WriteMain(ctx context.Context, boxID, source string) (string, error) {
	var result struct {
		Source string `json:"source"`
	}
	err := c.do(ctx, http.MethodPut, "/v1/boxes/"+boxID+"/main.lua", map[string]string{"source": source}, &result)
	return result.Source, err
}

// BoxSummary is one robot box container as the admin console lists it.
type BoxSummary struct {
	BoxID     string `json:"boxId"`
	State     string `json:"state"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
}

// List returns every robot box container known to Docker.
func (c *Client) List(ctx context.Context) ([]BoxSummary, error) {
	var result struct {
		Boxes []BoxSummary `json:"boxes"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/boxes", nil, &result); err != nil {
		return nil, err
	}
	return result.Boxes, nil
}

// Logs returns the last `tail` lines of a box's agent and supervisor output.
func (c *Client) Logs(ctx context.Context, boxID string, tail int) (string, error) {
	var result struct {
		Logs string `json:"logs"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/boxes/"+boxID+"/logs?tail="+strconv.Itoa(tail), nil, &result); err != nil {
		return "", err
	}
	return result.Logs, nil
}

// ClearAgent stops a box's agent and forgets its match configuration.
func (c *Client) ClearAgent(ctx context.Context, boxID string) error {
	var record model.BoxRecord
	return c.do(ctx, http.MethodDelete, "/v1/boxes/"+boxID+"/agent", nil, &record)
}

// LogsSince is Logs limited to output after an RFC 3339 time ("" for all).
func (c *Client) LogsSince(ctx context.Context, boxID string, tail int, since string) (string, error) {
	var result struct {
		Logs string `json:"logs"`
	}
	query := url.Values{"tail": {strconv.Itoa(tail)}}
	if since != "" {
		query.Set("since", since)
	}
	if err := c.do(ctx, http.MethodGet, "/v1/boxes/"+boxID+"/logs?"+query.Encode(), nil, &result); err != nil {
		return "", err
	}
	return result.Logs, nil
}

// Explore fetches one explorer view (stats, processes, files, or a file)
// as raw JSON for the API to pass through.
func (c *Client) Explore(ctx context.Context, boxID, view string, query url.Values) (json.RawMessage, error) {
	var result json.RawMessage
	path := "/v1/boxes/" + boxID + "/" + view
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) do(ctx context.Context, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = strings.NewReader(string(encoded))
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("box provisioner unavailable: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		message, _ := bufio.NewReader(io.LimitReader(response.Body, 4096)).ReadString('\n')
		return fmt.Errorf("box provisioner: %s", strings.TrimSpace(message))
	}
	// Callers that only need success (validation) pass a nil output.
	if output == nil {
		return nil
	}
	return json.NewDecoder(response.Body).Decode(output)
}

func (c *Client) WriteMainRevision(ctx context.Context, boxID, source, revision string) (string, error) {
	var result struct {
		Source string `json:"source"`
	}
	err := c.do(ctx, http.MethodPut, "/v1/boxes/"+boxID+"/main.lua", map[string]string{"source": source, "revision": revision}, &result)
	return result.Source, err
}
func (c *Client) ValidateMain(ctx context.Context, boxID, source string) error {
	return c.do(ctx, http.MethodPost, "/v1/boxes/"+boxID+"/validate-main", map[string]string{"source": source}, nil)
}

// Limits for a multi-file robot: modules besides main.lua, their combined
// size, and each file's size (the same 16 KiB as main.lua).
const (
	MaxModules     = 32
	MaxModuleBytes = 16 * 1024
	MaxBundleBytes = 128 * 1024
)

var modulePathPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_-]*(/[A-Za-z0-9_][A-Za-z0-9_-]*){0,2}\.lua$`)

// ValidModulePath accepts workspace-relative module paths such as
// "brain/plan.lua": up to three levels, no dots besides ".lua", never
// main.lua (which travels separately).
func ValidModulePath(path string) bool {
	return modulePathPattern.MatchString(path) && path != "main.lua"
}

// ValidateModules checks a module bundle against the path and size limits.
func ValidateModules(files map[string]string) error {
	if len(files) > MaxModules {
		return fmt.Errorf("at most %d Lua modules besides main.lua", MaxModules)
	}
	total := 0
	for path, source := range files {
		if !ValidModulePath(path) {
			return fmt.Errorf("invalid module path %q", path)
		}
		if len(source) > MaxModuleBytes {
			return fmt.Errorf("%s exceeds 16 KiB", path)
		}
		total += len(source)
	}
	if total > MaxBundleBytes {
		return fmt.Errorf("Lua modules exceed %d KiB in total", MaxBundleBytes/1024)
	}
	return nil
}

// ReadBundle returns the box's .lua modules other than main.lua.
func (c *Client) ReadBundle(ctx context.Context, boxID string) (map[string]string, error) {
	var result struct {
		Files map[string]string `json:"files"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/boxes/"+boxID+"/bundle", nil, &result); err != nil {
		return nil, err
	}
	return result.Files, nil
}

// WriteFiles writes Lua modules into the box workspace (templates).
func (c *Client) WriteFiles(ctx context.Context, boxID string, files map[string]string) error {
	var record model.BoxRecord
	return c.do(ctx, http.MethodPut, "/v1/boxes/"+boxID+"/files", map[string]any{"files": files}, &record)
}
