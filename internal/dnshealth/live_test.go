package dnshealth

import (
	"context"
	"net"
	"os"
	"testing"
)

// RUN_LIVE=1: once with a resolver that never answers (TEST-NET-1, RFC 5737) standing in
// for the stuck CoreDNS of 2026-10-10, once with the machine's real one.
func TestLiveAgainstADeadAndARealResolver(t *testing.T) {
	if os.Getenv("RUN_LIVE") != "1" {
		t.Skip("set RUN_LIVE=1")
	}
	names := func(context.Context) []string { return []string{"matrix.org", "github.com"} }

	dead := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, "192.0.2.1:53")
	}}
	c := New(names)
	c.cluster = dead.LookupHost
	s := c.Once(context.Background())
	t.Logf("dead resolver: ok=%v outside=%q failures=%d first=%+v", s.OK, s.Outside, len(s.Failures), s.Failures[0])
	if s.OK || s.Outside != "answers" {
		t.Errorf("a dead cluster resolver with the internet up should read as stuck resolution: %+v", s)
	}

	s = New(names).Once(context.Background())
	t.Logf("real resolver: ok=%v failures=%d", s.OK, len(s.Failures))
	if !s.OK {
		t.Errorf("the real resolver failed: %+v", s.Failures)
	}
}
