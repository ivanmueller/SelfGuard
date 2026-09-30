package main

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

// Exercise the exact commands and status fields the redesigned GUI relies on.
func TestRedesignBridge(t *testing.T) {
	t.Setenv("ProgramData", t.TempDir())
	e := &Engine{cfg: DefaultConfig(), lists: map[string]map[string]struct{}{}}
	e.dnsSrv = &DNSServer{}
	e.buildSnapshot()
	if err := e.startAPI(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(120 * time.Millisecond)
	b, _ := os.ReadFile(filepath.Join(dataDir(), "api.port"))
	base := "http://127.0.0.1:" + strings.TrimSpace(string(b))

	cmd := func(kind, payload string) map[string]any {
		body, _ := json.Marshal(map[string]string{"kind": kind, "payload": payload})
		r, err := http.Post(base+"/command", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		var o map[string]any
		json.NewDecoder(r.Body).Decode(&o)
		return o
	}
	getSites := func() []map[string]any {
		r, _ := http.Get(base + "/status")
		raw, _ := io.ReadAll(r.Body)
		r.Body.Close()
		var st struct {
			Sites []map[string]any `json:"sites"`
		}
		json.Unmarshal(raw, &st)
		return st.Sites
	}

	// capability flags present and correct
	for _, s := range getSites() {
		switch s["domain"] {
		case "youtube.com", "reddit.com", "x.com", "facebook.com", "pinterest.com":
			if s["can_images"] != true || s["can_video"] != true {
				t.Errorf("%s should allow both controls", s["domain"])
			}
		case "instagram.com":
			if s["can_images"] != true || s["can_video"] != false {
				t.Errorf("instagram: images yes, video no; got img=%v vid=%v", s["can_images"], s["can_video"])
			}
		case "tiktok.com":
			if s["can_images"] != false || s["can_video"] != false {
				t.Errorf("tiktok: both controls off; got img=%v vid=%v", s["can_images"], s["can_video"])
			}
		}
	}

	// toggle -> novideo level (the new one the GUI produces)
	if cmd("set_site_level", `{"domain":"reddit.com","level":"novideo"}`)["ok"] != true {
		t.Fatal("set novideo failed")
	}
	// per-site schedule (windows)
	if cmd("set_site_schedule", `{"domain":"reddit.com","schedule":{"mode":"windows","windows":{"monday":[{"start":"09:00","end":"17:00"}]}}}`)["ok"] != true {
		t.Fatal("set_site_schedule failed")
	}
	// section schedule
	if cmd("set_schedule", `{"category":"social","schedule":{"mode":"always"}}`)["ok"] != true {
		t.Fatal("set_schedule failed")
	}
	// request a site -> writes requests.txt
	if cmd("request_site", "discord.com")["ok"] != true {
		t.Fatal("request_site failed")
	}
	if _, err := os.Stat(filepath.Join(dataDir(), "requests.txt")); err != nil {
		t.Fatal("requests.txt not written")
	}
}
