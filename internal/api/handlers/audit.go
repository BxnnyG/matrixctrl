package handlers

import (
	"net/http"
	"strconv"

	"github.com/bxnnyg/matrixctrl/internal/audit"
)

type AuditHandler struct {
	store *audit.Store
}

func NewAuditHandler(store *audit.Store) *AuditHandler {
	return &AuditHandler{store: store}
}

// List returns audit entries newest-first.
//
//	GET /api/v1/audit?user=&result=&before=<id>&limit=
//
// `before` is the id of the oldest entry already shown — keyset pagination, so
// pages stay stable while new entries arrive at the head.
func (h *AuditHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
	limit, _ := strconv.Atoi(q.Get("limit"))

	entries, err := h.store.List(r.Context(), audit.Filter{
		UserID: q.Get("user"),
		Result: q.Get("result"),
		Before: before,
		Limit:  limit,
	})
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	// next_before is nil at the end of the list, so the UI does not have to
	// infer "no more pages" from a short page — which is wrong exactly when the
	// last page happens to be full.
	var next *int64
	if len(entries) > 0 {
		last := entries[len(entries)-1].ID
		next = &last
	}

	JSON(w, http.StatusOK, map[string]any{
		"entries":     entries,
		"next_before": next,
	})
}
