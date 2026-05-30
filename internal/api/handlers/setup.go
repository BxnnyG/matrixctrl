package handlers

import (
	"net/http"

	"github.com/bxnny/matrixctrl/internal/config"
	"github.com/bxnny/matrixctrl/internal/helm"
)

// SetupHandler powers the Phase 1.5 onboarding/health view: is ESS deployed, is
// the config seeded, is admin login wired up.
type SetupHandler struct {
	helm           *helm.Client
	store          *config.Store
	essRelease     string
	essNamespace   string
	oidcConfigured bool
}

func NewSetupHandler(h *helm.Client, store *config.Store, essRelease, essNamespace string, oidcConfigured bool) *SetupHandler {
	return &SetupHandler{helm: h, store: store, essRelease: essRelease, essNamespace: essNamespace, oidcConfigured: oidcConfigured}
}

// GET /api/v1/setup/status — onboarding checklist state.
func (h *SetupHandler) Status(w http.ResponseWriter, r *http.Request) {
	resp := map[string]interface{}{
		"ess_namespace":   h.essNamespace,
		"ess_release":     h.essRelease,
		"oidc_configured": h.oidcConfigured,
		"bootstrap_active": !h.oidcConfigured,
		"ess_installed":   false,
	}

	if h.helm != nil {
		if rel, err := h.helm.GetRelease(h.essRelease); err == nil && rel != nil {
			resp["ess_installed"] = true
			resp["ess_version"] = rel.Version
			resp["ess_status"] = rel.Status
		}
	}

	sections := 0
	if h.store != nil {
		if slices, err := h.store.List(r.Context()); err == nil {
			sections = len(slices)
		}
	}
	resp["config_sections"] = sections

	JSON(w, http.StatusOK, resp)
}

// GET /api/v1/setup/chart-defaults?version=X — the raw commented values.yaml of the
// ESS chart, used to seed a greenfield config.
func (h *SetupHandler) ChartDefaults(w http.ResponseWriter, r *http.Request) {
	if h.helm == nil {
		Error(w, http.StatusServiceUnavailable, "helm unavailable")
		return
	}
	version := r.URL.Query().Get("version")
	if version == "" {
		Error(w, http.StatusBadRequest, "version query param required")
		return
	}
	values, err := h.helm.DefaultChartValues(version)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"version": version, "values": values})
}
