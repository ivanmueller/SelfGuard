package main

import (
	"bufio"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// defaultSocialSites is the built-in set of managed social sites, each with the
// media/CDN hosts that get blocked at Text-only and Blocked levels. Editable in
// config.json under "social_sites". Levels start at Blocked during work hours
// (the social category's default schedule); change per site in the app.
func defaultSocialSites() []Site {
	inherit := Schedule{Mode: "inherit"}
	// Media lists target ONLY image/video CDN hosts, never a site's own JS/CSS
	// hosts — so "Text only" loads the page's text and layout while its pictures
	// and videos are blocked. (Blocking a site's script host instead would just
	// break the whole page, so text wouldn't load either.) matchSet does parent-
	// domain matching, so e.g. "redd.it" covers i.redd.it, v.redd.it,
	// preview.redd.it and external-preview.redd.it.
	return []Site{
		{Domain: "reddit.com", Level: LevelBlocked, Schedule: inherit,
			ImageHosts: []string{"i.redd.it", "preview.redd.it", "external-preview.redd.it"},
			// v.redd.it = DASH streams, packaged-media.redd.it = packaged MP4s the
			// new player uses (was missing, so video still played at Text level).
			VideoHosts: []string{"v.redd.it", "packaged-media.redd.it", "redditmedia.com"}},
		{Domain: "instagram.com", Level: LevelBlocked, Schedule: inherit,
			// IG media lives on its own CDN hosts (scontent*/instagram* prefixes),
			// NOT static.cdninstagram.com (scripts) and NOT Facebook's
			// scontent*.fbcdn.net. IG serves photos and video from the same
			// scontent hosts, so No-images ≈ Text on Instagram.
			ImageHosts: []string{
				"scontent*.cdninstagram.com", "instagram*.fbcdn.net",
				"video*.cdninstagram.com",
			}},
		{Domain: "x.com", Level: LevelBlocked, Schedule: inherit,
			ImageHosts: []string{"pbs.twimg.com"},
			// "video*.twimg.com" covers video.twimg.com AND the video-t-1,
			// video-t-2 … Fastly delivery shards X streams from.
			VideoHosts: []string{"video*.twimg.com", "ton.twitter.com"}},
		{Domain: "facebook.com", Level: LevelBlocked, Schedule: inherit,
			// Photos on scontent*/external*, video on video*. NEVER static*.fbcdn.net
			// — that's Facebook's own app scripts, and blocking it hangs the page
			// at the loading logo. Prefix matching also keeps FB's scontent
			// separate from Instagram's hosts.
			ImageHosts: []string{"scontent*.fbcdn.net", "external*.fbcdn.net"},
			VideoHosts: []string{"video*.fbcdn.net"}},
		{Domain: "tiktok.com", Level: LevelBlocked, Schedule: inherit,
			// TikTok is video-first and rotates media across MANY ByteDance CDNs,
			// several on shared akamaized.net. Text-only is best-effort here and
			// Blocked (the whole site) is the reliable choice. Image hosts use
			// the "p" picture prefix; video uses the many CDN domains + "*cdn*"
			// contains-patterns for the akamaized shards.
			ImageHosts: []string{
				"ibyteimg.com", "byteimg.com",
				"p*.tiktokcdn.com", "p*.tiktokcdn-us.com",
				"*p16-tiktok*.akamaized.net",
			},
			VideoHosts: []string{
				"tiktokcdn.com", "tiktokcdn-us.com", "tiktokcdn-eu.com", "tiktokcdn-in.com",
				"tiktokv.com", "byteoversea.com", "byteoversea.net",
				"bytefcdn-oversea.com", "bytefcdn-ttpeu.com", "ibytedtos.com",
				"muscdn.com", "tlivecdn.com", "ttlivecdn.com",
				"*tiktokcdn*.akamaized.net", "*hypstarcdn*.akamaized.net",
				"*byteicdn*.akamaized.net", "*muscdn*.akamaized.net",
			}},
		{Domain: "youtube.com", Level: LevelAllowed, Schedule: inherit,
			// Images = thumbnails + avatars (NOT s.ytimg.com scripts).
			// Video   = the streams. So "No images" hides thumbnails but still
			// plays a video you click; "Text only" also stops playback.
			ImageHosts: []string{
				"i.ytimg.com", "i1.ytimg.com", "i2.ytimg.com", "i3.ytimg.com",
				"i4.ytimg.com", "i5.ytimg.com", "i6.ytimg.com", "i7.ytimg.com",
				"i8.ytimg.com", "i9.ytimg.com", "img.youtube.com",
				"yt3.ggpht.com", "yt4.ggpht.com",
			},
			VideoHosts: []string{"googlevideo.com"},
			BlockAlso: []string{
				"ytimg.com", "youtu.be", "youtube-nocookie.com",
				"youtubei.googleapis.com", "youtube.googleapis.com",
			}},
		{Domain: "pinterest.com", Level: LevelBlocked, Schedule: inherit,
			// Pinterest serves images (incl. sponsored pins) from several CDN hosts:
			// i.pinimg.com, sm.pinimg.com, u.pinimg.com, media-cache-*.pinimg.com.
			// Cover them all so nothing shows. s.pinimg.com (scripts) and
			// v*.pinimg.com (video) use different prefixes, so the page still loads
			// and video stays in its own category.
			ImageHosts: []string{
				"i.pinimg.com", "sm.pinimg.com", "u.pinimg.com",
				"media-cache*.pinimg.com", "i-*.pinimg.com",
			},
			VideoHosts: []string{"v.pinimg.com", "v1.pinimg.com"},
			BlockAlso:  []string{"pinimg.com"}}, // full Blocked also kills scripts
	}
}

func cacheDir() string          { return filepath.Join(dataDir(), "lists") }
func cacheFile(n string) string { return filepath.Join(cacheDir(), n+".txt") }

// fetchList downloads a hosts-format or plain-domain list.
func fetchList(url string) ([]string, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var out []string
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 1024*1024), 8*1024*1024)
	for sc.Scan() {
		if d := parseListLine(sc.Text()); d != "" {
			out = append(out, d)
		}
	}
	return out, sc.Err()
}

func parseListLine(line string) string {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
		return ""
	}
	if i := strings.IndexByte(line, '#'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	fields := strings.Fields(line)
	var host string
	if len(fields) == 1 {
		host = fields[0] // plain domain list
	} else {
		host = fields[1] // hosts format: "0.0.0.0 example.com"
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")

	switch host {
	case "", "localhost", "localhost.localdomain", "local", "broadcasthost",
		"0.0.0.0", "127.0.0.1", "::1", "ip6-localhost", "ip6-loopback":
		return ""
	}
	if strings.ContainsAny(host, "/ \t") || !strings.Contains(host, ".") {
		return ""
	}
	return host
}

func saveCache(name string, domains []string) error {
	if err := os.MkdirAll(cacheDir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(cacheFile(name), []byte(strings.Join(domains, "\n")), 0o644)
}

func loadCache(name string) []string {
	b, err := os.ReadFile(cacheFile(name))
	if err != nil {
		return nil
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// defaultBypassDomains are encrypted-DNS (DoH) endpoints, which apps use to
// resolve names without going through this filter, plus Firefox's canary
// domain: answering NXDOMAIN for it tells Firefox to keep its DoH off.
func defaultBypassDomains() []string {
	return []string{
		"use-application-dns.net",
		"dns.google", "dns.google.com",
		"cloudflare-dns.com", "one.one.one.one",
		"dns.quad9.net", "dns9.quad9.net", "dns10.quad9.net", "dns11.quad9.net",
		"doh.opendns.com", "doh.familyshield.opendns.com",
		"dns.nextdns.io",
		"doh.cleanbrowsing.org",
		"dns.adguard.com", "dns.adguard-dns.com", "family.adguard-dns.com",
		"dns.mullvad.net", "doh.mullvad.net",
		"dns.controld.com", "freedns.controld.com",
		"doh.dns.sb", "dns0.eu", "odvr.nic.cz", "doh.pub", "dns.alidns.com",
	}
}
