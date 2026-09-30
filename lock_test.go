package main

import (
	"strings"
	"testing"
	"time"
)

func newTestEngine(t *testing.T) *Engine {
	t.Helper()
	t.Setenv("ProgramData", t.TempDir())
	e := &Engine{cfg: DefaultConfig(), lists: map[string]map[string]struct{}{}}
	e.dnsSrv = &DNSServer{}
	e.loadListsFromCache()
	e.buildSnapshot()
	return e
}

func TestSetupModeThenLocked(t *testing.T) {
	e := newTestEngine(t)
	if e.cfg.DelayHours != 0 {
		t.Fatalf("fresh install should start in setup mode, got %g h", e.cfg.DelayHours)
	}

	// Arming (raising the delay) is immediate.
	if msg, err := e.handleCommand("set_delay", "24"); err != nil || !strings.Contains(msg, "effective now") {
		t.Fatalf("arming should be immediate: %q %v", msg, err)
	}

	// Once locked, loosening changes are queued for 24 h, not applied.
	for _, c := range [][2]string{{"set_delay", "0"}, {"uninstall", ""}, {"disable_category", "porn"}, {"allow_domain", "example.com"}} {
		msg, err := e.handleCommand(c[0], c[1])
		if err != nil || !strings.Contains(msg, "queued") {
			t.Fatalf("%s should be queued: %q %v", c[0], msg, err)
		}
	}
	if e.cfg.DelayHours != 24 || !e.cfg.Categories["porn"].Enabled {
		t.Fatal("a loosening change took effect without waiting out the delay")
	}
	for _, p := range e.cfg.Pending {
		if left := time.Until(p.Effective); left < 23*time.Hour {
			t.Fatalf("%s matures too early (%v)", p.Kind, left)
		}
	}
}

func TestBareTLDRejectedFromGUI(t *testing.T) {
	e := newTestEngine(t)
	if _, err := e.handleCommand("block_domain", "com"); err == nil {
		t.Fatal(`blocking "com" should be refused`)
	}
	if _, err := e.handleCommand("block_domain", "https://www.reddit.com/r/all"); err != nil {
		t.Fatal(err)
	}
	if !e.dnsSrv.blocked("www.reddit.com", time.Now()) {
		t.Fatal("pasted URL wasn't blocked")
	}
}

func TestBypassEndpointsBlocked(t *testing.T) {
	e := newTestEngine(t)
	for _, d := range []string{"use-application-dns.net", "dns.google", "mozilla.cloudflare-dns.com", "chrome.cloudflare-dns.com"} {
		if !e.dnsSrv.blocked(d, time.Now()) {
			t.Errorf("%s should be blocked", d)
		}
	}
	if e.dnsSrv.blocked("www.cloudflare.com", time.Now()) {
		t.Error("ordinary sites must not be caught by the bypass list")
	}
}

func TestFailOpenOnlyAfterSustainedFailure(t *testing.T) {
	e := newTestEngine(t)
	e.dnsSrv = &DNSServer{upstream: []string{silentUpstream(t)}} // listener not running either
	if e.syncSystemDNS() || e.failedOpen {
		t.Fatal("should not fail open on the first failure")
	}
	e.failingSince = time.Now().Add(-failOpenAfter - time.Second)
	e.syncSystemDNS()
	if !e.failedOpen {
		t.Fatal("should fail open after sustained failure")
	}
}
