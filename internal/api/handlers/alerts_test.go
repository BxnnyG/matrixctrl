package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bxnnyg/matrixctrl/internal/notify"
)

func TestAlertsShapeAndNeverNull(t *testing.T) {
	h := NewAlertsHandler(func() notify.Current { return notify.Current{} })
	w := httptest.NewRecorder()
	h.Get(w, httptest.NewRequest("GET", "/api/v1/alerts", nil))
	b := w.Body.String()
	for _, want := range []string{`"alerts":[]`, `"checked_at":null`, `"unknown":[]`} {
		if !strings.Contains(b, want) {
			t.Fatalf("%s missing in %s", want, b)
		}
	}
	at := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	h = NewAlertsHandler(func() notify.Current {
		return notify.Current{CheckedAt: at, Unknown: []string{"pod"},
			Alerts: []notify.Alert{{Key: "dns", Trigger: "dns", Severity: "error", Title: "T", Detail: "D", Link: "/tls-dns"}}}
	})
	w = httptest.NewRecorder()
	h.Get(w, httptest.NewRequest("GET", "/api/v1/alerts", nil))
	var got struct {
		Alerts    []map[string]string `json:"alerts"`
		CheckedAt string              `json:"checked_at"`
		Unknown   []string            `json:"unknown"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Alerts) != 1 || got.Alerts[0]["link"] != "/tls-dns" || got.Alerts[0]["severity"] != "error" ||
		got.CheckedAt != "2026-11-18T10:00:00Z" || len(got.Unknown) != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestWhatsNew(t *testing.T) {
	md := "## [0.1.2] — 2026-01-02\n\n- b\n\n## [0.1.1] — 2026-01-01\n\n- a\n"
	h := NewWhatsNewHandler(md, "0.1.2-rc.1")
	w := httptest.NewRecorder()
	h.Get(w, httptest.NewRequest("GET", "/api/v1/whatsnew?since=0.1.1", nil))
	var got struct {
		Current  string `json:"current"`
		Sections []struct{ Version, Date, Body string }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Current != "0.1.2-rc.1" || len(got.Sections) != 1 || got.Sections[0].Version != "0.1.2" || got.Sections[0].Body != "- b" {
		t.Fatalf("got %+v", got)
	}
	w = httptest.NewRecorder()
	NewWhatsNewHandler(md, "dev").Get(w, httptest.NewRequest("GET", "/api/v1/whatsnew", nil))
	if !strings.Contains(w.Body.String(), `"sections":[]`) {
		t.Fatalf("dev: %s", w.Body.String())
	}
}
