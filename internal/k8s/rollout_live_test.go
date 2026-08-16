package k8s

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestWorkloadRolloutAgainstLiveCluster proves the reads the upgrade screen depends
// on actually return something under the real ServiceAccount's namespaced Role.
// Skipped unless RUN_LIVE=1 (needs KUBECONFIG).
//
// The unit tests in internal/rollout prove the reasoning with hand-written structs.
// They cannot prove what this does: that the workload list is non-empty against a
// real namespace, that Generation and ObservedGeneration are actually populated —
// a nil-valued pair would silently make every component read "waiting" forever —
// and that the field-selected event list is permitted rather than 403.
func TestWorkloadRolloutAgainstLiveCluster(t *testing.T) {
	if os.Getenv("RUN_LIVE") == "" {
		t.Skip("set RUN_LIVE=1 to run against a live cluster")
	}
	ns := os.Getenv("ESS_NAMESPACE")
	if ns == "" {
		ns = "ess"
	}

	client, err := New()
	if err != nil {
		t.Fatalf("k8s.New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	workloads := client.WorkloadRollout(ctx, ns)
	if len(workloads) == 0 {
		t.Fatalf("no workloads in %s — either the namespace is wrong or the Role does not permit listing them", ns)
	}

	var statefulSets, done int
	for _, w := range workloads {
		if w.Desired <= 0 {
			t.Errorf("%s %s: desired = %d, want > 0 (scaled-to-zero should have been skipped)", w.Kind, w.Name, w.Desired)
		}
		if w.Generation == 0 {
			t.Errorf("%s %s: Generation is 0 — every real object has one, so this read is not returning what it should", w.Kind, w.Name)
		}
		if w.Kind == "StatefulSet" {
			statefulSets++
		}
		if w.Done() {
			done++
		}
		t.Logf("%-12s %-45s %d/%d updated=%d gen=%d/%d done=%v",
			w.Kind, w.Name, w.Ready, w.Desired, w.Updated, w.Observed, w.Generation, w.Done())
	}

	// ess-synapse-main and ess-postgres are both StatefulSets, and a version of this
	// that listed only Deployments would hide the two components an operator most
	// wants to watch (CLAUDE.md).
	if statefulSets == 0 {
		t.Error("no StatefulSets found — synapse and postgres are StatefulSets, so this read is incomplete")
	}

	// On a settled cluster every workload is done. If this fails while nothing is
	// rolling, the Done() condition is wrong and the progress bar would never reach
	// 100 %.
	t.Logf("%d of %d workloads settled", done, len(workloads))

	// PullingPods returns nil on any failure and a non-nil (often empty) map on
	// success, so nil is the assertion that matters: an empty map is the expected
	// answer on a quiet cluster — Kubernetes drops events after an hour — while nil
	// means the field selector was rejected or the Role does not permit the list.
	// Checking len() alone cannot tell those apart, which is what the first version
	// of this test did.
	pulling := client.PullingPods(ctx, ns, time.Now().Add(-2*time.Hour))
	if pulling == nil {
		t.Error("PullingPods returned nil — the event list was rejected, not merely empty")
	}
	t.Logf("pods pulling in the last two hours: %d", len(pulling))
}
