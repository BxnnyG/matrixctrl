package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	authmw "github.com/bxnny/matrixctrl/internal/api/middleware"
	"github.com/bxnny/matrixctrl/internal/auth"
	"github.com/bxnny/matrixctrl/internal/config"
	"github.com/bxnny/matrixctrl/internal/helm"
	"github.com/bxnny/matrixctrl/internal/hooks"
)

type HelmHandler struct {
	helm        *helm.Client
	db          *pgxpool.Pool
	engine      *hooks.Engine
	essRelease  string
	configStore *config.Store
	// oidcReloader hot-reloads the auth service after connect-OIDC (set in main).
	oidcReloader func(context.Context) error
	// In-memory log streams for WebSocket consumers
	mu      sync.RWMutex
	streams map[string]*upgradeStream
}

// SetOIDCReloader wires the auth service's hot-reload so connect-OIDC can switch
// MatrixCtrl to OIDC without a restart.
func (h *HelmHandler) SetOIDCReloader(fn func(context.Context) error) { h.oidcReloader = fn }

type upgradeStream struct {
	logs   []string
	status string
	done   bool
	subs   []chan string
	mu     sync.Mutex
}

func NewHelmHandler(helmClient *helm.Client, db *pgxpool.Pool, engine *hooks.Engine, essRelease string, cfgStore *config.Store) *HelmHandler {
	return &HelmHandler{
		helm:        helmClient,
		db:          db,
		engine:      engine,
		essRelease:  essRelease,
		configStore: cfgStore,
		streams:     make(map[string]*upgradeStream),
	}
}

func (h *HelmHandler) GetRelease(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	rel, err := h.helm.GetRelease(name)
	if err != nil {
		Error(w, http.StatusNotFound, err.Error())
		return
	}
	JSON(w, http.StatusOK, rel)
}

func (h *HelmHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	// Helm history
	helmHistory, _ := h.helm.ListHistory(name, 20)

	// MatrixCtrl upgrade history
	rows, err := h.db.Query(r.Context(), `
		SELECT id, from_version, to_version, status, ts_initiated, helm_revision
		FROM upgrade_history WHERE true ORDER BY ts_initiated DESC LIMIT 20`)
	if err != nil {
		JSON(w, http.StatusOK, helmHistory)
		return
	}
	defer rows.Close()

	type entry struct {
		ID           string     `json:"id"`
		FromVersion  string     `json:"from_version"`
		ToVersion    string     `json:"to_version"`
		Status       string     `json:"status"`
		TsInitiated  time.Time  `json:"ts_initiated"`
		HelmRevision *int       `json:"helm_revision,omitempty"`
	}

	var result []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.ID, &e.FromVersion, &e.ToVersion, &e.Status, &e.TsInitiated, &e.HelmRevision); err != nil {
			continue
		}
		result = append(result, e)
	}
	if result == nil {
		result = []entry{}
	}
	JSON(w, http.StatusOK, result)
}

func (h *HelmHandler) Upgrade(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	userID := authmw.UserIDFromContext(r.Context())

	var req struct {
		ToVersion string `json:"to_version"`
		DryRun    bool   `json:"dry_run"`
	}
	if err := Decode(r, &req); err != nil || req.ToVersion == "" {
		Error(w, http.StatusBadRequest, "to_version required")
		return
	}

	// Get current version for history
	fromVersion := ""
	if rel, err := h.helm.GetRelease(name); err == nil {
		fromVersion = rel.ChartVersion
	}

	upgradeID := uuid.New().String()
	stream := &upgradeStream{status: "pending"}
	h.mu.Lock()
	h.streams[upgradeID] = stream
	h.mu.Unlock()

	// Create upgrade_history row
	upgradeUUID := uuid.New()
	_, _ = h.db.Exec(r.Context(), `
		INSERT INTO upgrade_history(id, user_id, from_version, to_version, status)
		VALUES($1, $2, $3, $4, 'pending')`,
		upgradeUUID, userID, fromVersion, req.ToVersion,
	)

	// Run upgrade async
	go func() {
		ctx := context.Background()
		stream.emit("Starting upgrade to " + req.ToVersion + "...")

		_, _ = h.db.Exec(ctx, "UPDATE upgrade_history SET status='running' WHERE id=$1", upgradeUUID)

		// Load merged config values from the config store.
		var values map[string]interface{}
		if h.configStore != nil {
			contents, err := h.configStore.MergedContent(ctx)
			if err != nil {
				stream.emit("WARNING: could not load config values: " + err.Error() + " — upgrading with empty values")
			} else {
				values, err = config.MergeToMap(contents)
				if err != nil {
					stream.emit("WARNING: could not merge config values: " + err.Error() + " — upgrading with empty values")
					values = nil
				} else {
					stream.emit(fmt.Sprintf("Loaded %d config slices from config store.", len(contents)))
				}
			}
		}

		result, err := h.helm.Upgrade(ctx, name, req.ToVersion, values)
		if err != nil {
			stream.emit("ERROR: " + err.Error())
			stream.finish("failed")
			_, _ = h.db.Exec(ctx, `UPDATE upgrade_history SET status='failed', error_message=$1, ts_completed=NOW() WHERE id=$2`,
				err.Error(), upgradeUUID)
			return
		}

		stream.emit(fmt.Sprintf(`{"revision":%s,"status":"%s"}`, intToStr(result.Revision), result.Status))
		stream.emit("Helm upgrade successful (revision " + intToStr(result.Revision) + "). Running post-upgrade hooks...")

		_, _ = h.db.Exec(ctx, "UPDATE upgrade_history SET status='running-hooks', helm_revision=$1 WHERE id=$2",
			result.Revision, upgradeUUID)

		hookRunIDs, hookErr := h.engine.RunTrigger(ctx, hooks.TriggerPostUpgrade, upgradeUUID.String(), userID)
		if hookErr != nil {
			stream.emit("Hook execution error: " + hookErr.Error())
		}

		finalStatus := "success"
		if hookErr != nil {
			finalStatus = "hooks-failed"
		}

		idsJSON, _ := json.Marshal(hookRunIDs)
		_, _ = h.db.Exec(ctx, `
			UPDATE upgrade_history SET status=$1, hooks_run=$2, ts_completed=NOW() WHERE id=$3`,
			finalStatus, idsJSON, upgradeUUID,
		)

		if finalStatus == "success" {
			stream.emit("All post-upgrade hooks completed successfully.")
		} else {
			stream.emit("WARNING: Post-upgrade hooks failed. Check hooks page and re-run manually.")
		}
		stream.finish(finalStatus)
	}()

	JSON(w, http.StatusAccepted, map[string]string{
		"upgrade_id": upgradeID,
		"history_id": upgradeUUID.String(),
	})
}

// ApplyConfig commits the current config to git and runs an in-place helm upgrade
// (same chart version, new merged values). Uses the same stream/WS mechanism as Upgrade.
func (h *HelmHandler) ApplyConfig(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	userID := authmw.UserIDFromContext(r.Context())

	var req struct {
		Message string `json:"message"`
	}
	_ = Decode(r, &req)
	if req.Message == "" {
		req.Message = "config: apply changes via MatrixCtrl"
	}

	rel, err := h.helm.GetRelease(name)
	if err != nil || rel == nil {
		Error(w, http.StatusBadRequest, "could not determine current chart version — is the release deployed?")
		return
	}
	currentVersion := rel.Version // semver only, e.g. "26.5.1"

	upgradeID := uuid.New().String()
	stream := &upgradeStream{status: "pending"}
	h.mu.Lock()
	h.streams[upgradeID] = stream
	h.mu.Unlock()

	upgradeUUID := uuid.New()
	_, _ = h.db.Exec(r.Context(), `
		INSERT INTO upgrade_history(id, user_id, from_version, to_version, status)
		VALUES($1, $2, $3, $4, 'pending')`,
		upgradeUUID, userID, currentVersion, currentVersion,
	)

	commitMsg := req.Message

	go func() {
		ctx := context.Background()

		sha, commitErr := h.configStore.Commit(ctx, commitMsg, userID)
		if commitErr != nil {
			if strings.Contains(commitErr.Error(), "nothing to commit") || strings.Contains(commitErr.Error(), "clean") {
				stream.emit("No config changes to commit — deploying current state.")
			} else {
				stream.emit("WARNING: git commit: " + commitErr.Error())
			}
		} else {
			stream.emit("Config committed to git: " + sha)
		}

		_, _ = h.db.Exec(ctx, "UPDATE upgrade_history SET status='running' WHERE id=$1", upgradeUUID)

		var values map[string]interface{}
		if h.configStore != nil {
			contents, err := h.configStore.MergedContent(ctx)
			if err != nil {
				stream.emit("WARNING: could not load config values: " + err.Error())
			} else {
				values, err = config.MergeToMap(contents)
				if err != nil {
					stream.emit("WARNING: could not merge config values: " + err.Error())
					values = nil
				} else {
					stream.emit(fmt.Sprintf("Loaded %d config slices.", len(contents)))
				}
			}
		}

		stream.emit("Applying config to cluster (version " + currentVersion + ")...")
		result, err := h.helm.Upgrade(ctx, name, currentVersion, values)
		if err != nil {
			stream.emit("ERROR: " + err.Error())
			stream.finish("failed")
			_, _ = h.db.Exec(ctx, `UPDATE upgrade_history SET status='failed', error_message=$1, ts_completed=NOW() WHERE id=$2`,
				err.Error(), upgradeUUID)
			return
		}

		stream.emit(fmt.Sprintf("Helm apply successful (revision %s). Running post-upgrade hooks...", intToStr(result.Revision)))
		_, _ = h.db.Exec(ctx, "UPDATE upgrade_history SET status='running-hooks', helm_revision=$1 WHERE id=$2",
			result.Revision, upgradeUUID)

		hookRunIDs, hookErr := h.engine.RunTrigger(ctx, hooks.TriggerPostUpgrade, upgradeUUID.String(), userID)
		if hookErr != nil {
			stream.emit("Hook execution error: " + hookErr.Error())
		}

		finalStatus := "success"
		if hookErr != nil {
			finalStatus = "hooks-failed"
		}

		idsJSON, _ := json.Marshal(hookRunIDs)
		_, _ = h.db.Exec(ctx, `
			UPDATE upgrade_history SET status=$1, hooks_run=$2, ts_completed=NOW() WHERE id=$3`,
			finalStatus, idsJSON, upgradeUUID)

		if finalStatus == "success" {
			stream.emit("Config deployed successfully.")
		} else {
			stream.emit("WARNING: Post-upgrade hooks failed. Check the Hooks page.")
		}
		stream.finish(finalStatus)
	}()

	JSON(w, http.StatusAccepted, map[string]string{
		"upgrade_id": upgradeID,
		"history_id": upgradeUUID.String(),
	})
}

// DeployESS performs a greenfield ESS install (Phase 1.5): seed the config from
// the chart's commented defaults, apply server name + derived hostnames, then
// helm install. Refuses if a release already exists. Streams progress like Upgrade.
func (h *HelmHandler) DeployESS(w http.ResponseWriter, r *http.Request) {
	userID := authmw.UserIDFromContext(r.Context())
	var req struct {
		Version    string `json:"version"`
		ServerName string `json:"server_name"`
	}
	if err := Decode(r, &req); err != nil || req.Version == "" || req.ServerName == "" {
		Error(w, http.StatusBadRequest, "version and server_name are required")
		return
	}

	// Guard: never clobber an existing release.
	if rel, err := h.helm.GetRelease(h.essRelease); err == nil && rel != nil {
		Error(w, http.StatusConflict, "release '"+h.essRelease+"' already exists — use Upgrade, not Deploy")
		return
	}

	upgradeID := uuid.New().String()
	stream := &upgradeStream{status: "pending"}
	h.mu.Lock()
	h.streams[upgradeID] = stream
	h.mu.Unlock()

	sn := req.ServerName
	version := req.Version

	go func() {
		ctx := context.Background()

		stream.emit("Pulling ESS chart " + version + " for default config…")
		values, err := h.helm.DefaultChartValues(version)
		if err != nil {
			stream.emit("ERROR: " + err.Error())
			stream.finish("failed")
			return
		}

		stream.emit("Seeding per-section config from chart defaults…")
		if err := h.configStore.SeedSections(ctx, values, false); err != nil {
			stream.emit("ERROR: seed config: " + err.Error())
			stream.finish("failed")
			return
		}

		// Server name + conventional component hostnames derived from it.
		changes := map[string]interface{}{
			"serverName":                               sn,
			"synapse.ingress.host":                     "matrix." + sn,
			"matrixAuthenticationService.ingress.host": "mas." + sn,
			"elementWeb.ingress.host":                  "element." + sn,
			"elementAdmin.ingress.host":                "admin." + sn,
			"matrixRTC.ingress.host":                   "mrtc." + sn,
			"wellKnownDelegation.ingress.host":         sn,
		}
		if err := h.configStore.SetSectionValues(ctx, changes, nil); err != nil {
			stream.emit("WARNING: could not apply hostnames: " + err.Error())
		}
		if _, err := h.configStore.Commit(ctx, "config: greenfield seed for "+sn, userID); err != nil {
			stream.emit("WARNING: git commit: " + err.Error())
		}
		stream.emit("Server name set to " + sn + " with derived hostnames.")

		contents, _ := h.configStore.MergedContent(ctx)
		merged, err := config.MergeToMap(contents)
		if err != nil {
			stream.emit("ERROR: merge config: " + err.Error())
			stream.finish("failed")
			return
		}

		stream.emit("Installing ESS " + version + " — this can take several minutes…")
		result, err := h.helm.Install(ctx, h.essRelease, version, merged)
		if err != nil {
			stream.emit("ERROR: " + err.Error())
			stream.finish("failed")
			return
		}

		stream.emit(fmt.Sprintf("ESS installed (revision %s). Running post-install hooks…", intToStr(result.Revision)))
		_, hookErr := h.engine.RunTrigger(ctx, hooks.TriggerPostUpgrade, "deploy:"+h.essRelease, userID)
		finalStatus := "success"
		if hookErr != nil {
			finalStatus = "hooks-failed"
			stream.emit("WARNING: post-install hooks failed: " + hookErr.Error())
		} else {
			stream.emit("ESS deployed successfully. Configure Matrix login under Setup once MAS is up.")
		}
		stream.finish(finalStatus)
	}()

	JSON(w, http.StatusAccepted, map[string]string{"upgrade_id": upgradeID})
}

// ConnectOIDC registers MatrixCtrl's own OIDC client in MAS — via the config it
// already manages (writes the client + admin_clients into the
// matrixAuthenticationService section, then helm-upgrades ESS so MAS picks it up),
// stores the OIDC settings in the DB, and hot-reloads auth into OIDC mode. This
// closes the bootstrap→OIDC loop without manual MAS patching or a restart.
func (h *HelmHandler) ConnectOIDC(w http.ResponseWriter, r *http.Request) {
	userID := authmw.UserIDFromContext(r.Context())
	var req struct {
		Issuer    string `json:"issuer"`     // MAS public URL, e.g. https://mas-matrix.example.com
		PublicURL string `json:"public_url"` // MatrixCtrl public base, e.g. https://matrixctrl.example.com
	}
	if err := Decode(r, &req); err != nil || req.Issuer == "" || req.PublicURL == "" {
		Error(w, http.StatusBadRequest, "issuer and public_url are required")
		return
	}

	// Idempotency guard: refuse if a MatrixCtrl client is already registered.
	if contents, err := h.configStore.MergedContent(r.Context()); err == nil {
		if merged, err := config.MergeToMap(contents); err == nil {
			if nestedGet(merged, "matrixAuthenticationService", "additional", "0-matrixctrl-client") != nil {
				Error(w, http.StatusConflict, "a MatrixCtrl OIDC client is already registered in MAS config")
				return
			}
		}
	}

	clientID := auth.GenerateULID()
	secret := auth.GenerateSecret()
	issuer := strings.TrimRight(req.Issuer, "/")
	redirect := strings.TrimRight(req.PublicURL, "/") + "/api/v1/auth/oidc/callback"
	fragment := buildMASClientConfig(clientID, secret, redirect)

	// Write the client into the matrixAuthenticationService section (comment-preserving).
	changes := map[string]interface{}{
		"matrixAuthenticationService.additional.0-matrixctrl-client.config": fragment,
	}
	if err := h.configStore.SetSectionValues(r.Context(), changes, nil); err != nil {
		Error(w, http.StatusInternalServerError, "write MAS client config: "+err.Error())
		return
	}
	if _, err := h.configStore.Commit(r.Context(), "config: register MatrixCtrl OIDC client in MAS", userID); err != nil {
		// non-fatal
		_ = err
	}
	// Persist OIDC settings so MatrixCtrl can use them after reload.
	if err := auth.SaveOIDCConfig(r.Context(), h.db, auth.OIDCConfig{
		Issuer: issuer, ClientID: clientID, ClientSecret: secret, RedirectURI: redirect,
	}); err != nil {
		Error(w, http.StatusInternalServerError, "save oidc config: "+err.Error())
		return
	}

	upgradeID := uuid.New().String()
	stream := &upgradeStream{status: "pending"}
	h.mu.Lock()
	h.streams[upgradeID] = stream
	h.mu.Unlock()

	go func() {
		ctx := context.Background()
		stream.emit("MatrixCtrl client written into MAS config (client_id=" + clientID + ").")

		rel, err := h.helm.GetRelease(h.essRelease)
		if err != nil || rel == nil {
			stream.emit("ERROR: ESS release not found — deploy ESS first.")
			stream.finish("failed")
			return
		}
		contents, _ := h.configStore.MergedContent(ctx)
		merged, _ := config.MergeToMap(contents)

		stream.emit("Upgrading ESS so MAS loads the new client (this restarts MAS)…")
		if _, err := h.helm.Upgrade(ctx, h.essRelease, rel.Version, merged); err != nil {
			stream.emit("ERROR: helm upgrade: " + err.Error())
			stream.finish("failed")
			return
		}

		stream.emit("Waiting for MAS to come back up with the client…")
		var reloadErr error
		for i := 0; i < 12; i++ {
			time.Sleep(5 * time.Second)
			if h.oidcReloader == nil {
				break
			}
			if reloadErr = h.oidcReloader(ctx); reloadErr == nil {
				break
			}
			stream.emit("  …MAS not ready yet, retrying")
		}
		if reloadErr != nil {
			stream.emit("WARNING: client registered but OIDC reload failed: " + reloadErr.Error())
			stream.emit("Reload manually from Setup once MAS is ready.")
			stream.finish("hooks-failed")
			return
		}

		stream.emit("Matrix login connected. Log out and back in via Matrix.")
		stream.finish("success")
	}()

	JSON(w, http.StatusAccepted, map[string]string{"upgrade_id": upgradeID, "client_id": clientID})
}

// buildMASClientConfig renders the inner MAS config fragment (a string the ESS
// chart embeds verbatim) registering a static client + granting it admin.
func buildMASClientConfig(clientID, secret, redirect string) string {
	return fmt.Sprintf(`clients:
  - client_id: "%s"
    client_auth_method: client_secret_basic
    client_secret: "%s"
    redirect_uris:
      - "%s"
policy:
  data:
    admin_clients:
      - "%s"
`, clientID, secret, redirect, clientID)
}

// nestedGet walks a decoded map by keys, returning nil if any step is missing.
func nestedGet(m map[string]interface{}, keys ...string) interface{} {
	var cur interface{} = m
	for _, k := range keys {
		asMap, ok := cur.(map[string]interface{})
		if !ok {
			return nil
		}
		cur, ok = asMap[k]
		if !ok {
			return nil
		}
	}
	return cur
}

func (h *HelmHandler) Rollback(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var req struct {
		Revision int `json:"revision"`
	}
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := h.helm.Rollback(name, req.Revision); err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *HelmHandler) GetUpgradeStatus(w http.ResponseWriter, r *http.Request) {
	upgradeID := chi.URLParam(r, "upgradeId")
	h.mu.RLock()
	stream := h.streams[upgradeID]
	h.mu.RUnlock()

	if stream == nil {
		Error(w, http.StatusNotFound, "upgrade not found")
		return
	}

	stream.mu.Lock()
	defer stream.mu.Unlock()
	JSON(w, http.StatusOK, map[string]interface{}{
		"status": stream.status,
		"logs":   stream.logs,
		"done":   stream.done,
	})
}

func (h *HelmHandler) ListVersions(w http.ResponseWriter, r *http.Request) {
	versions, err := helm.ListVersions(r.Context())
	if err != nil {
		// Return cached versions from DB as fallback
		rows, _ := h.db.Query(r.Context(), "SELECT version, published_at FROM ess_versions ORDER BY discovered_at DESC LIMIT 20")
		if rows != nil {
			defer rows.Close()
			type v struct {
				Version     string     `json:"version"`
				PublishedAt *time.Time `json:"published_at,omitempty"`
			}
			var result []v
			for rows.Next() {
				var ve v
				_ = rows.Scan(&ve.Version, &ve.PublishedAt)
				result = append(result, ve)
			}
			JSON(w, http.StatusOK, result)
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Cache to DB
	for _, v := range versions {
		_, _ = h.db.Exec(r.Context(),
			"INSERT INTO ess_versions(version) VALUES($1) ON CONFLICT DO NOTHING", v.Version)
	}

	JSON(w, http.StatusOK, versions)
}

func (s *upgradeStream) emit(msg interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var line string
	switch v := msg.(type) {
	case string:
		line = v
	default:
		b, _ := json.Marshal(v)
		line = string(b)
	}
	s.logs = append(s.logs, line)
	for _, sub := range s.subs {
		select {
		case sub <- line:
		default:
		}
	}
}

func (s *upgradeStream) finish(status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
	s.done = true
	for _, sub := range s.subs {
		close(sub)
	}
	s.subs = nil
}

func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	result := ""
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		result = string(rune('0'+n%10)) + result
		n /= 10
	}
	if neg {
		result = "-" + result
	}
	return result
}
