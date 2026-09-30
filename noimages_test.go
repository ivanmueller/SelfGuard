package main

import (
	"testing"
	"time"
)

// "No images" must hide thumbnails/photos while still letting a clicked video
// play and the page/text load. On YouTube: block i.ytimg.com (thumbnails) and
// avatars, but ALLOW googlevideo.com (video), s.ytimg.com (scripts), the API,
// and the page itself.
func TestNoImagesLevelYouTube(t *testing.T) {
	e := &Engine{cfg: DefaultConfig(), lists: map[string]map[string]struct{}{}}
	e.dnsSrv = &DNSServer{}
	c := e.cfg.Categories["social"]
	c.Schedule = Schedule{Mode: "always"}
	e.cfg.Categories["social"] = c
	e.cfg.Social[e.cfg.findSite("youtube.com")].Level = LevelNoImages
	e.buildSnapshot()
	now := time.Now()

	blocked := []string{"i.ytimg.com", "i9.ytimg.com", "img.youtube.com", "yt3.ggpht.com"}
	for _, d := range blocked {
		if !e.dnsSrv.blocked(d, now) {
			t.Errorf("No-images should block thumbnail host %s", d)
		}
	}
	allowed := []string{
		"rr3---sn-abc.googlevideo.com", // video must still play
		"www.youtube.com", "s.ytimg.com", "youtubei.googleapis.com",
	}
	for _, d := range allowed {
		if e.dnsSrv.blocked(d, now) {
			t.Errorf("No-images must NOT block %s (you should still watch clicked videos)", d)
		}
	}
}

// On a site with separate image and video hosts (X), No-images blocks photos
// but leaves video playable; Text blocks both.
func TestNoImagesVsTextOnX(t *testing.T) {
	mk := func(level string) *Engine {
		e := &Engine{cfg: DefaultConfig(), lists: map[string]map[string]struct{}{}}
		e.dnsSrv = &DNSServer{}
		cc := e.cfg.Categories["social"]
		cc.Schedule = Schedule{Mode: "always"}
		e.cfg.Categories["social"] = cc
		e.cfg.Social[e.cfg.findSite("x.com")].Level = level
		e.buildSnapshot()
		return e
	}
	now := time.Now()

	ni := mk(LevelNoImages)
	if !ni.dnsSrv.blocked("pbs.twimg.com", now) {
		t.Error("No-images should block X photos (pbs.twimg.com)")
	}
	if ni.dnsSrv.blocked("video.twimg.com", now) {
		t.Error("No-images should still allow X video")
	}

	tx := mk(LevelText)
	if !tx.dnsSrv.blocked("pbs.twimg.com", now) || !tx.dnsSrv.blocked("video.twimg.com", now) {
		t.Error("Text should block both photos and video on X")
	}
}

// Strictness ordering drives the delay lock: allowed < noimages < text < blocked.
func TestLevelStrictnessOrdering(t *testing.T) {
	if !(strictness(LevelAllowed) < strictness(LevelNoImages) &&
		strictness(LevelNoImages) < strictness(LevelText) &&
		strictness(LevelText) < strictness(LevelBlocked)) {
		t.Fatal("level strictness must be allowed < noimages < text < blocked")
	}
}
