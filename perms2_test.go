package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Simulate an unreadable config by pointing configPath at a directory (open
// for read returns an error that isn't IsNotExist), which works even as root.
func TestConfigReadErrorFallsBack(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ProgramData", dir)
	cfgDir := filepath.Join(dir, "SelfGuard")
	// Create config.json AS A DIRECTORY so ReadFile fails with a non-NotExist error.
	if err := os.MkdirAll(filepath.Join(cfgDir, "config.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("must fall back, not error: %v", err)
	}
	if cfg.DelayHours != 0 {
		t.Fatalf("expected defaults (setup mode), got delay=%g", cfg.DelayHours)
	}
}
