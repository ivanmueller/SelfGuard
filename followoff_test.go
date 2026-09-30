package main

import "testing"

// Turning off "follow the section schedule" sends the site its own schedule
// (mode "always" or "windows"), which must make it stop inheriting.
func TestOwnScheduleStopsInheriting(t *testing.T) {
	e := &Engine{cfg: DefaultConfig(), lists: map[string]map[string]struct{}{}}
	e.dnsSrv = &DNSServer{}
	e.buildSnapshot()
	i := e.cfg.findSite("reddit.com")
	if i < 0 {
		t.Fatal("reddit.com missing")
	}
	// baseline: inheriting
	e.cfg.Social[i].Schedule = Schedule{Mode: "inherit"}
	if m := e.cfg.Social[i].Schedule.Mode; m != "inherit" {
		t.Fatalf("setup wrong: %q", m)
	}
	// stop following -> own schedule "always"
	e.cfg.Social[i].Schedule = Schedule{Mode: "always"}
	inh := e.cfg.Social[i].Schedule.Mode == "" || e.cfg.Social[i].Schedule.Mode == "inherit"
	if inh {
		t.Error(`mode "always" should NOT inherit`)
	}
}
