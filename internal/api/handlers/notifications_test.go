package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bxnnyg/matrixctrl/internal/notify"
)

type nSettings struct{ s notify.Settings }

func (m *nSettings) Load(context.Context) (notify.Settings, error) { return m.s.Normalize(), nil }
func (m *nSettings) Save(_ context.Context, s notify.Settings) error {
	m.s = s
	return nil
}

type nSecrets struct{ s notify.Secrets }

func (m *nSecrets) Get(context.Context) (notify.Secrets, error) { return m.s, nil }
func (m *nSecrets) Put(_ context.Context, s notify.Secrets) error {
	m.s = s
	return nil
}

type nStore struct{}

func (nStore) Rows(context.Context) ([]notify.Row, error)               { return nil, nil }
func (nStore) SaveRow(context.Context, notify.Row) error                { return nil }
func (nStore) DeleteRow(context.Context, string) error                  { return nil }
func (nStore) Channels(context.Context) ([]notify.ChannelStatus, error) { return nil, nil }
func (nStore) SaveChannel(context.Context, notify.ChannelStatus) error  { return nil }

func notifyRig() (*NotificationsHandler, *nSettings, *nSecrets) {
	st, sec := &nSettings{}, &nSecrets{}
	e := notify.New(notify.Deps{Settings: st, Store: nStore{}})
	return NewNotificationsHandler(e, st, sec, nil, func(context.Context) bool { return false }), st, sec
}

func put(t *testing.T, h *NotificationsHandler, body string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.PutSettings(rec, httptest.NewRequest(http.MethodPut, "/", strings.NewReader(body)))
	return rec.Code, rec.Body.String()
}

func TestNotificationsSecretsNeverReturnedAndKeptOnEmpty(t *testing.T) {
	h, st, sec := notifyRig()
	code, out := put(t, h, `{"webhook":{"enabled":true,"url":"https://hook.example/SECRET"},"reminder":"6h","triggers":{"dns":true}}`)
	if code != 200 || sec.s.WebhookURL == "" || strings.Contains(out, "SECRET") || !strings.Contains(out, `"urlSet":true`) {
		t.Fatalf("save: %d %s", code, out)
	}
	// empty url keeps the stored one
	if code, _ := put(t, h, `{"webhook":{"enabled":true},"reminder":"24h","triggers":{"dns":true}}`); code != 200 || sec.s.WebhookURL != "https://hook.example/SECRET" || st.s.Reminder != "24h" {
		t.Fatalf("empty url must keep: %d %+v", code, sec.s)
	}
	// GET: no secret, urlSet true, masAvailable false, defaults for the other triggers
	rec := httptest.NewRecorder()
	h.GetSettings(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if strings.Contains(rec.Body.String(), "SECRET") || got["masAvailable"] != false || got["triggers"].(map[string]any)["pod"] != false {
		t.Fatalf("GET body: %s", rec.Body.String())
	}
	// explicit clear (webhook then has to be off, else 422)
	if code, _ := put(t, h, `{"webhook":{"enabled":true,"clearUrl":true}}`); code != 422 {
		t.Fatalf("enabled webhook without URL must be refused: %d", code)
	}
	if code, out := put(t, h, `{"webhook":{"enabled":false,"clearUrl":true}}`); code != 200 || sec.s.WebhookURL != "" || !strings.Contains(out, `"urlSet":false`) {
		t.Fatalf("clear: %d %s", code, out)
	}
	if code, _ := put(t, h, `{"webhook":{"url":"nonsense"}}`); code != 422 {
		t.Fatalf("bad url: %d", code)
	}
}

func TestNotificationsTestEndpointReportsError(t *testing.T) {
	h, _, _ := notifyRig()
	rec := httptest.NewRecorder()
	h.Test(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"channel":"webhook"}`)))
	// no webhook sender wired: ok=false with the reason, HTTP 200
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"ok":false`) || strings.Contains(rec.Body.String(), `"error":""`) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.Test(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"channel":"sms"}`)))
	if rec.Code != 400 {
		t.Fatalf("unknown channel: %d", rec.Code)
	}
}

func TestNotificationsStateShape(t *testing.T) {
	h, _, _ := notifyRig()
	rec := httptest.NewRecorder()
	h.State(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	for _, want := range []string{`"alerts":[]`, `"webhook":{"lastSentAt":null`, `"email":`, `"lastRun":null`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("missing %s in %s", want, rec.Body.String())
		}
	}
}

// E-mail through the MAS account is refused while MAS has no SMTP (found on the test cluster,
// rc.1 accepted it and the first notification would have failed silently until a run).
func TestNotificationsEmailViaMASRequiresMASSMTP(t *testing.T) {
	h, _, _ := notifyRig()
	if code, out := put(t, h, `{"email":{"enabled":true},"recipients":["ops@example.org"],"smtpSource":"mas"}`); code != 422 || !strings.Contains(out, "E-Mail-Seite") {
		t.Fatalf("email via MAS without MAS SMTP must be refused: %d %s", code, out)
	}
	if code, _ := put(t, h, `{"email":{"enabled":false},"recipients":["ops@example.org"],"smtpSource":"mas"}`); code != 200 {
		t.Fatalf("email off via MAS must still save: %d", code)
	}
}
