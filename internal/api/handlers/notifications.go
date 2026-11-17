package handlers

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bxnnyg/matrixctrl/internal/config"
	mc "github.com/bxnnyg/matrixctrl/internal/mail"
	"github.com/bxnnyg/matrixctrl/internal/notify"
)

// Notifications (etappe 123): settings, test message, state. The logic is in
// internal/notify; this file maps it to HTTP. The webhook URL and the SMTP password are
// never returned — only whether they are set.

type NotificationsHandler struct {
	engine   *notify.Engine
	settings notify.SettingsStore
	secrets  notify.SecretStore // nil without cluster access
	mas      func(ctx context.Context) (mc.Settings, error)
	saved    func(ctx context.Context) bool
	mu       sync.Mutex
}

func NewNotificationsHandler(e *notify.Engine, st notify.SettingsStore, sec notify.SecretStore,
	mas func(ctx context.Context) (mc.Settings, error), saved func(ctx context.Context) bool) *NotificationsHandler {
	return &NotificationsHandler{engine: e, settings: st, secrets: sec, mas: mas, saved: saved}
}

// ServerName is the instance name used in messages, from the settings; "" if unknown.
func ServerName(ctx context.Context, store *config.Store) string {
	if store == nil {
		return ""
	}
	contents, err := store.MergedContent(ctx)
	if err != nil {
		return ""
	}
	values, err := config.MergeToMap(contents)
	if err != nil {
		return ""
	}
	s, _ := values["serverName"].(string)
	return s
}

type notifyOwn struct {
	From        string `json:"from"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Encryption  string `json:"encryption"`
	Username    string `json:"username"`
	Password    string `json:"password,omitempty"`      // input only
	ClearPass   bool   `json:"clearPassword,omitempty"` // input only
	PasswordSet bool   `json:"passwordSet"`             // output only
}

type notifySettingsBody struct {
	Webhook struct {
		Enabled  bool   `json:"enabled"`
		URL      string `json:"url,omitempty"`      // input only
		ClearURL bool   `json:"clearUrl,omitempty"` // input only
		URLSet   bool   `json:"urlSet"`             // output only
	} `json:"webhook"`
	Email struct {
		Enabled bool `json:"enabled"`
	} `json:"email"`
	Recipients    []string        `json:"recipients"`
	SMTPSource    string          `json:"smtpSource"`
	Own           notifyOwn       `json:"own"`
	Reminder      string          `json:"reminder"`
	Triggers      map[string]bool `json:"triggers"`
	MasAvailable  bool            `json:"masAvailable"`
	Saved         bool            `json:"saved"`
	SecretsUsable bool            `json:"secretsUsable"`
}

func (h *NotificationsHandler) body(ctx context.Context, set notify.Settings, sec notify.Secrets) notifySettingsBody {
	var b notifySettingsBody
	b.Webhook.Enabled, b.Webhook.URLSet = set.WebhookEnabled, sec.WebhookURL != ""
	b.Email.Enabled = set.EmailEnabled
	b.Recipients, b.SMTPSource, b.Reminder, b.Triggers = set.Recipients, set.SMTPSource, set.Reminder, set.Triggers
	b.Own = notifyOwn{From: set.Own.From, Host: set.Own.Host, Port: set.Own.Port,
		Encryption: set.Own.Encryption, Username: set.Own.Username, PasswordSet: sec.SMTPPassword != ""}
	b.MasAvailable = notify.MASAvailable(ctx, h.mas)
	b.Saved = h.saved != nil && h.saved(ctx)
	b.SecretsUsable = h.secrets != nil
	return b
}

func (h *NotificationsHandler) loadSecrets(ctx context.Context) (notify.Secrets, error) {
	if h.secrets == nil {
		return notify.Secrets{}, nil
	}
	return h.secrets.Get(ctx)
}

// GET /api/v1/notifications/settings
func (h *NotificationsHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	set, err := h.settings.Load(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, "Die Einstellungen waren nicht lesbar: "+err.Error())
		return
	}
	sec, err := h.loadSecrets(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, h.body(r.Context(), set.Normalize(), sec))
}

// PUT /api/v1/notifications/settings — an empty url/password keeps the stored one;
// clearUrl / clearPassword remove it.
func (h *NotificationsHandler) PutSettings(w http.ResponseWriter, r *http.Request) {
	var req notifySettingsBody
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request")
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	ctx := r.Context()
	sec, err := h.loadSecrets(ctx)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	next := sec
	if req.Webhook.ClearURL {
		next.WebhookURL = ""
	}
	if u := strings.TrimSpace(req.Webhook.URL); u != "" {
		if err := notify.ValidateWebhookURL(u); err != nil {
			Error(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		next.WebhookURL = u
	}
	if req.Own.ClearPass {
		next.SMTPPassword = ""
	}
	if req.Own.Password != "" {
		next.SMTPPassword = req.Own.Password
	}

	set := notify.Settings{
		WebhookEnabled: req.Webhook.Enabled, EmailEnabled: req.Email.Enabled,
		SMTPSource: req.SMTPSource, Reminder: req.Reminder, Triggers: req.Triggers,
		Own: notify.Own{From: strings.TrimSpace(req.Own.From), Host: strings.TrimSpace(req.Own.Host),
			Port: req.Own.Port, Encryption: req.Own.Encryption, Username: req.Own.Username},
	}
	for _, rc := range req.Recipients {
		if rc = strings.TrimSpace(rc); rc != "" {
			set.Recipients = append(set.Recipients, rc)
		}
	}
	set = set.Normalize()
	if set.Own.Port == 0 {
		set.Own.Port = 587
	}
	if err := set.Validate(next.WebhookURL != "", next.SMTPPassword != ""); err != nil {
		Error(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if set.EmailEnabled && set.SMTPSource != notify.SMTPOwn && !notify.MASAvailable(ctx, h.mas) {
		Error(w, http.StatusUnprocessableEntity, "E-Mail über den Matrix-Zugang: auf der E-Mail-Seite ist noch kein SMTP eingerichtet. Dort einrichten oder einen eigenen Zugang wählen.")
		return
	}
	if next != sec {
		if h.secrets == nil {
			Error(w, http.StatusServiceUnavailable, "Ohne Cluster-Zugriff lassen sich Webhook-URL und Passwort nicht ablegen.")
			return
		}
		if err := h.secrets.Put(ctx, next); err != nil {
			Error(w, http.StatusInternalServerError, "Die Zugangsdaten ließen sich nicht ablegen: "+err.Error())
			return
		}
	}
	if err := h.settings.Save(ctx, set); err != nil {
		Error(w, http.StatusInternalServerError, "Die Einstellungen ließen sich nicht speichern: "+err.Error())
		return
	}
	out := h.body(ctx, set, next)
	out.Saved = true
	JSON(w, http.StatusOK, out)
}

// POST /api/v1/notifications/test {channel} — the error comes back in its own words.
func (h *NotificationsHandler) Test(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Channel string `json:"channel"`
	}
	if err := Decode(r, &req); err != nil || (req.Channel != notify.ChannelWebhook && req.Channel != notify.ChannelEmail) {
		Error(w, http.StatusBadRequest, "channel: webhook oder email erwartet")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	if err := h.engine.Test(ctx, req.Channel); err != nil {
		JSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	JSON(w, http.StatusOK, map[string]any{"ok": true, "error": ""})
}

type notifyAlertBody struct {
	Key      string     `json:"key"`
	Trigger  string     `json:"trigger"`
	Title    string     `json:"title"`
	Detail   string     `json:"detail"`
	Since    time.Time  `json:"since"`
	LastSent *time.Time `json:"lastSent"` // latest delivery over any channel
	Pending  []string   `json:"pending"`  // channels still lacking the current message
}

type notifyChannelBody struct {
	LastSentAt  *time.Time `json:"lastSentAt"`
	LastError   string     `json:"lastError"`
	LastErrorAt *time.Time `json:"lastErrorAt"`
}

// GET /api/v1/notifications/state
func (h *NotificationsHandler) State(w http.ResponseWriter, r *http.Request) {
	snap, err := h.engine.Snapshot(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	alerts := []notifyAlertBody{}
	for _, a := range snap.Alerts {
		pending := snap.Pending[a.Key]
		if pending == nil {
			pending = []string{}
		}
		alerts = append(alerts, notifyAlertBody{Key: a.Key, Trigger: a.Trigger, Title: a.Title,
			Detail: a.Detail, Since: a.Since, LastSent: a.LastSent(), Pending: pending})
	}
	chans := map[string]notifyChannelBody{}
	for name, c := range snap.Channels {
		chans[name] = notifyChannelBody{LastSentAt: c.LastSentAt, LastError: c.LastError, LastErrorAt: c.LastErrorAt}
	}
	JSON(w, http.StatusOK, map[string]any{"alerts": alerts, "channels": chans, "lastRun": snap.LastRun})
}
