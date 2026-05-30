package handlers

import (
	"net/http"

	"gopkg.in/yaml.v3"

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
	masHost := ""
	if h.store != nil {
		if slices, err := h.store.List(r.Context()); err == nil {
			sections = len(slices)
		}
		// Best-effort: surface the MAS ingress host to prefill the OIDC issuer.
		if contents, err := h.store.MergedContent(r.Context()); err == nil {
			if merged, err := config.MergeToMap(contents); err == nil {
				if mas, ok := merged["matrixAuthenticationService"].(map[string]interface{}); ok {
					if ing, ok := mas["ingress"].(map[string]interface{}); ok {
						if host, ok := ing["host"].(string); ok {
							masHost = host
						}
					}
				}
			}
		}
	}
	resp["config_sections"] = sections
	resp["mas_host"] = masHost

	JSON(w, http.StatusOK, resp)
}

// GET /api/v1/setup/discover — scan the cluster for ESS (matrix-stack) releases.
func (h *SetupHandler) Discover(w http.ResponseWriter, r *http.Request) {
	found, err := helm.Discover()
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if found == nil {
		found = []helm.ESSRelease{}
	}
	JSON(w, http.StatusOK, map[string]interface{}{
		"releases":         found,
		"managed_release":  h.essRelease,
		"managed_namespace": h.essNamespace,
	})
}

// POST /api/v1/setup/adopt — seed the (empty) config repo from the deployed ESS
// release's user-supplied values, so MatrixCtrl can manage an existing ESS.
func (h *SetupHandler) Adopt(w http.ResponseWriter, r *http.Request) {
	if h.helm == nil || h.store == nil {
		Error(w, http.StatusServiceUnavailable, "helm/store unavailable")
		return
	}
	values, err := h.helm.GetReleaseValues(h.essRelease)
	if err != nil {
		Error(w, http.StatusBadRequest, "could not read release values: "+err.Error())
		return
	}
	yamlBytes, err := yaml.Marshal(values)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.store.SeedSections(r.Context(), string(yamlBytes), false); err != nil {
		Error(w, http.StatusConflict, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "adopted", "release": h.essRelease})
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
