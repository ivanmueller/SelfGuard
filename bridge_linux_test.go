package main

// Runs the REAL service engine + API on Linux and drives it through the exact
// same HTTP calls the WebView2 bridge (sgStatus/sgCommand) makes, so the whole
// request/response contract the GUI depends on is exercised even though the
// WebView2 window itself can't run here.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func bridgeBase(t *testing.T) string {
	b, err := os.ReadFile(filepath.Join(dataDir(), "api.port"))
	if err != nil {
		t.Fatal(err)
	}
	return "http://127.0.0.1:" + strings.TrimSpace(string(b))
}

func TestGUIBridgeContract(t *testing.T) {
	t.Setenv("ProgramData", t.TempDir())
	e := &Engine{cfg: DefaultConfig(), lists: map[string]map[string]struct{}{}}
	e.dnsSrv = &DNSServer{}
	e.buildSnapshot()
	if err := e.startAPI(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	base := bridgeBase(t)

	// sgStatus -> must decode into the exact shape the JS reads.
	resp, err := http.Get(base + "/status")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var st struct {
		DelayHours float64 `json:"delay_hours"`
		Sites      []struct {
			Domain     string   `json:"domain"`
			Level      string   `json:"level"`
			MediaCount int      `json:"media_count"`
			Media      []string `json:"media"`
			Inherits   bool     `json:"inherits"`
			ActiveNow  bool     `json:"active_now"`
		} `json:"sites"`
		Categories []struct {
			Name    string `json:"name"`
			Domains int    `json:"domains"`
		} `json:"categories"`
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatalf("status JSON the GUI can't parse: %v\n%s", err, raw)
	}
	if len(st.Sites) == 0 {
		t.Fatal("GUI would render an empty social list")
	}
	if st.DelayHours != 0 {
		t.Fatalf("expected setup mode, got %g", st.DelayHours)
	}

	// sgCommand set_site_level -> the JSON payload shape the JS sends.
	cmd := func(kind, payload string) map[string]any {
		body, _ := json.Marshal(map[string]string{"kind": kind, "payload": payload})
		r, err := http.Post(base+"/command", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		var out map[string]any
		json.NewDecoder(r.Body).Decode(&out)
		return out
	}
	out := cmd("set_site_level", `{"domain":"reddit.com","level":"text"}`)
	if out["ok"] != true {
		t.Fatalf("set_site_level failed: %v", out)
	}
	// Re-read: reddit must now be text with its media intact for the detail view.
	resp2, _ := http.Get(base + "/status")
	raw2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	json.Unmarshal(raw2, &st)
	var found bool
	for _, s := range st.Sites {
		if s.Domain == "reddit.com" {
			found = true
			if s.Level != "text" {
				t.Fatalf("reddit level didn't update: %s", s.Level)
			}
			if s.MediaCount == 0 || len(s.Media) == 0 {
				t.Fatal("text-only detail view would show no media hosts")
			}
		}
	}
	if !found {
		t.Fatal("reddit missing from sites")
	}

	// add_site (the JSON shape the JS builds), then it should appear.
	if cmd("add_site", `{"domain":"linkedin.com","level":"blocked"}`)["ok"] != true {
		t.Fatal("add_site failed")
	}
	// bad TLD add should be rejected with ok:false and a message (JS toasts it).
	bad := cmd("block_domain", "com")
	if bad["ok"] != false || bad["message"] == "" {
		t.Fatalf("bare TLD should be rejected with a message, got %v", bad)
	}
}

// The GUI can set the new No-images level via the bridge, and status reflects it.
func TestBridgeNoImagesLevel(t *testing.T) {
	t.Setenv("ProgramData", t.TempDir())
	e := &Engine{cfg: DefaultConfig(), lists: map[string]map[string]struct{}{}}
	e.dnsSrv = &DNSServer{}
	e.buildSnapshot()
	if err := e.startAPI(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(120 * time.Millisecond)
	base := bridgeBase(t)
	body, _ := json.Marshal(map[string]string{"kind": "set_site_level", "payload": `{"domain":"youtube.com","level":"noimages"}`})
	r, err := http.Post(base+"/command", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	json.NewDecoder(r.Body).Decode(&out)
	r.Body.Close()
	if out["ok"] != true {
		t.Fatalf("set noimages failed: %v", out)
	}
	resp, _ := http.Get(base + "/status")
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var st struct {
		Sites []struct {
			Domain     string `json:"domain"`
			Level      string `json:"level"`
			ImageCount int    `json:"image_count"`
			VideoCount int    `json:"video_count"`
		} `json:"sites"`
	}
	json.Unmarshal(raw, &st)
	for _, s := range st.Sites {
		if s.Domain == "youtube.com" {
			if s.Level != "noimages" {
				t.Fatalf("level didn't persist: %s", s.Level)
			}
			if s.ImageCount == 0 || s.VideoCount == 0 {
				t.Fatalf("expected split image/video counts, got img=%d vid=%d", s.ImageCount, s.VideoCount)
			}
		}
	}
}
