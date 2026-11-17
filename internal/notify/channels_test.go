package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mc "github.com/bxnnyg/matrixctrl/internal/mail"
)

type memSecrets struct{ s Secrets }

func (m *memSecrets) Get(context.Context) (Secrets, error) { return m.s, nil }
func (m *memSecrets) Put(_ context.Context, s Secrets) error {
	m.s = s
	return nil
}

func TestWebhookPostsJSONAndHidesURL(t *testing.T) {
	var body map[string]string
	status := 200
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content type")
		}
		w.WriteHeader(status)
	}))
	url := srv.URL + "/hook/SECRET-TOKEN"
	w := &WebhookSender{Secrets: &memSecrets{Secrets{WebhookURL: url}}}
	if err := w.Send(ctx, Message{Text: "hi", HTML: "<b>hi</b>"}); err != nil || body["text"] != "hi" || body["html"] != "<b>hi</b>" {
		t.Fatalf("send: %v %v", err, body)
	}
	status = 500
	err := w.Send(ctx, Message{})
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") || strings.Contains(err.Error(), "SECRET-TOKEN") {
		t.Fatalf("500 must be reported without the URL: %v", err)
	}
	srv.Close()
	err = w.Send(ctx, Message{})
	if err == nil || strings.Contains(err.Error(), "SECRET-TOKEN") {
		t.Fatalf("connection error must not leak the URL: %v", err)
	}
	if err := (&WebhookSender{Secrets: &memSecrets{}}).Send(ctx, Message{}); err == nil {
		t.Fatalf("no URL must be an error")
	}
}

// The webhook 500 test the plan asks for, through the real sender and the engine.
func TestWebhook500ThenRecovery(t *testing.T) {
	status, hits := 500, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++; w.WriteHeader(status) }))
	defer srv.Close()
	r := newRig(TriggerDNS)
	r.ck.alerts = []Alert{alert("dns", TriggerDNS)}
	e := New(Deps{Settings: r.set, Store: r.store, Checkers: []Checker{r.ck}, Now: r.clk.now,
		Webhook: &WebhookSender{Secrets: &memSecrets{Secrets{WebhookURL: srv.URL}}}})
	e.Once(ctx)
	status = 200
	e.Once(ctx)
	e.Once(ctx)
	if hits != 2 {
		t.Fatalf("want 500 then one successful retry, then quiet: %d hits", hits)
	}
}

func TestEmailSourcesAndPartialDelivery(t *testing.T) {
	mas := mc.Settings{Enabled: true, From: "noreply@example.org", Host: "smtp.example.org", Port: 587, Encryption: mc.StartTLS}
	set := &memSettings{s: Settings{EmailEnabled: true, Recipients: []string{"a@example.org", "bad@example.org"}, SMTPSource: SMTPMas}}
	var used []mc.Settings
	e := &EmailSender{Settings: set, Secrets: &memSecrets{Secrets{SMTPPassword: "pw"}},
		MAS: func(context.Context) (mc.Settings, error) { return mas, nil },
		SendMail: func(_ context.Context, s mc.Settings, to, _, _ string) error {
			used = append(used, s)
			if strings.HasPrefix(to, "bad") {
				return errors.New("rejected")
			}
			return nil
		}}
	if err := e.Send(ctx, Message{Subject: "s", Text: "t"}); err != nil {
		t.Fatalf("one recipient reached => delivered: %v", err)
	}
	if err := e.SendStrict(ctx, Message{}); err == nil || !strings.Contains(err.Error(), "bad@example.org: rejected") {
		t.Fatalf("test button must name the failing recipient: %v", err)
	}
	if used[0].Host != "smtp.example.org" {
		t.Errorf("MAS account not used")
	}
	// own account
	set.s.SMTPSource = SMTPOwn
	set.s.Own = Own{From: "me@example.org", Host: "mail.own.example", Port: 465, Encryption: "tls", Username: "me"}
	used = nil
	_ = e.Send(ctx, Message{})
	if used[0].Host != "mail.own.example" || used[0].Password != "pw" {
		t.Errorf("own account not used: %+v", used[0])
	}
	// MAS without SMTP: not usable
	set.s.SMTPSource = SMTPMas
	e.MAS = func(context.Context) (mc.Settings, error) { return mc.Settings{}, nil }
	if err := e.Send(ctx, Message{}); err == nil || !strings.Contains(err.Error(), "nicht eingerichtet") {
		t.Fatalf("MAS without SMTP must say so: %v", err)
	}
	if MASAvailable(ctx, e.MAS) || MASAvailable(ctx, nil) {
		t.Errorf("masAvailable must be false")
	}
	set.s.Recipients = nil
	if err := e.Send(ctx, Message{}); err == nil {
		t.Fatalf("no recipients must be an error")
	}
}

func TestSettingsValidate(t *testing.T) {
	ok := Default()
	ok.WebhookEnabled = true
	if err := ok.Validate(true, false); err != nil {
		t.Fatal(err)
	}
	if err := ok.Validate(false, false); err == nil {
		t.Error("webhook without URL")
	}
	bad := Default()
	bad.Reminder = "1h"
	if bad.Validate(false, false) == nil {
		t.Error("reminder")
	}
	em := Default()
	em.EmailEnabled, em.Recipients = true, []string{"nope"}
	if em.Validate(false, false) == nil {
		t.Error("recipient")
	}
	em.Recipients = []string{"a@example.org"}
	em.SMTPSource = SMTPOwn
	if em.Validate(false, false) == nil {
		t.Error("own without host")
	}
	em.Own = Own{From: "me@example.org", Host: "h", Port: 587, Encryption: "starttls", Username: "u"}
	if em.Validate(false, false) == nil {
		t.Error("username without password")
	}
	if err := em.Validate(false, true); err != nil {
		t.Error(err)
	}
	if ValidateWebhookURL("ftp://x") == nil || ValidateWebhookURL("https://h.example/x") != nil {
		t.Error("url validation")
	}
}
