package main

import (
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// When a SafeSearch-target site (YouTube) is also set to Blocked, blocking must
// win: the query returns NXDOMAIN, NOT a rewrite to restrict.youtube.com that
// would still load. Regression test for the "blocked but still loading" bug.
func TestYouTubeBlockBeatsSafeSearch(t *testing.T) {
	good := fakeUpstream(t)
	s := &DNSServer{upstream: []string{good}}
	s.snap.Store(&snapshot{
		allow: map[string]struct{}{},
		sites: []siteRule{{
			main:   "youtube.com",
			videos: map[string]struct{}{"googlevideo.com": {}},
			level:  LevelBlocked,
			sched:  Schedule{Mode: "always"},
		}},
	})
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Stop()

	ask := func(name string) *dns.Msg {
		c := &dns.Client{Timeout: 3 * time.Second}
		m := new(dns.Msg)
		m.SetQuestion(dns.Fqdn(name), dns.TypeA)
		r, _, err := c.Exchange(m, "127.0.0.1:53")
		if err != nil {
			t.Fatalf("query %s: %v", name, err)
		}
		return r
	}

	if r := ask("www.youtube.com"); r.Rcode != dns.RcodeNameError {
		t.Fatalf("blocked YouTube should be NXDOMAIN, got %s (answers=%v)",
			dns.RcodeToString[r.Rcode], r.Answer)
	}
	if r := ask("youtube.com"); r.Rcode != dns.RcodeNameError {
		t.Fatalf("blocked youtube.com should be NXDOMAIN, got %s", dns.RcodeToString[r.Rcode])
	}

	// When NOT blocked (allowed), SafeSearch rewrite should still apply.
	s.snap.Store(&snapshot{allow: map[string]struct{}{}, sites: []siteRule{{
		main: "youtube.com", level: LevelAllowed, sched: Schedule{Mode: "always"},
	}}})
	r := ask("www.youtube.com")
	if r.Rcode != dns.RcodeSuccess {
		t.Fatalf("allowed YouTube should resolve (via SafeSearch), got %s", dns.RcodeToString[r.Rcode])
	}
	var sawCNAME bool
	for _, rr := range r.Answer {
		if cn, ok := rr.(*dns.CNAME); ok && cn.Target == "restrict.youtube.com." {
			sawCNAME = true
		}
	}
	if !sawCNAME {
		t.Error("allowed YouTube should be rewritten to restrict.youtube.com")
	}
}

var _ = net.IPv4len
