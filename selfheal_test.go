package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// fileSHA returns the hash of a file we can compare against a manifest.
func TestFileSHA(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "x.bin")
	os.WriteFile(f, []byte("hello"), 0o644)
	h := sha256.Sum256([]byte("hello"))
	if got := fileSHA(f); got != hex.EncodeToString(h[:]) {
		t.Fatalf("fileSHA mismatch: %s", got)
	}
	if fileSHA(filepath.Join(dir, "nope")) != "" {
		t.Fatal("missing file should hash to empty string")
	}
}

// updateReadyVersion, when the service is current, must clear the banner marker
// but must NOT delete a staged GUI (a lagging GUI needs it to self-apply).
func TestUpdateReadyKeepsStagedGui(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ProgramData", dir)
	t.Setenv("SystemDrive", dir) // installDir -> <dir>\SelfGuard
	os.MkdirAll(dataDir(), 0o755)
	os.MkdirAll(installDir(), 0o755)
	os.WriteFile(updateReadyFile(), []byte(version), 0o644) // marker == running version
	os.WriteFile(stagedGui(), []byte("new gui"), 0o644)
	os.WriteFile(stagedSvc(), []byte("new svc"), 0o644)

	if v := updateReadyVersion(); v != "" {
		t.Fatalf("marker equal to running version should clear the banner, got %q", v)
	}
	if _, err := os.Stat(stagedGui()); err != nil {
		t.Fatal("staged GUI must be kept so a lagging GUI can self-apply")
	}
	if _, err := os.Stat(stagedSvc()); !os.IsNotExist(err) {
		t.Fatal("staged service binary should be cleared when service is current")
	}
}
