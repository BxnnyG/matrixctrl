package dnshealth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"
)

// The production error of 2026-10-10, as Go reported it.
var servfail = &net.DNSError{Err: "server misbehaving", Name: "mas.example.com", Server: "10.43.0.10:53", IsTemporary: true}
var timeout = &net.DNSError{Err: "i/o timeout", Name: "mas.example.com", Server: "10.43.0.10:53", IsTimeout: true, IsTemporary: true}
var nxdomain = &net.DNSError{Err: "no such host", Name: "typo.example.com", Server: "10.43.0.10:53", IsNotFound: true}

func checker(cluster, outside func(string) error, names ...string) *Checker {
	c := New(func(context.Context) []string { return names })
	c.cluster = func(_ context.Context, h string) ([]string, error) {
		if err := cluster(h); err != nil {
			return nil, err
		}
		return []string{"192.0.2.1"}, nil
	}
	c.outside = func(_ context.Context, h string) ([]string, error) {
		if err := outside(h); err != nil {
			return nil, err
		}
		return []string{"192.0.2.1"}, nil
	}
	return c
}

func ok(string) error { return nil }

func TestTheClassificationOfTheProductionError(t *testing.T) {
	wrapped := fmt.Errorf("token exchange: Post \"https://mas.example.com/oauth2/token\": dial tcp: lookup: %w", servfail)
	for _, tc := range []struct {
		err  error
		want Kind
	}{{wrapped, KindServfail}, {timeout, KindTimeout}, {nxdomain, KindNotFound}, {errors.New("connection refused"), KindOther}} {
		if got := classify("x", tc.err).Kind; got != tc.want {
			t.Errorf("classify(%v) = %s, want %s", tc.err, got, tc.want)
		}
	}
	if !IsResolutionError(wrapped) || IsResolutionError(nxdomain) || IsResolutionError(errors.New("x")) {
		t.Error("IsResolutionError disagrees with the classification")
	}
}

// The case of 2026-10-10: CoreDNS stuck, the internet fine.
func TestOnlyTheClustersResolutionIsStuck(t *testing.T) {
	c := checker(func(string) error { return servfail }, ok, "mas.example.com", "matrix.example.com")
	s := c.Once(context.Background())
	if s.OK || s.Outside != "answers" || len(s.Failures) != 2 || s.Failures[0].Server != "10.43.0.10:53" {
		t.Fatalf("state: %+v", s)
	}
}

func TestNoWayOutAtAll(t *testing.T) {
	c := checker(func(string) error { return timeout }, func(string) error { return timeout }, "mas.example.com")
	if s := c.Once(context.Background()); s.OK || s.Outside != "silent" {
		t.Fatalf("state: %+v", s)
	}
}

// A typo in a hostname is a missing record, shown on TLS & DNS — not an outage that
// turns the dashboard red. And the public resolver is not asked about it.
func TestAMissingRecordIsNotAnOutage(t *testing.T) {
	asked := false
	c := checker(func(h string) error {
		if h == "typo.example.com" {
			return nxdomain
		}
		return nil
	}, func(string) error { asked = true; return nil }, "typo.example.com", "matrix.example.com")
	s := c.Once(context.Background())
	if !s.OK || len(s.Failures) != 1 || s.Failures[0].Kind != KindNotFound || asked {
		t.Fatalf("state: %+v, outside asked: %v", s, asked)
	}
}

// "Since" is when the outage began, not the latest check — the operator's first
// question is how long this has been going on.
func TestSinceStaysAtTheStartOfTheOutage(t *testing.T) {
	failing := true
	c := checker(func(string) error {
		if failing {
			return servfail
		}
		return nil
	}, ok, "mas.example.com")
	t0 := time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC)
	now := t0
	c.now = func() time.Time { return now }
	c.Once(context.Background())
	now = now.Add(5 * time.Minute)
	if s := c.Once(context.Background()); !s.Since.Equal(t0) {
		t.Fatalf("since = %v, want %v", s.Since, t0)
	}
	failing = false
	now = now.Add(time.Minute)
	if s := c.Once(context.Background()); !s.OK || !s.Since.IsZero() {
		t.Fatalf("after recovery: %+v", s)
	}
}

func TestNamesAreDedupedAndAddressesSkipped(t *testing.T) {
	got := dedupe([]string{"Matrix.Example.com.", "matrix.example.com", "", "192.0.2.7", "mas.example.com"})
	if len(got) != 2 || got[0] != "mas.example.com" || got[1] != "matrix.example.com" {
		t.Fatalf("got %v", got)
	}
}
