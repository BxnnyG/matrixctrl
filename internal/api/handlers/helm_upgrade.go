// Upgrading the managed ESS release: version upgrades and applying config
// changes. Both stream their progress through the stream in helm_stream.go.
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/bxnnyg/matrixctrl/internal/api/middleware"
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
