package main

import "testing"

// The set-based lock: turning ON any block is instant; turning OFF any block
// (or switching between No-images and No-video, which unblocks one) is delayed.
func TestStricterOrEqualMatrix(t *testing.T) {
	instant := [][2]string{
		{LevelNoImages, LevelAllowed}, {LevelNoVideo, LevelAllowed},
		{LevelText, LevelNoImages}, {LevelText, LevelNoVideo},
		{LevelBlocked, LevelText}, {LevelBlocked, LevelAllowed},
		{LevelText, LevelText},
	}
	for _, c := range instant {
		if !stricterOrEqual(c[0], c[1]) {
			t.Errorf("%s over %s should be instant (stricter-or-equal)", c[0], c[1])
		}
	}
	delayed := [][2]string{
		{LevelAllowed, LevelNoImages}, {LevelNoImages, LevelText},
		{LevelNoImages, LevelNoVideo}, {LevelNoVideo, LevelNoImages}, // switching unblocks one
		{LevelText, LevelBlocked}, {LevelAllowed, LevelBlocked},
	}
	for _, c := range delayed {
		if stricterOrEqual(c[0], c[1]) {
			t.Errorf("%s over %s should be delayed (unblocks something)", c[0], c[1])
		}
	}
}

// No-video blocks video hosts but leaves images and the page.
func TestNoVideoLevel(t *testing.T) {
	e := &Engine{cfg: DefaultConfig(), lists: map[string]map[string]struct{}{}}
	e.dnsSrv = &DNSServer{}
	c := e.cfg.Categories["social"]
	c.Schedule = Schedule{Mode: "always"}
	e.cfg.Categories["social"] = c
	for i := range e.cfg.Social {
		e.cfg.Social[i].Level = LevelAllowed
	}
	e.cfg.Social[e.cfg.findSite("reddit.com")].Level = LevelNoVideo
	e.buildSnapshot()
	now := timeNow()
	if !e.dnsSrv.blocked("v.redd.it", now) || !e.dnsSrv.blocked("packaged-media.redd.it", now) {
		t.Error("No-video should block reddit video hosts")
	}
	if e.dnsSrv.blocked("i.redd.it", now) {
		t.Error("No-video should leave reddit images")
	}
	if e.dnsSrv.blocked("www.reddit.com", now) {
		t.Error("No-video should leave the page")
	}
}
