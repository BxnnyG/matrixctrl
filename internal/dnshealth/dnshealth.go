// Package dnshealth watches whether the cluster can resolve the names it depends on
// (etappe 119a).
//
// On 2026-10-10 it could not: the host's resolver — NetBird, forwarding everything to
// two peers that were offline — stopped answering, CoreDNS forwards to the host, and
// every service in the cluster lost every external name at once. MatrixCtrl could not
// reach MAS, MAS could not reach the login provider, federation stalled. What the operator
// saw was "lookup … on 10.43.0.10:53: server misbehaving" on a login page they could not
// get past.
//
// The check resolves the configured names through the pod's own resolver — CoreDNS, the
// path every service takes. When that fails it asks one public resolver for the same
// name, once, to tell "the cluster's resolver is stuck" from "this machine has no way
// out". A name that does not exist (NXDOMAIN) is not an outage of resolution; it is a
// missing record, and that belongs to TLS & DNS.
package dnshealth

import (
	"context"
	"errors"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

// Kind classifies one failed lookup.
type Kind string

const (
	KindTimeout  Kind = "timeout"  // nobody answered
	KindServfail Kind = "servfail" // the resolver answered that it could not resolve
	KindNotFound Kind = "notfound" // the name does not exist
	KindOther    Kind = "other"
)

// Failure is one name that did not resolve.
type Failure struct {
	Name string `json:"name"`
	Kind Kind   `json:"kind"`
	// Server is the resolver that was asked, as Go's error names it (CoreDNS's service
	// address inside the cluster).
	Server string `json:"server,omitempty"`
	Error  string `json:"error"`
}

// State is the latest answer.
type State struct {
	// OK is false only for an outage of resolution — every timeout or SERVFAIL. A
	// missing record alone leaves it true.
	OK      bool      `json:"ok"`
	Checked time.Time `json:"checked"`
	// Since is when the current outage began; zero while OK.
	Since    time.Time `json:"since,omitempty"`
	Names    []string  `json:"names"`
	Failures []Failure `json:"failures,omitempty"`
	// Outside reports what the public resolver said about a failing name: "answers"
	// (only the cluster's resolution is stuck), "silent" (no way out either) or ""
	// (not asked).
	Outside string `json:"outside,omitempty"`
}

// Lookup resolves a host. net.Resolver.LookupHost fits.
type Lookup func(ctx context.Context, host string) ([]string, error)

// PublicResolver is asked only when the cluster's resolver fails.
const PublicResolver = "1.1.1.1:53"

// Checker runs the check on a timer and keeps the latest State.
type Checker struct {
	names   func(context.Context) []string
	cluster Lookup
	outside Lookup
	now     func() time.Time

	mu    sync.Mutex
	state State
}

func New(names func(context.Context) []string) *Checker {
	pub := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, PublicResolver)
	}}
	return &Checker{names: names, cluster: net.DefaultResolver.LookupHost, outside: pub.LookupHost,
		now: time.Now, state: State{OK: true}}
}

// Run checks once a minute until ctx ends.
func (c *Checker) Run(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	c.Once(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.Once(ctx)
		}
	}
}

// State is the latest result.
func (c *Checker) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// Once runs the check now.
func (c *Checker) Once(ctx context.Context) State {
	names := dedupe(c.names(ctx))
	next := State{OK: true, Checked: c.now(), Names: names}
	outage := 0
	for _, n := range names {
		lctx, cancel := context.WithTimeout(ctx, 6*time.Second)
		_, err := c.cluster(lctx, n)
		cancel()
		if err == nil {
			continue
		}
		f := classify(n, err)
		next.Failures = append(next.Failures, f)
		if f.Kind == KindTimeout || f.Kind == KindServfail {
			outage++
		}
	}
	if outage > 0 {
		next.OK = false
		// One name is enough to tell the two causes apart, and one is all that is sent
		// to the public resolver.
		for _, f := range next.Failures {
			if f.Kind != KindTimeout && f.Kind != KindServfail {
				continue
			}
			octx, cancel := context.WithTimeout(ctx, 4*time.Second)
			_, err := c.outside(octx, f.Name)
			cancel()
			if err == nil {
				next.Outside = "answers"
			} else {
				next.Outside = "silent"
			}
			break
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if !next.OK {
		next.Since = c.state.Since
		if c.state.OK || next.Since.IsZero() {
			next.Since = next.Checked
		}
	}
	c.state = next
	return next
}

func classify(name string, err error) Failure {
	f := Failure{Name: name, Kind: KindOther, Error: err.Error()}
	var de *net.DNSError
	if !errors.As(err, &de) {
		if errors.Is(err, context.DeadlineExceeded) {
			f.Kind = KindTimeout
		}
		return f
	}
	f.Server = de.Server
	switch {
	case de.IsNotFound:
		f.Kind = KindNotFound
	case de.IsTimeout || errors.Is(err, context.DeadlineExceeded):
		f.Kind = KindTimeout
	case strings.Contains(de.Err, "server misbehaving"):
		// Go's word for SERVFAIL: the resolver answered, and the answer was that it
		// could not get one — CoreDNS saying its upstream did not respond.
		f.Kind = KindServfail
	case de.IsTemporary:
		f.Kind = KindTimeout
	}
	return f
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, n := range in {
		n = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(n)), ".")
		if n == "" || seen[n] || net.ParseIP(n) != nil {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// IsResolutionError reports whether err is a failure to resolve a name at all — the
// cluster's resolution being down — as opposed to a missing record or anything else.
func IsResolutionError(err error) bool {
	if err == nil {
		return false
	}
	f := classify("", err)
	return f.Kind == KindTimeout && hasDNSError(err) || f.Kind == KindServfail
}

func hasDNSError(err error) bool {
	var de *net.DNSError
	return errors.As(err, &de)
}
