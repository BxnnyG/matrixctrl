package handlers

import (
	"context"
	"fmt"

	"github.com/bxnnyg/matrixctrl/internal/config"
)

// A block of MAS configuration that MatrixCtrl keeps in a Kubernetes Secret (etappe
// 114b, extracted from etappe 110).
//
// Two features need the same three steps, because both hold a password MAS reads and the
// settings repository must not: the login providers' client secrets (§4.112) and the SMTP
// credentials. The chart mounts such a Secret through
// `matrixAuthenticationService.additional.<name>.configSecret`, so the settings hold a
// reference and never the secret itself.
//
// Doing it twice by hand would mean two places to get the activation right — and the
// activation is the part with the edge cases: nothing mounted yet (a pending change), not
// deployed yet (the same), or mounted and deployed, where only the Secret's content
// changes and the pod template does not, so nothing would restart MAS on its own.
type masBlock struct {
	k8s   upstreamCluster
	store *config.Store
	essNS string
	// deployment is the MAS workload, for the targeted restart.
	deployment string
	// secret is the Kubernetes Secret, name is the key under `additional` that mounts
	// it, and key is the entry inside the Secret that MAS reads.
	secret, name, key string
}

// wiring says whether MAS reads the Secret: in the settings, and on the cluster.
type wiring struct {
	InConfig bool `json:"in_config"`
	Deployed bool `json:"deployed"`
}

func (b masBlock) values(ctx context.Context) map[string]interface{} {
	contents, err := b.store.MergedContent(ctx)
	if err != nil {
		return nil
	}
	merged, err := config.MergeToMap(contents)
	if err != nil {
		return nil
	}
	return merged
}

func (b masBlock) wiring(ctx context.Context, values map[string]interface{}) wiring {
	var w wiring
	if s, _ := nestedGet(values, "matrixAuthenticationService", "additional", b.name, "configSecret").(string); s == b.secret {
		w.InConfig = true
	}
	mounted, err := b.k8s.SecretVolumes(ctx, b.essNS, b.deployment)
	if err != nil {
		return w
	}
	for _, name := range mounted {
		if name == b.secret {
			w.Deployed = true
		}
	}
	return w
}

// read returns the entries of the Secret, empty when there is none.
func (b masBlock) read(ctx context.Context) (map[string][]byte, error) {
	return b.k8s.GetSecret(ctx, b.essNS, b.secret)
}

// write stores the Secret's entries.
func (b masBlock) write(ctx context.Context, data map[string][]byte) error {
	return b.k8s.PutSecret(ctx, b.essNS, b.secret, data)
}

// activate makes MAS read what was just written, and says what happens next:
//
//   - "apply": the settings do not mount the Secret yet. The reference is written as a
//     pending change; "Übernehmen" deploys it and restarts MAS.
//   - "apply-pending": mounted in the settings, not yet deployed — same next step.
//   - "restarting": deployed; only the Secret's content changed, which does not change
//     the pod template, so nothing would restart MAS on its own. Restarted here.
func (b masBlock) activate(ctx context.Context) (string, error) {
	values := b.values(ctx)
	wr := b.wiring(ctx, values)
	if !wr.InConfig {
		base := "matrixAuthenticationService.additional." + b.name + "."
		if err := b.store.SetSectionValues(ctx, map[string]interface{}{
			base + "configSecret":    b.secret,
			base + "configSecretKey": b.key,
		}, nil); err != nil {
			return "", fmt.Errorf("Einbindung in die Einstellungen: %w", err)
		}
		return "apply", nil
	}
	if !wr.Deployed {
		return "apply-pending", nil
	}
	if err := b.k8s.RolloutRestart(ctx, b.essNS, "deployment", b.deployment); err != nil {
		return "", err
	}
	return "restarting", nil
}
