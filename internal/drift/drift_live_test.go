package drift

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/bxnnyg/matrixctrl/internal/k8s"
)

// TestCheckAgainstLiveCluster runs the real built-in hook patches against a real
// cluster. Skipped unless RUN_LIVE=1 (needs KUBECONFIG).
//
// The unit tests prove the comparison logic with hand-written JSON. This proves the
// parts they cannot: that GetObjectJSON resolves the same resource the patch would
// be applied to, that a live object with its hundreds of defaulted and
// controller-set fields does not produce spurious diffs, and that the answer for a
// cluster whose state is known is the answer we know.
func TestCheckAgainstLiveCluster(t *testing.T) {
	if os.Getenv("RUN_LIVE") == "" {
		t.Skip("set RUN_LIVE=1 to run against a live cluster")
	}

	client, err := k8s.New()
	if err != nil {
		t.Fatalf("k8s.New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Verbatim from internal/hooks/builtin/ess_rtc_patches.go.
	actions := []Action{
		{
			Hook: "ESS RTC: SFU Host Network", Description: "hostNetwork=true",
			Resource: "deployment", Namespace: "ess", Name: "ess-matrix-rtc-sfu",
			PatchType: "json",
			Patch: `[{"op":"add","path":"/spec/template/spec/hostNetwork","value":true},` +
				`{"op":"add","path":"/spec/template/spec/dnsPolicy","value":"ClusterFirstWithHostNet"}]`,
		},
	}
	for _, svc := range []string{
		"ess-matrix-rtc-sfu-turn", "ess-matrix-rtc-sfu-muxed-udp", "ess-matrix-rtc-sfu-tcp",
	} {
		actions = append(actions, Action{
			Hook: "ESS RTC: Service ExternalTrafficPolicy", Description: "externalTrafficPolicy=Local",
			Resource: "service", Namespace: "ess", Name: svc,
			PatchType: "merge", Patch: `{"spec":{"externalTrafficPolicy":"Local"}}`,
		})
	}

	findings := Check(ctx, actions, client)
	for _, f := range findings {
		t.Logf("%-34s %-10s %s %v", f.Name, f.Status, f.Detail, f.Paths)
	}

	// A live object carries resourceVersion, creationTimestamp, defaulted ports,
	// controller-set status and much else. If the diff were naive, every finding
	// here would be "drifted" — so this assertion is really about false positives.
	s := Summary(findings)
	if s[Unknown] > 0 {
		t.Errorf("%d finding(s) unknown against a reachable cluster", s[Unknown])
	}
	if s[Drifted] > 0 {
		t.Errorf("%d finding(s) drifted — either the cluster really has drifted, "+
			"or the comparison produces false positives on real objects", s[Drifted])
	}

	// A checker that can only ever say "fine" proves nothing. `turn-tls` runs with
	// externalTrafficPolicy: Cluster by design — the built-in hook covers three of
	// four services (see DESIGN §4 / E19) — so checking it against a Local patch
	// must report drift. This exercises the negative case against a real object and
	// changes nothing in the cluster.
	negative := Check(ctx, []Action{{
		Hook: "(not a real hook)", Description: "would set externalTrafficPolicy=Local",
		Resource: "service", Namespace: "ess", Name: "ess-matrix-rtc-sfu-turn-tls",
		PatchType: "merge", Patch: `{"spec":{"externalTrafficPolicy":"Local"}}`,
	}}, client)

	if len(negative) != 1 || negative[0].Status != Drifted {
		t.Fatalf("turn-tls runs Cluster by design, so a Local patch must read as "+
			"drifted; got %+v", negative)
	}
	if len(negative[0].Paths) != 1 || negative[0].Paths[0] != "spec.externalTrafficPolicy" {
		t.Errorf("the finding must name the field that would change, got %v", negative[0].Paths)
	}
	t.Logf("negative case: %s → %s %v", negative[0].Name, negative[0].Status, negative[0].Paths)
}
