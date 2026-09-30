package handlers

import (
	"context"
	"net/http"
	"regexp"
	"strings"

	"github.com/bxnnyg/matrixctrl/internal/k8s"
	"github.com/bxnnyg/matrixctrl/internal/version"
)

// Updating MatrixCtrl from its own panel (etappe 116). The upgrade runs in a Job — see
// helm.UpgradeSelf — and this handler starts it, reports it, and says beforehand whether
// this installation has the rights for it.

type SelfUpdateHandler struct {
	k8s        *k8s.Client
	essNS      string
	release    string
	deployment string
}

func NewSelfUpdateHandler(k *k8s.Client, essNS, release string) *SelfUpdateHandler {
	return &SelfUpdateHandler{k8s: k, essNS: essNS, release: release, deployment: "matrixctrl"}
}

func (h *SelfUpdateHandler) ownNamespace() string {
	if ns := k8s.CurrentNamespace(); ns != "" {
		return ns
	}
	return "matrixctrl"
}

// ready reports whether every right the update needs is held, and names the missing ones.
func (h *SelfUpdateHandler) ready(ctx context.Context) (bool, []string) {
	var missing []string
	check := func(ns string, perms []k8s.Permission) {
		res, err := h.k8s.Check(ctx, ns, perms)
		if err != nil {
			missing = append(missing, "Rechte nicht prüfbar: "+err.Error())
			return
		}
		for _, r := range res {
			if !r.Allowed {
				missing = append(missing, r.Why)
			}
		}
	}
	p := k8s.SelfUpdatePermissions
	check("", p.Cluster)
	check(h.essNS, p.ESS)
	check(h.ownNamespace(), p.Own)
	return len(missing) == 0, missing
}

// GET /api/v1/self-update — the running version, whether an update can start from here,
// and the last update Job. After an update the *new* instance answers this, so the page
// that started it learns how it ended.
func (h *SelfUpdateHandler) Get(w http.ResponseWriter, r *http.Request) {
	out := map[string]interface{}{"current": strings.TrimPrefix(version.Version, "v")}
	if h.k8s == nil {
		out["ready"] = false
		out["missing"] = []string{"kein Cluster-Zugriff"}
		JSON(w, http.StatusOK, out)
		return
	}
	ok, missing := h.ready(r.Context())
	out["ready"] = ok
	if !ok {
		out["missing"] = missing
	}
	if job, err := h.k8s.LatestSelfUpdate(r.Context(), h.ownNamespace()); err == nil && job != nil {
		out["job"] = job
	}
	JSON(w, http.StatusOK, out)
}

var semverRE = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// POST /api/v1/self-update {version}
func (h *SelfUpdateHandler) Start(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Version string `json:"version"`
	}
	if err := Decode(r, &req); err != nil {
		Error(w, http.StatusBadRequest, "invalid request")
		return
	}
	target := strings.TrimPrefix(strings.TrimSpace(req.Version), "v")
	if !semverRE.MatchString(target) {
		Error(w, http.StatusBadRequest, "Version wie 0.1.113 erwartet")
		return
	}
	if h.k8s == nil {
		Error(w, http.StatusServiceUnavailable, "Kein Cluster-Zugriff.")
		return
	}
	if ok, missing := h.ready(r.Context()); !ok {
		JSON(w, http.StatusConflict, map[string]interface{}{
			"error": "Diese Installation hat die Rechte für ein Update aus dem Panel noch nicht. " +
				"Einmalig per install.sh update aktualisieren — die neuen Rechte kommen mit dieser Version, danach geht es von hier.",
			"missing": missing,
		})
		return
	}
	ns := h.ownNamespace()
	image, sa, err := h.k8s.SelfImage(r.Context(), ns, h.deployment)
	if err != nil {
		Error(w, http.StatusInternalServerError, "Das eigene Deployment ist nicht lesbar: "+err.Error())
		return
	}
	job, err := h.k8s.StartSelfUpdate(r.Context(), ns, image, sa, h.release, target)
	if err != nil {
		Error(w, http.StatusConflict, err.Error())
		return
	}
	JSON(w, http.StatusAccepted, job)
}
