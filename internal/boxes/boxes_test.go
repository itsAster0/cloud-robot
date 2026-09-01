package boxes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIDForUserStableAndOpaque(t *testing.T) {
	first := IDForUser("user_123")
	if first != IDForUser("user_123") || first == IDForUser("user_456") || first == "robot-box-user_123" {
		t.Fatalf("unexpected box id %q", first)
	}
}

func TestResolveSSHPortRange(t *testing.T) {
	from := func(values map[string]string) func(string) string {
		return func(key string) string { return values[key] }
	}

	defaults, err := ResolveSSHPortRange(from(nil))
	if err != nil || defaults.Start != DefaultSSHPortMin || defaults.End != DefaultSSHPortMin+DefaultSSHPortCount-1 {
		t.Fatalf("unexpected defaults: %+v, %v", defaults, err)
	}
	derived, err := ResolveSSHPortRange(from(map[string]string{"SSH_PORT_MIN": "31000", "SSH_PORT_COUNT": "20"}))
	if err != nil || derived != (SSHPortRange{Start: 31000, End: 31019}) {
		t.Fatalf("unexpected derived range: %+v, %v", derived, err)
	}
	overridden, err := ResolveSSHPortRange(from(map[string]string{"SSH_PORT_MIN": "31000", "SSH_PORT_COUNT": "20", "SSH_PORT_START": "32000", "SSH_PORT_END": "32009"}))
	if err != nil || overridden != (SSHPortRange{Start: 32000, End: 32009}) {
		t.Fatalf("unexpected override: %+v, %v", overridden, err)
	}
	for name, values := range map[string]map[string]string{
		"inverted":  {"SSH_PORT_START": "300", "SSH_PORT_END": "200"},
		"host ssh":  {"SSH_PORT_START": "20", "SSH_PORT_END": "30"},
		"too small": {"SSH_PORT_START": "30000", "SSH_PORT_END": "30001", "EXPECTED_BOX_COUNT": "3"},
		"overflow":  {"SSH_PORT_MIN": "65530", "SSH_PORT_COUNT": "20"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ResolveSSHPortRange(from(values)); err == nil {
				t.Fatal("invalid range accepted")
			}
		})
	}
}

func TestValidatePublicKey(t *testing.T) {
	payload := "AAAAC3NzaC1lZDI1NTE5AAAAIGZha2UtYnV0LWxvbmctZW5vdWdoLWtleQ=="
	key, fingerprint, err := ValidatePublicKey("ssh-ed25519 " + payload + " reviewer@example")
	if err != nil || key == "" || fingerprint == "" {
		t.Fatalf("valid key rejected: %v", err)
	}
	for _, invalid := range []string{"", "PRIVATE KEY", "ssh-ed25519 bad", "ssh-ed25519 " + payload + "\nssh-rsa " + payload} {
		if _, _, err := ValidatePublicKey(invalid); err == nil {
			t.Fatalf("invalid key accepted: %q", invalid)
		}
	}
}

func TestValidatePublicKeyRejectsOversized(t *testing.T) {
	payload := strings.Repeat("A", MaxPublicKeyBytes)
	if _, _, err := ValidatePublicKey("ssh-ed25519 " + payload); err == nil {
		t.Fatal("oversized key accepted")
	}
}

func TestParseSSHHostPorts(t *testing.T) {
	listing := strings.Join([]string{
		"0.0.0.0:5432->5432/tcp",
		"127.0.0.1:22000->22/tcp",
		"0.0.0.0:22001->22/tcp",
		"[::]:22005->22/tcp",
		"127.0.0.1:22002->80/tcp",
	}, "\n")
	used := ParseSSHHostPorts(listing)
	for _, port := range []int{22000, 22001, 22005} {
		if !used[port] {
			t.Fatalf("port %d missing: %v", port, used)
		}
	}
	for _, port := range []int{5432, 22002} {
		if used[port] {
			t.Fatalf("port %d wrongly mapped: %v", port, used)
		}
	}
}

func TestNextSSHPort(t *testing.T) {
	port, err := NextSSHPort(map[int]bool{22000: true, 22001: true}, 22000, 22002)
	if err != nil || port != 22002 {
		t.Fatalf("expected 22002, got %d err %v", port, err)
	}
	if _, err := NextSSHPort(map[int]bool{22000: true}, 22000, 22000); err == nil {
		t.Fatal("expected exhaustion error")
	}
	if _, err := NextSSHPort(nil, 300, 100); err == nil {
		t.Fatal("expected invalid range error")
	}
}

func TestQuotaBreached(t *testing.T) {
	if QuotaBreached(999, 1000) {
		t.Fatal("usage under quota flagged")
	}
	if !QuotaBreached(1001, 1000) {
		t.Fatal("usage over quota missed")
	}
	if QuotaBreached(1001, 0) {
		t.Fatal("disabled quota should never breach")
	}
}

func TestWorkspaceUsage(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.lua"), []byte(strings.Repeat("x", 1024)), 0644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "build")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "blob.bin"), make([]byte, 4096), 0644); err != nil {
		t.Fatal(err)
	}
	usage, err := WorkspaceUsage(root)
	if err != nil {
		t.Fatal(err)
	}
	if usage != 1024+4096 {
		t.Fatalf("expected 5120 bytes, got %d", usage)
	}
}

func TestValidateAgentConfig(t *testing.T) {
	valid := AgentConfig{RobotID: "r1", MatchID: "m1", URL: "ws://api:8080/agent/connect/r1", Token: "secret", StartCommand: "lua main.lua"}
	if err := ValidateAgentConfig(valid); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	broken := valid
	broken.StartCommand = ""
	if err := ValidateAgentConfig(broken); err == nil {
		t.Fatal("missing startCommand accepted")
	}
	broken = valid
	broken.StartCommand = "lua main.lua\nexec evil"
	if err := ValidateAgentConfig(broken); err == nil {
		t.Fatal("multiline startCommand accepted")
	}
	broken = valid
	broken.StartCommand = strings.Repeat("x", MaxAgentCommandSize+1)
	if err := ValidateAgentConfig(broken); err == nil {
		t.Fatal("oversized startCommand accepted")
	}
	broken = valid
	broken.Token = ""
	if err := ValidateAgentConfig(broken); err == nil {
		t.Fatal("missing token accepted")
	}
}
