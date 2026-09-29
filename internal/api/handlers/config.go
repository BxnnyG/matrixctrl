package handlers

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"path/filepath"

	"github.com/go-chi/chi/v5"

	"github.com/bxnnyg/matrixctrl/internal/config"
	cfgschema "github.com/bxnnyg/matrixctrl/internal/config/schema"
	gitpkg "github.com/bxnnyg/matrixctrl/internal/git"
	"github.com/bxnnyg/matrixctrl/internal/tasks"
)

type ConfigHandler struct {
	store      *config.Store
	git        *gitpkg.Repo
	essVersion string // ESS version read at startup — only the fallback's key now
	// deployedSchema reads the schema from the chart that is actually running
	// (etappe 107). Nil outside a cluster, and then the embedded schema is used —
	// visibly, see schemaFor.
	deployedSchema func() ([]byte, string, error)
	// repoPath and seedPath answer "where does my configuration live" — a question the
	// panel could not answer at all until etappe 71.
	repoPath string
	seedPath string
}

func NewConfigHandler(store *config.Store, git *gitpkg.Repo, essVersion, repoPath, seedPath string) *ConfigHandler {
	return &ConfigHandler{store: store, git: git, essVersion: essVersion, repoPath: repoPath, seedPath: seedPath}
}

// SetDeployedSchema wires the reader for the running chart's schema. Separate from the
// constructor so a handler without cluster access stays a valid handler.
func (h *ConfigHandler) SetDeployedSchema(f func() ([]byte, string, error)) { h.deployedSchema = f }

// schemaSource says where the schema a response is based on came from, so the form can
// say it too. A fallback that nobody can see is how the settings page validated against
// 26.5.x for months while 26.8.0 was running.
type schemaSource struct {
	Version  string `json:"version"`
	From     string `json:"from"` // "deployed" or "embedded"
	Fallback bool   `json:"fallback"`
	Reason   string `json:"reason,omitempty"`
}

// schemaFor returns the schema of the running chart, or the embedded one when that
// cannot be read — and in that case says why.
func (h *ConfigHandler) schemaFor() ([]byte, schemaSource, error) {
	var reason string
	if h.deployedSchema != nil {
		data, version, err := h.deployedSchema()
		if err == nil {
			return data, schemaSource{Version: version, From: "deployed"}, nil
		}
		reason = err.Error()
	} else {
		reason = "kein Cluster-Zugriff"
	}
	data, err := cfgschema.Get(h.essVersion)
	if err != nil {
		return nil, schemaSource{}, err
	}
	return data, schemaSource{Version: cfgschema.VersionOf(h.essVersion), From: "embedded", Fallback: true, Reason: reason}, nil
}

// GET /api/v1/config/location — where the configuration actually lives.
//
// Added because an operator who had been editing configuration for months still assumed
// it landed in the folder they had seeded it from (etappe 71). Ten screens about the
// configuration and not one named the volume it is kept on — an absent sentence about
// the product's most reassuring property.
func (h *ConfigHandler) Location(w http.ResponseWriter, r *http.Request) {
	type location struct {
		Path string `json:"path"`
		// Seed is where the initial import came from, read once at first start. Named
		// explicitly so it stops looking like the live source of truth.
		Seed    string `json:"seed,omitempty"`
		Bytes   int64  `json:"bytes"`
		Files   int    `json:"files"`
		Commits int    `json:"commits"`
		// Versioned is what makes this more than a directory: history, diff, rollback.
		Versioned bool `json:"versioned"`
	}
	loc := location{Path: h.repoPath, Seed: h.seedPath, Versioned: h.git != nil}

	_ = filepath.WalkDir(h.repoPath, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, ierr := d.Info(); ierr == nil {
			loc.Files++
			loc.Bytes += info.Size()
		}
		return nil
	})
	if h.git != nil {
		if commits, err := h.git.Log(1000); err == nil {
			loc.Commits = len(commits)
		}
	}
	JSON(w, http.StatusOK, loc)
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

	schemaData, _, schemaErr := h.schemaFor()
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
	if schemaData, src, err := h.schemaFor(); err == nil {
		resp["schema"] = json.RawMessage(schemaData)
		resp["schema_source"] = src
	}
	JSON(w, http.StatusOK, resp)
}

// POST /api/v1/config/discard — throw away every uncommitted edit (etappe 108).
//
// The "Verwerfen" of the pending-changes bar. Back to the last commit, comments and all;
// nothing on the cluster changes, because nothing uncommitted ever reached it.
func (h *ConfigHandler) Discard(w http.ResponseWriter, r *http.Request) {
	if err := h.store.Discard(); err != nil {
		Error(w, http.StatusInternalServerError, "Verwerfen fehlgeschlagen: "+err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "discarded"})
}

// POST /api/v1/config/follow-chart {items: [{component, kind}]} — let pinned images
// come from the chart again (etappe 109). "orphan" comments out the whole image block,
// anything else the tag. Nothing is committed; the edits are pending changes.
func (h *ConfigHandler) FollowChart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Items []struct {
			Component string `json:"component"`
			Kind      string `json:"kind"`
		} `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Items) == 0 {
		Error(w, http.StatusBadRequest, "items required")
		return
	}
	var changed []string
	for _, it := range req.Items {
		done, err := h.store.FollowChart(r.Context(), it.Component, it.Kind == "orphan")
		if err != nil {
			// What was changed so far stays: each edit stands on its own, and the
			// pending-changes bar shows them.
			JSON(w, http.StatusUnprocessableEntity, map[string]interface{}{"error": err.Error(), "changed": changed})
			return
		}
		changed = append(changed, done...)
	}
	JSON(w, http.StatusOK, map[string]interface{}{"changed": changed})
}

// GET /api/v1/config/tasks — the task layer: cards, fields and their current values
// (etappe 113).
func (h *ConfigHandler) GetTasks(w http.ResponseWriter, r *http.Request) {
	values, err := h.mergedValues(r)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	cards := tasks.Cards()
	JSON(w, http.StatusOK, map[string]interface{}{
		"cards":  cards,
		"values": tasks.Read(values, tasks.AllFields(cards)),
	})
}

// POST /api/v1/config/tasks {changes: {id: value|null}} — written to the working tree,
// comment-preserving; nothing is applied. The pending-changes bar takes it from there.
func (h *ConfigHandler) SetTasks(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Changes map[string]interface{} `json:"changes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Changes) == 0 {
		Error(w, http.StatusBadRequest, "changes required")
		return
	}
	values, err := h.mergedValues(r)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	plan, err := tasks.Write(values, tasks.AllFields(tasks.Cards()), req.Changes)
	if err != nil {
		Error(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := h.store.SetSectionValues(r.Context(), plan.Set, plan.Remove); err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

func (h *ConfigHandler) mergedValues(r *http.Request) (map[string]interface{}, error) {
	contents, err := h.store.MergedContent(r.Context())
	if err != nil {
		return nil, err
	}
	return config.MergeToMap(contents)
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
	data, _, err := h.schemaFor()
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
