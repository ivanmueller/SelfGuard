package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The CLI and GUI talk to the running service over this local API. The service
// is the sole authority on the delay lock — the API can only *request* changes,
// which still pass through the same rules. So exposing it locally grants no bypass.
//
// The port is chosen at runtime (see startAPI) and written to portFile, because
// a fixed port can collide with other loopback services — notably mDNSResponder
// (Apple Bonjour) on 5354, which silently took the port in older builds.

func portFile() string { return filepath.Join(dataDir(), "api.port") }

// apiBase returns the base URL of the running service's API, e.g.
// "http://127.0.0.1:52345", by reading the port the service recorded.
func apiBase() (string, error) {
	b, err := os.ReadFile(portFile())
	if err != nil {
		return "", err
	}
	port := strings.TrimSpace(string(b))
	if port == "" {
		return "", fmt.Errorf("empty port file")
	}
	return "http://127.0.0.1:" + port, nil
}

type cmdReq struct {
	Kind    string `json:"kind"`
	Payload string `json:"payload"`
}

type cmdResp struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

type catStatus struct {
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	BlockedNow bool   `json:"blocked_now"`
	Domains    int    `json:"domains"`
	Mode       string `json:"mode"`
}

type pendingStatus struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Payload   string `json:"payload"`
	Effective string `json:"effective"`
	Remaining string `json:"remaining"`
}

type siteStatus struct {
	Domain     string   `json:"domain"`
	Level      string   `json:"level"`       // allowed | text | blocked
	MediaCount int      `json:"media_count"` // total image+video hosts
	ImageCount int      `json:"image_count"`
	VideoCount int      `json:"video_count"`
	CanImages  bool     `json:"can_images"`
	CanVideo   bool     `json:"can_video"`
	Media      []string `json:"media"`
	Mode       string   `json:"mode"`        // effective schedule mode
	Inherits   bool     `json:"inherits"`    // uses the social category schedule
	BlockedNow bool     `json:"blocked_now"` // main site blocked right now
	ActiveNow  bool     `json:"active_now"`  // inside its blocking window right now
}

type statusResp struct {
	Version     string          `json:"version"`
	UpdateReady string          `json:"update_ready"` // staged version awaiting install, or ""
	LockedDown  bool            `json:"locked_down"`  // device hardening applied
	DelayHours  float64         `json:"delay_hours"`
	Now         string          `json:"now"`
	NetTimeOK   bool            `json:"net_time_ok"`
	Categories  []catStatus     `json:"categories"`
	Sites       []siteStatus    `json:"sites"`
	CustomBlock []string        `json:"custom_block"`
	Allow       []string        `json:"allow"`
	Pending     []pendingStatus `json:"pending"`
}

func apiGetStatus() (*statusResp, error) {
	base, err := apiBase()
	if err != nil {
		return nil, err
	}
	c := &http.Client{Timeout: 15 * time.Second}
	resp, err := c.Get(base + "/status")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var s statusResp
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return nil, err
	}
	return &s, nil
}

func apiSend(kind, payload string) (*cmdResp, error) {
	base, err := apiBase()
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(cmdReq{Kind: kind, Payload: payload})
	c := &http.Client{Timeout: 20 * time.Second}
	resp, err := c.Post(base+"/command", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var cr cmdResp
	if err := json.Unmarshal(b, &cr); err != nil {
		return nil, fmt.Errorf("bad response from service: %s", string(b))
	}
	return &cr, nil
}
