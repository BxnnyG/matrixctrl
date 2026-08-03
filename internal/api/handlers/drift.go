package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bxnnyg/matrixctrl/internal/drift"
	"github.com/bxnnyg/matrixctrl/internal/hooks"
	"github.com/bxnnyg/matrixctrl/internal/k8s"
)

type DriftHandler struct {
	db  *pgxpool.Pool
	k8s *k8s.Client
}

func NewDriftHandler(db *pgxpool.Pool, k8sClient *k8s.Client) *DriftHandler {
	return &DriftHandler{db: db, k8s: k8sClient}
}

// Status answers "are the hooks' patches still in effect?" — see internal/drift for
// why "enabled" and "in effect" are different questions, and what it cost to learn
// that.
func (h *DriftHandler) Status(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	actions, err := h.patchActions(ctx)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	// A nil client must reach drift.Check as a nil Fetcher, not as a non-nil
	// interface wrapping a nil pointer — the latter would satisfy `f != nil` and
	// panic on the first call.
	var fetcher drift.Fetcher
	if h.k8s != nil {
		fetcher = h.k8s
	}

	findings := drift.Check(ctx, actions, fetcher)
	counts := drift.Summary(findings)

	JSON(w, http.StatusOK, map[string]any{
		"findings":  findings,
		"satisfied": counts[drift.Satisfied],
		"drifted":   counts[drift.Drifted],
		"unknown":   counts[drift.Unknown],
	})
}

// patchActions flattens every enabled hook's kubectl_patch actions. Disabled hooks
// are skipped: their patches are not supposed to be in effect, so reporting them as
// drift would be reporting the operator's own decision back at them as a fault.
func (h *DriftHandler) patchActions(ctx context.Context) ([]drift.Action, error) {
	rows, err := h.db.Query(ctx,
		`SELECT name, actions FROM hooks WHERE enabled = TRUE ORDER BY priority, created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []drift.Action
	for rows.Next() {
		var name string
		var raw []byte
		if err := rows.Scan(&name, &raw); err != nil {
			return nil, err
		}

		var actions []hooks.HookAction
		if err := json.Unmarshal(raw, &actions); err != nil {
			// One unreadable hook must not blank the whole report — the other
			// hooks' answers are still worth having.
			continue
		}

		for _, a := range actions {
			if a.Type != hooks.ActionKubectlPatch {
				continue // wait_rollout and friends assert nothing about state
			}
			out = append(out, drift.Action{
				Hook:        name,
				Description: a.Description,
				Resource:    a.Resource,
				Namespace:   a.Namespace,
				Name:        a.Name,
				PatchType:   a.PatchType,
				Patch:       a.Patch,
			})
		}
	}
	return out, rows.Err()
}
