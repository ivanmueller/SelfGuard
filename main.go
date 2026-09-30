package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kardianos/service"
)

// ---------------------------------------------------------------------------
// Engine: the running filter. Holds authoritative state in memory, persists to
// the protected config, and reconciles system settings on a timer.
// ---------------------------------------------------------------------------

type Engine struct {
	mu     sync.Mutex
	cfg    *Config
	dnsSrv *DNSServer
	lists  map[string]map[string]struct{} // category -> domain set
	stop   chan struct{}

	// System-DNS state, touched only by the dnsLoop goroutine.
	failingSince time.Time // zero while healthy
	failedOpen   bool
}

// failOpenAfter is how long the filter must be continuously unable to resolve
// anything before Windows is put back on normal DNS. Long enough to ride out a
// slow network at boot (the PC is offline then anyway); short enough that a
// genuine fault can't leave the machine without internet.
const failOpenAfter = 2 * time.Minute

func (e *Engine) run() error {
	// Ensure SYSTEM/Admins own the data folder BEFORE reading config, so a
	// stale or bad ACL can't stop the service from reading its own files.
	_ = protectDataDir()

	cfg, err := LoadConfig()
	if err != nil {
		_ = resetSystemDNS() // never leave Windows pointed at a resolver that isn't running
		return err
	}
	e.cfg = cfg
	e.lists = map[string]map[string]struct{}{}
	e.stop = make(chan struct{})

	e.loadListsFromCache() // offline-friendly startup
	e.dnsSrv = &DNSServer{upstream: upstreamList(cfg.Upstream)}
	e.buildSnapshot()

	if err := e.dnsSrv.Start(); err != nil {
		_ = resetSystemDNS() // never leave Windows pointed at a resolver that isn't running
		return fmt.Errorf("could not start DNS on 127.0.0.1:53 — is another DNS service/VPN/ICS using port 53? underlying error: %w", err)
	}
	// System DNS is pointed at us by dnsLoop, and only once we've confirmed we
	// can actually resolve names (see syncSystemDNS).
	_ = applyPolicies()
	if err := e.startAPI(); err != nil {
		_ = resetSystemDNS()
		e.dnsSrv.Stop()
		return fmt.Errorf("control API: %w", err)
	}

	sweepUpdateLeftovers() // heal any half-finished previous update
	go e.refreshLists()
	go e.dnsLoop()
	go e.reconcileLoop()
	go e.updateLoop()
	return nil
}

// shutdown stops the engine. resetDNS is true for a deliberate service stop
// (admin-only): Windows goes back to normal DNS so the PC stays online. It is
// false when Windows itself is shutting down or restarting: DNS stays pointed
// at 127.0.0.1, so nothing resolves unfiltered before the service is back up,
// and Safe Mode (where the service doesn't run) has no DNS rather than
// unfiltered DNS. At the next start, dnsLoop fails open if it can't resolve.
func (e *Engine) shutdown(resetDNS bool) {
	logf("service stopping (resetDNS=%v)", resetDNS)
	if e.stop != nil {
		select {
		case <-e.stop:
		default:
			close(e.stop)
		}
	}
	if resetDNS {
		_ = resetSystemDNS()
	}
	if e.dnsSrv != nil {
		e.dnsSrv.Stop()
	}
}

// upstreamList is the configured resolvers plus the network's own DHCP DNS
// (the router) and two public fallbacks, so one unreachable or blocked
// provider can't take every lookup down with it.
func upstreamList(configured []string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(a string) {
		a = strings.TrimSpace(a)
		if a == "" {
			return
		}
		if _, _, err := net.SplitHostPort(a); err != nil {
			a = net.JoinHostPort(a, "53") // allow "1.1.1.1" without ":53"
		}
		if !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	for _, u := range configured {
		add(u)
	}
	for _, u := range dhcpDNSServers() {
		add(u)
	}
	add("8.8.8.8:53")
	add("9.9.9.9:53")
	return out
}

// dnsLoop keeps Windows pointed at the local filter while the filter works.
// It re-checks every minute when healthy (which also reverts anyone changing
// the adapter DNS) and every 10 seconds when not.
func (e *Engine) dnsLoop() {
	for {
		wait := 60 * time.Second
		if !e.syncSystemDNS() {
			wait = 10 * time.Second
		}
		select {
		case <-e.stop:
			return
		case <-time.After(wait):
		}
	}
}

// syncSystemDNS points Windows at the local filter when it can actually
// resolve names. If it has been unable to for failOpenAfter (a real fault, not
// just a slow boot), Windows is put back on normal DNS so the PC stays online.
// Blocking only ever returns NXDOMAIN for listed sites; it must never be able
// to take down every site.
func (e *Engine) syncSystemDNS() bool {
	if e.dnsSrv.healthy() {
		e.failingSince = time.Time{}
		e.failedOpen = false
		_ = setSystemDNS()
		return true
	}
	if e.failingSince.IsZero() {
		e.failingSince = time.Now()
	}
	if !e.failedOpen && time.Since(e.failingSince) >= failOpenAfter {
		_ = resetSystemDNS()
		e.failedOpen = true
	}
	return false
}

// ---- list building ----

func (e *Engine) loadListsFromCache() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for name, cat := range e.cfg.Categories {
		set := map[string]struct{}{}
		for _, d := range cat.ExtraDomains {
			addBlockable(set, d)
		}
		for _, d := range loadCache(name) {
			addBlockable(set, d)
		}
		e.lists[name] = set
	}
}

func (e *Engine) refreshLists() {
	e.mu.Lock()
	cats := map[string]Category{}
	for k, v := range e.cfg.Categories {
		cats[k] = v
	}
	e.mu.Unlock()

	for name, cat := range cats {
		set := map[string]struct{}{}
		for _, d := range cat.ExtraDomains {
			addBlockable(set, d)
		}
		var merged []string
		for _, u := range cat.BlocklistURLs {
			domains, err := fetchList(u)
			if err != nil {
				continue
			}
			merged = append(merged, domains...)
		}
		for _, d := range merged {
			addBlockable(set, d)
		}
		if len(merged) > 0 {
			_ = saveCache(name, merged)
		}
		e.mu.Lock()
		e.lists[name] = set
		e.mu.Unlock()
	}
	e.buildSnapshot()
}

func (e *Engine) buildSnapshot() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.buildSnapshotLocked()
}

func (e *Engine) buildSnapshotLocked() {
	allow := map[string]struct{}{}
	for _, d := range e.cfg.Allow {
		allow[normDomain(d)] = struct{}{}
	}
	var cats []catView
	if len(e.cfg.CustomBlock) > 0 {
		cset := map[string]struct{}{}
		for _, d := range e.cfg.CustomBlock {
			addBlockable(cset, d)
		}
		cats = append(cats, catView{name: "custom", set: cset, sched: Schedule{Mode: "always"}, enabled: true})
	}
	for name, cat := range e.cfg.Categories {
		if name == "social" {
			continue // social is handled per-site below, not as a flat set
		}
		set := e.lists[name]
		if set == nil {
			set = map[string]struct{}{}
		}
		cats = append(cats, catView{name: name, set: set, sched: cat.Schedule, enabled: cat.Enabled})
	}

	// Per-site social rules. If the social category is disabled entirely, no
	// site rules apply (everything social is allowed).
	var sites []siteRule
	if cat, ok := e.cfg.Categories["social"]; !ok || cat.Enabled {
		for _, site := range e.cfg.Social {
			images := map[string]struct{}{}
			var imagePats []hostPat
			for _, m := range site.ImageHosts {
				if pat, ok := parseHostPat(m); ok {
					imagePats = append(imagePats, pat)
				} else {
					addBlockable(images, m)
				}
			}
			videos := map[string]struct{}{}
			var videoPats []hostPat
			for _, m := range site.VideoHosts {
				if pat, ok := parseHostPat(m); ok {
					videoPats = append(videoPats, pat)
				} else {
					addBlockable(videos, m)
				}
			}
			full := map[string]struct{}{}
			for _, m := range site.BlockAlso {
				addBlockable(full, m)
			}
			level := site.Level
			if level == "" {
				level = LevelBlocked
			}
			sites = append(sites, siteRule{
				main:      normDomain(site.Domain),
				images:    images,
				videos:    videos,
				full:      full,
				imagePats: imagePats,
				videoPats: videoPats,
				level:     level,
				sched:     e.cfg.socialSchedule(site),
			})
		}
	}

	if e.dnsSrv != nil {
		e.dnsSrv.snap.Store(&snapshot{allow: allow, cats: cats, sites: sites})
	}
}

// ---- reconcile ----

func (e *Engine) reconcileLoop() {
	tick := time.NewTicker(60 * time.Second)
	defer tick.Stop()
	lastRefresh := time.Now()
	for {
		select {
		case <-e.stop:
			return
		case <-tick.C:
			_ = applyPolicies()
			e.processPending()
			if time.Since(lastRefresh) > 12*time.Hour {
				lastRefresh = time.Now()
				go e.refreshLists()
			}
		}
	}
}

func (e *Engine) processPending() {
	e.mu.Lock()
	n := len(e.cfg.Pending)
	e.mu.Unlock()
	if n == 0 {
		return
	}

	now, err := networkNow()
	if err != nil {
		return // fail closed: can't verify time -> mature nothing
	}

	e.mu.Lock()
	var remaining, matured []Pending
	for _, p := range e.cfg.Pending {
		if now.Before(p.Effective) {
			remaining = append(remaining, p)
		} else {
			matured = append(matured, p)
		}
	}
	e.cfg.Pending = remaining
	doUninstall := false
	for _, p := range matured {
		if p.Kind == "uninstall" {
			doUninstall = true
			continue
		}
		e.applyMatured(p)
	}
	_ = e.cfg.Save()
	e.buildSnapshotLocked()
	e.mu.Unlock()

	if doUninstall {
		e.performUninstall()
	}
}

// applyMatured must be called with e.mu held.
func (e *Engine) applyMatured(p Pending) {
	switch p.Kind {
	case "disable_category":
		if c, ok := e.cfg.Categories[p.Payload]; ok {
			c.Enabled = false
			e.cfg.Categories[p.Payload] = c
		}
	case "allow_domain":
		e.cfg.Allow = appendUnique(e.cfg.Allow, normDomain(p.Payload))
	case "unblock_custom":
		e.cfg.CustomBlock = removeItem(e.cfg.CustomBlock, normDomain(p.Payload))
	case "set_delay":
		if v, err := strconv.ParseFloat(p.Payload, 64); err == nil {
			e.cfg.DelayHours = v
		}
	case "set_schedule":
		var ps schedulePayload
		if json.Unmarshal([]byte(p.Payload), &ps) == nil {
			if c, ok := e.cfg.Categories[ps.Category]; ok {
				c.Schedule = ps.Schedule
				e.cfg.Categories[ps.Category] = c
			}
		}
	case "set_site_level":
		var lp siteLevelPayload
		if json.Unmarshal([]byte(p.Payload), &lp) == nil {
			if i := e.cfg.findSite(lp.Domain); i >= 0 {
				e.cfg.Social[i].Level = lp.Level
			}
		}
	case "set_site_schedule":
		var sp siteSchedulePayload
		if json.Unmarshal([]byte(p.Payload), &sp) == nil {
			if i := e.cfg.findSite(sp.Domain); i >= 0 {
				e.cfg.Social[i].Schedule = sp.Schedule
			}
		}
	case "remove_site":
		if i := e.cfg.findSite(p.Payload); i >= 0 {
			e.cfg.Social = append(e.cfg.Social[:i], e.cfg.Social[i+1:]...)
		}
	}
}

func (e *Engine) performUninstall() {
	_ = resetSystemDNS()
	_ = removePolicies()
	go func() {
		_ = serviceDelete()
		_ = os.RemoveAll(dataDir())
		_ = removeInstallDirSoon(installDir())
		os.Exit(0)
	}()
}

// ---- command handling (the delay-lock rules live here) ----

func (e *Engine) handleCommand(kind, payload string) (string, error) {
	if kind == "check_update" {
		return e.doCheckUpdate() // does its own network I/O; must not hold e.mu
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	switch kind {
	// ---------- immediate: these make things STRICTER ----------
	case "enable_category":
		c, ok := e.cfg.Categories[payload]
		if !ok {
			return "", fmt.Errorf("unknown category %q", payload)
		}
		c.Enabled = true
		e.cfg.Categories[payload] = c
		e.saveAndRebuild()
		return fmt.Sprintf("Category %q enabled (effective now).", payload), nil

	case "block_domain":
		d := normDomain(payload)
		if d == "" {
			return "", fmt.Errorf("empty domain")
		}
		if !isBlockable(d) {
			return "", fmt.Errorf("%q isn't a specific site: blocking it would block every site ending in .%s", payload, d)
		}
		e.cfg.CustomBlock = appendUnique(e.cfg.CustomBlock, d)
		e.cfg.Allow = removeItem(e.cfg.Allow, d)
		e.saveAndRebuild()
		return fmt.Sprintf("Now blocking %s (effective now).", d), nil

	case "lock_down":
		if err := applyLockdown(); err != nil {
			return "", fmt.Errorf("could not lock down: %w", err)
		}
		logf("device locked down")
		return "Device locked down.", nil

	case "apply_gui":
		if err := swapGuiOnly(stagedGui(), guiInstalledPath()); err != nil {
			return "", fmt.Errorf("gui swap failed: %w", err)
		}
		logf("gui updated via apply_gui")
		return "GUI updated.", nil

	case "apply_update":
		v := updateReadyVersion()
		if v == "" {
			return "", fmt.Errorf("no verified update is staged")
		}
		if err := swapAndRestart(stagedSvc(), installedExe(), updateReadyFile()); err != nil {
			return "", fmt.Errorf("could not start the update: %w", err)
		}
		logf("applying update %s (service will restart)", v)
		return fmt.Sprintf("Installing %s — the service will restart in a moment.", v), nil

	case "request_site":
		d := normDomain(payload)
		if d == "" {
			return "", fmt.Errorf("empty request")
		}
		line := time.Now().Format("2006-01-02 15:04") + "  " + d + "\r\n"
		f, err := os.OpenFile(filepath.Join(dataDir(), "requests.txt"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err == nil {
			_, _ = f.WriteString(line)
			f.Close()
		}
		return fmt.Sprintf("Requested %s — saved to requests.txt.", d), nil

	case "cancel_pending":
		before := len(e.cfg.Pending)
		e.cfg.Pending = filterPending(e.cfg.Pending, payload)
		if len(e.cfg.Pending) == before {
			return "", fmt.Errorf("no pending change with id %q", payload)
		}
		_ = e.cfg.Save()
		return "Pending change cancelled.", nil

	// ---------- delay-locked: these make things LOOSER ----------
	case "disable_category":
		if _, ok := e.cfg.Categories[payload]; !ok {
			return "", fmt.Errorf("unknown category %q", payload)
		}
		return e.queue(kind, payload)

	case "allow_domain", "unblock_custom", "set_schedule", "uninstall":
		return e.queue(kind, payload)

	case "set_delay":
		v, err := strconv.ParseFloat(payload, 64)
		if err != nil || v < 0 {
			return "", fmt.Errorf("invalid hours: %q", payload)
		}
		if v >= e.cfg.DelayHours { // raising the delay is stricter -> immediate
			e.cfg.DelayHours = v
			_ = e.cfg.Save()
			return fmt.Sprintf("Delay raised to %g hours (effective now).", v), nil
		}
		return e.queue(kind, payload) // lowering is looser -> delayed

	case "add_site":
		// Adding a blocked site is stricter -> immediate.
		var ap addSitePayload
		if json.Unmarshal([]byte(payload), &ap) != nil {
			ap.Domain = payload // allow a bare domain string
		}
		d := normDomain(ap.Domain)
		if !isBlockable(d) {
			return "", fmt.Errorf("%q isn't a specific site", ap.Domain)
		}
		if e.cfg.findSite(d) >= 0 {
			return "", fmt.Errorf("%s is already managed", d)
		}
		lvl := ap.Level
		if !validLevel(lvl) {
			lvl = LevelBlocked
		}
		var images []string
		for _, m := range ap.Media {
			if md := normDomain(m); isBlockable(md) {
				images = append(images, md)
			}
		}
		e.cfg.Social = append(e.cfg.Social, Site{Domain: d, Level: lvl, ImageHosts: images, Schedule: Schedule{Mode: "inherit"}})
		e.saveAndRebuild()
		return fmt.Sprintf("Now managing %s at %q (effective now).", d, lvl), nil

	case "set_site_level":
		var lp siteLevelPayload
		if json.Unmarshal([]byte(payload), &lp) != nil {
			return "", fmt.Errorf("bad payload")
		}
		i := e.cfg.findSite(lp.Domain)
		if i < 0 {
			return "", fmt.Errorf("%s isn't managed", lp.Domain)
		}
		if !validLevel(lp.Level) {
			return "", fmt.Errorf("unknown level %q", lp.Level)
		}
		cur := e.cfg.Social[i].Level
		if cur == "" {
			cur = LevelBlocked
		}
		if stricterOrEqual(lp.Level, cur) {
			// stricter or same -> immediate
			e.cfg.Social[i].Level = lp.Level
			e.saveAndRebuild()
			return fmt.Sprintf("%s set to %q (effective now).", lp.Domain, lp.Level), nil
		}
		return e.queue(kind, payload) // unblocks something -> delayed

	case "set_site_schedule":
		// Any schedule change is treated as a loosening (it can reduce blocked
		// time) -> delayed while locked, immediate in setup mode (delay 0).
		var sp siteSchedulePayload
		if json.Unmarshal([]byte(payload), &sp) != nil {
			return "", fmt.Errorf("bad payload")
		}
		if e.cfg.findSite(sp.Domain) < 0 {
			return "", fmt.Errorf("%s isn't managed", sp.Domain)
		}
		if e.cfg.DelayHours == 0 {
			i := e.cfg.findSite(sp.Domain)
			e.cfg.Social[i].Schedule = sp.Schedule
			e.saveAndRebuild()
			return fmt.Sprintf("Schedule for %s updated (effective now).", sp.Domain), nil
		}
		return e.queue(kind, payload)

	case "remove_site":
		// Removing a managed site is a loosening -> delayed.
		if e.cfg.findSite(payload) < 0 {
			return "", fmt.Errorf("%s isn't managed", payload)
		}
		return e.queue(kind, payload)
	}
	return "", fmt.Errorf("unknown command %q", kind)
}

func (e *Engine) saveAndRebuild() {
	_ = e.cfg.Save()
	e.buildSnapshotLocked()
	// A change that applied immediately is a tightening (blocking is instant;
	// loosening is delayed). Flush the OS DNS cache so a freshly-blocked site
	// stops resolving right away instead of serving a cached address.
	flushDNS()
}

// queue must be called with e.mu held.
func (e *Engine) queue(kind, payload string) (string, error) {
	// Setup mode (no delay set): apply immediately instead of queuing, so every
	// change is instant while the user is still testing. This is the single
	// place all "looser" commands funnel through, so they all become instant.
	if e.cfg.DelayHours == 0 {
		if kind == "uninstall" {
			e.performUninstall() // spawns its own goroutine, then exits
			return "Uninstalling now (no delay set).", nil
		}
		e.applyMatured(Pending{Kind: kind, Payload: payload})
		e.saveAndRebuild()
		return "Done — effective now (no delay set).", nil
	}
	now, err := networkNow()
	if err != nil {
		now = time.Now().UTC() // best effort; maturity is still checked vs network time
	}
	eff := now.Add(time.Duration(e.cfg.DelayHours * float64(time.Hour)))
	p := Pending{ID: newID(), Kind: kind, Payload: payload, Requested: now, Effective: eff}
	e.cfg.Pending = append(e.cfg.Pending, p)
	_ = e.cfg.Save()
	return fmt.Sprintf(
		"Request queued (id %s). Takes effect in ~%g h, at %s UTC. Everything stays blocked until then.\nCancel anytime with:  selfguard cancel %s",
		p.ID, e.cfg.DelayHours, eff.Format("2006-01-02 15:04"), p.ID), nil
}

func (e *Engine) status() statusResp {
	// Network call first, outside the lock: it can take many seconds when the
	// network is down, and holding the lock would stall every other request.
	nnow, nerr := networkNow()
	netOK := nerr == nil

	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now()

	var cats []catStatus
	for name, cat := range e.cfg.Categories {
		cats = append(cats, catStatus{
			Name:       name,
			Enabled:    cat.Enabled,
			BlockedNow: cat.blockedAt(now),
			Domains:    len(e.lists[name]),
			Mode:       cat.Schedule.Mode,
		})
	}
	socialOn := true
	if cat, ok := e.cfg.Categories["social"]; ok {
		socialOn = cat.Enabled
	}
	var sites []siteStatus
	for _, site := range e.cfg.Social {
		sc := e.cfg.socialSchedule(site)
		active := socialOn && sc.activeAt(now)
		level := site.Level
		if level == "" {
			level = LevelBlocked
		}
		blockedNow := active && level == LevelBlocked
		media := append(append([]string(nil), site.ImageHosts...), site.VideoHosts...)
		// Which partial controls the UI should offer. A site needs the relevant
		// hosts, and TikTok is excluded from partial control entirely because it
		// rotates CDNs too aggressively for image/video-only blocking to hold.
		canImg := len(site.ImageHosts) > 0 && site.Domain != "tiktok.com"
		canVid := len(site.VideoHosts) > 0 && site.Domain != "tiktok.com"
		sites = append(sites, siteStatus{
			Domain:     site.Domain,
			Level:      level,
			MediaCount: len(media),
			ImageCount: len(site.ImageHosts),
			VideoCount: len(site.VideoHosts),
			CanImages:  canImg,
			CanVideo:   canVid,
			Media:      media,
			Mode:       sc.Mode,
			Inherits:   site.Schedule.Mode == "" || site.Schedule.Mode == "inherit",
			BlockedNow: blockedNow,
			ActiveNow:  active,
		})
	}

	var pend []pendingStatus
	for _, p := range e.cfg.Pending {
		rem := "unknown (needs network time)"
		if netOK {
			left := p.Effective.Sub(nnow)
			if left < 0 {
				left = 0
			}
			rem = left.Round(time.Minute).String()
		}
		pend = append(pend, pendingStatus{
			ID: p.ID, Kind: p.Kind, Payload: p.Payload,
			Effective: p.Effective.Format("2006-01-02 15:04 UTC"),
			Remaining: rem,
		})
	}
	return statusResp{
		Version:     version,
		UpdateReady: updateReadyVersion(),
		LockedDown:  lockdownActive(),
		DelayHours:  e.cfg.DelayHours,
		Now:         now.Format("2006-01-02 15:04:05 Mon"),
		NetTimeOK:   netOK,
		Categories:  cats,
		Sites:       sites,
		CustomBlock: e.cfg.CustomBlock,
		Allow:       e.cfg.Allow,
		Pending:     pend,
	}
}

func (e *Engine) startAPI() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, e.status())
	})
	mux.HandleFunc("/command", func(w http.ResponseWriter, r *http.Request) {
		var req cmdReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, cmdResp{OK: false, Message: "bad request"})
			return
		}
		msg, err := e.handleCommand(req.Kind, req.Payload)
		if err != nil {
			writeJSON(w, cmdResp{OK: false, Message: err.Error()})
			return
		}
		writeJSON(w, cmdResp{OK: true, Message: msg})
	})

	// Bind loopback on an OS-assigned free port (":0"), so nothing else on the
	// machine — e.g. mDNSResponder, which squats on 5354 — can take it. Then
	// record the port so the CLI and GUI know where to reach us.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("api listen: %w", err)
	}
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		ln.Close()
		return err
	}
	if err := os.MkdirAll(dataDir(), 0o755); err != nil {
		ln.Close()
		return fmt.Errorf("data dir: %w", err)
	}
	if err := os.WriteFile(portFile(), []byte(port), 0o644); err != nil {
		ln.Close()
		return fmt.Errorf("write port file: %w", err)
	}
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	return nil
}

// ---------------------------------------------------------------------------
// Service integration (kardianos/service)
// ---------------------------------------------------------------------------

type program struct{ e *Engine }

func (p *program) Start(s service.Service) error {
	p.e = &Engine{}
	logf("service starting")
	if err := p.e.run(); err != nil { // must not block
		logf("START FAILED: %v", err)
		return err
	}
	logf("service started OK")
	return nil
}

// Stop is a deliberate service stop, which only an administrator can do.
func (p *program) Stop(s service.Service) error {
	if p.e != nil {
		p.e.shutdown(true)
	}
	return nil
}

// Shutdown is Windows shutting down or restarting.
func (p *program) Shutdown(s service.Service) error {
	if p.e != nil {
		p.e.shutdown(false)
	}
	return nil
}

// ---------------------------------------------------------------------------
// CLI
// ---------------------------------------------------------------------------

func main() {
	cfg := &service.Config{
		Executable:  installedExe(), // the protected copy, not wherever it was downloaded
		Name:        appName,
		DisplayName: "SelfGuard Content Filter",
		Description: "Local DNS content filter with a time-delay lock.",
		Arguments:   []string{"run"},
		Option:      service.KeyValue{"OnFailure": "restart"},
	}
	prg := &program{}
	s, err := service.New(prg, cfg)
	if err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}

	if len(os.Args) < 2 {
		printUsage()
		return
	}
	arg := ""
	if len(os.Args) > 2 {
		arg = strings.Join(os.Args[2:], " ")
	}

	switch os.Args[1] {
	case "run": // invoked by the service manager (or for foreground debugging)
		if err := s.Run(); err != nil {
			fmt.Println("run error:", err)
		}
	case "install":
		mustAdmin()
		if err := install(s); err != nil {
			fmt.Println("install error:", err)
			os.Exit(1)
		}
		fmt.Println("Installed and started. Check it with:  selfguard status")
	case "status":
		printStatus()
	case "block":
		sendCmd("block_domain", arg)
	case "allow":
		sendCmd("allow_domain", arg)
	case "enable":
		sendCmd("enable_category", strings.TrimSpace(arg))
	case "disable":
		sendCmd("disable_category", strings.TrimSpace(arg))
	case "set-delay":
		sendCmd("set_delay", strings.TrimSpace(arg))
	case "uninstall":
		sendCmd("uninstall", "")
	case "cancel":
		sendCmd("cancel_pending", strings.TrimSpace(arg))
	default:
		printUsage()
	}
}

func sendCmd(kind, payload string) {
	cr, err := apiSend(kind, payload)
	if err != nil {
		fmt.Println("Could not reach the SelfGuard service. Is it installed and running?  (selfguard install)")
		return
	}
	if !cr.OK {
		fmt.Println("Error:", cr.Message)
		return
	}
	fmt.Println(cr.Message)
}

func printStatus() {
	s, err := apiGetStatus()
	if err != nil {
		fmt.Println("Could not reach the SelfGuard service. Is it installed and running?")
		return
	}
	fmt.Printf("SelfGuard  (delay lock: %g hours)\n", s.DelayHours)
	fmt.Printf("Local time: %s   |  network time reachable: %v\n\n", s.Now, s.NetTimeOK)
	fmt.Println("Categories:")
	for _, c := range s.Categories {
		state := "allowed now"
		switch {
		case !c.Enabled:
			state = "disabled"
		case c.BlockedNow:
			state = "BLOCKED now"
		}
		mode := c.Mode
		if mode == "" {
			mode = "always"
		}
		fmt.Printf("  %-10s %-12s %7d domains  [%s]\n", c.Name, state, c.Domains, mode)
	}
	if len(s.CustomBlock) > 0 {
		fmt.Println("\nCustom blocked:", strings.Join(s.CustomBlock, ", "))
	}
	if len(s.Allow) > 0 {
		fmt.Println("Allow overrides:", strings.Join(s.Allow, ", "))
	}
	if len(s.Pending) > 0 {
		fmt.Println("\nPending (delay-locked) changes:")
		for _, p := range s.Pending {
			fmt.Printf("  [%s] %s %s  ->  effective %s  (%s left)\n",
				p.ID, p.Kind, p.Payload, p.Effective, p.Remaining)
		}
	} else {
		fmt.Println("\nNo pending changes.")
	}
}

func printUsage() {
	fmt.Print(`SelfGuard — self-hosted content filter with a time-delay lock

Setup (run once, from an elevated / Administrator prompt):
  selfguard install            Install + start the background service

Everyday use:
  selfguard status             Show what's blocked, schedules, pending changes
  selfguard block   <domain>   Block a site now              (immediate)
  selfguard enable  <cat>      Turn a category on             (immediate)
  selfguard disable <cat>      Turn a category off            (delay-locked)
  selfguard allow   <domain>   Un-block a site                (delay-locked)
  selfguard set-delay <hours>  Raise=immediate, lower=delay-locked
  selfguard uninstall          Remove everything              (delay-locked)
  selfguard cancel  <id>       Cancel a pending change

Categories out of the box: "porn" (always) and "social" (Mon-Fri 09:00-17:00).
Edit schedules/domains in:  %ProgramData%\SelfGuard\config.json
"Stricter" changes apply instantly; "looser" ones wait out the delay.
`)
}

// ---- install ----

// installDir is where the service binary lives once installed: C:\SelfGuard.
// It's created during an elevated install, so it inherits the C:\ root ACL
// where standard users get read & execute but not write — meaning a standard
// user still can't replace or delete the binary the SYSTEM service runs.
// (Program Files would be marginally stricter, but this fixed, predictable path
// keeps the recovery docs and scripts pointed at one stable location.)
func installDir() string {
	sd := os.Getenv("SystemDrive")
	if sd == "" {
		sd = "C:"
	}
	return sd + `\` + appName // e.g. C:\SelfGuard
}

func installedExe() string { return installDir() + `\selfguard-svc.exe` }

// install copies this exe into installDir and installs/starts the service
// from there. Re-running it on an existing install updates the binary and
// re-enables/starts the service (administrator only, like everything here).
func install(s service.Service) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	_, statErr := s.Status()
	exists := statErr != service.ErrNotInstalled
	if exists {
		_ = s.Stop()
	}
	if !samePath(self, installedExe()) {
		if err := os.MkdirAll(installDir(), 0o755); err != nil {
			return err
		}
		var copyErr error
		for i := 0; i < 20; i++ { // the old binary may take a moment to exit
			if copyErr = copyFile(self, installedExe()); copyErr == nil {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
		if copyErr != nil {
			return fmt.Errorf("copy to %s: %w", installDir(), copyErr)
		}
	}
	if exists {
		_ = serviceSetBinary(installedExe()) // older installs ran from the download folder
		_ = serviceEnable()
	} else if err := s.Install(); err != nil {
		return err
	}
	return s.Start()
}

func samePath(a, b string) bool {
	a, _ = filepath.Abs(a)
	b, _ = filepath.Abs(b)
	return strings.EqualFold(a, b)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".new"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// ---- small helpers ----

type schedulePayload struct {
	Category string   `json:"category"`
	Schedule Schedule `json:"schedule"`
}

type siteLevelPayload struct {
	Domain string `json:"domain"`
	Level  string `json:"level"`
}

type siteSchedulePayload struct {
	Domain   string   `json:"domain"`
	Schedule Schedule `json:"schedule"`
}

type addSitePayload struct {
	Domain string   `json:"domain"`
	Level  string   `json:"level"`
	Media  []string `json:"media"`
}

func mustAdmin() {
	if !isAdmin() {
		fmt.Println("This command must be run from an elevated (Administrator) command prompt.")
		os.Exit(1)
	}
}

// normDomain turns user input like "https://www.Reddit.com/r/x" or
// "*.reddit.com" into a bare domain ("www.reddit.com" / "reddit.com").
func normDomain(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndexByte(s, ':'); i >= 0 { // strip a port
		s = s[:i]
	}
	s = strings.TrimPrefix(s, "*.")
	return strings.Trim(s, ".")
}

// isBlockable rejects bare TLDs like "com" or "ca". Because blocking matches
// parent domains, one of those in a list would block every site under it.
func isBlockable(d string) bool { return strings.Contains(d, ".") }

func addBlockable(set map[string]struct{}, raw string) {
	if d := normDomain(raw); isBlockable(d) {
		set[d] = struct{}{}
	}
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

func removeItem(list []string, v string) []string {
	out := make([]string, 0, len(list))
	for _, x := range list {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

func filterPending(list []Pending, id string) []Pending {
	out := make([]Pending, 0, len(list))
	for _, p := range list {
		if p.ID != id {
			out = append(out, p)
		}
	}
	return out
}

func newID() string {
	return strconv.FormatInt(time.Now().UnixNano()%100000, 10)
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
