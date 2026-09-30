package main

import (
	"testing"
	"time"
)

// Text-only must block image/video CDN hosts while leaving the site's own
// scripts/CSS reachable, so the page still loads as text. This is the whole
// point of the mode and the thing that was broken before.
func TestTextOnlyPreservesAppScripts(t *testing.T) {
	e := &Engine{cfg: DefaultConfig(), lists: map[string]map[string]struct{}{}}
	e.dnsSrv = &DNSServer{}
	c := e.cfg.Categories["social"]
	c.Schedule = Schedule{Mode: "always"}
	e.cfg.Categories["social"] = c
	for _, d := range []string{"reddit.com", "x.com"} {
		e.cfg.Social[e.cfg.findSite(d)].Level = LevelText
	}
	e.buildSnapshot()
	now := time.Now()

	type tc struct {
		host    string
		blocked bool
		why     string
	}
	cases := []tc{
		// Reddit: media blocked, page + its JS/CSS allowed.
		{"i.redd.it", true, "reddit image host"},
		{"v.redd.it", true, "reddit video host"},
		{"preview.redd.it", true, "reddit preview host"},
		{"external-preview.redd.it", true, "reddit external preview"},
		{"redditmedia.com", true, "reddit media"},
		{"www.reddit.com", false, "reddit page must load at text level"},
		{"redditstatic.com", false, "reddit's own JS/CSS must load (or text breaks)"},
		{"www.redditstatic.com", false, "reddit static subdomain must load"},
		// X: images/video blocked, app scripts (abs.twimg.com) allowed.
		{"pbs.twimg.com", true, "x image host"},
		{"video.twimg.com", true, "x video host"},
		{"abs.twimg.com", false, "x scripts/CSS must load (or text breaks)"},
		{"x.com", false, "x page must load at text level"},
		{"api.x.com", false, "x api must load at text level"},
	}
	for _, c := range cases {
		got := e.dnsSrv.blocked(c.host, now)
		if got != c.blocked {
			t.Errorf("%s: blocked=%v, want %v (%s)", c.host, got, c.blocked, c.why)
		}
	}
}
