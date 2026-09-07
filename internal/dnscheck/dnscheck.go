// Package dnscheck answers whether the names a deployment needs actually point here.
//
// The deploy wizard used to say, in eleven-pixel grey: "Hostnames werden abgeleitet:
// matrix., mas., element., admin., mrtc." That is the entire guidance an operator got
// about the one part of the install nobody else can do for them. No records, no
// target, no check, and no way to say "I know, I set it, carry on".
//
// It also omitted one: well-known delegation is served at the server name itself, so
// the bare domain needs a record too. An operator following the footnote exactly would
// have created five of the six.
package dnscheck

import (
	"context"
	"net"
	"sort"
	"sync"
	"time"
)

// Status is deliberately four values, not a boolean.
//
// "Does not resolve" and "could not be looked up" are the same shape in the data and
// opposite in meaning: one tells the operator to create a record, the other tells them
// the check itself is broken. Collapsing them sends people to fix DNS that is fine
// (§4.78, the same mistake one layer up).
type Status string

const (
	StatusOK        Status = "ok"        // resolves to something we expect
	StatusElsewhere Status = "elsewhere" // resolves, but not here
	StatusMissing   Status = "missing"   // does not resolve at all
	StatusUnknown   Status = "unknown"   // the lookup itself failed
)

type Record struct {
	// Key identifies the record in the deploy's own hostname map, so a client can offer
	// to override exactly the names that will be created — without keeping a second
	// list of them, which is the thing that drifts.
	Key  string `json:"key,omitempty"`
	Type string `json:"type"`
	Name string `json:"name"`
	// What breaks if this one is absent. An operator staring at six near-identical
	// rows deserves to know which one costs them calling and which one costs login.
	Purpose string `json:"purpose"`
}

type Result struct {
	Record
	Want     []string `json:"want"`
	Resolved []string `json:"resolved"`
	Status   Status   `json:"status"`
	Detail   string   `json:"detail,omitempty"`
}

// Resolver is net.Resolver's LookupHost, narrowed so tests need no network.
type Resolver interface {
	LookupHost(ctx context.Context, host string) ([]string, error)
}

// Check looks every record up concurrently and compares against want.
//
// An empty want means the target address is not known — behind NAT MatrixCtrl cannot
// see its own public address. Then a name that resolves at all is reported as
// StatusElsewhere with the addresses it found, and the operator decides. Claiming
// StatusOK against nothing would be inventing a verification.
func Check(ctx context.Context, r Resolver, records []Record, want []string) []Result {
	if r == nil {
		r = net.DefaultResolver
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	wanted := make(map[string]bool, len(want))
	for _, w := range want {
		wanted[w] = true
	}

	out := make([]Result, len(records))
	var wg sync.WaitGroup
	for i, rec := range records {
		wg.Add(1)
		go func(i int, rec Record) {
			defer wg.Done()
			res := Result{Record: rec, Want: want}

			addrs, err := r.LookupHost(ctx, rec.Name)
			switch {
			case err != nil:
				var dnsErr *net.DNSError
				if ok := asDNSError(err, &dnsErr); ok && dnsErr.IsNotFound {
					res.Status = StatusMissing
					res.Detail = "kein Eintrag gefunden"
				} else {
					res.Status = StatusUnknown
					res.Detail = err.Error()
				}
			case len(addrs) == 0:
				res.Status = StatusMissing
				res.Detail = "kein Eintrag gefunden"
			default:
				sort.Strings(addrs)
				res.Resolved = addrs
				res.Status = StatusElsewhere
				for _, a := range addrs {
					if wanted[a] {
						res.Status = StatusOK
						break
					}
				}
				if res.Status == StatusElsewhere && len(want) == 0 {
					res.Detail = "Zieladresse unbekannt — bitte selbst vergleichen"
				}
			}
			out[i] = res
		}(i, rec)
	}
	wg.Wait()
	return out
}

// AllPointHere reports whether every record resolves to an expected address. Used to
// decide whether the wizard needs to insist — never to prevent proceeding.
func AllPointHere(results []Result) bool {
	for _, r := range results {
		if r.Status != StatusOK {
			return false
		}
	}
	return len(results) > 0
}

func asDNSError(err error, target **net.DNSError) bool {
	for err != nil {
		if e, ok := err.(*net.DNSError); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
