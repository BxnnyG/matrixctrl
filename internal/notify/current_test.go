package notify

import (
	"context"
	"errors"
	"testing"
	"time"
)

type failSettings struct{}

func (failSettings) Load(context.Context) (Settings, error) { return Settings{}, errors.New("db down") }
func (failSettings) Save(context.Context, Settings) error   { return errors.New("db down") }

func TestCurrentHoldsSwitchedOffTriggerButSendsNothing(t *testing.T) {
	r := newRig(TriggerDNS)
	r.set.s.Triggers[TriggerDNS] = false
	r.ck.alerts = []Alert{alert("dns", TriggerDNS)}
	r.e.Once(ctx)
	c := r.e.Current()
	if len(c.Alerts) != 1 || c.Alerts[0].Key != "dns" {
		t.Fatalf("current = %+v", c.Alerts)
	}
	if c.Alerts[0].Severity != "error" || c.Alerts[0].Link != "/tls-dns" {
		t.Fatalf("meta = %q %q", c.Alerts[0].Severity, c.Alerts[0].Link)
	}
	if c.CheckedAt.IsZero() || r.hook.count() != 0 {
		t.Fatalf("checked %v, sent %d", c.CheckedAt, r.hook.count())
	}
}

func TestCurrentSurvivesSettingsError(t *testing.T) {
	r := newRig(TriggerPod)
	r.ck.alerts = []Alert{alert("pod:x", TriggerPod)}
	r.e.d.Settings = failSettings{}
	r.e.Once(ctx)
	if n := len(r.e.Current().Alerts); n != 1 {
		t.Fatalf("settings error emptied current (%d)", n)
	}
}

func TestCurrentUnknownKeepsPreviousAndClearsWhenGone(t *testing.T) {
	r := newRig(TriggerDNS)
	r.ck.alerts = []Alert{alert("dns", TriggerDNS)}
	r.e.Once(ctx)
	r.ck.err = errUnknown
	r.e.Once(ctx)
	c := r.e.Current()
	if len(c.Unknown) != 1 || c.Unknown[0] != TriggerDNS || len(c.Alerts) != 1 {
		t.Fatalf("unknown pass: %+v", c)
	}
	r.ck.err, r.ck.alerts = nil, nil
	r.e.Once(ctx)
	c = r.e.Current()
	if len(c.Alerts) != 0 || len(c.Unknown) != 0 || c.Alerts == nil {
		t.Fatalf("after recovery: %+v", c)
	}
}

func TestReleaseTriggerOnByDefault(t *testing.T) {
	if !Default().Normalize().Triggers[TriggerRelease] {
		t.Fatal("release must default to on")
	}
	s := Default()
	s.Triggers[TriggerRelease] = false
	if s.Normalize().Triggers[TriggerRelease] {
		t.Fatal("explicit off must stay off")
	}
}

type fakeRel struct {
	st  *ReleaseState
	err error
}

func (f fakeRel) Release(context.Context) (*ReleaseState, error) { return f.st, f.err }

func TestReleaseChecker(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	nowf := func() time.Time { return now }
	cases := []struct {
		name    string
		src     fakeRel
		alert   bool
		wantErr bool
	}{
		{"failed", fakeRel{st: &ReleaseState{Name: "ess", Status: "failed", Since: now}}, true, false},
		{"deployed", fakeRel{st: &ReleaseState{Name: "ess", Status: "deployed", Since: now.Add(-time.Hour)}}, false, false},
		{"pending 5 min", fakeRel{st: &ReleaseState{Name: "ess", Status: "pending-upgrade", Since: now.Add(-5 * time.Minute)}}, false, false},
		{"pending 15 min", fakeRel{st: &ReleaseState{Name: "ess", Status: "pending-upgrade", Since: now.Add(-15 * time.Minute)}}, true, false},
		{"no release", fakeRel{}, false, false},
		{"error", fakeRel{err: errors.New("boom")}, false, true},
	}
	for _, c := range cases {
		got, err := NewRelease(c.src, nowf).Check(context.Background())
		if (err != nil) != c.wantErr || (len(got) == 1) != c.alert {
			t.Errorf("%s: alerts=%d err=%v", c.name, len(got), err)
		}
		if c.alert && got[0].Trigger != TriggerRelease {
			t.Errorf("%s: trigger %q", c.name, got[0].Trigger)
		}
	}
}
