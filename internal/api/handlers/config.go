package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/bxnny/matrixctrl/internal/config"
	cfgschema "github.com/bxnny/matrixctrl/internal/config/schema"
	gitpkg "github.com/bxnny/matrixctrl/internal/git"
)

type ConfigHandler struct {
	store      *config.Store
	git        *gitpkg.Repo
	essVersion string // current deployed ESS version for schema selection
}

func NewConfigHandler(store *config.Store, git *gitpkg.Repo, essVersion string) *ConfigHandler {
	return &ConfigHandler{store: store, git: git, essVersion: essVersion}
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

// POST /api/v1/config/validate — validates YAML syntax of a single config slice.
// Full schema validation is done via /api/v1/config/validate-merged (validates the merged result).
func (h *ConfigHandler) Validate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := config.ParseYAML(req.Content); err != nil {
		JSON(w, http.StatusOK, map[string]interface{}{
			"valid":  false,
			"errors": []map[string]string{{"field": "(root)", "message": err.Error()}},
		})
		return
	}
	JSON(w, http.StatusOK, map[string]interface{}{"valid": true, "errors": nil})
}

// POST /api/v1/config/validate-merged — merges all slices and validates against JSON Schema.
func (h *ConfigHandler) ValidateMerged(w http.ResponseWriter, r *http.Request) {
	contents, err := h.store.MergedContent(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	merged, err := config.Merge(contents)
	if err != nil {
		JSON(w, http.StatusOK, map[string]interface{}{
			"valid":  false,
			"errors": []map[string]string{{"field": "(root)", "message": err.Error()}},
		})
		return
	}

	schemaData, schemaErr := cfgschema.Get(h.essVersion)
	if schemaErr != nil {
		// No schema — just confirm YAML is syntactically valid
		JSON(w, http.StatusOK, map[string]interface{}{"valid": true, "errors": nil, "note": "no schema available"})
		return
	}

	errs, err := config.ValidateYAMLWithSchema(merged, schemaData)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	apiErrs := make([]map[string]string, len(errs))
	for i, e := range errs {
		apiErrs[i] = map[string]string{"field": e.Field, "message": e.Message}
	}
	JSON(w, http.StatusOK, map[string]interface{}{
		"valid":  len(errs) == 0,
		"errors": apiErrs,
	})
}

// GET /api/v1/config/easy — returns the Easy-Mode field registry plus the current
// value of each field (read from the merged config).
func (h *ConfigHandler) GetEasy(w http.ResponseWriter, r *http.Request) {
	contents, err := h.store.MergedContent(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	merged, err := config.MergeToMap(contents)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]interface{}{
		"fields": config.EasyFields,
		"values": config.GetEasyValues(merged),
	})
}

// POST /api/v1/config/easy — writes the Easy-Mode overlay slice from submitted
// {path: value} pairs. Does not commit or deploy; the UI drives those separately.
func (h *ConfigHandler) PutEasy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Values map[string]interface{} `json:"values"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	overlay, err := config.EasyOverlayYAML(req.Values)
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.store.WriteEasyOverlay(overlay); err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "ok", "yaml": overlay})
}

// GET /api/v1/config/settings — everything the schema-driven settings UI needs:
// the ESS JSON Schema (structure/types/enums), the current merged values, per-path
// help text extracted from the commented template, and the current overlay.
func (h *ConfigHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	contents, err := h.store.MergedContent(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	merged, err := config.MergeToMap(contents)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Help text: extract `##` comments from every section file (they carry the docs).
	comments := map[string]string{}
	if slices, err := h.store.List(r.Context()); err == nil {
		for _, sl := range slices {
			for k, v := range config.ExtractComments(sl.Content) {
				if _, ok := comments[k]; !ok {
					comments[k] = v
				}
			}
		}
	}

	// top-level key → owning section file, so the UI can deep-link to YAML mode.
	files, _ := h.store.SectionFileMap(r.Context())

	resp := map[string]interface{}{
		"values":   merged,
		"comments": comments,
		"files":    files,
	}
	if schemaData, err := cfgschema.Get(h.essVersion); err == nil {
		resp["schema"] = json.RawMessage(schemaData)
	}
	JSON(w, http.StatusOK, resp)
}

// POST /api/v1/config/settings — apply form edits (path→value + removals) directly
// to the owning section files, preserving comments. No commit (UI commits/deploys).
func (h *ConfigHandler) PutSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Changes  map[string]interface{} `json:"changes"`
		Removals []string               `json:"removals"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.store.SetSectionValues(r.Context(), req.Changes, req.Removals); err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GET /api/v1/config/schema — returns the ESS values JSON Schema for the current version
func (h *ConfigHandler) GetSchema(w http.ResponseWriter, r *http.Request) {
	data, err := cfgschema.Get(h.essVersion)
	if err != nil {
		// Return minimal schema if not found
		JSON(w, http.StatusOK, map[string]interface{}{
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type":    "object",
		})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
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

// GET /api/v1/config/history/{sha}/diff
func (h *ConfigHandler) GetCommitDiff(w http.ResponseWriter, r *http.Request) {
	sha := chi.URLParam(r, "sha")
	diff, err := h.git.DiffAtCommit(sha)
	if err != nil {
		Error(w, http.StatusNotFound, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"diff": diff})
}

// POST /api/v1/config/history/{sha}/rollback — hard-reset working tree to commit
func (h *ConfigHandler) RollbackToCommit(w http.ResponseWriter, r *http.Request) {
	sha := chi.URLParam(r, "sha")
	if err := h.git.ResetToCommit(sha); err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Invalidate any cached state by re-reading from disk.
	JSON(w, http.StatusOK, map[string]string{"sha": sha, "status": "rolled back"})
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
