package notify

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

var ctx = context.Background()

func TestProblemIsSentOnceAndNotRepeated(t *testing.T) {
	r := newRig(TriggerDNS)
	r.ck.alerts = []Alert{alert("dns", TriggerDNS)}
	r.e.Once(ctx)
	r.clk.t = r.clk.t.Add(time.Minute)
	r.e.Once(ctx)
	if r.hook.count() != 1 {
		t.Fatalf("want 1 message, got %d", r.hook.count())
	}
	if m := r.hook.got[0]; !strings.Contains(m.Text, "test.example") || !strings.Contains(m.Text, "Problem") || m.HTML == "" {
		t.Errorf("message lacks instance/kind/html: %+v", m)
	}
}

// Gegenprobe 1: without the all-clear this fails.
func TestProblemGoneSendsResolvedExactlyOnce(t *testing.T) {
	r := newRig(TriggerDNS)
	r.ck.alerts = []Alert{alert("dns", TriggerDNS)}
	r.e.Once(ctx)
	r.ck.alerts = nil
	r.e.Once(ctx)
	r.e.Once(ctx)
	if r.hook.count() != 2 || !strings.Contains(r.hook.got[1].Text, "Entwarnung") {
		t.Fatalf("want problem + Entwarnung, got %d: %+v", r.hook.count(), r.hook.got)
	}
	if rows, _ := r.store.Rows(ctx); len(rows) != 0 {
		t.Errorf("row should be gone after resolution: %+v", rows)
	}
}

// Gegenprobe 2: marking a failed delivery as sent makes this fail.
func TestFailedDeliveryIsRetriedNextRun(t *testing.T) {
	r := newRig(TriggerDNS)
	r.ck.alerts = []Alert{alert("dns", TriggerDNS)}
	r.hook.fail = errors.New("HTTP 500")
	r.e.Once(ctx)
	if rows, _ := r.store.Rows(ctx); len(rows) != 1 || rows[0].LastSent != nil {
		t.Fatalf("failed send must leave last_sent empty: %+v", rows)
	}
	r.hook.fail = nil
	r.clk.t = r.clk.t.Add(time.Minute)
	r.e.Once(ctx)
	if r.hook.count() != 1 {
		t.Fatalf("next run must send again, got %d", r.hook.count())
	}
}

// Gegenprobe 3: state only in memory (a fresh store per engine) makes this fail.
func TestRestartDoesNotSendTwice(t *testing.T) {
	r := newRig(TriggerDNS)
	r.ck.alerts = []Alert{alert("dns", TriggerDNS)}
	r.e.Once(ctx)
	restarted := r.engine()
	restarted.Once(ctx)
	if r.hook.count() != 1 {
		t.Fatalf("restart re-sent: %d messages", r.hook.count())
	}
}

func TestReminderFollowsInterval(t *testing.T) {
	r := newRig(TriggerDNS)
	r.set.s.Reminder = Reminder6h
	r.ck.alerts = []Alert{alert("dns", TriggerDNS)}
	r.e.Once(ctx)
	r.clk.t = r.clk.t.Add(5*time.Hour + 59*time.Minute)
	r.e.Once(ctx)
	if r.hook.count() != 1 {
		t.Fatalf("too early for a reminder: %d", r.hook.count())
	}
	r.clk.t = r.clk.t.Add(2 * time.Minute)
	r.e.Once(ctx)
	if r.hook.count() != 2 || !strings.Contains(r.hook.got[1].Text, "Erinnerung") {
		t.Fatalf("want a reminder, got %+v", r.hook.got)
	}
	// and the clock restarts from the reminder
	r.clk.t = r.clk.t.Add(time.Hour)
	r.e.Once(ctx)
	if r.hook.count() != 2 {
		t.Fatalf("second reminder too soon")
	}
}

func TestReminderOffNeverReminds(t *testing.T) {
	r := newRig(TriggerDNS)
	r.ck.alerts = []Alert{alert("dns", TriggerDNS)}
	r.e.Once(ctx)
	r.clk.t = r.clk.t.Add(72 * time.Hour)
	r.e.Once(ctx)
	if r.hook.count() != 1 {
		t.Fatalf("reminder off but %d messages", r.hook.count())
	}
}

func TestUnknownIsNeitherAlarmNorAllClear(t *testing.T) {
	r := newRig(TriggerDNS)
	r.ck.err = errUnknown
	r.e.Once(ctx)
	if r.hook.count() != 0 {
		t.Fatalf("unknown must not alarm")
	}
	// a real problem, then unknown: no all-clear
	r.ck.err, r.ck.alerts = nil, []Alert{alert("dns", TriggerDNS)}
	r.e.Once(ctx)
	r.ck.err, r.ck.alerts = errUnknown, nil
	r.e.Once(ctx)
	if r.hook.count() != 1 {
		t.Fatalf("unknown must not resolve: %d messages", r.hook.count())
	}
	if lr := r.e.LastRun(); lr == nil || len(lr.Unknown) != 1 {
		t.Errorf("last run should list the unknown trigger: %+v", lr)
	}
	r.ck.err = nil
	r.e.Once(ctx)
	if r.hook.count() != 2 {
		t.Fatalf("known again and gone: want all-clear")
	}
}

func TestUpdateNoticeHasNoReminderNoResolved(t *testing.T) {
	r := newRig(TriggerUpdate)
	r.set.s.Reminder = Reminder6h
	r.ck.alerts = []Alert{{Key: "update:1.2.3", Trigger: TriggerUpdate, Title: "Update", Once: true}}
	r.e.Once(ctx)
	r.clk.t = r.clk.t.Add(30 * time.Hour)
	r.e.Once(ctx)
	r.ck.alerts = nil // installed
	r.e.Once(ctx)
	if r.hook.count() != 1 {
		t.Fatalf("want exactly the one notice, got %d", r.hook.count())
	}
	if rows, _ := r.store.Rows(ctx); len(rows) != 0 {
		t.Errorf("row should be cleaned up")
	}
	// a newer version is a new notice
	r.ck.alerts = []Alert{{Key: "update:1.2.4", Trigger: TriggerUpdate, Title: "Update", Once: true}}
	r.e.Once(ctx)
	if r.hook.count() != 2 {
		t.Fatalf("new version must notify again")
	}
}

func TestSwitchedOffTriggerIsSilent(t *testing.T) {
	r := newRig(TriggerDNS)
	r.set.s.Triggers[TriggerDNS] = false
	r.ck.alerts = []Alert{alert("dns", TriggerDNS)}
	r.e.Once(ctx)
	if r.hook.count() != 0 {
		t.Fatalf("switched-off trigger sent")
	}
}

func TestNoSettingsMeansEverythingOff(t *testing.T) {
	r := newRig(TriggerDNS)
	r.set.s = Default()
	r.ck.alerts = []Alert{alert("dns", TriggerDNS)}
	r.e.Once(ctx)
	if r.hook.count() != 0 {
		t.Fatalf("default settings sent something")
	}
}

func TestNoChannelKeepsProblemPendingUntilOneIsOn(t *testing.T) {
	r := newRig(TriggerDNS)
	r.set.s.WebhookEnabled = false
	r.ck.alerts = []Alert{alert("dns", TriggerDNS)}
	r.e.Once(ctx)
	r.set.s.WebhookEnabled = true
	r.e.Once(ctx)
	if r.hook.count() != 1 {
		t.Fatalf("channel switched on later must announce, got %d", r.hook.count())
	}
}

func TestOneDeadChannelDoesNotRepeatTheOther(t *testing.T) {
	r := newRig(TriggerDNS)
	mail := &recorder{}
	r.hook.fail = errors.New("down")
	r.set.s.EmailEnabled = true
	d := Deps{Settings: r.set, Store: r.store, Checkers: []Checker{r.ck}, Webhook: r.hook, Email: mail, Now: r.clk.now}
	e := New(d)
	r.ck.alerts = []Alert{alert("dns", TriggerDNS)}
	e.Once(ctx)
	e.Once(ctx)
	if mail.count() != 1 {
		t.Fatalf("e-mail repeated: %d", mail.count())
	}
	snap, _ := e.Snapshot(ctx)
	if snap.Channels[ChannelWebhook].LastError != "down" || snap.Channels[ChannelWebhook].LastErrorAt == nil {
		t.Errorf("webhook error not held: %+v", snap.Channels[ChannelWebhook])
	}
	if snap.Channels[ChannelEmail].LastSentAt == nil || snap.Channels[ChannelEmail].LastError != "" {
		t.Errorf("email status wrong: %+v", snap.Channels[ChannelEmail])
	}
	if len(snap.Alerts) != 1 {
		t.Errorf("active alert missing from snapshot")
	}
}

func TestTestMessageReportsErrorVerbatim(t *testing.T) {
	r := newRig(TriggerDNS)
	r.hook.fail = errors.New("Webhook antwortete mit HTTP 500")
	if err := r.e.Test(ctx, ChannelWebhook); err == nil || err.Error() != "Webhook antwortete mit HTTP 500" {
		t.Fatalf("error not verbatim: %v", err)
	}
	r.hook.fail = nil
	if err := r.e.Test(ctx, ChannelWebhook); err != nil {
		t.Fatal(err)
	}
	if err := r.e.Test(ctx, "sms"); err == nil {
		t.Fatal("unknown channel accepted")
	}
}
