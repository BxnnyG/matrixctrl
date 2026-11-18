package handlers

import (
	"net/http"
	"time"

	"github.com/bxnnyg/matrixctrl/internal/changelog"
	"github.com/bxnnyg/matrixctrl/internal/notify"
)

// AlertsHandler serves what is wrong right now to the global warning bar (etappe 124),
// independent of whether notifications are switched on.
type AlertsHandler struct{ current func() notify.Current }

func NewAlertsHandler(current func() notify.Current) *AlertsHandler {
	return &AlertsHandler{current: current}
}

type alertBody struct {
	Key      string `json:"key"`
	Trigger  string `json:"trigger"`
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Link     string `json:"link"`
}

// GET /api/v1/alerts
func (h *AlertsHandler) Get(w http.ResponseWriter, r *http.Request) {
	c := h.current()
	alerts := []alertBody{}
	for _, a := range c.Alerts {
		alerts = append(alerts, alertBody{Key: a.Key, Trigger: a.Trigger, Severity: a.Severity,
			Title: a.Title, Detail: a.Detail, Link: a.Link})
	}
	var checked *string
	if !c.CheckedAt.IsZero() {
		s := c.CheckedAt.UTC().Format(time.RFC3339)
		checked = &s
	}
	unknown := c.Unknown
	if unknown == nil {
		unknown = []string{}
	}
	JSON(w, http.StatusOK, map[string]any{"alerts": alerts, "checked_at": checked, "unknown": unknown})
}

// WhatsNewHandler serves the CHANGELOG sections between the last seen version and the
// running one.
type WhatsNewHandler struct {
	md      string
	version string
}

func NewWhatsNewHandler(changelogMD, version string) *WhatsNewHandler {
	return &WhatsNewHandler{md: changelogMD, version: version}
}

// GET /api/v1/whatsnew?since=X.Y.Z
func (h *WhatsNewHandler) Get(w http.ResponseWriter, r *http.Request) {
	secs := changelog.Since(h.md, r.URL.Query().Get("since"), h.version)
	JSON(w, http.StatusOK, map[string]any{"current": h.version, "sections": secs})
}
