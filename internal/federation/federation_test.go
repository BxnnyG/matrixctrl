package federation

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
)

// A homeserver behind a real TLS listener. httptest's certificate is valid for
// example.com, so that is the server name that can pass; any other name reaching the
// same listener meets a certificate that does not fit — as it would in the wild.
type fixture struct {
	wellKnown  string // body of /.well-known/matrix/server; "" → 404
	keyName    string // server_name in the key response
	clientCORS bool
}

func (f fixture) checker(t *testing.T, refuse ...string) *Checker {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/matrix/server", func(w http.ResponseWriter, r *http.Request) {
		if f.wellKnown == "" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, f.wellKnown)
	})
	mux.HandleFunc("/.well-known/matrix/client", func(w http.ResponseWriter, r *http.Request) {
		if f.clientCORS {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
		fmt.Fprint(w, `{"m.homeserver":{"base_url":"https://matrix.example.com"}}`)
	})
	mux.HandleFunc("/_matrix/key/v2/server", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"server_name":%q,"verify_keys":{"ed25519:a":{"key":"k"}}}`, f.keyName)
	})
	mux.HandleFunc("/_matrix/federation/v1/version", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"server":{"name":"Synapse","version":"1.200.0"}}`)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	d := &net.Dialer{}
	return &Checker{
		LookupSRV: func(context.Context, string, string, string) (string, []*net.SRV, error) {
			return "", nil, errors.New("no SRV")
		},
		Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			for _, r := range refuse {
				if addr == r {
					return nil, errors.New("connection refused")
				}
			}
			return d.DialContext(ctx, network, srv.Listener.Addr().String())
		},
		RootCAs: pool,
	}
}

func step(r Report, key string) Step {
	for _, s := range r.Steps {
		if s.Key == key {
			return s
		}
	}
	return Step{}
}

func TestAHealthyDelegationIsReachable(t *testing.T) {
	c := fixture{wellKnown: `{"m.server":"example.com:443"}`, keyName: "example.com", clientCORS: true}.checker(t)
	r := c.Check(context.Background(), "example.com")
	if !r.Reachable || r.Target != "example.com:443" || r.TargetFrom != "well-known" {
		t.Fatalf("report: %+v", r)
	}
	if r.Software != "Synapse 1.200.0" {
		t.Errorf("software = %q", r.Software)
	}
	for _, s := range r.Steps {
		if s.Level != "ok" {
			t.Errorf("step %s = %s: %s", s.Key, s.Level, s.Detail)
		}
	}
}

// The common failure behind Cloudflare: no delegation, so remote servers try 8448,
// which the proxy does not pass through. The report must say that, not just "failed".
func TestNoDelegationAndNothingOn8448(t *testing.T) {
	c := fixture{keyName: "example.com"}.checker(t, "example.com:8448")
	r := c.Check(context.Background(), "example.com")
	if r.Reachable {
		t.Fatalf("reachable without delegation and with 8448 closed: %+v", r)
	}
	if r.TargetFrom != "default" || r.Target != "example.com:8448" {
		t.Errorf("target %q from %q, want example.com:8448 from default", r.Target, r.TargetFrom)
	}
	if d := step(r, "delegation"); d.Level != "warn" {
		t.Errorf("delegation step: %+v", d)
	}
	if k := step(r, "key"); k.Level != "err" || !strings.Contains(k.Detail, "Cloudflare") {
		t.Errorf("key step should name the likely cause: %+v", k)
	}
}

// A web site's catch-all answers every path with its start page — including the
// delegation file. 200, but not a delegation.
func TestAStartPageIsNotADelegation(t *testing.T) {
	c := fixture{wellKnown: "<!DOCTYPE html><html><body>Hallo</body></html>", keyName: "example.com"}.checker(t, "example.com:8448")
	r := c.Check(context.Background(), "example.com")
	d := step(r, "delegation")
	if d.Level != "err" || !strings.Contains(d.Detail, "HTML-Seite") {
		t.Fatalf("delegation step: %+v", d)
	}
	if r.Reachable {
		t.Errorf("reachable through a start page: %+v", r)
	}
}

// A delegation that points at somebody else's homeserver connects fine and would pass
// any check that stops at TLS. The key names the server that actually answers.
func TestADelegationToAnotherHomeserverIsCaught(t *testing.T) {
	c := fixture{wellKnown: `{"m.server":"example.com:443"}`, keyName: "someone-else.example"}.checker(t)
	r := c.Check(context.Background(), "example.com")
	k := step(r, "key")
	if r.Reachable || k.Level != "err" || !strings.Contains(k.Detail, "someone-else.example") {
		t.Fatalf("key step: %+v", k)
	}
}

// The certificate must fit the delegated name. Here the delegation points at a name
// the listener's certificate does not cover.
func TestACertificateForTheWrongNameIsCaught(t *testing.T) {
	c := fixture{wellKnown: `{"m.server":"matrix.other.test:443"}`, keyName: "example.com"}.checker(t)
	r := c.Check(context.Background(), "example.com")
	k := step(r, "key")
	if r.Reachable || k.Level != "err" || !strings.Contains(k.Detail, "gilt nicht für matrix.other.test") {
		t.Fatalf("key step: %+v", k)
	}
}

func TestAClientDelegationWithoutCORSIsAWarning(t *testing.T) {
	c := fixture{wellKnown: `{"m.server":"example.com:443"}`, keyName: "example.com"}.checker(t)
	r := c.Check(context.Background(), "example.com")
	if s := step(r, "client"); s.Level != "warn" || !strings.Contains(s.Detail, "Access-Control-Allow-Origin") {
		t.Fatalf("client step: %+v", s)
	}
}

// SRV: the connection goes to the SRV target, the certificate is checked against the
// delegated name.
func TestSRVDialsTheTargetButChecksTheDelegatedName(t *testing.T) {
	c := fixture{wellKnown: `{"m.server":"example.com"}`, keyName: "example.com"}.checker(t)
	var dialled []string
	inner := c.Dial
	c.Dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialled = append(dialled, addr)
		return inner(ctx, network, addr)
	}
	c.LookupSRV = func(_ context.Context, service, _, name string) (string, []*net.SRV, error) {
		if service == "matrix-fed" && name == "example.com" {
			return "", []*net.SRV{{Target: "fed-backend.internal.", Port: 8443}}, nil
		}
		return "", nil, errors.New("none")
	}
	r := c.Check(context.Background(), "example.com")
	if !r.Reachable || r.TargetFrom != "srv" {
		t.Fatalf("report: %+v", r)
	}
	found := false
	for _, a := range dialled {
		if a == "fed-backend.internal:8443" {
			found = true
		}
	}
	if !found {
		t.Errorf("never dialled the SRV target; dialled %v", dialled)
	}
}

func TestNetworkFailuresAreSaidInWords(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{&net.DNSError{Err: "no such host", Name: "x.example"}, "nicht auflösen"},
		{fmt.Errorf("dial: %w", syscall.ECONNREFUSED), "abgelehnt"},
		{fmt.Errorf("get: %w", context.DeadlineExceeded), "keine Antwort"},
	} {
		if got := short(tc.err); !strings.Contains(got, tc.want) {
			t.Errorf("short(%v) = %q, want it to say %q", tc.err, got, tc.want)
		}
	}
}
