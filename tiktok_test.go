package main

import (
	"testing"
	"time"
)

func ttEngine(t *testing.T, level string) *Engine {
	t.Helper()
	e := &Engine{cfg: DefaultConfig(), lists: map[string]map[string]struct{}{}}
	e.dnsSrv = &DNSServer{}
	c := e.cfg.Categories["social"]
	c.Schedule = Schedule{Mode: "always"}
	e.cfg.Categories["social"] = c
	for i := range e.cfg.Social {
		e.cfg.Social[i].Level = LevelAllowed
	}
	e.cfg.Social[e.cfg.findSite("tiktok.com")].Level = level
	e.buildSnapshot()
	return e
}

func TestTikTokTextBlocksVideoHosts(t *testing.T) {
	e := ttEngineText(t)
	now := time.Now()
	videoHosts := []string{
		"v16a.tiktokcdn.com", "v16m.tiktokcdn.com", "v19.tiktokcdn.com",
		"pull-hls-f5.tiktokcdn.com", "api.tiktokv.com",
		"v16-tiktokcdn-com.akamaized.net", "p16-tiktokcdn-com.akamaized.net",
		"tiktokcdn-com.akamaized.net", "v16m.tiktokcdn.com.akamaized.net",
		"abtest-va-tiktok.byteoversea.com", "x.bytefcdn-oversea.com",
		"tiktokcdn-eu.com",
	}
	for _, h := range videoHosts {
		if !e.dnsSrv.blocked(h, now) {
			t.Errorf("Text should block TikTok host %s", h)
		}
	}
	// The main page still resolves at Text (so it's not a full block).
	if e.dnsSrv.blocked("www.tiktok.com", now) {
		t.Error("Text should leave the tiktok.com page resolvable")
	}
	// A shared akamaized host with nothing to do with TikTok must NOT be caught.
	if e.dnsSrv.blocked("www.example.akamaized.net", now) {
		t.Error("contains-pattern must not block unrelated akamaized.net hosts")
	}
}

func ttEngineText(t *testing.T) *Engine { return ttEngine(t, LevelText) }

func TestTikTokNoImagesBlocksThumbs(t *testing.T) {
	e := ttEngine(t, LevelNoImages)
	now := time.Now()
	// Image/thumbnail hosts blocked...
	for _, h := range []string{"p16-va.ibyteimg.com", "p16.tiktokcdn.com", "p16-tiktok-va.akamaized.net"} {
		if !e.dnsSrv.blocked(h, now) {
			t.Errorf("No-images should block TikTok image host %s", h)
		}
	}
	// ...while video hosts still resolve (No-images allows video).
	for _, h := range []string{"v16a.tiktokcdn.com", "v16-tiktokcdn-com.akamaized.net"} {
		if e.dnsSrv.blocked(h, now) {
			t.Errorf("No-images should still allow TikTok video host %s", h)
		}
	}
}

func TestTikTokBlockedIsWholeSite(t *testing.T) {
	e := ttEngine(t, LevelBlocked)
	now := time.Now()
	for _, h := range []string{"www.tiktok.com", "m.tiktok.com", "v16a.tiktokcdn.com", "api.tiktokv.com"} {
		if !e.dnsSrv.blocked(h, now) {
			t.Errorf("Blocked should take down %s", h)
		}
	}
}
