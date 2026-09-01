package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func requireLua(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath(luaInterpreter); err == nil {
		return
	}
	if _, err := exec.LookPath("lua"); err == nil {
		luaInterpreter = "lua"
		return
	}
	t.Skip("no Lua interpreter available for syntax validation tests")
}

func TestValidateLuaSyntax(t *testing.T) {
	requireLua(t)
	dir := t.TempDir()
	write := func(name, source string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
		return path
	}

	valid := write("valid.lua", "local x = 1\nreturn x\n")
	if err := validateLuaSyntax(valid); err != nil {
		t.Fatalf("valid script rejected: %v", err)
	}

	// Regression: template scripts read ROBOT_* env with top-level asserts at
	// load time. Parse-only validation must not execute the chunk, or match
	// start fails with "assertion failed" because validation has no env.
	runtimeAssert := write("runtime.lua", "assert(os.getenv(\"DEFINITELY_UNSET_CLOUD_ROBOT_PROBE\"))\n")
	if err := validateLuaSyntax(runtimeAssert); err != nil {
		t.Fatalf("validation executed the script instead of parsing it: %v", err)
	}

	broken := write("broken.lua", "return = 1\n")
	err := validateLuaSyntax(broken)
	if err == nil || !strings.Contains(err.Error(), "Lua syntax error") {
		t.Fatalf("expected syntax error, got %v", err)
	}
}

// The boot script must parse cleanly (configure-agent runs the same parse
// check) and stay under the 16 KiB snapshot cap so it can always be
// registered without edits.
func TestDefaultScriptIsValidAndSized(t *testing.T) {
	requireLua(t)
	if len(defaultScript) == 0 {
		t.Fatal("default script is empty")
	}
	if len(defaultScript) > 16*1024 {
		t.Fatalf("default script is %d bytes, exceeds 16 KiB cap", len(defaultScript))
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "main.lua")
	if err := os.WriteFile(path, []byte(defaultScript), 0644); err != nil {
		t.Fatal(err)
	}
	if err := validateLuaSyntax(path); err != nil {
		t.Fatalf("default script rejected: %v", err)
	}
}
