package handlers

import (
	"net/http"

	authmw "github.com/bxnnyg/matrixctrl/internal/api/middleware"

	"github.com/bxnnyg/matrixctrl/internal/updatecheck"
)

// VersionHandler answers "which MatrixCtrl is this, and is there a newer one".
//
// Both questions were unanswerable from inside the product: the version reached a
// startup log line and backup manifests, and nothing else. An operator asked for it
// three ways in one message — which version, how do I see there's an update, how do
// I install it — which is a good sign that one screen should say all three.
type VersionHandler struct {
	version string
	commit  string
	// Nil when the operator has turned the check off. The version itself is still
	// reported: knowing what is running must never depend on reaching a registry.
	checker *updatecheck.Checker
	// mayWrite answers the question every screen needs before it offers a button: is
	// this session allowed to change anything? The navigation rail already asks this
	// endpoint on every page load, so the answer travels with it rather than costing a
	// second request (etappe 100).
	mayWrite func(userID string) bool
}

func NewVersionHandler(version, commit string, checker *updatecheck.Checker, mayWrite func(string) bool) *VersionHandler {
	return &VersionHandler{version: version, commit: commit, checker: checker, mayWrite: mayWrite}
}

type versionResponse struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	// Absent when the update check is disabled — which the UI shows as "not checked",
	// a different thing from "up to date".
	Update *updatecheck.Result `json:"update,omitempty"`
	// MayWrite is true unless this session may only look.
	MayWrite bool `json:"may_write"`
}

// GET /api/v1/version
func (h *VersionHandler) Get(w http.ResponseWriter, r *http.Request) {
	out := versionResponse{Version: h.version, Commit: h.commit, MayWrite: true}
	if h.mayWrite != nil {
		out.MayWrite = h.mayWrite(authmw.UserIDFromContext(r.Context()))
	}
	if h.checker != nil {
		res := h.checker.Check(r.Context())
		out.Update = &res
	}
	JSON(w, http.StatusOK, out)
}
