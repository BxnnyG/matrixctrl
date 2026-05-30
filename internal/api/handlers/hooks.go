package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	authmw "github.com/bxnnyg/matrixctrl/internal/api/middleware"
	"github.com/bxnnyg/matrixctrl/internal/hooks"
)

type HooksHandler struct {
	db     *pgxpool.Pool
	engine *hooks.Engine
}

func NewHooksHandler(db *pgxpool.Pool, engine *hooks.Engine) *HooksHandler {
	return &HooksHandler{db: db, engine: engine}
}

func (h *HooksHandler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(r.Context(), `
		SELECT hk.id, hk.name, hk.description, hk.trigger, hk.enabled, hk.priority,
		       hk.actions, hk.builtin, hk.created_at, hk.updated_at, hk.created_by,
		       rl.status as last_run_status
		FROM hooks hk
		LEFT JOIN LATERAL (
			SELECT status FROM hook_run_log
			WHERE hook_id = hk.id ORDER BY ts_start DESC LIMIT 1
		) rl ON TRUE
		ORDER BY hk.priority, hk.created_at`)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	type hookRow struct {
		hooks.Hook
		LastStatus *string
	}

	var result []map[string]interface{}
	for rows.Next() {
		var hk hooks.Hook
		var actionsJSON []byte
		var lastStatus *string
		if err := rows.Scan(&hk.ID, &hk.Name, &hk.Description, &hk.Trigger, &hk.Enabled,
			&hk.Priority, &actionsJSON, &hk.Builtin, &hk.CreatedAt, &hk.UpdatedAt, &hk.CreatedBy,
			&lastStatus); err != nil {
			continue
		}
		_ = json.Unmarshal(actionsJSON, &hk.Actions)
		if lastStatus != nil {
			hk.LastRunStatus = *lastStatus
		}
		result = append(result, hookToMap(hk))
	}
	if result == nil {
		result = []map[string]interface{}{}
	}
	JSON(w, http.StatusOK, result)
}

func (h *HooksHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid hook id")
		return
	}

	var hk hooks.Hook
	var actionsJSON []byte
	err = h.db.QueryRow(r.Context(), `
		SELECT id, name, description, trigger, enabled, priority, actions, builtin, created_at, updated_at, created_by
		FROM hooks WHERE id=$1`, id).
		Scan(&hk.ID, &hk.Name, &hk.Description, &hk.Trigger, &hk.Enabled,
			&hk.Priority, &actionsJSON, &hk.Builtin, &hk.CreatedAt, &hk.UpdatedAt, &hk.CreatedBy)
	if err != nil {
		Error(w, http.StatusNotFound, "hook not found")
		return
	}
	_ = json.Unmarshal(actionsJSON, &hk.Actions)
	JSON(w, http.StatusOK, hookToMap(hk))
}

func (h *HooksHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req hooks.Hook
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request")
		return
	}

	actionsJSON, _ := json.Marshal(req.Actions)
	userID := authmw.UserIDFromContext(r.Context())
	id := uuid.New()

	_, err := h.db.Exec(r.Context(), `
		INSERT INTO hooks(id, name, description, trigger, enabled, priority, actions, created_by)
		VALUES($1, $2, $3, $4, $5, $6, $7, $8)`,
		id, req.Name, req.Description, req.Trigger, req.Enabled, req.Priority, actionsJSON, userID,
	)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	req.ID = id
	JSON(w, http.StatusCreated, hookToMap(req))
}

func (h *HooksHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid hook id")
		return
	}

	var req hooks.Hook
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request")
		return
	}

	actionsJSON, _ := json.Marshal(req.Actions)
	_, err = h.db.Exec(r.Context(), `
		UPDATE hooks SET name=$1, description=$2, enabled=$3, priority=$4, actions=$5, updated_at=NOW()
		WHERE id=$6 AND builtin=FALSE`,
		req.Name, req.Description, req.Enabled, req.Priority, actionsJSON, id,
	)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *HooksHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid hook id")
		return
	}

	var builtin bool
	err = h.db.QueryRow(r.Context(), "SELECT builtin FROM hooks WHERE id=$1", id).Scan(&builtin)
	if err != nil {
		Error(w, http.StatusNotFound, "hook not found")
		return
	}
	if builtin {
		Error(w, http.StatusForbidden, "cannot delete built-in hooks; disable them instead")
		return
	}

	_, err = h.db.Exec(r.Context(), "DELETE FROM hooks WHERE id=$1", id)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *HooksHandler) Trigger(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid hook id")
		return
	}

	userID := authmw.UserIDFromContext(r.Context())
	runID, err := h.engine.RunHookByID(context.Background(), id, userID)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	JSON(w, http.StatusOK, map[string]string{"run_id": runID.String()})
}

func (h *HooksHandler) ListRuns(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid hook id")
		return
	}

	rows, err := h.db.Query(r.Context(), `
		SELECT id, hook_id, ts_start, ts_end, trigger_type, trigger_ref, status, action_results, triggered_by
		FROM hook_run_log WHERE hook_id=$1 ORDER BY ts_start DESC LIMIT 50`, id)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var result []hooks.HookRun
	for rows.Next() {
		var run hooks.HookRun
		var resultsJSON []byte
		if err := rows.Scan(&run.ID, &run.HookID, &run.TsStart, &run.TsEnd,
			&run.TriggerType, &run.TriggerRef, &run.Status, &resultsJSON, &run.TriggeredBy); err != nil {
			continue
		}
		_ = json.Unmarshal(resultsJSON, &run.ActionResults)
		result = append(result, run)
	}
	if result == nil {
		result = []hooks.HookRun{}
	}
	JSON(w, http.StatusOK, result)
}

func (h *HooksHandler) GetRun(w http.ResponseWriter, r *http.Request) {
	runID, err := uuid.Parse(chi.URLParam(r, "runId"))
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid run id")
		return
	}

	var run hooks.HookRun
	var resultsJSON []byte
	err = h.db.QueryRow(r.Context(), `
		SELECT id, hook_id, ts_start, ts_end, trigger_type, trigger_ref, status, action_results, triggered_by
		FROM hook_run_log WHERE id=$1`, runID).
		Scan(&run.ID, &run.HookID, &run.TsStart, &run.TsEnd,
			&run.TriggerType, &run.TriggerRef, &run.Status, &resultsJSON, &run.TriggeredBy)
	if err != nil {
		Error(w, http.StatusNotFound, "run not found")
		return
	}
	_ = json.Unmarshal(resultsJSON, &run.ActionResults)
	JSON(w, http.StatusOK, run)
}

func hookToMap(h hooks.Hook) map[string]interface{} {
	return map[string]interface{}{
		"id":             h.ID,
		"name":           h.Name,
		"description":    h.Description,
		"trigger":        h.Trigger,
		"enabled":        h.Enabled,
		"priority":       h.Priority,
		"actions":        h.Actions,
		"builtin":        h.Builtin,
		"created_at":     h.CreatedAt,
		"updated_at":     h.UpdatedAt,
		"created_by":     h.CreatedBy,
		"lastRunStatus":  h.LastRunStatus,
	}
}
