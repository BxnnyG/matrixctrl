package dnscheck

import (
	"context"
	"errors"
	"net"
	"testing"
)

type fakeResolver map[string]struct {
	addrs []string
	err   error
}

func (f fakeResolver) LookupHost(_ context.Context, host string) ([]string, error) {
	e, ok := f[host]
	if !ok {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	return e.addrs, e.err
}

func recs(names ...string) []Record {
	out := make([]Record, len(names))
	for i, n := range names {
		out[i] = Record{Type: "A", Name: n}
	}
	return out
}

func byName(results []Result, name string) Result {
	for _, r := range results {
		if r.Name == name {
			return r
		}
	}
	return Result{}
}

func TestTheFourAnswersAreDistinguished(t *testing.T) {
	r := fakeResolver{
		"matrix.example.com":  {addrs: []string{"203.0.113.10"}},
		"mas.example.com":     {addrs: []string{"198.51.100.4"}},
		"element.example.com": {err: errors.New("server misbehaving")},
	}
	got := Check(context.Background(), r,
		recs("matrix.example.com", "mas.example.com", "element.example.com", "admin.example.com"),
		[]string{"203.0.113.10"})

	cases := map[string]Status{
		"matrix.example.com":  StatusOK,
		"mas.example.com":     StatusElsewhere,
		"element.example.com": StatusUnknown,
		"admin.example.com":   StatusMissing,
	}
	for name, want := range cases {
		if s := byName(got, name).Status; s != want {
			t.Errorf("%s: status = %q, want %q", name, s, want)
		}
	}

	// The distinction that matters most: a resolver failure must never read as a
	// missing record, or the operator goes and "fixes" DNS that is already correct.
	if byName(got, "element.example.com").Status == StatusMissing {
		t.Error("a failed lookup was reported as a missing record")
	}
	if d := byName(got, "mas.example.com").Resolved; len(d) != 1 || d[0] != "198.51.100.4" {
		t.Errorf("a record pointing elsewhere must say where it points, got %v", d)
	}
}

// Behind NAT the pod cannot see its own public address. Reporting "ok" against an
// empty expectation would be inventing a verification that never happened.
func TestUnknownTargetNeverClaimsOK(t *testing.T) {
	r := fakeResolver{"matrix.example.com": {addrs: []string{"203.0.113.10"}}}
	got := Check(context.Background(), r, recs("matrix.example.com"), nil)

	if got[0].Status == StatusOK {
		t.Error("with no expected address, nothing can be confirmed as pointing here")
	}
	if got[0].Detail == "" {
		t.Error("it must say why it cannot confirm, not just fail to confirm")
	}
	if len(got[0].Resolved) == 0 {
		t.Error("what it did resolve to is the only useful thing left to show")
	}
}

func TestAllPointHere(t *testing.T) {
	r := fakeResolver{
		"a.example.com": {addrs: []string{"203.0.113.10"}},
		"b.example.com": {addrs: []string{"203.0.113.10"}},
	}
	want := []string{"203.0.113.10"}
	if !AllPointHere(Check(context.Background(), r, recs("a.example.com", "b.example.com"), want)) {
		t.Error("two correct records should be all of them")
	}
	if AllPointHere(Check(context.Background(), r, recs("a.example.com", "c.example.com"), want)) {
		t.Error("one missing record means not all of them")
	}
	if AllPointHere(nil) {
		t.Error("nothing checked is not the same as everything correct")
	}
}
