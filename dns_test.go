package main

import (
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// fakeUpstream answers every A query with 1.2.3.4.
func fakeUpstream(t *testing.T) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	mux := dns.NewServeMux()
	mux.HandleFunc(".", func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		q := r.Question[0]
		if q.Qtype == dns.TypeA {
			rr, _ := dns.NewRR(q.Name + " 60 IN A 1.2.3.4")
			m.Answer = append(m.Answer, rr)
		}
		_ = w.WriteMsg(m)
	})
	srv := &dns.Server{PacketConn: pc, Handler: mux}
	go srv.ActivateAndServe()
	t.Cleanup(func() { srv.Shutdown() })
	return pc.LocalAddr().String()
}

// silentUpstream swallows queries and never answers, like a resolver that a
// firewall or the network is black-holing.
func silentUpstream(t *testing.T) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, _, err := pc.ReadFrom(buf); err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() { pc.Close() })
	return pc.LocalAddr().String()
}

func query(t *testing.T, name string) *dns.Msg {
	t.Helper()
	c := &dns.Client{Timeout: 8 * time.Second}
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(name), dns.TypeA)
	resp, _, err := c.Exchange(m, "127.0.0.1:53")
	if err != nil {
		t.Fatalf("query %s: %v", name, err)
	}
	return resp
}

func TestNormDomain(t *testing.T) {
	cases := map[string]string{
		"Reddit.com":                  "reddit.com",
		"https://www.reddit.com/r/x":  "www.reddit.com",
		"*.reddit.com":                "reddit.com",
		".reddit.com.":                "reddit.com",
		"http://example.com:8080/a?b": "example.com",
		" com ":                       "com",
	}
	for in, want := range cases {
		if got := normDomain(in); got != want {
			t.Errorf("normDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBareTLDNeverBlocksEverything(t *testing.T) {
	set := map[string]struct{}{}
	for _, d := range []string{"com", ".ca", "reddit.com"} {
		addBlockable(set, d)
	}
	if matchSet(set, "www.google.com") || matchSet(set, "cbc.ca") {
		t.Fatal("a bare TLD entry made it into the blocklist")
	}
	if !matchSet(set, "old.reddit.com") {
		t.Fatal("reddit.com should still block subdomains")
	}
}

func TestUpstreamList(t *testing.T) {
	got := upstreamList([]string{"1.1.1.1", "1.1.1.1:53", " "})
	if got[0] != "1.1.1.1:53" || len(got) != 3 { // + 8.8.8.8, 9.9.9.9 (no DHCP off-Windows)
		t.Fatalf("unexpected upstreams: %v", got)
	}
}

func TestEndToEnd(t *testing.T) {
	good := fakeUpstream(t)
	deadUpstream := silentUpstream(t)
	s := &DNSServer{upstream: []string{deadUpstream, good}}
	s.snap.Store(&snapshot{
		allow: map[string]struct{}{},
		cats: []catView{{name: "custom", enabled: true, sched: Schedule{Mode: "always"},
			set: map[string]struct{}{"blocked.example": {}}}},
	})
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	// First lookup pays for the dead upstream once, then falls through.
	start := time.Now()
	if r := query(t, "example.com"); r.Rcode != dns.RcodeSuccess || len(r.Answer) != 1 {
		t.Fatalf("example.com not resolved via fallback: %v", r)
	}
	t.Logf("first lookup (dead upstream first): %v", time.Since(start))

	// Later lookups go straight to the upstream that worked.
	start = time.Now()
	if r := query(t, "example.org"); r.Rcode != dns.RcodeSuccess {
		t.Fatalf("example.org failed: %v", r)
	}
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Fatalf("second lookup still slow (%v): last-good upstream not remembered", d)
	}

	if r := query(t, "www.blocked.example"); r.Rcode != dns.RcodeNameError {
		t.Fatalf("blocked domain not NXDOMAIN: %v", dns.RcodeToString[r.Rcode])
	}
	if !s.healthy() {
		t.Fatal("server with a working upstream reported unhealthy")
	}

	// Same listener, but only dead upstreams: must report unhealthy so the
	// service puts Windows back on normal DNS instead of breaking every site.
	dead := &DNSServer{upstream: []string{deadUpstream}}
	if dead.healthy() {
		t.Fatal("server with no reachable upstream reported healthy")
	}
}
