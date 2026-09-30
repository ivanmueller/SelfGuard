package main

import (
	"testing"
	"time"
)

// X shards video across video.twimg.com and video-t-N.twimg.com; all must be
// blocked at Text/Blocked, while photos (pbs) and scripts (abs) behave per level.
func TestXVideoShardsBlocked(t *testing.T) {
	mk := func(level string) *Engine {
		e := &Engine{cfg: DefaultConfig(), lists: map[string]map[string]struct{}{}}
		e.dnsSrv = &DNSServer{}
		c := e.cfg.Categories["social"]
		c.Schedule = Schedule{Mode: "always"}
		e.cfg.Categories["social"] = c
		for i := range e.cfg.Social {
			e.cfg.Social[i].Level = LevelAllowed
		}
		e.cfg.Social[e.cfg.findSite("x.com")].Level = level
		e.buildSnapshot()
		return e
	}
	now := time.Now()
	videoHosts := []string{
		"video.twimg.com", "video-t-1.twimg.com", "video-t-2.twimg.com",
		"video-t-9.twimg.com",
	}

	// Text: all video shards blocked, plus photos.
	tx := mk(LevelText)
	for _, h := range videoHosts {
		if !tx.dnsSrv.blocked(h, now) {
			t.Errorf("Text should block X video shard %s", h)
		}
	}
	if !tx.dnsSrv.blocked("pbs.twimg.com", now) {
		t.Error("Text should block X photos")
	}
	if tx.dnsSrv.blocked("abs.twimg.com", now) {
		t.Error("Text must leave X scripts (abs.twimg.com) reachable")
	}

	// No-images: photos blocked, video shards still allowed.
	ni := mk(LevelNoImages)
	if !ni.dnsSrv.blocked("pbs.twimg.com", now) {
		t.Error("No-images should block X photos")
	}
	for _, h := range videoHosts {
		if ni.dnsSrv.blocked(h, now) {
			t.Errorf("No-images should still allow X video shard %s", h)
		}
	}

	// Wildcard must not catch non-video twimg hosts.
	if tx.dnsSrv.blocked("abs.twimg.com", now) || tx.dnsSrv.blocked("ton.twimg.com", now) {
		t.Error("video* pattern should only match video hosts")
	}
}
