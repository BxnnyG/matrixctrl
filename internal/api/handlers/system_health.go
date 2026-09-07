package handlers

import (
	"net/http"
	"strings"

	"github.com/bxnnyg/matrixctrl/internal/k8s"
)

// The conditions under which Helm refuses everything else until they are cleared.
// `superseded` is not one of them — that is what every old revision looks like.
var blockingReleaseStates = map[string]bool{
	"pending-install":  true,
	"pending-upgrade":  true,
	"pending-rollback": true,
	"failed":           true,
}

type releaseHealth struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Status    string `json:"status"`
	// Blocking means: until this is resolved, every deploy, upgrade and connect will
	// fail with "another operation (install/upgrade/rollback) is in progress".
	Blocking bool `json:"blocking"`
	// Absent is not a problem — a homeserver that has not been deployed yet is a
	// state, not a fault.
	Present bool `json:"present"`
}

type systemHealth struct {
	Releases []releaseHealth `json:"releases"`
	DeadPods []k8s.DeadPod   `json:"dead_pods"`
	// Problems counts what actually needs attention, so a client does not have to
	// re-derive the judgement and reach a different one.
	Problems int `json:"problems"`
}

// GET /api/v1/system/health — the conditions that make operations fail.
//
// The same questions `install.sh doctor` asks, from inside. The app knew the state of
// ESS and nothing about the cluster it lives in, so "mein remote ist verbugget" had no
// screen that answered it (§4.86). Read-only on purpose: what removes things stays in
// the script, where there is no session to lose and no button to hit by accident.
func (h *StatusHandler) SystemHealth(w http.ResponseWriter, r *http.Request) {
	out := systemHealth{Releases: []releaseHealth{}, DeadPods: []k8s.DeadPod{}}

	// Only the managed release, deliberately.
	//
	// Helm's action configuration is bound to one namespace, and this one points at the
	// ESS namespace. MatrixCtrl's own release lives elsewhere and would need a second
	// configuration — reporting it as "unknown" from here would be a guess dressed as
	// an answer, and `install.sh doctor` already covers it from outside.
	if h.helm != nil && h.essRelease != "" {
		rh := releaseHealth{Name: h.essRelease, Namespace: h.essNS}
		if rel, err := h.helm.GetRelease(h.essRelease); err == nil && rel != nil {
			rh.Present = true
			rh.Status = rel.Status
			rh.Blocking = blockingReleaseStates[strings.ToLower(rel.Status)]
			if rh.Blocking {
				out.Problems++
			}
		}
		out.Releases = append(out.Releases, rh)
	}

	if h.k8s != nil {
		namespaces := []string{h.essNS}
		if h.selfNS != "" && h.selfNS != h.essNS {
			namespaces = append(namespaces, h.selfNS)
		}
		if dead, err := h.k8s.DeadPods(r.Context(), namespaces...); err == nil && len(dead) > 0 {
			out.DeadPods = dead
			out.Problems++
		}
	}

	JSON(w, http.StatusOK, out)
}
