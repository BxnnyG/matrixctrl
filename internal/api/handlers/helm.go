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
	"github.com/bxnnyg/matrixctrl/internal/imagepin"
	"github.com/bxnnyg/matrixctrl/internal/k8s"
	"github.com/bxnnyg/matrixctrl/internal/rollout"

	"gopkg.in/yaml.v3"
)

type HelmHandler struct {
	helm        *helm.Client
	db          *pgxpool.Pool
	engine      *hooks.Engine
	essRelease  string
	configStore *config.Store
	// k8s and essNS let the progress ticker say *what* a rollout is waiting for
	// rather than only how long it has been waiting (E31). Both may be zero: the
	// probe is additive and its absence costs detail, not correctness.
	k8s   *k8s.Client
	essNS string
	// oidcReloader hot-reloads the auth service after connect-OIDC (set in main).
	oidcReloader func(context.Context) error
	// In-memory log streams for WebSocket consumers
	mu      sync.RWMutex
	streams map[string]*upgradeStream
}

// SetOIDCReloader wires the auth service's hot-reload so connect-OIDC can switch
// MatrixCtrl to OIDC without a restart.
func (h *HelmHandler) SetOIDCReloader(fn func(context.Context) error) { h.oidcReloader = fn }

func NewHelmHandler(helmClient *helm.Client, db *pgxpool.Pool, engine *hooks.Engine, essRelease string, cfgStore *config.Store, k8sClient *k8s.Client, essNS string) *HelmHandler {
	return &HelmHandler{
		helm:        helmClient,
		db:          db,
		engine:      engine,
		essRelease:  essRelease,
		configStore: cfgStore,
		k8s:         k8sClient,
		essNS:       essNS,
		streams:     make(map[string]*upgradeStream),
	}
}

// pinnedTagWarning compares the image tags the config holds against the ones the
// target chart ships, and returns a line when the config is behind.
//
// Silent on every failure. This runs on the path of a live upgrade, and a
// diagnostic that can stop a deploy — because a chart could not be pulled, or a
// values map had an unexpected shape — is a worse defect than the one it reports.
func (h *HelmHandler) pinnedTagWarning(ctx context.Context, toVersion string, values map[string]interface{}) string {
	if h.helm == nil || toVersion == "" || len(values) == 0 {
		return ""
	}

	raw, err := h.helm.DefaultChartValues(toVersion)
	if err != nil {
		return ""
	}
	var chartValues map[string]interface{}
	if err := yaml.Unmarshal([]byte(raw), &chartValues); err != nil {
		return ""
	}

	return imagepin.Describe(imagepin.Compare(
		imagepin.ExtractTags(values),
		imagepin.ExtractTags(chartValues),
	))
}

// rolloutProbe builds the function the progress ticker calls to describe what the
// rollout is stuck on. Returns nil when there is no cluster connection, which the
// ticker treats as "no extra detail" rather than as an error.
func (h *HelmHandler) rolloutProbe(ctx context.Context) probeFunc {
	if h.k8s == nil || h.essNS == "" {
		return nil
	}
	return func() string {
		pods := h.k8s.RolloutState(ctx, h.essNS)
		states := make([]rollout.PodState, 0, len(pods))
		for _, p := range pods {
			cs := make([]rollout.ContainerState, 0, len(p.Containers))
			for _, c := range p.Containers {
				cs = append(cs, rollout.ContainerState{
					Name: c.Name, Init: c.Init, Waiting: c.Waiting,
					LastExitCode: c.LastExitCode, Terminated: c.Terminated, Message: c.Message,
				})
			}
			states = append(states, rollout.PodState{
				Name: p.Name, Ready: p.Ready, Phase: p.Phase, Containers: cs,
			})
		}
		return rollout.Describe(rollout.Assess(states))
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

// ReleaseNotes answers GET /api/v1/helm/versions/{version}/notes.
//
// Always 200 when the version is well-formed: a page that shows the notes should
// still render when they cannot be fetched, and "not available, because …" is a
// different thing to tell the operator than an error box.
func (h *HelmHandler) ReleaseNotes(w http.ResponseWriter, r *http.Request) {
	version := chi.URLParam(r, "version")
	notes, err := helm.FetchReleaseNotes(r.Context(), version)
	if err != nil {
		Error(w, http.StatusBadRequest, "ungültige Version")
		return
	}
	JSON(w, http.StatusOK, notes)
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
