package main

import (
	"testing"
	"time"
)

func ytEngine(t *testing.T, level string) *Engine {
	t.Helper()
	e := &Engine{cfg: DefaultConfig(), lists: map[string]map[string]struct{}{}}
	e.dnsSrv = &DNSServer{}
	c := e.cfg.Categories["social"]
	c.Schedule = Schedule{Mode: "always"}
	e.cfg.Categories["social"] = c
	e.cfg.Social[e.cfg.findSite("youtube.com")].Level = level
	e.buildSnapshot()
	return e
}

// Text-only on YouTube must behave like Instagram: page, titles, comments and
// the app itself load, but thumbnails and video are blocked. That means the
// image/video hosts are blocked while the player SCRIPTS (s.ytimg.com) and the
// feed API (youtubei.googleapis.com) stay reachable — otherwise the page is
// blank, not text-only.
func TestYouTubeTextOnlyLikeInstagram(t *testing.T) {
	e := ytEngine(t, LevelText)
	now := time.Now()

	blocked := []string{
		"i.ytimg.com", "i9.ytimg.com", "img.youtube.com", // thumbnails
		"rr3---sn-abc.googlevideo.com", // video stream
		"yt3.ggpht.com",                // avatars
	}
	for _, d := range blocked {
		if !e.dnsSrv.blocked(d, now) {
			t.Errorf("Text-only should block media host %s", d)
		}
	}
	allowed := []string{
		"www.youtube.com",         // the page itself
		"s.ytimg.com",             // player scripts + core CSS (must load!)
		"youtubei.googleapis.com", // feed/comments data API (must load!)
		"youtu.be",                // short links (not media; leave for Blocked)
	}
	for _, d := range allowed {
		if e.dnsSrv.blocked(d, now) {
			t.Errorf("Text-only must NOT block %s (page would go blank)", d)
		}
	}
}

// Blocked on YouTube must take down everything, including the scripts/API/embeds
// that Text-only deliberately leaves alone.
func TestYouTubeBlockedTakesDownEverything(t *testing.T) {
	e := ytEngine(t, LevelBlocked)
	now := time.Now()
	for _, d := range []string{
		"www.youtube.com", "i.ytimg.com", "s.ytimg.com", "rr3---sn-abc.googlevideo.com",
		"youtu.be", "youtube-nocookie.com", "youtubei.googleapis.com",
	} {
		if !e.dnsSrv.blocked(d, now) {
			t.Errorf("Blocked YouTube should block %s", d)
		}
	}
	// Other Google services stay up.
	if e.dnsSrv.blocked("mail.google.com", now) || e.dnsSrv.blocked("drive.google.com", now) {
		t.Error("blocking YouTube must not touch other Google services")
	}
}
