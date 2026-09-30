package main

import (
	"testing"
	"time"
)

// No-images / Text on Pinterest must block every image CDN host (including the
// sponsored-pin hosts), while leaving scripts (s.pinimg.com) and, for No-images,
// video (v.pinimg.com) reachable.
func TestPinterestAllImageHosts(t *testing.T) {
	mk := func(level string) *Engine {
		e := &Engine{cfg: DefaultConfig(), lists: map[string]map[string]struct{}{}}
		e.dnsSrv = &DNSServer{}
		c := e.cfg.Categories["social"]
		c.Schedule = Schedule{Mode: "always"}
		e.cfg.Categories["social"] = c
		for i := range e.cfg.Social {
			e.cfg.Social[i].Level = LevelAllowed
		}
		e.cfg.Social[e.cfg.findSite("pinterest.com")].Level = level
		e.buildSnapshot()
		return e
	}
	now := time.Now()
	imageHosts := []string{
		"i.pinimg.com", "sm.pinimg.com", "u.pinimg.com",
		"media-cache-ak0.pinimg.com", "media-cache-ec0.pinimg.com",
		"i-dualstack-pinterest-map-fastly-net.pinimg.com",
	}

	ni := mk(LevelNoImages)
	for _, h := range imageHosts {
		if !ni.dnsSrv.blocked(h, now) {
			t.Errorf("No-images should block Pinterest image host %s", h)
		}
	}
	// Scripts and video must survive at No-images (page loads, video plays).
	if ni.dnsSrv.blocked("s.pinimg.com", now) {
		t.Error("No-images must leave Pinterest scripts (s.pinimg.com) reachable")
	}
	if ni.dnsSrv.blocked("v.pinimg.com", now) {
		t.Error("No-images should still allow Pinterest video")
	}
	if ni.dnsSrv.blocked("www.pinterest.com", now) {
		t.Error("No-images: the page must load")
	}

	tx := mk(LevelText)
	for _, h := range imageHosts {
		if !tx.dnsSrv.blocked(h, now) {
			t.Errorf("Text should block Pinterest image host %s", h)
		}
	}
	if !tx.dnsSrv.blocked("v.pinimg.com", now) {
		t.Error("Text should block Pinterest video too")
	}
	if tx.dnsSrv.blocked("s.pinimg.com", now) {
		t.Error("Text must leave Pinterest scripts reachable")
	}
}
