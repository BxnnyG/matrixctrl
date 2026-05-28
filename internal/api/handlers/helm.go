package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	authmw "github.com/bxnny/matrixctrl/internal/api/middleware"
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
	// In-memory log streams for WebSocket consumers
	mu      sync.RWMutex
	streams map[string]*upgradeStream
}

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
