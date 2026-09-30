package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const appName = "SelfGuard"

// dataDir returns the protected config/data directory. On Windows this lives
// under C:\ProgramData\SelfGuard, which a standard (non-admin) user cannot edit.
func dataDir() string {
	base := os.Getenv("ProgramData")
	if base == "" {
		base = os.Getenv("PROGRAMDATA")
	}
	if base == "" {
		base = filepath.Join(os.TempDir(), "SelfGuard-data") // dev fallback
	}
	return filepath.Join(base, appName)
}

func configPath() string { return filepath.Join(dataDir(), "config.json") }

func logPath() string { return filepath.Join(dataDir(), "selfguard.log") }

// logf appends a timestamped line to the service log. It's best-effort: the
// service must never fail because it couldn't write a log line. This is the
// only window into why a start failed, since Windows only reports "exit code 1".
func logf(format string, args ...interface{}) {
	_ = os.MkdirAll(dataDir(), 0o755)
	f, err := os.OpenFile(logPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	line := time.Now().Format("2006-01-02 15:04:05") + "  " + fmt.Sprintf(format, args...) + "\r\n"
	_, _ = f.WriteString(line)
}

// Window is a daily time span (24h clock). End may be earlier than Start to
// express a span that wraps past midnight.
type Window struct {
	Start string `json:"start"` // "HH:MM"
	End   string `json:"end"`   // "HH:MM"
}

// Schedule says WHEN a category is blocked.
//
//	Mode "always"  -> blocked all the time.
//	Mode "windows" -> blocked only inside the listed windows (keyed by weekday).
type Schedule struct {
	Mode    string              `json:"mode"`
	Windows map[string][]Window `json:"windows,omitempty"`
}

type Category struct {
	Enabled       bool     `json:"enabled"`
	BlocklistURLs []string `json:"blocklist_urls,omitempty"`
	ExtraDomains  []string `json:"extra_domains,omitempty"`
	Schedule      Schedule `json:"schedule"`
}

// Blocking levels for a managed social Site, from loosest to strictest.
const (
	LevelAllowed  = "allowed"  // nothing blocked
	LevelNoImages = "noimages" // images/thumbnails blocked; video + text still load
	LevelNoVideo  = "novideo"  // video blocked; images + text still load
	LevelText     = "text"     // all media (images AND video) blocked; text loads
	LevelBlocked  = "blocked"  // main site and everything blocked
)

// blockedSet returns what a level blocks, as flags, for comparing two levels
// without a single linear ranking (No-images and No-video are siblings).
func blockedSet(level string) (img, vid, all bool) {
	switch level {
	case LevelBlocked:
		return true, true, true
	case LevelText:
		return true, true, false
	case LevelNoImages:
		return true, false, false
	case LevelNoVideo:
		return false, true, false
	default: // LevelAllowed
		return false, false, false
	}
}

// stricterOrEqual reports whether newLevel blocks everything oldLevel blocks
// (and maybe more). The delay lock applies a change immediately when it is
// stricter-or-equal, and queues it when it would UNBLOCK anything.
func stricterOrEqual(newLevel, oldLevel string) bool {
	ni, nv, na := blockedSet(newLevel)
	oi, ov, oa := blockedSet(oldLevel)
	if oa && !na {
		return false // was fully blocked; new isn't
	}
	if oi && !ni && !na {
		return false // stops blocking images
	}
	if ov && !nv && !na {
		return false // stops blocking video
	}
	return true
}

// strictness lets the delay-lock tell "stricter" (instant) from "looser"
// (delayed) changes. Higher = stricter.
func strictness(level string) int {
	switch level {
	case LevelBlocked:
		return 3
	case LevelText:
		return 2
	case LevelNoImages:
		return 1
	default: // LevelAllowed
		return 0
	}
}

// validLevel reports whether s is a known blocking level.
func validLevel(s string) bool {
	return s == LevelAllowed || s == LevelNoImages || s == LevelNoVideo || s == LevelText || s == LevelBlocked
}

// Site is one managed social site with its own blocking level and schedule.
// Media lists the site's image/video/CDN hosts, blocked at Text and Blocked
// levels so the page can load as text-only. A Site's Schedule may be
// Mode "inherit" (the default), meaning "use the social category's schedule".
type Site struct {
	Domain string `json:"domain"`
	Level  string `json:"level"`
	// ImageHosts carry photos/thumbnails. Blocked at No-images, Text and Blocked.
	ImageHosts []string `json:"image_hosts,omitempty"`
	// VideoHosts carry video streams. Blocked at Text and Blocked (NOT No-images,
	// so "No images" lets you still watch what you deliberately click).
	VideoHosts []string `json:"video_hosts,omitempty"`
	// BlockAlso are extra domains blocked ONLY at the Blocked level: a site's
	// app scripts, data API, short links and embeds. Blocking these at lighter
	// levels would break the page, so only full Blocked touches them.
	BlockAlso []string `json:"block_also,omitempty"`
	// Media is the legacy combined list, kept so older config.json files still
	// parse. On load it's migrated into ImageHosts (see migrateLegacyMedia).
	Media    []string `json:"media,omitempty"`
	Schedule Schedule `json:"schedule"`
}

// Pending is a queued "loosening" change that only takes effect after the delay.
type Pending struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Payload   string    `json:"payload"`
	Requested time.Time `json:"requested"` // network time, UTC
	Effective time.Time `json:"effective"` // network time, UTC
}

// currentConfigVersion drives one-time migrations in runVersionMigrations.
const currentConfigVersion = 1

type Config struct {
	Version     int                 `json:"version,omitempty"`
	DelayHours  float64             `json:"delay_hours"`
	Upstream    []string            `json:"upstream_dns"`
	Categories  map[string]Category `json:"categories"`
	Social      []Site              `json:"social_sites"` // per-site social controls
	Allow       []string            `json:"allow"`        // overrides blocks
	CustomBlock []string            `json:"custom_block"` // always-blocked
	Pending     []Pending           `json:"pending"`
}

// findSite returns the index of the managed site for domain, or -1.
func (c *Config) findSite(domain string) int {
	domain = normDomain(domain)
	for i := range c.Social {
		if c.Social[i].Domain == domain {
			return i
		}
	}
	return -1
}

// syncDefaultMedia makes the built-in sites' media lists authoritative: for any
// managed site that matches a built-in domain, it replaces the media list with
// the current curated one, while KEEPING the user's level and schedule choices.
//
// This is deliberately a replace, not a merge: earlier builds shipped media
// hosts that were wrong for Text-only (e.g. reddit's redditstatic.com or the
// broad twimg.com, which are a site's own scripts/CSS — blocking them breaks the
// page instead of just its images). A merge would leave those bad hosts in
// place. User-added custom sites (not matching a built-in domain) are untouched.
func (c *Config) syncDefaultMedia() {
	byDomain := map[string]Site{}
	for _, d := range defaultSocialSites() {
		byDomain[d.Domain] = d
	}
	for i := range c.Social {
		if d, ok := byDomain[c.Social[i].Domain]; ok {
			c.Social[i].ImageHosts = append([]string(nil), d.ImageHosts...)
			c.Social[i].VideoHosts = append([]string(nil), d.VideoHosts...)
			c.Social[i].BlockAlso = append([]string(nil), d.BlockAlso...)
			c.Social[i].Media = nil // legacy field no longer used for built-ins
		}
	}
}

// migrateLegacyMedia moves any old combined "media" list on a custom (non
// built-in) site into ImageHosts, so it's still blocked at No-images/Text/Blocked.
// Built-in sites are handled authoritatively by syncDefaultMedia.
func (c *Config) migrateLegacyMedia() {
	for i := range c.Social {
		s := &c.Social[i]
		if len(s.Media) > 0 && len(s.ImageHosts) == 0 && len(s.VideoHosts) == 0 {
			s.ImageHosts = s.Media
		}
		s.Media = nil
	}
}

// runVersionMigrations applies one-time schema changes and stamps the config
// with the current version, so each migration runs at most once even if the
// user later re-adds something a migration removed.
func (c *Config) runVersionMigrations() {
	if c.Version < 1 {
		// v1: stop managing these by default (user asked to drop them).
		c.removeSites("snapchat.com", "tumblr.com", "twitter.com")
	}
	c.Version = currentConfigVersion
}

// removeSites drops the named domains from the managed social list.
func (c *Config) removeSites(domains ...string) {
	drop := map[string]bool{}
	for _, d := range domains {
		drop[normDomain(d)] = true
	}
	kept := c.Social[:0]
	for _, s := range c.Social {
		if !drop[s.Domain] {
			kept = append(kept, s)
		}
	}
	c.Social = kept
}

// migrateWorkHoursToAlways flips the old default "work hours" social schedule to
// always-on. Earlier builds shipped social blocked only Mon–Fri 9–5, which
// surprised users ("it's set to Blocked but loads in the evening"). The UI has
// no category-schedule editor, so this corrects existing configs on load. It
// only touches schedules that still match the old shipped work-hours default;
// a schedule a user deliberately customised is left alone.
func (c *Config) migrateWorkHoursToAlways() {
	if cat, ok := c.Categories["social"]; ok && isOldWorkHours(cat.Schedule) {
		cat.Schedule = Schedule{Mode: "always"}
		c.Categories["social"] = cat
	}
	for i := range c.Social {
		if isOldWorkHours(c.Social[i].Schedule) {
			c.Social[i].Schedule = Schedule{Mode: "inherit"} // follow the (now always) category
		}
	}
}

// isOldWorkHours reports whether sched is exactly the old Mon–Fri 09:00–17:00
// default (5 weekday windows, each 09:00–17:00, no weekend entries).
func isOldWorkHours(sched Schedule) bool {
	if sched.Mode != "windows" || len(sched.Windows) != 5 {
		return false
	}
	for _, day := range []string{"monday", "tuesday", "wednesday", "thursday", "friday"} {
		w, ok := sched.Windows[day]
		if !ok || len(w) != 1 || w[0].Start != "09:00" || w[0].End != "17:00" {
			return false
		}
	}
	return true
}

// socialSchedule is the schedule to use for a site:

// socialSchedule is the schedule to use for a site: its own, unless it's set to
// inherit, in which case the social category's schedule applies.
func (c *Config) socialSchedule(site Site) Schedule {
	if site.Schedule.Mode == "" || site.Schedule.Mode == "inherit" {
		if cat, ok := c.Categories["social"]; ok {
			return cat.Schedule
		}
		return Schedule{Mode: "always"}
	}
	return site.Schedule
}

func DefaultConfig() *Config {
	return &Config{
		Version: currentConfigVersion,
		// Setup mode: no delay until you set one (raising it is instant).
		DelayHours: 0,
		Upstream:   []string{"1.1.1.1:53", "1.0.0.1:53"},
		Categories: map[string]Category{
			"porn": {
				Enabled: true,
				BlocklistURLs: []string{
					"https://blocklistproject.github.io/Lists/porn.txt",
					"https://raw.githubusercontent.com/StevenBlack/hosts/master/alternates/porn/hosts",
				},
				Schedule: Schedule{Mode: "always"},
			},
			"social": {
				Enabled: true,
				// Block around the clock by default. Set per-site "work hours"
				// in the app if you want a lighter schedule for some sites.
				Schedule: Schedule{Mode: "always"},
			},
			"bypass": {
				Enabled:      true,
				ExtraDomains: defaultBypassDomains(),
				Schedule:     Schedule{Mode: "always"},
			},
		},
		Social:      defaultSocialSites(),
		Allow:       []string{},
		CustomBlock: []string{},
		Pending:     []Pending{},
	}
}

func LoadConfig() (*Config, error) {
	b, err := os.ReadFile(configPath())
	if err != nil {
		if os.IsNotExist(err) {
			c := DefaultConfig()
			return c, c.Save()
		}
		// The file exists but can't be read (e.g. a bad ACL). Never brick the
		// service over this — run with defaults in memory so the PC keeps
		// filtering and stays online. Don't rewrite the file: the permission
		// problem may be transient and we don't want to clobber real settings.
		logf("WARNING: cannot read config (%v); using defaults for this session", err)
		return DefaultConfig(), nil
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	if c.Categories == nil {
		c.Categories = map[string]Category{}
	}
	if len(c.Upstream) == 0 {
		c.Upstream = []string{"1.1.1.1:53", "1.0.0.1:53"}
	}
	if c.Social == nil {
		c.Social = defaultSocialSites() // migrate older configs
	} else {
		c.syncDefaultMedia()   // built-in sites: authoritative image/video hosts
		c.migrateLegacyMedia() // custom sites: move old combined media -> images
	}
	c.migrateWorkHoursToAlways()
	c.runVersionMigrations()
	return &c, nil
}

// Save writes atomically so a crash mid-write can't corrupt the config.
func (c *Config) Save() error {
	if err := os.MkdirAll(dataDir(), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := configPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, configPath())
}

// ---- schedule evaluation ----

func parseHM(s string) (int, bool) {
	parts := strings.SplitN(strings.TrimSpace(s), ":", 2)
	if len(parts) != 2 {
		return 0, false
	}
	h, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	m, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

func inWindow(nowMin int, w Window) bool {
	s, ok1 := parseHM(w.Start)
	e, ok2 := parseHM(w.End)
	if !ok1 || !ok2 || s == e {
		return false
	}
	if s < e {
		return nowMin >= s && nowMin < e
	}
	return nowMin >= s || nowMin < e // wraps midnight
}

// activeAt reports whether the schedule says "blocked" at time t (ignores Enabled).
func (sc Schedule) activeAt(t time.Time) bool {
	switch sc.Mode {
	case "", "always":
		return true
	case "windows":
		day := strings.ToLower(t.Weekday().String())
		nowMin := t.Hour()*60 + t.Minute()
		for _, w := range sc.Windows[day] {
			if inWindow(nowMin, w) {
				return true
			}
		}
		return false
	default:
		return true
	}
}

func (cat Category) blockedAt(t time.Time) bool {
	return cat.Enabled && cat.Schedule.activeAt(t)
}
