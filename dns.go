package main

import (
	"fmt"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"
)

// snapshot is an immutable view the DNS handler reads without locks.
type snapshot struct {
	allow map[string]struct{}
	cats  []catView
	sites []siteRule
}

type catView struct {
	name    string
	set     map[string]struct{}
	sched   Schedule
	enabled bool
}

// siteRule is one managed social site, evaluated with its own level+schedule.
// main is the site's registrable domain; media are its CDN/image/video hosts.
type siteRule struct {
	main      string
	images    map[string]struct{} // blocked at No-images, Text, Blocked
	videos    map[string]struct{} // blocked at Text, Blocked
	full      map[string]struct{} // blocked only at Blocked (scripts, API, embeds)
	imagePats []hostPat           // wildcard image hosts, e.g. thumb*.cdn.com
	videoPats []hostPat           // wildcard video hosts, e.g. video-*.twimg.com
	level     string
	sched     Schedule
}

// hostPat matches hosts under Parent. Two forms:
//
//	"video*.twimg.com"       -> match=first-label prefix ("video")
//	"*tiktokcdn*.akamaized.net" -> contains=substring anywhere in the name
//
// It exists because some CDNs shard media across numbered hosts (X's
// video-t-1.twimg.com) or hide media on a shared CDN under a name that contains
// the brand (TikTok's p16-tiktokcdn-com.akamaized.net), with no single
// blockable parent distinct from unrelated sites on the same CDN.
type hostPat struct {
	match    string
	parent   string
	contains bool
}

// parseHostPat turns a wildcard entry into a hostPat. Plain hosts (no "*")
// return ok=false and are matched by exact/parent rules instead.
func parseHostPat(entry string) (hostPat, bool) {
	if !strings.Contains(entry, "*") {
		return hostPat{}, false
	}
	i := strings.IndexByte(entry, '.')
	if i < 0 {
		return hostPat{}, false
	}
	first, parent := entry[:i], entry[i+1:]
	if strings.HasPrefix(first, "*") && strings.HasSuffix(first, "*") {
		return hostPat{match: strings.Trim(first, "*"), parent: parent, contains: true}, true
	}
	return hostPat{match: strings.TrimSuffix(first, "*"), parent: parent}, true
}

func matchPats(pats []hostPat, name string) bool {
	for _, p := range pats {
		if name != p.parent && !strings.HasSuffix(name, "."+p.parent) {
			continue
		}
		if p.contains {
			if strings.Contains(name, p.match) {
				return true
			}
			continue
		}
		first := name
		if i := strings.IndexByte(name, '.'); i >= 0 {
			first = name[:i]
		}
		if strings.HasPrefix(first, p.match) {
			return true
		}
	}
	return false
}

// SafeSearch: rewrite these search/video hosts to their enforced-safe variants.
var safeSearch = map[string]string{
	"google.com":      "forcesafesearch.google.com",
	"www.google.com":  "forcesafesearch.google.com",
	"bing.com":        "strict.bing.com",
	"www.bing.com":    "strict.bing.com",
	"duckduckgo.com":  "safe.duckduckgo.com",
	"youtube.com":     "restrict.youtube.com",
	"www.youtube.com": "restrict.youtube.com",
	"m.youtube.com":   "restrict.youtube.com",
}

// selfTestName is answered locally (never forwarded), so the listener can be
// checked without depending on the network being up. ".invalid" is reserved.
const selfTestName = "selfguard-selftest.invalid"

// upstreamTimeout is per upstream attempt. Kept short so one dead upstream
// doesn't stall every lookup while we move on to the next.
const upstreamTimeout = 2 * time.Second

type DNSServer struct {
	snap     atomic.Value // *snapshot
	upstream []string     // fixed after construction
	lastGood atomic.Int32 // index of the upstream that answered most recently
	servers  []*dns.Server
	closers  []io.Closer
}

func (s *DNSServer) load() *snapshot {
	if v := s.snap.Load(); v != nil {
		return v.(*snapshot)
	}
	return &snapshot{}
}

// matchSet returns true if name or any parent domain is in set.
func matchSet(set map[string]struct{}, name string) bool {
	for {
		if _, ok := set[name]; ok {
			return true
		}
		i := strings.IndexByte(name, '.')
		if i < 0 {
			return false
		}
		name = name[i+1:]
	}
}

// underDomain reports whether name is parent or equal to it (main or a subdomain).
func underDomain(name, parent string) bool {
	return name == parent || strings.HasSuffix(name, "."+parent)
}

func (s *DNSServer) blocked(name string, now time.Time) bool {
	snap := s.load()
	if matchSet(snap.allow, name) {
		return false
	}
	// Always-on categories (porn, custom, bypass) first.
	for _, c := range snap.cats {
		if !c.enabled || !c.sched.activeAt(now) {
			continue
		}
		if matchSet(c.set, name) {
			return true
		}
	}
	// Per-site social rules. The first rule whose domain (or media host) covers
	// the name decides, using that site's own level and schedule.
	// Check every matching site rule and block if ANY active rule wants this
	// host blocked at its level. This "strictest wins" pass matters for CDNs
	// shared between sites (e.g. fbcdn.net is used by both Facebook and
	// Instagram): the host is blocked if either site is blocking it, rather than
	// whichever site happens to be listed first deciding for both.
	for _, r := range snap.sites {
		isMain := underDomain(name, r.main)
		isImage := matchSet(r.images, name) || matchPats(r.imagePats, name)
		isVideo := matchSet(r.videos, name) || matchPats(r.videoPats, name)
		isFull := matchSet(r.full, name)
		if !isMain && !isImage && !isVideo && !isFull {
			continue
		}
		if !r.sched.activeAt(now) {
			continue // this rule is off-schedule; another rule may still block
		}
		switch r.level {
		case LevelBlocked:
			return true // main, media, scripts/API — everything for this site
		case LevelText:
			if isImage || isVideo {
				return true // no pictures or video; page/scripts/API still load
			}
		case LevelNoImages:
			if isImage {
				return true // thumbnails/photos blocked; video + text still load
			}
		case LevelNoVideo:
			if isVideo {
				return true // video blocked; images + text still load
			}
		}
		// LevelAllowed (or a non-matching level for this host) blocks nothing,
		// but we keep scanning: another site sharing this host may block it.
	}
	return false
}

func reply(w dns.ResponseWriter, r *dns.Msg, rcode int) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Rcode = rcode
	_ = w.WriteMsg(m)
}

func (s *DNSServer) handle(w dns.ResponseWriter, r *dns.Msg) {
	defer func() {
		if recover() != nil {
			reply(w, r, dns.RcodeServerFailure) // answer rather than make the client wait
		}
	}()

	if len(r.Question) == 0 {
		reply(w, r, dns.RcodeSuccess)
		return
	}
	q := r.Question[0]
	name := strings.TrimSuffix(strings.ToLower(q.Name), ".")
	now := time.Now()

	if name == selfTestName {
		reply(w, r, dns.RcodeSuccess)
		return
	}

	// Blocking wins over SafeSearch: if the site is blocked, return NXDOMAIN
	// rather than rewriting it to a "safe" variant that would still load. (This
	// order matters for YouTube, which is both a SafeSearch target and a
	// blockable site — check the block FIRST.)
	if s.blocked(name, now) {
		reply(w, r, dns.RcodeNameError) // NXDOMAIN
		return
	}

	if q.Qtype == dns.TypeA || q.Qtype == dns.TypeAAAA {
		if target, ok := safeSearch[name]; ok {
			if m := s.safeSearchReply(r, q, target); m != nil {
				_ = w.WriteMsg(m)
			} else {
				reply(w, r, dns.RcodeServerFailure)
			}
			return
		}
	}

	// If the client came over TCP (usually a retry after a truncated UDP
	// answer), forward over TCP too, or it would just get truncated again.
	_, overTCP := w.RemoteAddr().(*net.TCPAddr)
	if resp := s.forward(r, overTCP); resp != nil {
		_ = w.WriteMsg(resp)
		return
	}
	reply(w, r, dns.RcodeServerFailure)
}

// forward sends r to the upstream resolvers, starting with whichever answered
// last time, so a dead or blocked upstream costs one timeout, not one per lookup.
func (s *DNSServer) forward(r *dns.Msg, overTCP bool) *dns.Msg {
	n := len(s.upstream)
	if n == 0 {
		return nil
	}
	c := &dns.Client{Timeout: upstreamTimeout}
	if overTCP {
		c.Net = "tcp"
	}
	start := int(s.lastGood.Load())
	for i := 0; i < n; i++ {
		idx := (start + i) % n
		in, _, err := c.Exchange(r, s.upstream[idx])
		if err == nil && in != nil {
			s.lastGood.Store(int32(idx))
			return in
		}
	}
	return nil
}

// safeSearchReply returns a CNAME to the safe host plus its resolved addresses,
// so the client transparently gets the enforced-safe endpoint. Returns nil if
// the safe host couldn't be resolved (a bare CNAME would just fail client-side).
func (s *DNSServer) safeSearchReply(r *dns.Msg, q dns.Question, target string) *dns.Msg {
	tq := new(dns.Msg)
	tq.SetQuestion(dns.Fqdn(target), q.Qtype)
	resp := s.forward(tq, false)
	if resp == nil {
		return nil
	}
	m := new(dns.Msg)
	m.SetReply(r)
	m.Answer = append(m.Answer, &dns.CNAME{
		Hdr:    dns.RR_Header{Name: q.Name, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 300},
		Target: dns.Fqdn(target),
	})
	for _, rr := range resp.Answer {
		switch rr.(type) {
		case *dns.A, *dns.AAAA, *dns.CNAME:
			m.Answer = append(m.Answer, rr)
		}
	}
	return m
}

// Start binds the listeners synchronously, so a port conflict (something else
// already on port 53) is reported as an error instead of being silently lost.
func (s *DNSServer) Start() error {
	dns.HandleFunc(".", s.handle)
	if err := s.listen("127.0.0.1:53"); err != nil {
		return err
	}
	_ = s.listen("[::1]:53") // IPv6 loopback is best-effort
	if err := s.selfTest(); err != nil {
		s.Stop()
		return err
	}
	return nil
}

func (s *DNSServer) listen(addr string) error {
	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		return fmt.Errorf("listen udp %s: %w", addr, err)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		pc.Close()
		return fmt.Errorf("listen tcp %s: %w", addr, err)
	}
	s.closers = append(s.closers, pc, ln)
	for _, sv := range []*dns.Server{{PacketConn: pc}, {Listener: ln}} {
		s.servers = append(s.servers, sv)
		go func(sv *dns.Server) { _ = sv.ActivateAndServe() }(sv)
	}
	return nil
}

// selfTest confirms that OUR listener answers on 127.0.0.1:53. It uses a name
// answered locally, so it passes even while the network is still coming up.
func (s *DNSServer) selfTest() error {
	c := &dns.Client{Timeout: 2 * time.Second}
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(selfTestName), dns.TypeA)
	var lastErr error
	for i := 0; i < 5; i++ { // listeners start asynchronously; give them a moment
		resp, _, err := c.Exchange(m, "127.0.0.1:53")
		if err == nil && resp != nil {
			if resp.Rcode != dns.RcodeSuccess {
				return fmt.Errorf("something else is answering on 127.0.0.1:53")
			}
			return nil
		}
		lastErr = err
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("local DNS listener not answering: %v", lastErr)
}

// healthy reports whether the filter can actually serve the machine right now:
// our listener answers AND at least one upstream resolves names. The upstream
// probe deliberately bypasses the blocklist.
func (s *DNSServer) healthy() bool {
	if s.selfTest() != nil {
		return false
	}
	for _, name := range []string{"www.msftconnecttest.com.", "cloudflare.com."} {
		m := new(dns.Msg)
		m.SetQuestion(name, dns.TypeA)
		resp := s.forward(m, false)
		if resp != nil && (resp.Rcode == dns.RcodeSuccess || resp.Rcode == dns.RcodeNameError) {
			return true
		}
	}
	return false
}

func (s *DNSServer) Stop() {
	for _, sv := range s.servers {
		_ = sv.Shutdown()
	}
	for _, c := range s.closers {
		_ = c.Close()
	}
}
