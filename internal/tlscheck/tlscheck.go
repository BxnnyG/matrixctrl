// Package tlscheck reads the certificate a host serves — from the internet, and at the
// origin (etappe 115).
//
// Two blicks, because they differ where it hurts. On 2026-09-28 Element Call failed with
// OPEN_ID_ERROR: the name pointed straight at the server, Traefik answered with its
// self-signed default certificate, and every client refused it. Behind Cloudflare the
// same origin certificate is invisible from outside — the public view is valid, the
// in-cluster caller still fails. Only asking both says which.
//
// Nothing here trusts what it reads: the handshake is made with verification disabled on
// purpose, so an invalid certificate can be *reported* instead of ending the connection.
// No request is sent, nothing is stored.
package tlscheck

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"math"
	"net"
	"strings"
	"time"
)

const timeout = 8 * time.Second

// Cert is what a host serves, in the words a person needs.
type Cert struct {
	// Reachable is false when no TLS connection could be made at all.
	Reachable bool   `json:"reachable"`
	Error     string `json:"error,omitempty"`

	Issuer   string    `json:"issuer,omitempty"`
	Subject  string    `json:"subject,omitempty"`
	Names    []string  `json:"names,omitempty"`
	NotAfter time.Time `json:"not_after,omitempty"`
	// SelfSigned means the certificate signed itself — Traefik's default, and never
	// something a browser or another server accepts.
	SelfSigned bool `json:"self_signed,omitempty"`
	// NameMatches is false when the certificate is for other names than the one asked
	// for; that is a different fault from an expired or self-signed one.
	NameMatches bool `json:"name_matches"`
	// DaysLeft is how long it is still valid, rounded down. Expired is its own field:
	// a certificate that ran out an hour ago has zero days left, not negative ones, so
	// deriving "expired" from the number said "läuft in 0 Tagen ab" about something
	// already invalid (found by the test).
	DaysLeft int  `json:"days_left"`
	Expired  bool `json:"expired,omitempty"`
}

// Probe performs the handshake for host. addr overrides where to connect — the origin's
// address for the in-cluster view; empty means host:443, the public one.
func Probe(ctx context.Context, host, addr string) Cert {
	if addr == "" {
		addr = net.JoinHostPort(host, "443")
	} else if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(addr, "443")
	}
	d := &net.Dialer{Timeout: timeout}
	conn, err := tls.DialWithDialer(d, "tcp", addr, &tls.Config{
		ServerName: host,
		// Deliberate: the point is to report what is served, including certificates a
		// client would reject. Nothing is sent over this connection.
		InsecureSkipVerify: true,
	})
	if err != nil {
		return Cert{Error: reachableError(err, addr)}
	}
	defer conn.Close()

	chain := conn.ConnectionState().PeerCertificates
	if len(chain) == 0 {
		return Cert{Reachable: true, Error: "Die Gegenstelle hat kein Zertifikat gesendet."}
	}
	return describe(chain[0], host, time.Now())
}

func describe(c *x509.Certificate, host string, now time.Time) Cert {
	out := Cert{
		Reachable: true,
		Issuer:    name(c.Issuer.Organization, c.Issuer.CommonName),
		Subject:   name(c.Subject.Organization, c.Subject.CommonName),
		Names:     c.DNSNames,
		NotAfter:  c.NotAfter,
		DaysLeft:  int(math.Floor(c.NotAfter.Sub(now).Hours() / 24)),
		Expired:   now.After(c.NotAfter),
	}
	out.SelfSigned = c.Issuer.String() == c.Subject.String()
	out.NameMatches = c.VerifyHostname(host) == nil
	return out
}

func name(org []string, cn string) string {
	switch {
	case cn != "" && len(org) > 0 && org[0] != cn:
		return fmt.Sprintf("%s (%s)", cn, org[0])
	case cn != "":
		return cn
	case len(org) > 0:
		return org[0]
	}
	return "unbenannt"
}

// reachableError turns a dial failure into something actionable.
func reachableError(err error, addr string) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "no such host"):
		return "Der Name lässt sich nicht auflösen."
	case strings.Contains(s, "connection refused"):
		return fmt.Sprintf("%s nimmt keine Verbindung auf Port 443 an.", addr)
	case strings.Contains(s, "i/o timeout"), strings.Contains(s, "deadline exceeded"):
		return fmt.Sprintf("%s antwortet nicht (Zeitüberschreitung) — oft eine Firewall.", addr)
	}
	return s
}

// Summary is one sentence about a certificate, for a reader who should not have to
// assemble four fields into a verdict themselves.
func (c Cert) Summary() string {
	switch {
	case !c.Reachable:
		return c.Error
	case c.Error != "":
		return c.Error
	case c.SelfSigned:
		return fmt.Sprintf("Selbstsigniert (%s) — Browser und andere Server lehnen das ab.", c.Issuer)
	case !c.NameMatches:
		return fmt.Sprintf("Das Zertifikat gilt nicht für diesen Namen, sondern für %s.", strings.Join(c.Names, ", "))
	case c.Expired:
		return fmt.Sprintf("Abgelaufen am %s.", c.NotAfter.Format("02.01.2006 15:04"))
	case c.DaysLeft < 14:
		return fmt.Sprintf("Gültig von %s, läuft in %d Tagen ab (%s).", c.Issuer, c.DaysLeft, c.NotAfter.Format("02.01.2006"))
	default:
		return fmt.Sprintf("Gültig von %s, noch %d Tage.", c.Issuer, c.DaysLeft)
	}
}

// Level is how loud the summary should be shown.
func (c Cert) Level() string {
	switch {
	case !c.Reachable, c.Error != "", c.SelfSigned, !c.NameMatches, c.Expired:
		return "err"
	case c.DaysLeft < 14:
		return "warn"
	default:
		return "ok"
	}
}
