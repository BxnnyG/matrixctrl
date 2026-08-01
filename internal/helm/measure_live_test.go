package helm

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestReleaseReadCostLive measures what a release read actually costs against a
// real cluster, and — more importantly — checks that the fast path and the old
// slow path return the same thing. Skipped unless RUN_LIVE=1.
//
// It exists because the design in docs/plans/etappe-20-release-read.md rests on
// measured numbers, and a number written into a comment decays silently. This lets
// anyone re-check them in two seconds instead of trusting them.
func TestReleaseReadCostLive(t *testing.T) {
	if os.Getenv("RUN_LIVE") == "" {
		t.Skip("set RUN_LIVE=1 to run against a live cluster")
	}
	ns := envOr("MATRIXCTRL_ESS_NAMESPACE", "ess")
	name := envOr("MATRIXCTRL_ESS_RELEASE", "ess")

	c, err := New(ns)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.meta == nil {
		t.Fatal("no metadata client — this run would only measure the fallback")
	}

	start := time.Now()
	id, err := c.probeNewestRelease(context.Background(), name)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	t.Logf("probe (metadata only): %v  → revision %d, status %s", time.Since(start), id.Revision, id.Status)

	start = time.Now()
	cold, err := c.GetRelease(name)
	if err != nil {
		t.Fatalf("GetRelease (cold): %v", err)
	}
	coldCost := time.Since(start)

	start = time.Now()
	if _, err := c.GetRelease(name); err != nil {
		t.Fatalf("GetRelease (warm): %v", err)
	}
	warmCost := time.Since(start)

	// The fallback still has to produce the same answer, or the fast path is not
	// an optimisation but a second, differently-wrong source of truth.
	start = time.Now()
	slow, err := c.getReleaseUncached(name)
	if err != nil {
		t.Fatalf("getReleaseUncached: %v", err)
	}
	slowCost := time.Since(start)

	t.Logf("cold=%v  warm=%v  fallback(action.NewGet)=%v", coldCost, warmCost, slowCost)

	if *cold != *slow {
		t.Errorf("fast and slow path disagree:\n  fast: %+v\n  slow: %+v", *cold, *slow)
	}
	if cold.Revision != id.Revision {
		t.Errorf("probe saw revision %d, decode returned %d", id.Revision, cold.Revision)
	}
	if cold.Status != id.Status {
		t.Errorf("probe read status %q from labels, decode returned %q", id.Status, cold.Status)
	}
}

func envOr(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}
