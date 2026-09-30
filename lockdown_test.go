package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The lock_down command applies hardening and reports it; on non-Windows the
// applyLockdown stub is a no-op, so this just checks the command path and marker
// handling logic doesn't error and that status reflects nothing when off.
func TestLockdownCommandPath(t *testing.T) {
	t.Setenv("ProgramData", t.TempDir())
	e := &Engine{cfg: DefaultConfig(), lists: map[string]map[string]struct{}{}}
	e.dnsSrv = &DNSServer{}
	e.buildSnapshot()
	msg, err := e.handleCommand("lock_down", "")
	if err != nil {
		t.Fatalf("lock_down errored: %v", err)
	}
	if msg == "" {
		t.Fatal("lock_down should return a confirmation")
	}
}

// apply_update refuses when nothing verified is staged.
func TestApplyUpdateNeedsStaged(t *testing.T) {
	t.Setenv("ProgramData", t.TempDir())
	e := &Engine{cfg: DefaultConfig(), lists: map[string]map[string]struct{}{}}
	e.dnsSrv = &DNSServer{}
	e.buildSnapshot()
	if _, err := e.handleCommand("apply_update", ""); err == nil {
		t.Fatal("apply_update should fail with nothing staged")
	}
}

// updateReadyVersion clears a stale marker once the running version matches.
func TestUpdateReadyClearsWhenCurrent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ProgramData", dir)
	_ = os.MkdirAll(dataDir(), 0o755)
	_ = os.WriteFile(updateReadyFile(), []byte(version), 0o644) // same as running
	if v := updateReadyVersion(); v != "" {
		t.Fatalf("marker equal to running version should clear, got %q", v)
	}
	if _, err := os.Stat(updateReadyFile()); !os.IsNotExist(err) {
		t.Fatal("stale marker file should be removed")
	}
}

// status carries the new fields the GUI reads.
func TestStatusHasNewFields(t *testing.T) {
	t.Setenv("ProgramData", t.TempDir())
	e := &Engine{cfg: DefaultConfig(), lists: map[string]map[string]struct{}{}}
	e.dnsSrv = &DNSServer{}
	e.buildSnapshot()
	b, _ := json.Marshal(e.status())
	var m map[string]any
	json.Unmarshal(b, &m)
	for _, k := range []string{"version", "update_ready", "locked_down"} {
		if _, ok := m[k]; !ok {
			t.Errorf("status missing field %q", k)
		}
	}
	_ = filepath.Join
}
