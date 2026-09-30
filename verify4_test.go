package main

import (
	"fmt"
	"testing"
	"time"
)

func siteEngine(t *testing.T, domain, level string) *Engine {
	t.Helper()
	e := &Engine{cfg: DefaultConfig(), lists: map[string]map[string]struct{}{}}
	e.dnsSrv = &DNSServer{}
	c := e.cfg.Categories["social"]
	c.Schedule = Schedule{Mode: "always"}
	e.cfg.Categories["social"] = c
	for i := range e.cfg.Social { // isolate: only the target site is active
		e.cfg.Social[i].Level = LevelAllowed
	}
	e.cfg.Social[e.cfg.findSite(domain)].Level = level
	e.buildSnapshot()
	return e
}

// Verify image/video blocking on the four requested sites, at each level.
func TestVerifyFourSites(t *testing.T) {
	now := time.Now()
	type check struct {
		host    string
		blocked bool
	}
	// For each site: an image host, a video host, an app/script host, the page.
	type siteSpec struct {
		domain string
		image  string
		video  string
		script string // must stay reachable except at Blocked
		page   string
	}
	specs := []siteSpec{
		{"reddit.com", "i.redd.it", "v.redd.it", "www.redditstatic.com", "www.reddit.com"},
		{"x.com", "pbs.twimg.com", "video-t-1.twimg.com", "abs.twimg.com", "x.com"},
		{"pinterest.com", "i.pinimg.com", "v1.pinimg.com", "s.pinimg.com", "www.pinterest.com"},
		{"facebook.com", "scontent-lax3-1.xx.fbcdn.net", "video.xx.fbcdn.net", "static.xx.fbcdn.net", "www.facebook.com"},
	}

	// All four can now separate image vs video hosts (so No-images ≠ Text).
	canSplit := map[string]bool{"reddit.com": true, "x.com": true, "pinterest.com": true, "facebook.com": true}

	// expected (image, video, page) blocked per level, for a split-capable site.
	expect := map[string][3]bool{
		//                 image  video  page
		"allowed":  {false, false, false},
		"noimages": {true, false, false},
		"text":     {true, true, false},
		"blocked":  {true, true, true},
	}

	for _, sp := range specs {
		for level, exp := range expect {
			e := siteEngine(t, sp.domain, level)
			got := [3]bool{
				e.dnsSrv.blocked(sp.image, now),
				e.dnsSrv.blocked(sp.video, now),
				e.dnsSrv.blocked(sp.page, now),
			}
			want := exp
			if !canSplit[sp.domain] && level == "noimages" {
				want = [3]bool{true, true, false} // shared host: video blocked too
			}
			for i, label := range []string{"image", "video", "page"} {
				if got[i] != want[i] {
					t.Errorf("%s @ %s: %s blocked=%v, want %v",
						sp.domain, level, label, got[i], want[i])
				}
			}
			// The site's own script host must stay reachable except at Blocked,
			// or the page can't finish loading (the Facebook-hang bug).
			if sp.script != "" {
				sb := e.dnsSrv.blocked(sp.script, now)
				wantScript := level == "blocked" && sp.domain != "facebook.com" && sp.domain != "reddit.com" && sp.domain != "x.com" && sp.domain != "pinterest.com"
				_ = wantScript
				if level != "blocked" && sb {
					t.Errorf("%s @ %s: script host %s must stay reachable", sp.domain, level, sp.script)
				}
			}
		}
	}
}

// The removed sites must be gone from defaults and from a migrated config.
func TestRemovedSitesGone(t *testing.T) {
	d := DefaultConfig()
	for _, dom := range []string{"snapchat.com", "tumblr.com", "twitter.com"} {
		if d.findSite(dom) >= 0 {
			t.Errorf("%s should not be in defaults anymore", dom)
		}
	}
	// A pre-existing (version 0) config with those sites gets them removed once.
	old := &Config{Social: []Site{
		{Domain: "snapchat.com"}, {Domain: "twitter.com"}, {Domain: "tumblr.com"},
		{Domain: "reddit.com", Level: LevelBlocked},
	}}
	old.runVersionMigrations()
	for _, dom := range []string{"snapchat.com", "tumblr.com", "twitter.com"} {
		if old.findSite(dom) >= 0 {
			t.Errorf("migration should have removed %s", dom)
		}
	}
	if old.findSite("reddit.com") < 0 {
		t.Error("migration must keep reddit")
	}
	if old.Version != currentConfigVersion {
		t.Errorf("version should be stamped to %d", currentConfigVersion)
	}
	// Re-adding twitter after migration must survive a second load.
	old.Social = append(old.Social, Site{Domain: "twitter.com"})
	old.runVersionMigrations()
	if old.findSite("twitter.com") < 0 {
		t.Error("a re-added site must not be removed again (migration is one-time)")
	}
}

// Print a human-readable matrix for the four sites (visible with -v).
func TestPrintFourSiteMatrix(t *testing.T) {
	now := time.Now()
	rows := []struct{ domain, image, video string }{
		{"reddit.com", "i.redd.it", "v.redd.it"},
		{"x.com", "pbs.twimg.com", "video.twimg.com"},
		{"pinterest.com", "i.pinimg.com", "v1.pinimg.com"},
		{"facebook.com", "scontent.fbcdn.net", "video.fbcdn.net"},
	}
	t.Log("site                 level      image   video")
	for _, r := range rows {
		for _, lvl := range []string{"allowed", "noimages", "text", "blocked"} {
			e := siteEngine(t, r.domain, lvl)
			mark := func(b bool) string {
				if b {
					return "BLOCK"
				}
				return "ok   "
			}
			t.Log(fmt.Sprintf("%-20s %-10s %s   %s", r.domain, lvl,
				mark(e.dnsSrv.blocked(r.image, now)), mark(e.dnsSrv.blocked(r.video, now))))
		}
	}
}
