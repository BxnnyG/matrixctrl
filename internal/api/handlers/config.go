package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/bxnny/matrixctrl/internal/config"
	gitpkg "github.com/bxnny/matrixctrl/internal/git"
)

type ConfigHandler struct {
	store *config.Store
	git   *gitpkg.Repo
}

func NewConfigHandler(store *config.Store, git *gitpkg.Repo) *ConfigHandler {
	return &ConfigHandler{store: store, git: git}
}

// GET /api/v1/config/slices
func (h *ConfigHandler) ListSlices(w http.ResponseWriter, r *http.Request) {
	slices, err := h.store.List(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	type sliceListItem struct {
		Name        string `json:"name"`
		File        string `json:"file"`
		Description string `json:"description,omitempty"`
		Lines       int    `json:"lines"`
	}
	items := make([]sliceListItem, len(slices))
	for i, s := range slices {
		items[i] = sliceListItem{
			Name:        s.Name,
			File:        s.File,
			Description: s.Description,
			Lines:       countLines(s.Content),
		}
	}
	JSON(w, http.StatusOK, items)
}

// GET /api/v1/config/slices/{name}
func (h *ConfigHandler) GetSlice(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	sl, err := h.store.Get(r.Context(), name)
	if err != nil {
		Error(w, http.StatusNotFound, err.Error())
		return
	}
	JSON(w, http.StatusOK, sl)
}

// PUT /api/v1/config/slices/{name}
func (h *ConfigHandler) PutSlice(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.store.Put(r.Context(), name, req.Content); err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GET /api/v1/config/merged
func (h *ConfigHandler) GetMerged(w http.ResponseWriter, r *http.Request) {
	contents, err := h.store.MergedContent(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	merged, err := config.Merge(contents)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"yaml": merged})
}

// POST /api/v1/config/validate
func (h *ConfigHandler) Validate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// For now just check YAML syntax; full schema validation is Phase 1b.
	var doc interface{}
	if err := json.Unmarshal([]byte(req.Content), &doc); err != nil {
		JSON(w, http.StatusOK, map[string]interface{}{"valid": false, "errors": []string{"invalid YAML syntax"}})
		return
	}
	JSON(w, http.StatusOK, map[string]interface{}{"valid": true, "errors": nil})
}

// GET /api/v1/config/diff
func (h *ConfigHandler) GetDiff(w http.ResponseWriter, r *http.Request) {
	diff, err := h.store.Diff()
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"diff": diff})
}

// POST /api/v1/config/apply — commit staged changes
func (h *ConfigHandler) Apply(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Message == "" {
		req.Message = "config: apply changes via MatrixCtrl"
	}
	sha, err := h.store.Commit(r.Context(), req.Message, "admin")
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"sha": sha, "status": "committed"})
}

// GET /api/v1/config/history
func (h *ConfigHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
	commits, err := h.git.Log(50)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if commits == nil {
		commits = []gitpkg.CommitInfo{}
	}
	JSON(w, http.StatusOK, commits)
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	n := 1
	for _, c := range s {
		if c == '\n' {
			n++
		}
	}
	return n
}
