package builtin

import (
	"context"
	"encoding/json"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bxnny/matrixctrl/internal/hooks"
)

// ESSRTCHooks are the built-in hooks that reproduce the manual patches
// required after every ESS helm upgrade on NAT deployments.
// See ~/patch-svc.sh and /opt/docs/ for the rationale.
var ESSRTCHooks = []hooks.Hook{
	{
		Name:        "ESS RTC: SFU Host Network",
		Description: "Enables hostNetwork=true on the LiveKit SFU deployment (required for NAT traversal). Reproduces: kubectl patch deployment -n ess ess-matrix-rtc-sfu --type=json -p='[{\"op\":\"add\",\"path\":\"/spec/template/spec/hostNetwork\",\"value\":true}]'",
		Trigger:     hooks.TriggerPostUpgrade,
		Enabled:     true,
		Builtin:     true,
		Priority:    10,
		CreatedBy:   "system",
		Actions: []hooks.HookAction{
			{
				Type:        hooks.ActionKubectlPatch,
				Description: "Set hostNetwork=true on ess-matrix-rtc-sfu deployment",
				Resource:    "deployment",
				Name:        "ess-matrix-rtc-sfu",
				Namespace:   "ess",
				PatchType:   "json",
				Patch:       `[{"op":"add","path":"/spec/template/spec/hostNetwork","value":true},{"op":"add","path":"/spec/template/spec/dnsPolicy","value":"ClusterFirstWithHostNet"}]`,
			},
			{
				Type:        hooks.ActionWaitRollout,
				Description: "Wait for SFU rollout after hostNetwork patch",
				Resource:    "deployment",
				Name:        "ess-matrix-rtc-sfu",
				Namespace:   "ess",
				TimeoutSecs: 120,
			},
		},
	},
	{
		Name:        "ESS RTC: Service ExternalTrafficPolicy",
		Description: "Sets externalTrafficPolicy=Local on the three SFU NodePort services to preserve source IPs (required for STUN). Reproduces: ~/patch-svc.sh",
		Trigger:     hooks.TriggerPostUpgrade,
		Enabled:     true,
		Builtin:     true,
		Priority:    20,
		CreatedBy:   "system",
		Actions: []hooks.HookAction{
			{
				Type:        hooks.ActionKubectlPatch,
				Description: "externalTrafficPolicy=Local on ess-matrix-rtc-sfu-turn",
				Resource:    "service",
				Name:        "ess-matrix-rtc-sfu-turn",
				Namespace:   "ess",
				PatchType:   "merge",
				Patch:       `{"spec":{"externalTrafficPolicy":"Local"}}`,
			},
			{
				Type:        hooks.ActionKubectlPatch,
				Description: "externalTrafficPolicy=Local on ess-matrix-rtc-sfu-muxed-udp",
				Resource:    "service",
				Name:        "ess-matrix-rtc-sfu-muxed-udp",
				Namespace:   "ess",
				PatchType:   "merge",
				Patch:       `{"spec":{"externalTrafficPolicy":"Local"}}`,
			},
			{
				Type:        hooks.ActionKubectlPatch,
				Description: "externalTrafficPolicy=Local on ess-matrix-rtc-sfu-tcp",
				Resource:    "service",
				Name:        "ess-matrix-rtc-sfu-tcp",
				Namespace:   "ess",
				PatchType:   "merge",
				Patch:       `{"spec":{"externalTrafficPolicy":"Local"}}`,
			},
		},
	},
}

// Seed inserts the built-in hooks into the database if they don't exist yet.
func Seed(ctx context.Context, db *pgxpool.Pool) error {
	for _, h := range ESSRTCHooks {
		var exists bool
		err := db.QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM hooks WHERE name=$1 AND builtin=TRUE)", h.Name,
		).Scan(&exists)
		if err != nil {
			return err
		}
		actionsJSON, err := json.Marshal(h.Actions)
		if err != nil {
			return err
		}

		if exists {
			// Always update built-in hooks so patch content stays in sync with the binary.
			_, err = db.Exec(ctx, `
				UPDATE hooks SET description=$2, actions=$3, priority=$4, updated_at=NOW()
				WHERE name=$1 AND builtin=TRUE`,
				h.Name, h.Description, actionsJSON, h.Priority,
			)
			if err != nil {
				return err
			}
			continue
		}

		_, err = db.Exec(ctx, `
			INSERT INTO hooks(name, description, trigger, enabled, priority, actions, builtin, created_by)
			VALUES($1, $2, $3, $4, $5, $6, TRUE, $7)`,
			h.Name, h.Description, h.Trigger, h.Enabled, h.Priority, actionsJSON, h.CreatedBy,
		)
		if err != nil {
			return err
		}
		log.Printf("MatrixCtrl: seeded built-in hook: %s", h.Name)
	}
	return nil
}
