package main

import (
	"testing"
	"time"

	"github.com/miekg/dns"
)

// Blocked YouTube must take down ALL its domains — main site, video streams,
// thumbnails, short links, app/API, embeds — not just youtube.com.
func TestBlockedYouTubeCoversAllDomains(t *testing.T) {
	e := &Engine{cfg: DefaultConfig(), lists: map[string]map[string]struct{}{}}
	e.dnsSrv = &DNSServer{}
	c := e.cfg.Categories["social"]
	c.Schedule = Schedule{Mode: "always"}
	e.cfg.Categories["social"] = c
	i := e.cfg.findSite("youtube.com")
	e.cfg.Social[i].Level = LevelBlocked
	e.buildSnapshot()

	now := time.Now()
	mustBlock := []string{
		"youtube.com", "www.youtube.com", "m.youtube.com",
		"rr1---sn-abc.googlevideo.com", "i.ytimg.com", "youtu.be",
		"www.youtube-nocookie.com", "youtubei.googleapis.com",
	}
	for _, d := range mustBlock {
		if !e.dnsSrv.blocked(d, now) {
			t.Errorf("Blocked YouTube should block %s", d)
		}
	}
	// A normal Google service must NOT be caught.
	if e.dnsSrv.blocked("mail.google.com", now) {
		t.Error("blocking YouTube must not block other Google services")
	}
}

// The migration replaces an old config's bad media hosts (e.g. reddit's
// redditstatic.com, x's broad twimg.com) with the curated CDN-only list, while
// keeping the user's level and schedule.
func TestSyncDefaultMediaFixesBadHosts(t *testing.T) {
	c := &Config{Social: []Site{
		{Domain: "reddit.com", Level: LevelText, Schedule: Schedule{Mode: "always"},
			Media: []string{"redd.it", "redditmedia.com", "redditstatic.com"}},
		{Domain: "x.com", Level: LevelText, Media: []string{"twimg.com"}},
		{Domain: "my.custom.site", Level: LevelBlocked, Media: []string{"keepme.cdn"}},
	}}
	c.syncDefaultMedia()   // built-ins: authoritative image/video hosts
	c.migrateLegacyMedia() // custom sites: old media -> images
	all := func(i int) []string {
		return append(append([]string(nil), c.Social[i].ImageHosts...), c.Social[i].VideoHosts...)
	}
	has := func(hosts []string, host string) bool {
		for _, m := range hosts {
			if m == host {
				return true
			}
		}
		return false
	}
	if has(all(0), "redditstatic.com") {
		t.Error("migration should have dropped reddit's script host")
	}
	if !has(c.Social[0].ImageHosts, "i.redd.it") {
		t.Error("reddit should still block its image host after sync")
	}
	if c.Social[0].Level != LevelText || c.Social[0].Schedule.Mode != "always" {
		t.Error("migration must preserve the user's level and schedule")
	}
	if has(all(1), "twimg.com") || !has(c.Social[1].ImageHosts, "pbs.twimg.com") {
		t.Errorf("x media should be curated CDN hosts, got img=%v vid=%v", c.Social[1].ImageHosts, c.Social[1].VideoHosts)
	}
	if !has(c.Social[2].ImageHosts, "keepme.cdn") {
		t.Error("a custom site's legacy media should migrate into image hosts")
	}
}

var _ = dns.TypeA
