// Package federation answers "can other homeservers reach mine?" by walking the path a
// remote server walks: the delegation file, the target it names, a TLS connection whose
// certificate must fit the delegated name, and the signing key the server hands out.
//
// Each step reports what it found and why that matters, because the symptom of a
// broken step is the same for all of them — invitations from elsewhere never arrive —
// and only the step says what to fix (etappe 117).
//
// It checks from *this* machine over the public network, not from outside. That is
// said wherever the result is shown; the outside view is one click to matrix.org's
// federation tester, made by the operator, never by this package.
package federation

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Step is one stage of the path, with its outcome.
type Step struct {
	Key    string `json:"key"`
	Title  string `json:"title"`
	Level  string `json:"level"` // ok | warn | err | skip
	Value  string `json:"value,omitempty"`
	Detail string `json:"detail"`
}

// Report is the whole walk.
type Report struct {
	ServerName string `json:"server_name"`
	// Target is where other servers connect, and TargetFrom how they found it:
	// "well-known", "srv" or "default" (port 8448, the fallback of last resort).
	Target     string    `json:"target,omitempty"`
	TargetFrom string    `json:"target_from,omitempty"`
	Reachable  bool      `json:"reachable"`
	Software   string    `json:"software,omitempty"`
	Steps      []Step    `json:"steps"`
	CheckedAt  time.Time `json:"checked_at"`
}

// Checker holds the network access, injectable so tests make real TLS connections to a
// test server instead of trusting a stub.
type Checker struct {
	LookupSRV func(ctx context.Context, service, proto, name string) (string, []*net.SRV, error)
	Dial      func(ctx context.Context, network, addr string) (net.Conn, error)
	RootCAs   *x509.CertPool // nil: the system's
	Timeout   time.Duration
	now       func() time.Time
}

func New() *Checker {
	d := &net.Dialer{Timeout: 8 * time.Second}
	return &Checker{
		LookupSRV: net.DefaultResolver.LookupSRV,
		Dial:      d.DialContext,
		Timeout:   8 * time.Second,
		now:       time.Now,
	}
}

const (
	wellKnownLimit = 64 << 10
	keyLimit       = 1 << 20
	defaultPort    = "8448"
)

// Check walks the path for serverName.
func (c *Checker) Check(ctx context.Context, serverName string) Report {
	now := time.Now
	if c.now != nil {
		now = c.now
	}
	r := Report{ServerName: serverName, CheckedAt: now().UTC()}
	host, port := splitHostPort(serverName)

	// 1. The delegation file. Skipped by the spec when the name already carries a port
	// or is an address — then the name *is* the target.
	var delegated string
	switch {
	case port != "" || net.ParseIP(host) != nil:
		r.Steps = append(r.Steps, Step{Key: "delegation", Title: "Wegweiser", Level: "skip",
			Detail: "Der Server-Name enthält einen Port oder eine IP-Adresse — andere Server verbinden sich direkt dorthin, ohne Wegweiser."})
	default:
		delegated, r.Steps = c.delegation(ctx, host, r.Steps)
	}

	// 2. The target.
	tlsHost, target, dialAddr, from := c.resolve(ctx, host, port, delegated)
	r.Target, r.TargetFrom = target, from
	r.Steps = append(r.Steps, Step{Key: "target", Title: "Ziel", Level: "ok", Value: target, Detail: targetDetail(from, target, dialAddr)})

	// 3. Connect and fetch the signing key — the step that decides "reachable".
	keyStep, ok := c.key(ctx, serverName, tlsHost, target, dialAddr, from)
	r.Steps = append(r.Steps, keyStep)
	r.Reachable = ok

	// 4. Which software answers. Informational; its failure does not undo step 3.
	if ok {
		sw, st := c.version(ctx, target, dialAddr)
		r.Software = sw
		r.Steps = append(r.Steps, st)
	} else {
		r.Steps = append(r.Steps, Step{Key: "version", Title: "Software", Level: "skip", Detail: "Ohne Verbindung nicht prüfbar."})
	}

	// 5. The client-side delegation — not federation, but the same file tree, and the
	// reason Element asks users to type a homeserver address by hand.
	if port == "" && net.ParseIP(host) == nil {
		r.Steps = append(r.Steps, c.clientWellKnown(ctx, host))
	}
	return r
}

func (c *Checker) delegation(ctx context.Context, host string, steps []Step) (string, []Step) {
	url := "https://" + host + "/.well-known/matrix/server"
	status, _, body, err := c.get(ctx, url, "", wellKnownLimit)
	step := Step{Key: "delegation", Title: "Wegweiser", Value: url}
	switch {
	case err != nil:
		step.Level = "warn"
		step.Detail = fmt.Sprintf("Nicht abrufbar (%s). Andere Server suchen dann per SRV-Eintrag oder auf Port 8448.", short(err))
		return "", append(steps, step)
	case status == http.StatusNotFound:
		step.Level = "warn"
		step.Detail = "Gibt es nicht (404). Andere Server suchen dann per SRV-Eintrag oder auf Port 8448."
		return "", append(steps, step)
	case status != http.StatusOK:
		step.Level = "warn"
		step.Detail = fmt.Sprintf("Antwortet mit %d. Andere Server behandeln das wie keinen Wegweiser.", status)
		return "", append(steps, step)
	}
	var doc struct {
		Server string `json:"m.server"`
	}
	if err := json.Unmarshal(body, &doc); err != nil || strings.TrimSpace(doc.Server) == "" {
		step.Level = "err"
		step.Detail = "Antwortet, aber nicht mit einem Wegweiser: " + describeBody(body) +
			" Andere Server behandeln das wie keinen Wegweiser — oft liefert eine Webseite unter dieser Adresse ihre Startseite aus."
		return "", append(steps, step)
	}
	step.Level = "ok"
	step.Value = strings.TrimSpace(doc.Server)
	step.Detail = "Zeigt auf " + step.Value + "."
	return step.Value, append(steps, step)
}

// resolve returns the name the certificate must fit, the target as shown, the address
// actually dialled (differs from the target only through SRV), and where it came from.
func (c *Checker) resolve(ctx context.Context, host, port, delegated string) (tlsHost, target, dialAddr, from string) {
	if delegated == "" {
		if port != "" || net.ParseIP(host) != nil {
			if port == "" {
				port = defaultPort
			}
			t := net.JoinHostPort(host, port)
			return host, t, t, "name"
		}
		if addr, ok := c.srv(ctx, host); ok {
			return host, host, addr, "srv"
		}
		t := net.JoinHostPort(host, defaultPort)
		return host, t, t, "default"
	}
	dh, dp := splitHostPort(delegated)
	if dp != "" || net.ParseIP(dh) != nil {
		if dp == "" {
			dp = defaultPort
		}
		t := net.JoinHostPort(dh, dp)
		return dh, t, t, "well-known"
	}
	if addr, ok := c.srv(ctx, dh); ok {
		return dh, dh, addr, "srv"
	}
	t := net.JoinHostPort(dh, defaultPort)
	return dh, t, t, "well-known"
}

func (c *Checker) srv(ctx context.Context, host string) (string, bool) {
	if c.LookupSRV == nil {
		return "", false
	}
	for _, service := range []string{"matrix-fed", "matrix"} {
		if _, addrs, err := c.LookupSRV(ctx, service, "tcp", host); err == nil && len(addrs) > 0 {
			a := addrs[0]
			return net.JoinHostPort(strings.TrimSuffix(a.Target, "."), strconv.Itoa(int(a.Port))), true
		}
	}
	return "", false
}

func targetDetail(from, target, dialAddr string) string {
	switch from {
	case "well-known":
		return "Aus dem Wegweiser."
	case "srv":
		return "Aus dem SRV-Eintrag: verbunden wird mit " + dialAddr + ", das Zertifikat muss aber für " + target + " gelten."
	case "name":
		return "Direkt aus dem Server-Namen."
	default:
		return "Kein Wegweiser, kein SRV-Eintrag: andere Server versuchen Port 8448. Hinter Cloudflare wird dieser Port nicht durchgereicht."
	}
}

func (c *Checker) key(ctx context.Context, serverName, tlsHost, target, dialAddr, from string) (Step, bool) {
	step := Step{Key: "key", Title: "Verbindung und Schlüssel", Value: target}
	url := "https://" + urlHost(target) + "/_matrix/key/v2/server"
	status, _, body, err := c.get(ctx, url, dialAddr, keyLimit)
	if err != nil {
		step.Level = "err"
		var cert *tls.CertificateVerificationError
		var hostErr x509.HostnameError
		var unknown x509.UnknownAuthorityError
		switch {
		case errors.As(err, &hostErr):
			step.Detail = fmt.Sprintf("Das Zertifikat unter %s gilt nicht für %s. Andere Server brechen hier ab.", target, tlsHost)
		case errors.As(err, &unknown) || errors.As(err, &cert):
			step.Detail = fmt.Sprintf("Das Zertifikat unter %s ist nicht vertrauenswürdig (%s). Andere Server brechen hier ab.", target, short(err))
		default:
			step.Detail = fmt.Sprintf("Keine Verbindung zu %s: %s.", target, short(err))
			if from == "default" {
				step.Detail += " Ohne Wegweiser ist das der Normalfall hinter Cloudflare — ein Wegweiser auf den Homeserver mit Port 443 löst es."
			}
		}
		return step, false
	}
	if status != http.StatusOK {
		step.Level = "err"
		step.Detail = fmt.Sprintf("%s antwortet auf die Schlüsselabfrage mit %d — dort läuft kein Homeserver, oder er lässt die Föderation nicht durch.", target, status)
		return step, false
	}
	var doc struct {
		ServerName string                     `json:"server_name"`
		VerifyKeys map[string]json.RawMessage `json:"verify_keys"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		step.Level = "err"
		step.Detail = "Die Schlüsselabfrage liefert kein JSON: " + describeBody(body)
		return step, false
	}
	if !strings.EqualFold(doc.ServerName, serverName) {
		step.Level = "err"
		step.Detail = fmt.Sprintf("Unter %s antwortet der Homeserver „%s“, nicht „%s“. Der Wegweiser zeigt auf einen anderen Server.", target, doc.ServerName, serverName)
		return step, false
	}
	if len(doc.VerifyKeys) == 0 {
		step.Level = "err"
		step.Detail = "Der Homeserver antwortet, nennt aber keinen Signaturschlüssel."
		return step, false
	}
	step.Level = "ok"
	step.Detail = fmt.Sprintf("TLS gültig für %s, Homeserver „%s“ mit %d Signaturschlüssel(n).", tlsHost, doc.ServerName, len(doc.VerifyKeys))
	return step, true
}

func (c *Checker) version(ctx context.Context, target, dialAddr string) (string, Step) {
	step := Step{Key: "version", Title: "Software"}
	status, _, body, err := c.get(ctx, "https://"+urlHost(target)+"/_matrix/federation/v1/version", dialAddr, wellKnownLimit)
	if err != nil || status != http.StatusOK {
		step.Level = "warn"
		step.Detail = "Die Versionsabfrage beantwortet der Server nicht; für die Föderation selbst ist sie nicht nötig."
		return "", step
	}
	var doc struct {
		Server struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"server"`
	}
	_ = json.Unmarshal(body, &doc)
	sw := strings.TrimSpace(doc.Server.Name + " " + doc.Server.Version)
	step.Level = "ok"
	step.Value = sw
	step.Detail = "Antwortet als " + sw + "."
	if sw == "" {
		step.Detail = "Antwortet, ohne Software zu nennen."
	}
	return sw, step
}

func (c *Checker) clientWellKnown(ctx context.Context, host string) Step {
	url := "https://" + host + "/.well-known/matrix/client"
	step := Step{Key: "client", Title: "Wegweiser für Apps", Value: url}
	status, header, body, err := c.get(ctx, url, "", wellKnownLimit)
	if err != nil || status != http.StatusOK {
		step.Level = "warn"
		step.Detail = "Fehlt. Apps finden den Homeserver dann nicht selbst — wer sich anmeldet, muss die Adresse von Hand eingeben."
		return step
	}
	var doc struct {
		Homeserver struct {
			BaseURL string `json:"base_url"`
		} `json:"m.homeserver"`
	}
	if err := json.Unmarshal(body, &doc); err != nil || doc.Homeserver.BaseURL == "" {
		step.Level = "warn"
		step.Detail = "Antwortet, aber ohne m.homeserver.base_url: " + describeBody(body)
		return step
	}
	step.Value = doc.Homeserver.BaseURL
	if header.Get("Access-Control-Allow-Origin") == "" {
		step.Level = "warn"
		step.Detail = "Zeigt auf " + doc.Homeserver.BaseURL + ", aber ohne Access-Control-Allow-Origin — Element Web im Browser kann ihn so nicht lesen."
		return step
	}
	step.Level = "ok"
	step.Detail = "Zeigt auf " + doc.Homeserver.BaseURL + "."
	return step
}

// get fetches url. dialAddr, when set, is where the connection goes regardless of the
// URL's host — that is how an SRV target is reached while the certificate is checked
// against the delegated name, which stays in the URL.
func (c *Checker) get(ctx context.Context, url, dialAddr string, limit int64) (int, http.Header, []byte, error) {
	dial := c.Dial
	if dial == nil {
		dial = (&net.Dialer{Timeout: 8 * time.Second}).DialContext
	}
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if dialAddr != "" {
				addr = dialAddr
			}
			return dial(ctx, network, addr)
		},
		TLSClientConfig:     &tls.Config{RootCAs: c.RootCAs, MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout: 8 * time.Second,
		DisableKeepAlives:   true,
	}
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 8 * time.Second
	}
	client := &http.Client{Transport: tr, Timeout: timeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return resp.StatusCode, resp.Header, nil, err
	}
	return resp.StatusCode, resp.Header, body, nil
}

// splitHostPort splits "host:port", "host", "[v6]:port" and "[v6]".
func splitHostPort(s string) (string, string) {
	s = strings.TrimSpace(s)
	if h, p, err := net.SplitHostPort(s); err == nil {
		return h, p
	}
	return strings.TrimSuffix(strings.TrimPrefix(s, "["), "]"), ""
}

// urlHost is target as it goes into a URL: port 443 dropped, so the Host header reads
// as the delegated name the way remote servers send it.
func urlHost(target string) string {
	h, p := splitHostPort(target)
	if p == "" || p == "443" {
		if strings.Contains(h, ":") {
			return "[" + h + "]"
		}
		return h
	}
	return net.JoinHostPort(h, p)
}

// describeBody names what came back instead of a JSON document, in a few words.
func describeBody(b []byte) string {
	s := strings.TrimSpace(string(b))
	switch {
	case s == "":
		return "eine leere Antwort."
	case strings.HasPrefix(strings.ToLower(s), "<!doctype") || strings.HasPrefix(strings.ToLower(s), "<html"):
		return "eine HTML-Seite."
	}
	if len(s) > 60 {
		s = s[:60] + "…"
	}
	return "„" + s + "“."
}

// short says what went wrong in words a person can act on: the three network failures
// by name, anything else as the last link of Go's error chain. "context deadline
// exceeded (Client.Timeout exceeded while awaiting headers)" was the first live answer.
func short(err error) string {
	var dnsErr *net.DNSError
	var netErr net.Error
	switch {
	case errors.As(err, &dnsErr):
		return "der Name lässt sich nicht auflösen"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "Verbindung abgelehnt"
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()):
		return "keine Antwort innerhalb von 8 Sekunden"
	}
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 && i+2 < len(s) {
		s = s[i+2:]
	}
	return s
}
