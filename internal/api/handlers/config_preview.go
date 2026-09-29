package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	authmw "github.com/bxnnyg/matrixctrl/internal/api/middleware"
	"github.com/bxnnyg/matrixctrl/internal/capacity"
	"github.com/bxnnyg/matrixctrl/internal/config"
	"github.com/bxnnyg/matrixctrl/internal/hooks"
	"github.com/bxnnyg/matrixctrl/internal/preview"
)

// What applying the pending configuration would do, before it is done (etappe 108).
//
// The settings page had one button that committed, rendered, applied and waited, and it
// was only while that ran that the log said which services were restarting and whether
// they still fitted. The capacity check existed and warned — after the commit, in a log
// line scrolling past a deploy that went ahead anyway. It was right twice in production,
// both times with a real outage, and both times the deploy was not stopped (P1-16c).
//
// One verdict now answers the preview and gates the apply, so the two cannot disagree.

// configVerdict is what the pending configuration would do to the cluster.
type configVerdict struct {
	// Rendered says whether the configuration could be rendered at all. A config that
	// could not be checked is not blocked — "unknown" is not "does not fit" — but it is
	// said, not passed off as fine (§4.55).
	Rendered bool   `json:"rendered"`
	Note     string `json:"note,omitempty"`

	Restarts []preview.Restart  `json:"restarts"`
	Findings []capacity.Finding `json:"findings"`
	Blocking bool               `json:"blocking"`

	// ReleaseStatus and Stuck: a release in pending-* refuses every upgrade until it is
	// rolled back, and the button should say so instead of trying (§4.88). "failed" is
	// not stuck: Helm upgrades out of it, and applying a corrected configuration is how
	// a failed release is usually repaired.
	ReleaseStatus string `json:"release_status,omitempty"`
	Stuck         bool   `json:"stuck"`

	// StaleAliases are hostAliases into the service network at an address no Service
	// holds — a warning, never a block: an alias may point somewhere on purpose
	// (etappe 109). AliasesUnchecked says the Services could not be listed, so silence
	// is not mistaken for "all fine".
	StaleAliases     []preview.StaleAlias `json:"stale_aliases"`
	AliasesUnchecked bool                 `json:"aliases_unchecked,omitempty"`

	// Requests and the largest node, for the capacity bar of the task layer (etappe
	// 113): what everything reserves after this change, against what there is.
	Requests []capacity.Request `json:"requests,omitempty"`
	Node     *nodeSize          `json:"node,omitempty"`
}

type nodeSize struct {
	CPUMillis int64 `json:"cpu_millis"`
	MemMi     int64 `json:"mem_mi"`
}

// pendingValues is the configuration as it stands in the working tree — what an apply
// would ship.
func (h *HelmHandler) pendingValues(ctx context.Context) (map[string]interface{}, error) {
	if h.configStore == nil {
		return nil, nil
	}
	contents, err := h.configStore.MergedContent(ctx)
	if err != nil {
		return nil, err
	}
	return config.MergeToMap(contents)
}

// verdictFor renders the values with the deployed chart and measures the result.
func (h *HelmHandler) verdictFor(ctx context.Context, release string, values map[string]interface{}) configVerdict {
	v := configVerdict{Restarts: []preview.Restart{}, Findings: []capacity.Finding{}, StaleAliases: []preview.StaleAlias{}}
	if h.helm == nil {
		// No cluster at startup: there is no release to render against.
		v.Note = "Ohne Cluster-Zugriff lässt sich nicht vorhersagen, was das Übernehmen täte."
		return v
	}

	if rel, err := h.helm.GetRelease(release); err == nil && rel != nil {
		v.ReleaseStatus = rel.Status
		v.Stuck = strings.HasPrefix(strings.ToLower(rel.Status), "pending-")
	}

	current, rendered, err := h.helm.RenderDeployed(ctx, release, values)
	if err != nil {
		v.Note = "Die Konfiguration ließ sich nicht rendern: " + err.Error()
		return v
	}
	v.Rendered = true
	// Never null: "nothing restarts" is an empty list. A null here crashed the pending-
	// changes bar on the one change that restarts nothing — a setting switched on and
	// back off (etappe 114, reported by the operator).
	if r := preview.Restarts(current, rendered); r != nil {
		v.Restarts = r
	}

	if h.k8s == nil {
		v.Note = "Ohne Cluster-Zugriff lässt sich die Kapazität nicht prüfen."
		return v
	}
	if svcs, err := h.k8s.ServiceIPs(ctx); err == nil {
		ips := make([]preview.ServiceIP, 0, len(svcs))
		for _, s := range svcs {
			ips = append(ips, preview.ServiceIP{Namespace: s.Namespace, Name: s.Name, IP: s.IP})
		}
		if stale := preview.StaleHostAliases(rendered, ips); stale != nil {
			v.StaleAliases = stale
		}
	} else {
		// Typically RBAC: listing Services outside the ESS namespace is an optional
		// permission. Without kube-system the ingress controller is invisible, and a
		// correct alias to it would be reported as stale — so no verdict at all.
		v.AliasesUnchecked = true
	}

	nodes, err := h.k8s.NodeInfo(ctx)
	if err != nil {
		v.Note = "Die Kapazität der Nodes war nicht lesbar: " + err.Error()
		return v
	}
	if f := capacity.Check(rendered, capacity.FromNodeInfo(nodes)); f != nil {
		v.Findings = f
	}
	v.Requests = capacity.Requests(rendered)
	for _, n := range nodes {
		if v.Node == nil || n.MemTotalMi > v.Node.MemMi {
			v.Node = &nodeSize{CPUMillis: n.CPUTotalMillis, MemMi: n.MemTotalMi}
		}
	}
	v.Blocking = capacity.Blocking(v.Findings)
	return v
}

// POST /api/v1/config/preview — what "Übernehmen" would do to the managed release.
//
// Changes nothing: no commit, no apply, no hook. About two seconds, because it renders
// the chart the release already carries rather than pulling one from the registry.
//
// Under /config rather than /helm/releases/{name}: the read-only allowlist matches exact
// paths, and one with a release name in it would silently stop matching on an install
// whose release is not called "ess" (PROZESS edge case 1).
func (h *HelmHandler) PreviewConfig(w http.ResponseWriter, r *http.Request) {
	name := h.essRelease
	values, err := h.pendingValues(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, "Die ausstehende Konfiguration war nicht lesbar: "+err.Error())
		return
	}
	JSON(w, http.StatusOK, h.verdictFor(r.Context(), name, values))
}

// recordVerdict streams the capacity findings of an apply and stores them with the
// upgrade, so "were we warned before applying that?" still has an answer once the tab is
// closed (etappe 63). No second render: the verdict that gated the apply is the one that
// is recorded.
func (h *HelmHandler) recordVerdict(ctx context.Context, stream *upgradeStream, v configVerdict, overridden bool, upgradeID uuid.UUID) {
	for _, f := range v.Findings {
		switch f.Level {
		case capacity.LevelBlocked:
			stream.emit("WARNING: " + f.Message)
		case capacity.LevelWarn, capacity.LevelUnknown:
			stream.emit("NOTE: " + f.Message)
		}
	}
	switch {
	case !v.Rendered:
		stream.emit("NOTE: capacity preflight skipped — " + v.Note)
	case overridden && v.Blocking:
		stream.emit("WARNING: Die Kapazitätsprüfung hat abgelehnt und wurde bewusst übergangen.")
	case len(v.Findings) == 0:
		stream.emit("Capacity preflight: every workload fits the cluster.")
	}
	if blob, err := json.Marshal(v.Findings); err == nil {
		if _, err := h.db.Exec(ctx, "UPDATE upgrade_history SET pre_flight=$1 WHERE id=$2", blob, upgradeID); err != nil {
			log.Printf("preflight: could not record findings: %v", err)
		}
	}
}

// POST /api/v1/config/revert-apply {upgrade_id} — take a failed "Übernehmen" back.
//
// Back to exactly where that apply started (etappe 108): the cluster to the Helm revision
// it replaced, the configuration to the commit it was built on. The existing rollback
// button covers the cluster only, with "the previous revision" — which leaves the
// repository on the broken values for the next apply to roll out again, and is one
// revision too far when the upgrade failed before Helm wrote one.
//
// Cluster first. If Helm cannot go back, the configuration is left alone: it still
// describes what was attempted, and that is the truth an operator debugging it needs.
func (h *HelmHandler) RevertApply(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UpgradeID string `json:"upgrade_id"`
	}
	if err := Decode(r, &req); err != nil || req.UpgradeID == "" {
		Error(w, http.StatusBadRequest, "upgrade_id fehlt")
		return
	}
	h.mu.RLock()
	stream := h.streams[req.UpgradeID]
	h.mu.RUnlock()
	if stream == nil {
		Error(w, http.StatusNotFound, "Dieser Vorgang ist nicht mehr bekannt — vermutlich wurde MatrixCtrl seitdem neu gestartet. "+
			"Zurück geht es über den Verlauf der Einstellungen und den Rollback auf der Update-Seite.")
		return
	}
	origin, done := stream.originIfDone()
	if !done {
		Error(w, http.StatusConflict, "Der Vorgang läuft noch.")
		return
	}
	if origin.configSHA == "" && origin.revision == 0 {
		Error(w, http.StatusBadRequest, "Dieser Vorgang war kein Übernehmen der Einstellungen.")
		return
	}

	if h.helm == nil {
		Error(w, http.StatusServiceUnavailable, "Kein Cluster-Zugriff.")
		return
	}
	name := h.essRelease
	userID := authmw.UserIDFromContext(r.Context())
	out := map[string]any{"cluster": "unchanged", "config": "unchanged"}

	rel, err := h.helm.GetRelease(name)
	if err != nil {
		Error(w, http.StatusInternalServerError, "Release nicht lesbar: "+err.Error())
		return
	}
	if origin.revision > 0 && rel.Revision != origin.revision {
		if err := h.helm.Rollback(name, origin.revision); err != nil {
			Error(w, http.StatusInternalServerError, fmt.Sprintf(
				"Der Cluster ließ sich nicht auf Revision %d zurücksetzen: %v — die Einstellungen wurden nicht angefasst.", origin.revision, err))
			return
		}
		out["cluster"] = fmt.Sprintf("revision %d", origin.revision)
		// The same patches a rollback always drops (see Rollback).
		runIDs, hookErr := h.engine.RunTrigger(r.Context(), hooks.TriggerPostRollback,
			"rollback:"+name+":"+strconv.Itoa(origin.revision), userID)
		out["hook_runs"] = runIDs
		if hookErr != nil {
			out["hooks_failed"] = true
		}
	}

	if origin.configSHA != "" {
		sha, err := h.configStore.RestoreCommit(origin.configSHA, "config: Rücksprung nach fehlgeschlagenem Übernehmen", userID)
		if err != nil {
			out["config_error"] = err.Error()
		} else if sha != "" {
			out["config"] = sha
		}
	}
	JSON(w, http.StatusOK, out)
}
