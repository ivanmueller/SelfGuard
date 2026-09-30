package main

import (
	"encoding/json"
	"testing"
	"time"
)

func engineWithSites(t *testing.T) *Engine {
	t.Helper()
	t.Setenv("ProgramData", t.TempDir())
	e := &Engine{cfg: DefaultConfig(), lists: map[string]map[string]struct{}{}}
	e.dnsSrv = &DNSServer{}
	// Make social "always" so tests don't depend on the work-hours window.
	c := e.cfg.Categories["social"]
	c.Schedule = Schedule{Mode: "always"}
	e.cfg.Categories["social"] = c
	e.buildSnapshot()
	return e
}

func TestTextOnlyBlocksMediaNotMain(t *testing.T) {
	e := engineWithSites(t)
	now := time.Now()
	set := func(domain, level string) {
		p, _ := json.Marshal(siteLevelPayload{Domain: domain, Level: level})
		if _, err := e.handleCommand("set_site_level", string(p)); err != nil {
			t.Fatalf("set %s %s: %v", domain, level, err)
		}
	}
	// reddit starts Blocked; move to Text (looser, but setup mode delay=0 => instant)
	set("reddit.com", LevelText)

	if e.dnsSrv.blocked("www.reddit.com", now) {
		t.Error("reddit main page should load at Text level")
	}
	if e.dnsSrv.blocked("comments.reddit.com", now) {
		t.Error("reddit subdomain (text) should load")
	}
	if !e.dnsSrv.blocked("i.redd.it", now) {
		t.Error("reddit media host should be blocked at Text level")
	}
	if !e.dnsSrv.blocked("preview.redd.it", now) {
		t.Error("reddit media subdomain should be blocked at Text level")
	}
}

func TestLevelTransitions(t *testing.T) {
	e := engineWithSites(t)
	now := time.Now()
	set := func(domain, level string) (string, error) {
		p, _ := json.Marshal(siteLevelPayload{Domain: domain, Level: level})
		return e.handleCommand("set_site_level", string(p))
	}

	// Allowed: nothing blocked.
	set("x.com", LevelAllowed)
	if e.dnsSrv.blocked("x.com", now) || e.dnsSrv.blocked("pbs.twimg.com", now) {
		t.Error("Allowed should block nothing")
	}
	// Blocked: main + media.
	set("x.com", LevelBlocked)
	if !e.dnsSrv.blocked("x.com", now) || !e.dnsSrv.blocked("pbs.twimg.com", now) {
		t.Error("Blocked should block main and media")
	}
}

func TestLooserLevelQueuesWhenLocked(t *testing.T) {
	e := engineWithSites(t)
	e.cfg.DelayHours = 24 // locked
	now := time.Now()

	// reddit is Blocked; try to loosen to Allowed -> must queue, not apply.
	p, _ := json.Marshal(siteLevelPayload{Domain: "reddit.com", Level: LevelAllowed})
	msg, err := e.handleCommand("set_site_level", string(p))
	if err != nil {
		t.Fatal(err)
	}
	if e.cfg.Social[e.cfg.findSite("reddit.com")].Level != LevelBlocked {
		t.Fatal("looser level took effect immediately while locked")
	}
	if len(e.cfg.Pending) != 1 {
		t.Fatalf("expected a queued change, got %d (%s)", len(e.cfg.Pending), msg)
	}
	if e.dnsSrv.blocked("reddit.com", now) == false {
		t.Fatal("reddit should still be blocked until the delay matures")
	}

	// Stricter direction is always instant, even while locked.
	e.cfg.DelayHours = 24
	pp, _ := json.Marshal(siteLevelPayload{Domain: "youtube.com", Level: LevelBlocked})
	if _, err := e.handleCommand("set_site_level", string(pp)); err != nil {
		t.Fatal(err)
	}
	if e.cfg.Social[e.cfg.findSite("youtube.com")].Level != LevelBlocked {
		t.Fatal("stricter change should be instant even when locked")
	}
}

func TestPerSiteSchedule(t *testing.T) {
	e := engineWithSites(t)
	// Give reddit its own window: blocked only 09:00-17:00 Mondays.
	i := e.cfg.findSite("reddit.com")
	e.cfg.Social[i].Level = LevelBlocked
	e.cfg.Social[i].Schedule = Schedule{Mode: "windows", Windows: map[string][]Window{
		"monday": {{"09:00", "17:00"}},
	}}
	e.buildSnapshot()

	mon10 := time.Date(2026, 1, 5, 10, 0, 0, 0, time.Local) // a Monday
	mon20 := time.Date(2026, 1, 5, 20, 0, 0, 0, time.Local)
	if !e.dnsSrv.blocked("reddit.com", mon10) {
		t.Error("reddit should be blocked inside its window")
	}
	if e.dnsSrv.blocked("reddit.com", mon20) {
		t.Error("reddit should load outside its window")
	}
}

func TestDisablingSocialAllowsAllSites(t *testing.T) {
	e := engineWithSites(t)
	c := e.cfg.Categories["social"]
	c.Enabled = false
	e.cfg.Categories["social"] = c
	e.buildSnapshot()
	now := time.Now()
	if e.dnsSrv.blocked("reddit.com", now) || e.dnsSrv.blocked("i.redd.it", now) {
		t.Error("disabling social should allow every managed site")
	}
	// But porn stays blocked regardless.
	e.lists["porn"] = map[string]struct{}{"badsite.example": {}}
	e.buildSnapshot()
	if !e.dnsSrv.blocked("badsite.example", now) {
		t.Error("porn must remain blocked when social is off")
	}
}

func TestAddAndRemoveSite(t *testing.T) {
	e := engineWithSites(t)
	if _, err := e.handleCommand("add_site", `{"domain":"linkedin.com","level":"blocked","media":["licdn.com"]}`); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if !e.dnsSrv.blocked("linkedin.com", now) || !e.dnsSrv.blocked("static.licdn.com", now) {
		t.Fatal("added site (and its media) should be blocked")
	}
	// Setup mode (delay 0): removing is immediate.
	if _, err := e.handleCommand("remove_site", "linkedin.com"); err != nil {
		t.Fatal(err)
	}
	if e.cfg.findSite("linkedin.com") >= 0 {
		t.Fatal("in setup mode a removal should take effect immediately")
	}
}

func TestRemoveSiteQueuesWhenLocked(t *testing.T) {
	e := engineWithSites(t)
	e.cfg.DelayHours = 24 // locked
	if _, err := e.handleCommand("remove_site", "reddit.com"); err != nil {
		t.Fatal(err)
	}
	if len(e.cfg.Pending) != 1 || e.cfg.Pending[0].Kind != "remove_site" {
		t.Fatalf("locked remove should queue, pending=%v", e.cfg.Pending)
	}
	if e.cfg.findSite("reddit.com") < 0 {
		t.Fatal("reddit should still be managed until the delay matures")
	}
	e.mu.Lock()
	e.applyMatured(e.cfg.Pending[0])
	e.mu.Unlock()
	if e.cfg.findSite("reddit.com") >= 0 {
		t.Fatal("reddit should be gone once the queued removal matures")
	}
}
