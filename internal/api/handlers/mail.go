package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/bxnnyg/matrixctrl/internal/config"
	"github.com/bxnnyg/matrixctrl/internal/mail"
)

// E-mail for MAS: registration confirmations and forgotten passwords (etappe 114b).
// The SMTP password lives in a Kubernetes Secret, mounted into MAS by the chart — see
// mas_block.go for the mechanism it shares with the login providers.

const (
	mailSecret    = "matrixctrl-mas-email"
	mailConfigKey = "email.yaml" // what MAS reads
	// mailSettingsKey is what the assistant reads. Kept apart because a disabled
	// transport must not carry SMTP fields into MAS's configuration — its `email`
	// section is a tagged union, and `transport: blackhole` beside a `hostname` is
	// not a document MAS promises to accept. Switching off would otherwise mean
	// typing the server, user and password again to switch back on.
	mailSettingsKey = "settings.json"
	mailAdditional  = "matrixctrl-email"
)

type MailHandler struct {
	k8s        upstreamCluster
	store      *config.Store
	essNS      string
	essRelease string
	mu         sync.Mutex
}

func NewMailHandler(k upstreamCluster, store *config.Store, essNS, essRelease string) *MailHandler {
	return &MailHandler{k8s: k, store: store, essNS: essNS, essRelease: essRelease}
}

func (h *MailHandler) block() masBlock {
	return masBlock{k8s: h.k8s, store: h.store, essNS: h.essNS,
		deployment: h.essRelease + "-matrix-authentication-service",
		secret:     mailSecret, name: mailAdditional, key: mailConfigKey}
}

func (h *MailHandler) load(ctx context.Context) (mail.Settings, error) {
	data, err := h.block().read(ctx)
	if err != nil {
		return mail.Settings{}, err
	}
	if raw := data[mailSettingsKey]; len(raw) > 0 {
		var s mail.Settings
		if err := json.Unmarshal(raw, &s); err == nil {
			return s, nil
		}
	}
	// Written before etappe 114b's settings entry existed, or by hand.
	if doc := string(data[mailConfigKey]); doc != "" {
		return mail.Parse(doc)
	}
	return mail.Settings{Port: 587, Encryption: mail.StartTLS}, nil
}

// publicMail is what the browser sees: everything but the password.
type publicMail struct {
	mail.Settings
	Password    string `json:"password,omitempty"` // always empty; shadows the real one
	HasPassword bool   `json:"has_password"`
}

// GET /api/v1/mail
func (h *MailHandler) Get(w http.ResponseWriter, r *http.Request) {
	s, err := h.load(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	values := h.block().values(r.Context())
	out := publicMail{Settings: s, HasPassword: s.Password != ""}
	out.Settings.Password = ""
	// The default sender uses the server's own name, so the field is not empty on a
	// first visit — a sender is required and hard to invent on the spot.
	suggested := ""
	if name, _ := nestedGet(values, "serverName").(string); name != "" {
		suggested = "noreply@" + name
	}
	JSON(w, http.StatusOK, map[string]interface{}{
		"settings":          out,
		"suggested_from":    suggested,
		"wiring":            h.block().wiring(r.Context(), values),
		"registration_uses": nestedGet(values, "matrixAuthenticationService") != nil,
	})
}

// PUT /api/v1/mail — an empty password keeps the stored one.
func (h *MailHandler) Put(w http.ResponseWriter, r *http.Request) {
	var req mail.Settings
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request")
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	cur, err := h.load(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.Password == "" {
		req.Password = cur.Password
	}
	if req.Encryption == "" {
		req.Encryption = mail.DefaultEncryption(req.Port)
	}
	if err := req.Validate(); err != nil {
		Error(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	doc, err := mail.Render(req)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	kept, err := json.Marshal(req)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.block().write(r.Context(), map[string][]byte{
		mailConfigKey: []byte(doc), mailSettingsKey: kept,
	}); err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	next, err := h.block().activate(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, "Gespeichert, aber nicht aktiviert: "+err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "saved", "next": next})
}

// POST /api/v1/mail/probe — connect, encrypt, log in, hang up. Sends nothing.
func (h *MailHandler) Probe(w http.ResponseWriter, r *http.Request) {
	s, err := h.load(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !s.Enabled {
		JSON(w, http.StatusOK, map[string]interface{}{"ok": false, "note": "E-Mail-Versand ist ausgeschaltet."})
		return
	}
	if err := mail.Probe(r.Context(), s); err != nil {
		JSON(w, http.StatusOK, map[string]interface{}{"ok": false, "note": err.Error()})
		return
	}
	JSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

// POST /api/v1/mail/test {to} — one real message, the only proof that reaches an inbox.
func (h *MailHandler) Test(w http.ResponseWriter, r *http.Request) {
	var req struct {
		To string `json:"to"`
	}
	if err := Decode(r, &req); err != nil || req.To == "" {
		Error(w, http.StatusBadRequest, "Empfängeradresse fehlt")
		return
	}
	s, err := h.load(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !s.Enabled {
		JSON(w, http.StatusOK, map[string]interface{}{"ok": false, "note": "E-Mail-Versand ist ausgeschaltet."})
		return
	}
	if err := mail.Send(r.Context(), s, req.To); err != nil {
		JSON(w, http.StatusOK, map[string]interface{}{"ok": false, "note": err.Error()})
		return
	}
	JSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}
