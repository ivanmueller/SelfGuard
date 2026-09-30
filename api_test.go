package main

import (
	"testing"
	"time"
)

// The control API must come up on a free port, publish it, and be reachable
// through the same apiBase() the CLI/GUI use — even when the old fixed port
// (5354) is already taken by something else, like mDNSResponder.
func TestAPIPortDiscovery(t *testing.T) {
	t.Setenv("ProgramData", t.TempDir())

	// Simulate mDNSResponder squatting on 5354.
	squat, err := listenTCP("127.0.0.1:5354")
	if err != nil {
		t.Skipf("can't bind 5354 to simulate the collision: %v", err)
	}
	defer squat.Close()

	e := &Engine{cfg: DefaultConfig(), lists: map[string]map[string]struct{}{}}
	e.dnsSrv = &DNSServer{}
	e.buildSnapshot()
	if err := e.startAPI(); err != nil {
		t.Fatalf("startAPI failed even though it should pick a free port: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	base, err := apiBase()
	if err != nil {
		t.Fatalf("apiBase (reads the published port file): %v", err)
	}
	if base == "http://127.0.0.1:5354" {
		t.Fatal("API bound the contended port instead of a free one")
	}

	s, err := apiGetStatus()
	if err != nil {
		t.Fatalf("status not reachable on the published port: %v", err)
	}
	if s.DelayHours != 0 { // DefaultConfig starts in setup mode
		t.Fatalf("unexpected status payload: delay=%g", s.DelayHours)
	}
}
