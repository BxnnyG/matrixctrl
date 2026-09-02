// Upgrading the managed ESS release: version upgrades and applying config
// changes. Both stream their progress through the stream in helm_stream.go.
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/bxnnyg/matrixctrl/internal/api/middleware"
	"github.com/bxnnyg/matrixctrl/internal/capacity"
	"github.com/bxnnyg/matrixctrl/internal/config"
	"github.com/bxnnyg/matrixctrl/internal/hooks"
	"github.com/bxnnyg/matrixctrl/internal/rollout"
)

func (h *HelmHandler) Upgrade(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	userID := authmw.UserIDFromContext(r.Context())

	var req struct {
		ToVersion string `json:"to_version"`
		DryRun    bool   `json:"dry_run"`
	}
	if err := Decode(r, &req); err != nil || req.ToVersion == "" {
		Error(w, http.StatusBadRequest, "to_version required")
		return
	}

	// Get current version for history
	fromVersion := ""
	if rel, err := h.helm.GetRelease(name); err == nil {
		fromVersion = rel.ChartVersion
	}

	upgradeID := uuid.New().String()
	stream := &upgradeStream{status: "pending"}
	h.mu.Lock()
	h.streams[upgradeID] = stream
	h.mu.Unlock()

	// Create upgrade_history row
	upgradeUUID := uuid.New()
	_, _ = h.db.Exec(r.Context(), `
		INSERT INTO upgrade_history(id, user_id, from_version, to_version, status)
		VALUES($1, $2, $3, $4, 'pending')`,
		upgradeUUID, userID, fromVersion, req.ToVersion,
	)

	// Run upgrade async
	go func() {
		ctx := context.Background()
		started := time.Now()

		// The snapshot goroutine runs for the whole operation, across every phase,
		// so the component table is live during the hooks too — the phase after the
		// one the old progress ticker covered, and the one that restarts the SFU.
		stream.setPhase(rollout.PhaseConfig)
		stopSnapshots := stream.startSnapshots(snapshotInterval,
			h.progressSnapshot(ctx, stream, started))
		defer stopSnapshots()

		stream.emit("Starting upgrade to " + req.ToVersion + "...")

		_, _ = h.db.Exec(ctx, "UPDATE upgrade_history SET status='running' WHERE id=$1", upgradeUUID)

		// Load merged config values from the config store.
		var values map[string]interface{}
		if h.configStore != nil {
			contents, err := h.configStore.MergedContent(ctx)
			if err != nil {
				stream.emit("WARNING: could not load config values: " + err.Error() + " — upgrading with empty values")
			} else {
				values, err = config.MergeToMap(contents)
				if err != nil {
					stream.emit("WARNING: could not merge config values: " + err.Error() + " — upgrading with empty values")
					values = nil
				} else {
					stream.emit(fmt.Sprintf("Loaded %d config slices from config store.", len(contents)))
				}
			}
		}

		// Reported before the rollout starts, so the operator sees it while there is
		// still something to decide. Not blocking and not auto-fixed: unpinning is an
		// upgrade decision with consequences — here a seven-minor-version MAS jump
		// with database migrations — and that belongs to the operator (E31).
		if line := h.pinnedTagWarning(ctx, req.ToVersion, values); line != "" {
			stream.emit("WARNUNG: " + line)
		}

		// Apply, not rollout: helm has not written anything yet. The snapshot
		// promotes this to `rollout` the moment a workload stops being settled,
		// which is the observable definition of "the rollout has begun".
		stream.setPhase(rollout.PhaseApply)
		stopProgress := stream.startProgressWithProbe("Waiting for Helm rollout", upgradeProgressInterval, h.rolloutProbe(ctx))
		result, err := h.helm.Upgrade(ctx, name, req.ToVersion, values)
		stopProgress()
		if err != nil {
			stream.emit("ERROR: " + err.Error())
			stream.finish("failed")
			_, _ = h.db.Exec(ctx, `UPDATE upgrade_history SET status='failed', error_message=$1, ts_completed=NOW() WHERE id=$2`,
				err.Error(), upgradeUUID)
			return
		}

		// The raw `{"revision":…,"status":…}` line that used to be emitted here went
		// straight into the operator's log view as a JSON blob, immediately above the
		// human sentence saying the same thing. Nothing consumed it (E43).
		stream.setPhase(rollout.PhaseHooks)
		stream.emit("Helm upgrade successful (revision " + intToStr(result.Revision) + "). Running post-upgrade hooks...")

		_, _ = h.db.Exec(ctx, "UPDATE upgrade_history SET status='running-hooks', helm_revision=$1 WHERE id=$2",
			result.Revision, upgradeUUID)

		hookRunIDs, hookErr := h.engine.RunTrigger(ctx, hooks.TriggerPostUpgrade, upgradeUUID.String(), userID)
		if hookErr != nil {
			stream.emit("Hook execution error: " + hookErr.Error())
		}

		finalStatus := "success"
		if hookErr != nil {
			finalStatus = "hooks-failed"
		}

		idsJSON, _ := json.Marshal(hookRunIDs)
		_, _ = h.db.Exec(ctx, `
			UPDATE upgrade_history SET status=$1, hooks_run=$2, ts_completed=NOW() WHERE id=$3`,
			finalStatus, idsJSON, upgradeUUID,
		)

		if finalStatus == "success" {
			stream.emit("All post-upgrade hooks completed successfully.")
		} else {
			stream.emit("WARNING: Post-upgrade hooks failed. Check hooks page and re-run manually.")
		}
		// Phase before finish, not after: finish closes the subscriber channels, and
		// a client that gets "done" while the stepper still reads "hooks" shows a
		// finished upgrade stuck on its second-to-last step.
		stream.setPhase(rollout.PhaseDone)
		stream.finish(finalStatus)
	}()

	JSON(w, http.StatusAccepted, map[string]string{
		"upgrade_id": upgradeID,
		"history_id": upgradeUUID.String(),
	})
}

// ApplyConfig commits the current config to git and runs an in-place helm upgrade
// (same chart version, new merged values). Uses the same stream/WS mechanism as Upgrade.
func (h *HelmHandler) ApplyConfig(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	userID := authmw.UserIDFromContext(r.Context())

	var req struct {
		Message string `json:"message"`
	}
	_ = Decode(r, &req)
	if req.Message == "" {
		req.Message = "config: apply changes via MatrixCtrl"
	}

	rel, err := h.helm.GetRelease(name)
	if err != nil || rel == nil {
		Error(w, http.StatusBadRequest, "could not determine current chart version — is the release deployed?")
		return
	}
	currentVersion := rel.Version // semver only, e.g. "26.5.1"

	upgradeID := uuid.New().String()
	stream := &upgradeStream{status: "pending"}
	h.mu.Lock()
	h.streams[upgradeID] = stream
	h.mu.Unlock()

	upgradeUUID := uuid.New()
	_, _ = h.db.Exec(r.Context(), `
		INSERT INTO upgrade_history(id, user_id, from_version, to_version, status)
		VALUES($1, $2, $3, $4, 'pending')`,
		upgradeUUID, userID, currentVersion, currentVersion,
	)

	commitMsg := req.Message

	go func() {
		ctx := context.Background()
		started := time.Now()

		// Applying config is the same operation with the same chart version, so it
		// gets the same view. It is also the one an operator runs most often.
		stream.setPhase(rollout.PhaseConfig)
		stopSnapshots := stream.startSnapshots(snapshotInterval,
			h.progressSnapshot(ctx, stream, started))
		defer stopSnapshots()

		sha, commitErr := h.configStore.Commit(ctx, commitMsg, userID)
		if commitErr != nil {
			if strings.Contains(commitErr.Error(), "nothing to commit") || strings.Contains(commitErr.Error(), "clean") {
				stream.emit("No config changes to commit — deploying current state.")
			} else {
				stream.emit("WARNING: git commit: " + commitErr.Error())
			}
		} else {
			stream.emit("Config committed to git: " + sha)
		}

		_, _ = h.db.Exec(ctx, "UPDATE upgrade_history SET status='running' WHERE id=$1", upgradeUUID)

		var values map[string]interface{}
		if h.configStore != nil {
			contents, err := h.configStore.MergedContent(ctx)
			if err != nil {
				stream.emit("WARNING: could not load config values: " + err.Error())
			} else {
				values, err = config.MergeToMap(contents)
				if err != nil {
					stream.emit("WARNING: could not merge config values: " + err.Error())
					values = nil
				} else {
					stream.emit(fmt.Sprintf("Loaded %d config slices.", len(contents)))
				}
			}
		}

		// Capacity preflight, before anything is applied (etappe 55). The values that
		// took this homeserver down for 37 hours were written through this panel; this
		// is the last moment they can be measured against the cluster they are about
		// to reach. It renders the chart rather than reading the values, because the
		// multipliers that matter live in the chart (§4.53).
		h.emitCapacityPreflight(ctx, stream, name, currentVersion, values, upgradeUUID)

		stream.emit("Applying config to cluster (version " + currentVersion + ")...")
		stream.setPhase(rollout.PhaseApply)
		stopProgress := stream.startProgressWithProbe("Waiting for Helm rollout", upgradeProgressInterval, h.rolloutProbe(ctx))
		result, err := h.helm.Upgrade(ctx, name, currentVersion, values)
		stopProgress()
		if err != nil {
			stream.emit("ERROR: " + err.Error())
			stream.finish("failed")
			_, _ = h.db.Exec(ctx, `UPDATE upgrade_history SET status='failed', error_message=$1, ts_completed=NOW() WHERE id=$2`,
				err.Error(), upgradeUUID)
			return
		}

		stream.setPhase(rollout.PhaseHooks)
		stream.emit(fmt.Sprintf("Helm apply successful (revision %s). Running post-upgrade hooks...", intToStr(result.Revision)))
		_, _ = h.db.Exec(ctx, "UPDATE upgrade_history SET status='running-hooks', helm_revision=$1 WHERE id=$2",
			result.Revision, upgradeUUID)

		hookRunIDs, hookErr := h.engine.RunTrigger(ctx, hooks.TriggerPostUpgrade, upgradeUUID.String(), userID)
		if hookErr != nil {
			stream.emit("Hook execution error: " + hookErr.Error())
		}

		finalStatus := "success"
		if hookErr != nil {
			finalStatus = "hooks-failed"
		}

		idsJSON, _ := json.Marshal(hookRunIDs)
		_, _ = h.db.Exec(ctx, `
			UPDATE upgrade_history SET status=$1, hooks_run=$2, ts_completed=NOW() WHERE id=$3`,
			finalStatus, idsJSON, upgradeUUID)

		if finalStatus == "success" {
			stream.emit("Config deployed successfully.")
		} else {
			stream.emit("WARNING: Post-upgrade hooks failed. Check the Hooks page.")
		}
		stream.setPhase(rollout.PhaseDone)
		stream.finish(finalStatus)
	}()

	JSON(w, http.StatusAccepted, map[string]string{
		"upgrade_id": upgradeID,
		"history_id": upgradeUUID.String(),
	})
}

func (h *HelmHandler) GetUpgradeStatus(w http.ResponseWriter, r *http.Request) {
	upgradeID := chi.URLParam(r, "upgradeId")
	h.mu.RLock()
	stream := h.streams[upgradeID]
	h.mu.RUnlock()

	if stream == nil {
		Error(w, http.StatusNotFound, "upgrade not found")
		return
	}

	stream.mu.Lock()
	defer stream.mu.Unlock()
	JSON(w, http.StatusOK, map[string]interface{}{
		"status": stream.status,
		"logs":   stream.logs,
		"done":   stream.done,
	})
}

// emitCapacityPreflight renders the pending config and reports any workload that would
// not fit, into the stream the operator is already watching.
//
// Deliberately not a refusal. A config that can schedule nothing is exactly the thing
// to block, and E49 set the precedent that a skipped check is a failure unless someone
// says otherwise — but a false positive here would block every deployment, and this
// check has never run in anger. Warn first, watch it be right, then decide (P1-16c).
func (h *HelmHandler) emitCapacityPreflight(ctx context.Context, stream *upgradeStream,
	releaseName, version string, values map[string]interface{}, upgradeID uuid.UUID) {

	if h.helm == nil || h.k8s == nil {
		return
	}
	manifest, err := h.helm.Render(ctx, releaseName, version, values)
	if err != nil {
		// Not checked is not the same as fine, and saying so costs one line.
		stream.emit("NOTE: capacity preflight skipped — the chart could not be rendered: " + err.Error())
		return
	}
	nodes, err := h.k8s.NodeInfo(ctx)
	if err != nil {
		stream.emit("NOTE: capacity preflight skipped — node capacity unavailable: " + err.Error())
		return
	}

	findings := capacity.Check(manifest, capacity.FromNodeInfo(nodes))
	for _, f := range findings {
		switch f.Level {
		case capacity.LevelBlocked:
			stream.emit("WARNING: " + f.Message)
		case capacity.LevelWarn:
			stream.emit("NOTE: " + f.Message)
		case capacity.LevelUnknown:
			stream.emit("NOTE: " + f.Message)
		}
	}
	if capacity.Blocking(findings) {
		stream.emit("WARNING: Diese Konfiguration wird angewendet, aber mindestens ein Pod " +
			"kann danach auf keinem Node laufen. Genau so entstand der Ausfall vom 16.–18.08.")
	} else if len(findings) == 0 {
		stream.emit("Capacity preflight: every workload fits the cluster.")
	}

	// Recorded, not only streamed (etappe 63). Until now the preflight's verdict existed
	// solely in a WebSocket, so once the tab closed "were we warned before applying
	// that?" had no answer — and that is precisely when the question gets asked.
	//
	// An empty findings list is stored as an empty array rather than left NULL: "checked
	// and nothing was wrong" and "never checked" are different answers, and NULL is
	// reserved for the second (§4.59's rule against inventing a past).
	if findings == nil {
		findings = []capacity.Finding{}
	}
	if blob, err := json.Marshal(findings); err == nil {
		if _, err := h.db.Exec(ctx,
			"UPDATE upgrade_history SET pre_flight=$1 WHERE id=$2", blob, upgradeID); err != nil {
			log.Printf("preflight: could not record findings: %v", err)
		}
	}
}
