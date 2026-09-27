package main

import "testing"

func TestSafeWorkspacePathRejectsEscapes(t *testing.T) {
	for _, path := range []string{"main.lua", "lib/util.lua", "notes-1.txt"} {
		if !safeWorkspacePath(path) {
			t.Errorf("%q should be allowed", path)
		}
	}
	for _, path := range []string{"", "../etc/passwd", "/etc/passwd", "a/../../b", ".ssh/id", "a//b", "x;rm -rf"} {
		if safeWorkspacePath(path) {
			t.Errorf("%q should be rejected", path)
		}
	}
}

func TestParseDockerSizesAndListings(t *testing.T) {
	used, limit := splitPair("12.5MiB / 512MiB")
	if used != 13107200 || limit != 536870912 {
		t.Fatalf("memory pair: %d %d", used, limit)
	}
	if rx, tx := splitPair("1.2kB / 0B"); rx != 1200 || tx != 0 {
		t.Fatalf("net pair: %d %d", rx, tx)
	}
	files := parseFind("d\t4096\t1790000000.5\t\nf\t120\t1790000001.0\tmain.lua\nd\t4096\t1790000002.0\tlib\n", 10)
	if len(files) != 2 || files[0].Path != "main.lua" || files[0].Size != 120 || !files[1].Dir {
		t.Fatalf("find listing: %+v", files)
	}
	procs := parseTop("PID USER RSS ELAPSED TIME COMMAND\n12 developer 9000 01:02 0:03 lua main.lua\n")
	if len(procs) != 1 || procs[0].Command != "lua main.lua" || procs[0].CPUTime != "0:03" {
		t.Fatalf("top: %+v", procs)
	}
}
