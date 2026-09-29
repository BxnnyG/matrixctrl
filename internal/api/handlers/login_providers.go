package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bxnnyg/matrixctrl/internal/config"
	"github.com/bxnnyg/matrixctrl/internal/masupstream"
	"github.com/go-chi/chi/v5"
)

// Sign-in through Google, GitHub or an OIDC provider, configured in MAS (etappe 110).
// The safety decisions are in internal/masupstream; this is storage, wiring and the
// order of steps.

const (
	upstreamSecret      = "matrixctrl-mas-upstream"
	upstreamConfigKey   = "upstream.yaml"  // what MAS reads
	upstreamProvidersKy = "providers.json" // what the assistant reads, drafts included
	// The key under matrixAuthenticationService.additional that mounts the Secret.
	upstreamAdditional = "matrixctrl-upstream"
)

// upstreamCluster is what the handler needs from the cluster — an interface so the
// promise that matters most (the client secret never leaves the Secret) is tested
// without one.
type upstreamCluster interface {
	GetSecret(ctx context.Context, namespace, name string) (map[string][]byte, error)
	PutSecret(ctx context.Context, namespace, name string, data map[string][]byte) error
	SecretVolumes(ctx context.Context, namespace, deployment string) ([]string, error)
	RolloutRestart(ctx context.Context, namespace, kind, name string) error
}

type LoginProvidersHandler struct {
	k8s        upstreamCluster
	store      *config.Store
	essNS      string
	essRelease string
	// One writer at a time: two tabs saving at once would each write the list they
	// read, and the second would drop the first's provider — and its callback URL,
	// already registered at Google, would point at nothing.
	mu sync.Mutex
}

func NewLoginProvidersHandler(k upstreamCluster, store *config.Store, essNS, essRelease string) *LoginProvidersHandler {
	return &LoginProvidersHandler{k8s: k, store: store, essNS: essNS, essRelease: essRelease}
}

// publicProvider is a provider as the browser sees it: never the secret.
type publicProvider struct {
	ID        string           `json:"id"`
	Kind      masupstream.Kind `json:"kind"`
	Name      string           `json:"name"`
	Issuer    string           `json:"issuer,omitempty"`
	ClientID  string           `json:"client_id,omitempty"`
	HasSecret bool             `json:"has_secret"`
	Enabled   bool             `json:"enabled"`
	Complete  bool             `json:"complete"`
	Callback  string           `json:"callback,omitempty"`
}

func (h *LoginProvidersHandler) public(p masupstream.Provider, masHost string) publicProvider {
	name := p.Name
	if name == "" {
		name = masupstream.DefaultName(p.Kind)
	}
	out := publicProvider{
		ID: p.ID, Kind: p.Kind, Name: name, Issuer: p.Issuer, ClientID: p.ClientID,
		HasSecret: p.ClientSecret != "", Enabled: p.Enabled, Complete: p.Complete(),
	}
	if masHost != "" {
		out.Callback = masupstream.Callback(masHost, p.ID)
	}
	return out
}

func (h *LoginProvidersHandler) load(ctx context.Context) ([]masupstream.Provider, error) {
	data, err := h.k8s.GetSecret(ctx, h.essNS, upstreamSecret)
	if err != nil {
		return nil, err
	}
	var ps []masupstream.Provider
	if raw := data[upstreamProvidersKy]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &ps); err != nil {
			return nil, fmt.Errorf("%s/%s ist nicht lesbar: %w", upstreamSecret, upstreamProvidersKy, err)
		}
	}
	return ps, nil
}

func (h *LoginProvidersHandler) save(ctx context.Context, ps []masupstream.Provider) error {
	rendered, err := masupstream.Render(ps)
	if err != nil {
		return err
	}
	list, err := json.Marshal(ps)
	if err != nil {
		return err
	}
	return h.k8s.PutSecret(ctx, h.essNS, upstreamSecret, map[string][]byte{
		upstreamConfigKey:   []byte(rendered),
		upstreamProvidersKy: list,
	})
}

func (h *LoginProvidersHandler) values(ctx context.Context) map[string]interface{} {
	contents, err := h.store.MergedContent(ctx)
	if err != nil {
		return nil
	}
	merged, err := config.MergeToMap(contents)
	if err != nil {
		return nil
	}
	return merged
}

func (h *LoginProvidersHandler) masHost(values map[string]interface{}) string {
	host, _ := nestedGet(values, "matrixAuthenticationService", "ingress", "host").(string)
	return host
}

func (h *LoginProvidersHandler) masDeployment() string {
	return h.essRelease + "-matrix-authentication-service"
}

// wiring says whether MAS reads the Secret: in the settings, and on the cluster.
type wiring struct {
	InConfig bool `json:"in_config"`
	Deployed bool `json:"deployed"`
}

func (h *LoginProvidersHandler) wiring(ctx context.Context, values map[string]interface{}) wiring {
	var w wiring
	if s, _ := nestedGet(values, "matrixAuthenticationService", "additional", upstreamAdditional, "configSecret").(string); s == upstreamSecret {
		w.InConfig = true
	}
	mounted, err := h.k8s.SecretVolumes(ctx, h.essNS, h.masDeployment())
	if err != nil {
		return w
	}
	for _, name := range mounted {
		if name == upstreamSecret {
			w.Deployed = true
		}
	}
	return w
}

// GET /api/v1/login-providers
func (h *LoginProvidersHandler) List(w http.ResponseWriter, r *http.Request) {
	ps, err := h.load(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	values := h.values(r.Context())
	host := h.masHost(values)
	out := make([]publicProvider, 0, len(ps))
	for _, p := range ps {
		out = append(out, h.public(p, host))
	}
	JSON(w, http.StatusOK, map[string]interface{}{
		"mas_host":  host,
		"providers": out,
		"wiring":    h.wiring(r.Context(), values),
	})
}

// POST /api/v1/login-providers {kind, name, issuer} — a draft, so the callback URL
// exists before the operator registers the app at the provider.
func (h *LoginProvidersHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind   masupstream.Kind `json:"kind"`
		Name   string           `json:"name"`
		Issuer string           `json:"issuer"`
	}
	if err := Decode(r, &req); err != nil || !masupstream.ValidKind(req.Kind) {
		Error(w, http.StatusBadRequest, "kind muss google, github oder oidc sein")
		return
	}
	values := h.values(r.Context())
	host := h.masHost(values)
	if host == "" {
		// The callback URL is the first thing the operator needs; one made up from
		// a guess would be registered at Google and fail at the first login.
		Error(w, http.StatusConflict, "Der Hostname der Anmeldung (matrixAuthenticationService.ingress.host) ist nicht gesetzt — ohne ihn gibt es keine Callback-URL.")
		return
	}
	id, err := masupstream.NewULID(time.Now())
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	ps, err := h.load(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	p := masupstream.Provider{
		ID: id, Kind: req.Kind, Name: strings.TrimSpace(req.Name),
		Issuer: strings.TrimSpace(req.Issuer), Enabled: true, Created: time.Now().UTC(),
	}
	ps = append(ps, p)
	if err := h.save(r.Context(), ps); err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusCreated, h.public(p, host))
}

// PUT /api/v1/login-providers/{id} {client_id, client_secret, name, issuer, enabled}
//
// An empty client_secret keeps the stored one: the browser never has it to send back.
func (h *LoginProvidersHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		ClientID     *string `json:"client_id"`
		ClientSecret string  `json:"client_secret"`
		Name         *string `json:"name"`
		Issuer       *string `json:"issuer"`
		Enabled      *bool   `json:"enabled"`
	}
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request")
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	ps, err := h.load(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	i := indexOf(ps, id)
	if i < 0 {
		Error(w, http.StatusNotFound, "Anbieter nicht gefunden")
		return
	}
	before, err := masupstream.Render(ps)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	p := &ps[i]
	if req.ClientID != nil {
		p.ClientID = strings.TrimSpace(*req.ClientID)
	}
	if s := strings.TrimSpace(req.ClientSecret); s != "" {
		p.ClientSecret = s
	}
	if req.Name != nil {
		p.Name = strings.TrimSpace(*req.Name)
	}
	if req.Issuer != nil {
		p.Issuer = strings.TrimSpace(*req.Issuer)
	}
	if req.Enabled != nil {
		p.Enabled = *req.Enabled
	}
	if err := h.save(r.Context(), ps); err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Only when what MAS reads changed. Fixing a draft's issuer changes nothing MAS
	// sees (drafts are not rendered), and must neither add the mount as a pending
	// change nor restart the login for everyone.
	after, err := masupstream.Render(ps)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	next := "unchanged"
	if after != before {
		next, err = h.activate(r.Context())
	}
	if err != nil {
		Error(w, http.StatusInternalServerError, "Gespeichert, aber nicht aktiviert: "+err.Error())
		return
	}
	values := h.values(r.Context())
	JSON(w, http.StatusOK, map[string]interface{}{"provider": h.public(*p, h.masHost(values)), "next": next})
}

// DELETE /api/v1/login-providers/{id} — drafts only. A provider MAS has seen is
// switched off instead (see internal/masupstream): deleting it here would leave it in
// MAS's database, invisible to the assistant.
func (h *LoginProvidersHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	h.mu.Lock()
	defer h.mu.Unlock()
	ps, err := h.load(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	i := indexOf(ps, id)
	if i < 0 {
		Error(w, http.StatusNotFound, "Anbieter nicht gefunden")
		return
	}
	if ps[i].Complete() {
		Error(w, http.StatusConflict, "Ein eingerichteter Anbieter wird abgeschaltet, nicht gelöscht — MAS behält ihn sonst in seiner Datenbank.")
		return
	}
	ps = append(ps[:i], ps[i+1:]...)
	if err := h.save(r.Context(), ps); err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// activate makes MAS read what was just saved, and says what happens next:
//
//   - "apply": the settings do not mount the Secret yet. The entry is written as a
//     pending change; "Übernehmen" deploys it and restarts MAS.
//   - "apply-pending": mounted in the settings, not yet deployed — same next step.
//   - "restarting": deployed; only the Secret's content changed, which does not change
//     the pod template, so nothing would restart MAS on its own. Restarted here.
func (h *LoginProvidersHandler) activate(ctx context.Context) (string, error) {
	values := h.values(ctx)
	wr := h.wiring(ctx, values)
	if !wr.InConfig {
		base := "matrixAuthenticationService.additional." + upstreamAdditional + "."
		if err := h.store.SetSectionValues(ctx, map[string]interface{}{
			base + "configSecret":    upstreamSecret,
			base + "configSecretKey": upstreamConfigKey,
		}, nil); err != nil {
			return "", err
		}
		return "apply", nil
	}
	if !wr.Deployed {
		return "apply-pending", nil
	}
	if err := h.k8s.RolloutRestart(ctx, h.essNS, "Deployment", h.masDeployment()); err != nil {
		return "", err
	}
	return "restarting", nil
}

// POST /api/v1/login-providers/{id}/check — asks the provider's discovery document,
// before the operator relies on it. GitHub has none; that is said, not failed.
func (h *LoginProvidersHandler) Check(w http.ResponseWriter, r *http.Request) {
	ps, err := h.load(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	i := indexOf(ps, chi.URLParam(r, "id"))
	if i < 0 {
		Error(w, http.StatusNotFound, "Anbieter nicht gefunden")
		return
	}
	p := ps[i]
	issuer := p.Issuer
	switch p.Kind {
	case masupstream.GitHub:
		JSON(w, http.StatusOK, map[string]interface{}{"checked": false, "note": "GitHub bietet keine OIDC-Discovery — geprüft wird beim ersten Login."})
		return
	case masupstream.Google:
		issuer = "https://accounts.google.com"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(issuer, "/")+"/.well-known/openid-configuration", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		JSON(w, http.StatusOK, map[string]interface{}{"checked": false, "ok": false, "note": "Nicht erreichbar: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	var d struct {
		Issuer string `json:"issuer"`
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &d) != nil {
		JSON(w, http.StatusOK, map[string]interface{}{"checked": true, "ok": false, "note": fmt.Sprintf("Kein gültiges Discovery-Dokument (HTTP %d).", resp.StatusCode)})
		return
	}
	// MAS validates strictly: the issuer must match exactly, trailing slash included.
	if d.Issuer != issuer {
		JSON(w, http.StatusOK, map[string]interface{}{"checked": true, "ok": false,
			"note": fmt.Sprintf("Der Anbieter nennt sich %q, eingetragen ist %q — MAS verlangt exakt dieselbe Schreibweise.", d.Issuer, issuer)})
		return
	}
	JSON(w, http.StatusOK, map[string]interface{}{"checked": true, "ok": true})
}

// GET /api/v1/login-providers/verify — does MAS's login page offer the providers?
//
// Read through the in-cluster Service, not the public hostname: pods on this
// installation could not reach Cloudflare (2026-09-28), and a check that times out on
// its own network path says nothing about MAS.
func (h *LoginProvidersHandler) Verify(w http.ResponseWriter, r *http.Request) {
	ps, err := h.load(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	url := fmt.Sprintf("http://%s.%s.svc.cluster.local:8080/login", h.masDeployment(), h.essNS)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if host := h.masHost(h.values(r.Context())); host != "" {
		req.Host = host
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		JSON(w, http.StatusOK, map[string]interface{}{"reachable": false, "note": err.Error()})
		return
	}
	defer resp.Body.Close()
	page, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	shown := map[string]bool{}
	for _, p := range ps {
		if !p.Complete() || !p.Enabled {
			continue
		}
		// The login page links each provider by its id.
		shown[p.ID] = strings.Contains(string(page), "/upstream/authorize/"+p.ID)
	}
	JSON(w, http.StatusOK, map[string]interface{}{"reachable": true, "status": resp.StatusCode, "shown": shown})
}

func indexOf(ps []masupstream.Provider, id string) int {
	for i := range ps {
		if ps[i].ID == id {
			return i
		}
	}
	return -1
}
