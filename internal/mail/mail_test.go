package mail

import (
	"bufio"
	"context"
	"net"
	"strings"
	"sync"
	"testing"
)

// fakeSMTP is enough of a mail server to answer the conversation Probe and Send have.
type fakeSMTP struct {
	addr     string
	starttls bool // announce STARTTLS
	authOK   bool
	mu       sync.Mutex
	received []string
}

func newFakeSMTP(t *testing.T, starttls, authOK bool) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSMTP{addr: ln.Addr().String(), starttls: starttls, authOK: authOK}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(conn)
		}
	}()
	return f
}

func (f *fakeSMTP) serve(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	w := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
	w("220 fake ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			w("250-fake")
			if f.starttls {
				w("250-STARTTLS")
			}
			w("250 AUTH PLAIN")
		case strings.HasPrefix(cmd, "AUTH"):
			if f.authOK {
				w("235 2.7.0 Authentication successful")
			} else {
				w("535 5.7.8 Benutzername oder Passwort falsch")
			}
		case strings.HasPrefix(cmd, "MAIL FROM"), strings.HasPrefix(cmd, "RCPT TO"):
			w("250 OK")
		case strings.HasPrefix(cmd, "DATA"):
			w("354 End data with <CR><LF>.<CR><LF>")
			var body strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil || strings.TrimSpace(l) == "." {
					break
				}
				body.WriteString(l)
			}
			f.mu.Lock()
			f.received = append(f.received, body.String())
			f.mu.Unlock()
			w("250 OK queued")
		case strings.HasPrefix(cmd, "QUIT"):
			w("221 Bye")
			return
		default:
			w("250 OK")
		}
	}
}

func (f *fakeSMTP) settings() Settings {
	host, port, _ := net.SplitHostPort(f.addr)
	p := 0
	for _, c := range port {
		p = p*10 + int(c-'0')
	}
	return Settings{Enabled: true, From: `"Mein Server" <noreply@example.org>`, Host: host, Port: p,
		Encryption: None, Username: "user", Password: "geheim"}
}

func TestProbeSucceedsAndSendsNothing(t *testing.T) {
	f := newFakeSMTP(t, false, true)
	if err := Probe(context.Background(), f.settings()); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if len(f.received) != 0 {
		t.Error("a probe must not send a message")
	}
}

// Wrong password: the sentence says so, and does not read like a bug.
func TestARejectedLoginIsExplained(t *testing.T) {
	f := newFakeSMTP(t, false, false)
	err := Probe(context.Background(), f.settings())
	if err == nil || !strings.Contains(err.Error(), "Anmeldung als user abgelehnt") {
		t.Fatalf("got %v", err)
	}
}

// STARTTLS asked for but not offered: refused, not silently sent in the clear.
func TestStartTLSIsNotSilentlySkipped(t *testing.T) {
	f := newFakeSMTP(t, false, true)
	s := f.settings()
	s.Encryption = StartTLS
	err := Probe(context.Background(), s)
	if err == nil || !strings.Contains(err.Error(), "kein STARTTLS") {
		t.Fatalf("got %v", err)
	}
}

func TestSendDeliversTheMessage(t *testing.T) {
	f := newFakeSMTP(t, false, true)
	if err := Send(context.Background(), f.settings(), "admin@example.org"); err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(f.received) != 1 {
		t.Fatalf("messages: %d", len(f.received))
	}
	body := f.received[0]
	for _, want := range []string{"To: admin@example.org", "Subject: Testnachricht von MatrixCtrl", "charset=utf-8"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in\n%s", want, body)
		}
	}
}

func TestRenderAndParseRoundTrip(t *testing.T) {
	in := Settings{Enabled: true, From: "noreply@example.org", ReplyTo: "hilfe@example.org",
		Host: "mail.example.org", Port: 587, Encryption: StartTLS, Username: "u", Password: "p"}
	doc, err := Render(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Parse(doc)
	if err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Errorf("round trip:\n got %+v\nwant %+v\n%s", out, in, doc)
	}
}

// Switched off keeps the values, so switching back on is not retyping everything.
func TestDisabledIsBlackholeWithTheValuesKept(t *testing.T) {
	s := Settings{From: "noreply@example.org", Host: "mail.example.org", Port: 587, Encryption: StartTLS, Username: "u", Password: "p"}
	doc, _ := Render(s)
	if !strings.Contains(doc, "transport: blackhole") {
		t.Errorf("disabled must be blackhole: %s", doc)
	}
	if strings.Contains(doc, "password") || strings.Contains(doc, "hostname") {
		t.Errorf("a disabled transport carries no credentials into the config: %s", doc)
	}
	if !strings.Contains(doc, "from: noreply@example.org") {
		t.Errorf("the sender is kept: %s", doc)
	}
}

func TestValidate(t *testing.T) {
	base := Settings{Enabled: true, From: "noreply@example.org", Host: "h", Port: 587, Encryption: StartTLS}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := base
	bad.From = "kein-absender"
	if err := bad.Validate(); err == nil {
		t.Error("an invalid sender must be refused")
	}
	bad = base
	bad.Username = "u"
	if err := bad.Validate(); err == nil {
		t.Error("a username without a password must be refused")
	}
	off := bad
	off.Enabled = false
	if err := off.Validate(); err != nil {
		t.Errorf("switched off, half-filled values are fine: %v", err)
	}
}

// A password over an unencrypted connection to a server outside the cluster.
func TestPlainAuthOutsideTheClusterIsRefused(t *testing.T) {
	s := Settings{Enabled: true, From: "a@example.org", Host: "mail.example.org", Port: 25,
		Encryption: None, Username: "u", Password: "p"}
	if isLocal(s.Host) {
		t.Fatal("fixture host must not count as local")
	}
	if !isLocal("ess-mail.ess.svc.cluster.local") || !isLocal("localhost") {
		t.Error("a cluster-internal server must count as local")
	}
}
