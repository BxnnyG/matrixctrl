package notify

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/bxnnyg/matrixctrl/internal/backup/offsite"
	"github.com/bxnnyg/matrixctrl/internal/dnshealth"
	"github.com/bxnnyg/matrixctrl/internal/k8s"
	"github.com/bxnnyg/matrixctrl/internal/updatecheck"
)

func certPEM(t *testing.T, notAfter time.Time) []byte {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "x"},
		NotBefore: notAfter.Add(-90 * 24 * time.Hour), NotAfter: notAfter}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

type fakeCerts struct {
	hosts   map[string]string
	secrets map[string]map[string][]byte
	err     error
}

func (f *fakeCerts) IngressTLSSecrets(context.Context, string) (map[string]string, error) {
	return f.hosts, f.err
}
func (f *fakeCerts) GetSecret(_ context.Context, _, n string) (map[string][]byte, error) {
	if s, ok := f.secrets[n]; ok {
		return s, nil
	}
	return map[string][]byte{}, nil
}

// Gegenprobe 4: moving the threshold from 14 days to 1 makes the 13-day case fail.
func TestCertThreshold(t *testing.T) {
	clk := newClock()
	for _, c := range []struct {
		name string
		days float64
		want int
	}{{"13 days", 13, 1}, {"15 days", 15, 0}, {"expired", -2, 1}} {
		t.Run(c.name, func(t *testing.T) {
			src := &fakeCerts{hosts: map[string]string{"matrix.example.org": "tls-matrix"},
				secrets: map[string]map[string][]byte{"tls-matrix": {"tls.crt": certPEM(t, clk.t.Add(time.Duration(c.days*24)*time.Hour))}}}
			got, err := NewCert(src, "ess", clk.now).Check(ctx)
			if err != nil || len(got) != c.want {
				t.Fatalf("want %d alerts, got %d (%v)", c.want, len(got), err)
			}
			if c.want == 1 && (got[0].Key != "cert:tls-matrix" || !strings.Contains(got[0].Detail, "matrix.example.org")) {
				t.Errorf("alert wrong: %+v", got[0])
			}
		})
	}
}

func TestCertQuietCases(t *testing.T) {
	clk := newClock()
	if got, err := NewCert(nil, "ess", clk.now).Check(ctx); got != nil || err != nil {
		t.Errorf("no cluster must give nothing: %v %v", got, err)
	}
	empty := &fakeCerts{hosts: map[string]string{"h": "not-issued"}}
	if got, err := NewCert(empty, "ess", clk.now).Check(ctx); got != nil || err != nil {
		t.Errorf("secret without a certificate must stay quiet: %v %v", got, err)
	}
	if _, err := NewCert(&fakeCerts{err: errors.New("timeout")}, "ess", clk.now).Check(ctx); err == nil {
		t.Errorf("unreadable source must be unknown")
	}
}

type countingChecker struct{ n int }

func (c *countingChecker) Trigger() string { return TriggerCert }
func (c *countingChecker) Check(context.Context) ([]Alert, error) {
	c.n++
	return nil, nil
}

func TestCachedAsksOncePerInterval(t *testing.T) {
	clk := newClock()
	in := &countingChecker{}
	c := Cached(in, time.Hour, clk.now)
	c.Check(ctx)
	c.Check(ctx)
	clk.t = clk.t.Add(61 * time.Minute)
	c.Check(ctx)
	if in.n != 2 {
		t.Fatalf("want 2 inner checks, got %d", in.n)
	}
}

type fakeHealth struct {
	comps []k8s.ComponentHealth
	err   error
}

func (f *fakeHealth) ComponentHealth(context.Context, string) ([]k8s.ComponentHealth, error) {
	return f.comps, f.err
}

// Gegenprobe 5: leaving StatefulSets out of the watched kinds makes this fail.
func TestPodStatefulSetStuckRaisesAlert(t *testing.T) {
	clk := newClock()
	h := &fakeHealth{comps: []k8s.ComponentHealth{
		{Name: "ess-postgres", Kind: "StatefulSet", Status: "down", Ready: 0, Desired: 1},
		{Name: "ess-element-web", Kind: "Deployment", Status: "healthy", Ready: 1, Desired: 1},
	}}
	c := NewPod(h, "ess", clk.now)
	if got, _ := c.Check(ctx); len(got) != 0 {
		t.Fatalf("must wait out the grace period, got %+v", got)
	}
	clk.t = clk.t.Add(PodGrace + time.Minute)
	got, err := c.Check(ctx)
	if err != nil || len(got) != 1 || got[0].Key != "pod:ess-postgres" {
		t.Fatalf("ess-postgres hangs => alert, got %+v (%v)", got, err)
	}
}

func TestPodRules(t *testing.T) {
	clk := newClock()
	h := &fakeHealth{comps: []k8s.ComponentHealth{
		{Name: "off", Kind: "Deployment", Status: "scaled-zero", Desired: 0},
		{Name: "loop", Kind: "Deployment", Status: "degraded", Ready: 0, Desired: 1, Looping: true},
		{Name: "pending", Kind: "Deployment", Status: "down", Desired: 1,
			Unschedulable: &k8s.Unschedulable{Pod: "p", Reason: "0/1 nodes: Insufficient cpu"}},
		{Name: "flaky", Kind: "Deployment", Status: "degraded", Ready: 1, Desired: 2},
	}}
	c := NewPod(h, "ess", clk.now)
	c.Check(ctx)
	clk.t = clk.t.Add(11 * time.Minute)
	got, _ := c.Check(ctx)
	keys := map[string]string{}
	for _, a := range got {
		keys[a.Key] = a.Detail
	}
	if _, ok := keys["pod:off"]; ok {
		t.Errorf("scaled to zero is not a fault")
	}
	if !strings.Contains(keys["pod:loop"], "Neustart-Schleife") || !strings.Contains(keys["pod:pending"], "Insufficient cpu") || keys["pod:flaky"] == "" {
		t.Errorf("alerts wrong: %+v", keys)
	}
	// healthy again resets the clock
	h.comps = []k8s.ComponentHealth{{Name: "loop", Kind: "Deployment", Status: "healthy", Ready: 1, Desired: 1}}
	c.Check(ctx)
	h.comps = []k8s.ComponentHealth{{Name: "loop", Kind: "Deployment", Status: "down", Desired: 1}}
	if got, _ := c.Check(ctx); len(got) != 0 {
		t.Errorf("grace period must restart after recovery")
	}
	// unreadable: unknown, grace kept
	h.err = errors.New("timeout")
	if _, err := c.Check(ctx); err == nil {
		t.Errorf("unreadable cluster must be unknown")
	}
	if got, err := NewPod(nil, "ess", nil).Check(ctx); got != nil || err != nil {
		t.Errorf("no cluster => nothing")
	}
}

type fakeRuns struct {
	runs []offsite.RunRecord
	err  error
}

func (f *fakeRuns) Recent(context.Context, int) ([]offsite.RunRecord, error) { return f.runs, f.err }

func TestBackupFailedThenOk(t *testing.T) {
	now := time.Now()
	runs := &fakeRuns{runs: []offsite.RunRecord{
		{Status: "skipped", StartedAt: now},
		{Status: "failed", Error: "AccessDenied", Occasion: "zeitplan", StartedAt: now.Add(-time.Hour)},
		{Status: "ok", StartedAt: now.Add(-48 * time.Hour)},
	}}
	c := NewBackup(runs)
	got, err := c.Check(ctx)
	if err != nil || len(got) != 1 || !strings.Contains(got[0].Detail, "AccessDenied") {
		t.Fatalf("failed run => alert: %+v %v", got, err)
	}
	runs.runs = append([]offsite.RunRecord{{Status: "running", StartedAt: now}, {Status: "ok", StartedAt: now}}, runs.runs...)
	if got, _ := c.Check(ctx); len(got) != 0 {
		t.Fatalf("newer ok run => clear")
	}
	runs.runs, runs.err = nil, errors.New("db down")
	if _, err := c.Check(ctx); err == nil {
		t.Fatalf("unreadable history is unknown")
	}
}

func TestDNSChecker(t *testing.T) {
	st := dnshealth.State{}
	c := NewDNS(func() dnshealth.State { return st })
	if _, err := c.Check(ctx); err == nil {
		t.Fatal("never checked => unknown")
	}
	st = dnshealth.State{OK: true, Checked: time.Now()}
	if got, _ := c.Check(ctx); len(got) != 0 {
		t.Fatal("ok => no alert")
	}
	st = dnshealth.State{OK: false, Checked: time.Now(), Failures: []dnshealth.Failure{{Name: "matrix.example.org"}}}
	got, _ := c.Check(ctx)
	if len(got) != 1 || !strings.Contains(got[0].Detail, "matrix.example.org") {
		t.Fatalf("outage => alert: %+v", got)
	}
}

func TestUpdateChecker(t *testing.T) {
	r := updatecheck.Result{Current: "1.0.0", Latest: "1.1.0", Available: true, Channel: "stable"}
	c := NewUpdate(func(context.Context) updatecheck.Result { return r })
	got, _ := c.Check(ctx)
	if len(got) != 1 || got[0].Key != "update:1.1.0" || !got[0].Once {
		t.Fatalf("available => one notice per version: %+v", got)
	}
	r = updatecheck.Result{Current: "1.1.0", Latest: "1.1.0"}
	if got, err := c.Check(ctx); len(got) != 0 || err != nil {
		t.Fatalf("installed => nothing")
	}
	r = updatecheck.Result{Error: "kein Internet"}
	if _, err := c.Check(ctx); err == nil {
		t.Fatalf("no result => unknown (silent)")
	}
	if got, err := NewUpdate(nil).Check(ctx); got != nil || err != nil {
		t.Fatalf("disabled => nothing")
	}
}
