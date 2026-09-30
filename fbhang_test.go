package main

import (
	"testing"
	"time"
)

// Regression: Facebook must never have its own script host (static*.fbcdn.net)
// blocked — that's what hung the page at the loading logo. This must hold even
// when Facebook is Allowed but Instagram is Blocked (the real failing case),
// and Instagram's block must not reach Facebook's scontent image host.
func TestFacebookNotHungByInstagram(t *testing.T) {
	e := &Engine{cfg: DefaultConfig(), lists: map[string]map[string]struct{}{}}
	e.dnsSrv = &DNSServer{}
	c := e.cfg.Categories["social"]
	c.Schedule = Schedule{Mode: "always"}
	e.cfg.Categories["social"] = c
	// Facebook Allowed, Instagram Blocked (defaults have IG blocked).
	e.cfg.Social[e.cfg.findSite("facebook.com")].Level = LevelAllowed
	e.cfg.Social[e.cfg.findSite("instagram.com")].Level = LevelBlocked
	e.buildSnapshot()
	now := time.Now()

	mustLoad := []string{
		"static.xx.fbcdn.net",       // FB scripts — the hang culprit
		"scontent-lax3-1.xx.fbcdn.net", // FB photos (Allowed, and IG shouldn't touch them)
		"video.xx.fbcdn.net",        // FB video
		"www.facebook.com",          // the page
	}
	for _, h := range mustLoad {
		if e.dnsSrv.blocked(h, now) {
			t.Errorf("FB Allowed / IG Blocked: %s must stay reachable", h)
		}
	}
	// Instagram itself is still fully blocked.
	for _, h := range []string{"www.instagram.com", "scontent.cdninstagram.com", "instagram.fabc-1.fna.fbcdn.net"} {
		if !e.dnsSrv.blocked(h, now) {
			t.Errorf("Instagram should still be blocked: %s", h)
		}
	}
}

// Facebook Text-only: photos and video blocked, but scripts and page load.
func TestFacebookTextOnlyLoads(t *testing.T) {
	e := &Engine{cfg: DefaultConfig(), lists: map[string]map[string]struct{}{}}
	e.dnsSrv = &DNSServer{}
	c := e.cfg.Categories["social"]
	c.Schedule = Schedule{Mode: "always"}
	e.cfg.Categories["social"] = c
	for i := range e.cfg.Social {
		e.cfg.Social[i].Level = LevelAllowed
	}
	e.cfg.Social[e.cfg.findSite("facebook.com")].Level = LevelText
	e.buildSnapshot()
	now := time.Now()
	if !e.dnsSrv.blocked("scontent-lax3-1.xx.fbcdn.net", now) {
		t.Error("Text: FB photos should be blocked")
	}
	if !e.dnsSrv.blocked("video.xx.fbcdn.net", now) {
		t.Error("Text: FB video should be blocked")
	}
	if e.dnsSrv.blocked("static.xx.fbcdn.net", now) {
		t.Error("Text: FB scripts must load or the page hangs")
	}
	if e.dnsSrv.blocked("www.facebook.com", now) {
		t.Error("Text: FB page must load")
	}
}
