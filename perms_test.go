package main

import (
	"os"
	"path/filepath"
	"testing"
)

// A config file that exists but can't be read must NOT bring the service down.
// It should fall back to defaults in memory and leave the file untouched, so a
// permission glitch can never brick filtering (the bug that stopped the service
// from reading config.json).
func TestUnreadableConfigFallsBackNotFatal(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ProgramData", dir)
	cfgDir := filepath.Join(dir, "SelfGuard")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"delay_hours": 999, "categories": {}}`)
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), original, 0o644); err != nil {
		t.Fatal(err)
	}
	// Make it unreadable (chmod works on Linux CI; skip if running as root where
	// perms are ignored).
	path := filepath.Join(cfgDir, "config.json")
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root: file mode is not enforced, can't simulate denied read")
	}
	defer os.Chmod(path, 0o644)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig must not error on an unreadable config, got: %v", err)
	}
	if cfg == nil || cfg.DelayHours != 0 { // defaults => setup mode
		t.Fatalf("expected default config on unreadable file, got %+v", cfg)
	}
	// The original file must be left intact, not clobbered.
	os.Chmod(path, 0o644)
	back, _ := os.ReadFile(path)
	if string(back) != string(original) {
		t.Fatal("LoadConfig overwrote a config it couldn't read")
	}
}
