// Helm release state: what is deployed, its history, and moving between
// revisions. Upgrades live in helm_upgrade.go, first-time setup in helm_setup.go,
// and the live log stream in helm_stream.go.
package handlers

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bxnnyg/matrixctrl/internal/config"
	"github.com/bxnnyg/matrixctrl/internal/helm"
	"github.com/bxnnyg/matrixctrl/internal/hooks"
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
		ID           string    `json:"id"`
		FromVersion  string    `json:"from_version"`
		ToVersion    string    `json:"to_version"`
		Status       string    `json:"status"`
		TsInitiated  time.Time `json:"ts_initiated"`
		HelmRevision *int      `json:"helm_revision,omitempty"`
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
