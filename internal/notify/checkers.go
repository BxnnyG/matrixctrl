package notify

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bxnnyg/matrixctrl/internal/backup/offsite"
	"github.com/bxnnyg/matrixctrl/internal/dnshealth"
	"github.com/bxnnyg/matrixctrl/internal/k8s"
	"github.com/bxnnyg/matrixctrl/internal/updatecheck"
)

// ---- DNS ----------------------------------------------------------------------

type dnsChecker struct{ state func() dnshealth.State }

// NewDNS alerts while the cluster cannot resolve the names it depends on.
func NewDNS(state func() dnshealth.State) Checker { return &dnsChecker{state: state} }

func (dnsChecker) Trigger() string { return TriggerDNS }

func (c *dnsChecker) Check(context.Context) ([]Alert, error) {
	s := c.state()
	if s.Checked.IsZero() {
		return nil, errors.New("noch nicht geprüft")
	}
	if s.OK {
		return nil, nil
	}
	var names []string
	for _, f := range s.Failures {
		names = append(names, f.Name)
	}
	detail := "Der Cluster kann Namen nicht auflösen"
	if len(names) > 0 {
		detail += ": " + strings.Join(names, ", ")
	}
	if !s.Since.IsZero() {
		detail += fmt.Sprintf(" (seit %s)", s.Since.UTC().Format("02.01. 15:04 UTC"))
	}
	return []Alert{{Key: "dns", Trigger: TriggerDNS, Title: "Namensauflösung im Cluster gestört", Detail: detail}}, nil
}

// ---- Certificates ---------------------------------------------------------------

// CertWarnDays is how long before expiry a certificate raises an alert.
const CertWarnDays = 14

// CertSource is the slice of the cluster client the certificate check needs.
type CertSource interface {
	IngressTLSSecrets(ctx context.Context, namespace string) (map[string]string, error)
	GetSecret(ctx context.Context, namespace, name string) (map[string][]byte, error)
}

type certChecker struct {
	src CertSource
	ns  string
	now func() time.Time
}

// NewCert reads the TLS Secrets of the Ingresses in the managed namespace — no network,
// no public names. A nil source (no cluster) or empty namespace yields nothing.
func NewCert(src CertSource, ns string, now func() time.Time) Checker {
	if now == nil {
		now = time.Now
	}
	return &certChecker{src: src, ns: ns, now: now}
}

func (certChecker) Trigger() string { return TriggerCert }

func (c *certChecker) Check(ctx context.Context) ([]Alert, error) {
	if c.src == nil || c.ns == "" {
		return nil, nil
	}
	hosts, err := c.src.IngressTLSSecrets(ctx, c.ns)
	if err != nil {
		return nil, err
	}
	bySecret := map[string][]string{}
	for h, s := range hosts {
		if s != "" {
			bySecret[s] = append(bySecret[s], h)
		}
	}
	names := make([]string, 0, len(bySecret))
	for s := range bySecret {
		names = append(names, s)
	}
	sort.Strings(names)
	limit := c.now().Add(CertWarnDays * 24 * time.Hour)
	var out []Alert
	for _, name := range names {
		data, err := c.src.GetSecret(ctx, c.ns, name)
		if err != nil {
			return nil, err
		}
		raw := data["tls.crt"]
		if len(raw) == 0 {
			continue // not issued yet: nothing to expire
		}
		notAfter, ok := leafNotAfter(raw)
		if !ok || notAfter.After(limit) {
			continue
		}
		hs := bySecret[name]
		sort.Strings(hs)
		title := "Zertifikat läuft bald ab"
		left := notAfter.Sub(c.now())
		var detail string
		if left <= 0 {
			title = "Zertifikat ist abgelaufen"
			detail = fmt.Sprintf("%s: abgelaufen am %s.", strings.Join(hs, ", "), notAfter.UTC().Format("02.01.2006"))
		} else {
			detail = fmt.Sprintf("%s: gültig bis %s (noch %d Tage).", strings.Join(hs, ", "),
				notAfter.UTC().Format("02.01.2006"), int(left.Hours()/24))
		}
		out = append(out, Alert{Key: "cert:" + name, Trigger: TriggerCert, Title: title, Detail: detail})
	}
	return out, nil
}

// leafNotAfter is the expiry of the first certificate in a PEM chain.
func leafNotAfter(pemData []byte) (time.Time, bool) {
	for {
		var b *pem.Block
		b, pemData = pem.Decode(pemData)
		if b == nil {
			return time.Time{}, false
		}
		if b.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(b.Bytes)
		if err != nil {
			return time.Time{}, false
		}
		return cert.NotAfter, true
	}
}

// Cached asks the inner checker at most once per interval and answers from memory in
// between (certificates change by the day; the engine passes by the minute).
func Cached(inner Checker, every time.Duration, now func() time.Time) Checker {
	if now == nil {
		now = time.Now
	}
	return &cachedChecker{inner: inner, every: every, now: now}
}

type cachedChecker struct {
	inner Checker
	every time.Duration
	now   func() time.Time
	mu    sync.Mutex
	at    time.Time
	last  []Alert
	err   error
}

func (c *cachedChecker) Trigger() string { return c.inner.Trigger() }

func (c *cachedChecker) Check(ctx context.Context) ([]Alert, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.at.IsZero() && c.now().Sub(c.at) < c.every {
		return c.last, c.err
	}
	c.last, c.err = c.inner.Check(ctx)
	c.at = c.now()
	return c.last, c.err
}

// ---- Update ---------------------------------------------------------------------

type updateChecker struct {
	check func(ctx context.Context) updatecheck.Result
}

// NewUpdate notifies once per new version. check may be nil (update check disabled).
func NewUpdate(check func(ctx context.Context) updatecheck.Result) Checker {
	return &updateChecker{check: check}
}

func (updateChecker) Trigger() string { return TriggerUpdate }

func (c *updateChecker) Check(ctx context.Context) ([]Alert, error) {
	if c.check == nil {
		return nil, nil
	}
	r := c.check(ctx)
	if r.Available && r.Latest != "" {
		return []Alert{{
			Key: "update:" + r.Latest, Trigger: TriggerUpdate, Once: true,
			Title:  "Update verfügbar: MatrixCtrl " + r.Latest,
			Detail: fmt.Sprintf("Installiert ist %s (Kanal %s).", r.Current, r.Channel),
		}}, nil
	}
	if r.Error != "" {
		return nil, errors.New(r.Error)
	}
	return nil, nil
}

// ---- Pods -----------------------------------------------------------------------

// PodGrace is how long a workload may be unwell before it counts as stuck.
const PodGrace = 10 * time.Minute

// HealthSource is the slice of the cluster client the pod check needs.
type HealthSource interface {
	ComponentHealth(ctx context.Context, namespace string) ([]k8s.ComponentHealth, error)
}

type podChecker struct {
	src HealthSource
	ns  string
	now func() time.Time
	mu  sync.Mutex
	bad map[string]time.Time
}

// NewPod alerts for a Deployment or StatefulSet that has been looping, not ready or
// unschedulable for longer than PodGrace. Scaled to zero is a decision, not a fault.
func NewPod(src HealthSource, ns string, now func() time.Time) Checker {
	if now == nil {
		now = time.Now
	}
	return &podChecker{src: src, ns: ns, now: now, bad: map[string]time.Time{}}
}

func (*podChecker) Trigger() string { return TriggerPod }

// watchedKinds: Synapse and Postgres are StatefulSets; leaving them out hid the two most
// important components once already (CLAUDE.md).
var watchedKinds = map[string]bool{"Deployment": true, "StatefulSet": true}

func (c *podChecker) Check(ctx context.Context) ([]Alert, error) {
	if c.src == nil || c.ns == "" {
		return nil, nil
	}
	comps, err := c.src.ComponentHealth(ctx, c.ns)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	seen := map[string]bool{}
	var out []Alert
	for _, h := range comps {
		if !watchedKinds[h.Kind] || h.Status == "scaled-zero" {
			continue
		}
		reason := ""
		switch {
		case h.Looping:
			reason = "Neustart-Schleife"
		case h.Unschedulable != nil:
			reason = "nicht planbar: " + h.Unschedulable.Reason
		case h.Status != "healthy":
			reason = fmt.Sprintf("nicht bereit (%d von %d)", h.Ready, h.Desired)
		}
		if reason == "" {
			continue
		}
		seen[h.Name] = true
		first, ok := c.bad[h.Name]
		if !ok {
			first = now
			c.bad[h.Name] = first
		}
		if now.Sub(first) < PodGrace {
			continue
		}
		out = append(out, Alert{
			Key: "pod:" + h.Name, Trigger: TriggerPod,
			Title:  h.Name + " hängt",
			Detail: fmt.Sprintf("%s %s seit %d min.", h.Kind, reason, int(now.Sub(first).Minutes())),
		})
	}
	for n := range c.bad {
		if !seen[n] {
			delete(c.bad, n)
		}
	}
	return out, nil
}

// ---- Backup ---------------------------------------------------------------------

// RunLister is the slice of the backup run history the check needs.
type RunLister interface {
	Recent(ctx context.Context, n int) ([]offsite.RunRecord, error)
}

type backupChecker struct{ runs RunLister }

// NewBackup alerts while the newest finished off-site run failed; the next ok run
// clears it. Skipped and running attempts say nothing either way.
func NewBackup(runs RunLister) Checker { return &backupChecker{runs: runs} }

func (backupChecker) Trigger() string { return TriggerBackup }

func (c *backupChecker) Check(ctx context.Context) ([]Alert, error) {
	if c.runs == nil {
		return nil, nil
	}
	rs, err := c.runs.Recent(ctx, 20)
	if err != nil {
		return nil, err
	}
	for _, r := range rs { // newest first
		switch r.Status {
		case "ok":
			return nil, nil
		case "failed":
			detail := fmt.Sprintf("Letzter Lauf (%s, %s) ist fehlgeschlagen", r.Occasion, r.StartedAt.UTC().Format("02.01. 15:04 UTC"))
			if r.Error != "" {
				detail += ": " + r.Error
			}
			return []Alert{{Key: "backup", Trigger: TriggerBackup, Title: "Backup außer Haus fehlgeschlagen", Detail: detail}}, nil
		}
	}
	return nil, nil
}
