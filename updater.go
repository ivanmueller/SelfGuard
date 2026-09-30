package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// version is stamped at build time via -ldflags "-X main.version=1.6.0".
// "dev" (the default) is treated as older than any real release.
var version = "dev"

// The update source. The service (running as SYSTEM, so it can write into the
// locked install folder) checks this, downloads the newest build, verifies its
// SHA-256, and STAGES it next to the running binary. The actual swap+restart is
// done by the GUI's one-click "Install" (elevated) using the staged files —
// nothing is swapped silently, and a download is never run without a hash match.
const updateBase = "https://github.com/ivanmueller/SelfGuard/releases/latest/download"

const (
	manifestURL = updateBase + "/manifest.json"
	svcURL      = updateBase + "/selfguard-svc.exe"
	guiURL      = updateBase + "/SelfGuard.exe"
)

type updateManifest struct {
	Version string `json:"version"`
	SvcSHA  string `json:"svc_sha256"`
	GuiSHA  string `json:"gui_sha256"`
}

// stagedSvc/stagedGui are where verified downloads wait for the user to install.
func stagedSvc() string { return filepath.Join(installDir(), "selfguard-svc.new.exe") }
func stagedGui() string { return filepath.Join(installDir(), "SelfGuard.new.exe") }

// guiInstalledPath is where the GUI exe lives (beside the service).
func guiInstalledPath() string { return filepath.Join(installDir(), "SelfGuard.exe") }

// updateReadyFile records the version that is downloaded and staged, if any.
func updateReadyFile() string { return filepath.Join(dataDir(), "update.ready") }

// isNewer compares dotted numeric versions (e.g. "1.6.0" > "1.5.9"). A "dev" or
// unparseable local version is always older; a malformed remote is never newer.
func isNewer(remote, local string) bool {
	rp, ok := parseVer(remote)
	if !ok {
		return false
	}
	lp, ok := parseVer(local)
	if !ok {
		return true // local is "dev"/unknown -> any real release is newer
	}
	for i := 0; i < len(rp) || i < len(lp); i++ {
		var a, b int
		if i < len(rp) {
			a = rp[i]
		}
		if i < len(lp) {
			b = lp[i]
		}
		if a != b {
			return a > b
		}
	}
	return false
}

func parseVer(v string) ([]int, bool) {
	v = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(v), "v"))
	if v == "" || v == "dev" {
		return nil, false
	}
	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

func fetchManifest() (*updateManifest, error) {
	c := &http.Client{Timeout: 10 * time.Second}
	resp, err := c.Get(manifestURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("manifest HTTP %d", resp.StatusCode)
	}
	var m updateManifest
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&m); err != nil {
		return nil, err
	}
	if m.Version == "" {
		return nil, fmt.Errorf("manifest missing version")
	}
	return &m, nil
}

// download fetches url to dst and returns its SHA-256 (lowercase hex).
func download(url, dst string) (string, error) {
	c := &http.Client{Timeout: 5 * time.Minute}
	resp, err := c.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("download HTTP %d", resp.StatusCode)
	}
	tmp := dst + ".partial"
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, 64<<20)); err != nil {
		f.Close()
		os.Remove(tmp)
		return "", err
	}
	f.Close()
	sum := hex.EncodeToString(h.Sum(nil))
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return sum, nil
}

// stageVerified downloads url, checks its hash against want, and keeps it only
// if they match. A mismatch (corrupt or tampered download) is discarded.
func stageVerified(url, dst, want string) error {
	got, err := download(url, dst)
	if err != nil {
		return err
	}
	if !strings.EqualFold(got, want) {
		os.Remove(dst)
		return fmt.Errorf("hash mismatch for %s (got %s want %s)", filepath.Base(dst), got[:8], want[:8])
	}
	return nil
}

// updateLoop checks for a newer release shortly after start and every 6 hours.
func (e *Engine) updateLoop() {
	select {
	case <-e.stop:
		return
	case <-time.After(45 * time.Second): // let the network settle after boot
	}
	for {
		e.checkForUpdate()
		select {
		case <-e.stop:
			return
		case <-time.After(30 * time.Minute):
		}
	}
}

// checkForUpdate fetches the manifest and, if a newer version exists, downloads
// and verifies both binaries into the staging files, then records the version in
// update.ready. It NEVER swaps a running binary and NEVER touches config — so a
// bad or hostile update can, at worst, sit unused on disk. This is why an update
// cannot loosen your rules: it only ever stages a new *program*, and installing
// it re-reads your existing (delay-locked) config unchanged.
func (e *Engine) checkForUpdate() {
	m, err := fetchManifest()
	if err != nil {
		logf("update check: %v", err)
		return
	}
	if !isNewer(m.Version, version) {
		return
	}
	logf("update available: %s (running %s) - downloading", m.Version, version)
	if err := stageVerified(svcURL, stagedSvc(), m.SvcSHA); err != nil {
		logf("stage service: %v", err)
		return
	}
	if err := stageVerified(guiURL, stagedGui(), m.GuiSHA); err != nil {
		logf("stage gui: %v", err)
		return
	}
	_ = os.WriteFile(updateReadyFile(), []byte(m.Version), 0o644)
	logf("update %s downloaded and verified; awaiting install", m.Version)
}

// doCheckUpdate is the on-demand check behind the app's Update button. It reaches
// the update server, and: reports "up to date" if not newer; reports "ready" if
// the newer build is already downloaded; otherwise kicks off the download in the
// background (the banner appears when it's staged). No file picker, ever.
func (e *Engine) doCheckUpdate() (string, error) {
	m, err := fetchManifest()
	if err != nil {
		return "", fmt.Errorf("couldn't reach the update server")
	}
	if !isNewer(m.Version, version) {
		return fmt.Sprintf("You're on the latest version (%s).", version), nil
	}
	if updateReadyVersion() == m.Version {
		return fmt.Sprintf("Update %s is downloaded - click Install.", m.Version), nil
	}
	go e.checkForUpdate()
	return fmt.Sprintf("Update %s found - downloading now.", m.Version), nil
}

// updateReadyVersion returns the staged, verified version awaiting install, or "".
func updateReadyVersion() string {
	b, err := os.ReadFile(updateReadyFile())
	if err != nil {
		return ""
	}
	v := strings.TrimSpace(string(b))
	if !isNewer(v, version) { // already running it (or stale) -> clear
		_ = os.Remove(updateReadyFile())
		_ = os.Remove(stagedSvc())
		_ = os.Remove(stagedGui())
		return ""
	}
	return v
}
