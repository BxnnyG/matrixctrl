package drift

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bxnnyg/matrixctrl/internal/hooks"
)

// HookCoverage decides, for each hook, whether the chart's rendered manifest already
// carries everything the hook patches (etappe 119b).
//
// The two built-in hooks were written when the ESS chart could not put the call server
// on the host network or keep source addresses; every upgrade undid the hand patches and
// calls broke. The chart has since learned both, and on the production server both values
// sat in the Helm values — the hooks were patching what Helm had just set, and rolling
// the SFU for it. A hook is skipped only when every one of its patches is covered; a hook
// with an HTTP call, a removing JSON patch or nothing to compare always runs.
func HookCoverage(manifest func() (string, error)) hooks.Coverage {
	return func(h hooks.Hook) string {
		var covered []string
		var m string
		loaded := false
		for _, a := range h.Actions {
			switch a.Type {
			case hooks.ActionWaitRollout:
				continue
			case hooks.ActionKubectlPatch:
			default:
				return ""
			}
			sets, err := a.Sets()
			if err != nil || len(sets) == 0 {
				return ""
			}
			if !loaded {
				if m, err = manifest(); err != nil || m == "" {
					return ""
				}
				loaded = true
			}
			if ok, _ := PatchCovered(m, a.Resource, a.Name, sets); !ok {
				return ""
			}
			var leaves []string
			for p := range sets {
				leaves = append(leaves, p[strings.LastIndex(p, ".")+1:])
			}
			sort.Strings(leaves)
			covered = append(covered, fmt.Sprintf("%s (%s)", a.Name, strings.Join(leaves, ", ")))
		}
		if len(covered) == 0 {
			return ""
		}
		return "Nicht mehr nötig: Die ESS-Konfiguration setzt das bereits selbst — " + strings.Join(covered, "; ") +
			". Der Patch würde nichts ändern."
	}
}
