package tlscheck

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

// serve starts a TLS listener with a certificate made to order, and returns its address.
func serve(t *testing.T, names []string, notAfter time.Time, selfSigned bool) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: names[0]},
		DNSNames:     names,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
	}
	parent, parentKey := tmpl, key
	if !selfSigned {
		caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		ca := &x509.Certificate{
			SerialNumber: big.NewInt(2),
			Subject:      pkix.Name{CommonName: "Test CA", Organization: []string{"Beispiel-CA"}},
			NotBefore:    time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
			IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
		}
		parent, parentKey = ca, caKey
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, parentKey)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { _ = c.(*tls.Conn).Handshake(); c.Close() }()
		}
	}()
	return ln.Addr().String()
}

// The Traefik default certificate of 2026-09-28, in miniature.
func TestASelfSignedOriginIsNamedAsSuch(t *testing.T) {
	addr := serve(t, []string{"TRAEFIK DEFAULT CERT"}, time.Now().Add(365*24*time.Hour), true)
	c := Probe(context.Background(), "rtc.example.org", addr)
	if !c.Reachable || !c.SelfSigned {
		t.Fatalf("got %+v", c)
	}
	if c.Level() != "err" || !strings.Contains(c.Summary(), "Selbstsigniert") {
		t.Errorf("summary %q level %q", c.Summary(), c.Level())
	}
}

// Counter-probe: a certificate from a CA for the right name is fine, and says so.
func TestAProperCertificateIsAccepted(t *testing.T) {
	addr := serve(t, []string{"rtc.example.org"}, time.Now().Add(60*24*time.Hour), false)
	c := Probe(context.Background(), "rtc.example.org", addr)
	if c.SelfSigned || !c.NameMatches || c.Level() != "ok" {
		t.Fatalf("got %+v — %s", c, c.Summary())
	}
	if !strings.Contains(c.Summary(), "Beispiel-CA") || !strings.Contains(c.Summary(), "59 Tage") {
		t.Errorf("summary %q", c.Summary())
	}
}

func TestAWrongNameIsItsOwnFault(t *testing.T) {
	addr := serve(t, []string{"andere.example.org"}, time.Now().Add(60*24*time.Hour), false)
	c := Probe(context.Background(), "rtc.example.org", addr)
	if c.NameMatches || !strings.Contains(c.Summary(), "gilt nicht für diesen Namen") {
		t.Errorf("got %+v — %s", c, c.Summary())
	}
}

func TestAnExpiringCertificateWarnsBeforeItExpires(t *testing.T) {
	addr := serve(t, []string{"a.example.org"}, time.Now().Add(5*24*time.Hour), false)
	if c := Probe(context.Background(), "a.example.org", addr); c.Level() != "warn" {
		t.Errorf("level %q — %s", c.Level(), c.Summary())
	}
	addr = serve(t, []string{"b.example.org"}, time.Now().Add(-time.Hour), false)
	if c := Probe(context.Background(), "b.example.org", addr); c.Level() != "err" || !strings.Contains(c.Summary(), "Abgelaufen") {
		t.Errorf("expired: %s", c.Summary())
	}
}

// Nothing listening: reported as unreachable, not as a bad certificate.
func TestAClosedPortIsNotACertificateProblem(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	c := Probe(context.Background(), "a.example.org", addr)
	if c.Reachable || c.SelfSigned || !strings.Contains(c.Error, "Port 443") && !strings.Contains(c.Error, "nimmt keine Verbindung") {
		t.Errorf("got %+v", c)
	}
}
