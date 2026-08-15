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

// TestHistoryReadCostLive measures the history read and checks that the fast path
// and the fallback agree, which is the only thing that makes the fast path safe.
//
// Same intent as TestReleaseReadCostLive one page over: the design in
// docs/plans/etappe-39-history-read.md rests on measured numbers, and a number
// written into a comment decays silently. Skipped unless RUN_LIVE=1.
func TestHistoryReadCostLive(t *testing.T) {
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
	cold, err := c.ListHistory(name, 20)
	coldCost := time.Since(start)
	if err != nil {
		t.Fatalf("cold ListHistory: %v", err)
	}

	start = time.Now()
	warm, err := c.ListHistory(name, 20)
	warmCost := time.Since(start)
	if err != nil {
		t.Fatalf("warm ListHistory: %v", err)
	}
	t.Logf("history: cold %v, warm %v, %d revisions", coldCost.Round(time.Millisecond), warmCost.Round(time.Millisecond), len(warm))

	if warmCost > coldCost {
		t.Errorf("the warm read (%v) should not cost more than the cold one (%v)", warmCost, coldCost)
	}

	// `max` has to bound the result. Helm's own History action accepts a Max and
	// never reads it, which is the defect this replaced.
	if len(warm) > 2 {
		short, err := c.ListHistory(name, 2)
		if err != nil {
			t.Fatalf("ListHistory(max=2): %v", err)
		}
		if len(short) != 2 {
			t.Errorf("max=2 returned %d entries", len(short))
		}
		// A small page must not evict facts for the revisions it left out, or the
		// next full read pays for them again (the prune-ordering bug).
		start = time.Now()
		again, err := c.ListHistory(name, 20)
		againCost := time.Since(start)
		if err != nil {
			t.Fatalf("ListHistory after a short page: %v", err)
		}
		t.Logf("after a max=2 page, the full read cost %v", againCost.Round(time.Millisecond))
		if len(again) != len(warm) {
			t.Errorf("full read after a short page returned %d, want %d", len(again), len(warm))
		}
		if againCost > coldCost/2 {
			t.Errorf("a short page evicted cached revisions: full read cost %v against a cold %v", againCost, coldCost)
		}
	}

	// Newest first, and every row complete. A blank chart column would be the
	// signature of a fast path that answered without the decode it needed.
	for i, e := range cold {
		if i > 0 && cold[i-1].Revision <= e.Revision {
			t.Errorf("revisions are not newest-first at index %d: %d then %d", i, cold[i-1].Revision, e.Revision)
		}
		if e.Chart == "" || e.Status == "" || e.DeployedAt.IsZero() {
			t.Errorf("incomplete row: %+v", e)
		}
	}

	// The fallback has to produce the same answer, or the fast path is a guess.
	// Nil the metadata client to force it, exactly as a cluster refusing the
	// metadata API would.
	meta := c.meta
	c.meta = nil
	start = time.Now()
	slow, err := c.ListHistory(name, 20)
	slowCost := time.Since(start)
	c.meta = meta
	if err != nil {
		t.Fatalf("fallback ListHistory: %v", err)
	}
	t.Logf("fallback (no metadata client): %v", slowCost.Round(time.Millisecond))

	if len(slow) != len(cold) {
		t.Fatalf("fallback returned %d revisions, fast path %d", len(slow), len(cold))
	}
	for i := range slow {
		if slow[i].Revision != cold[i].Revision ||
			slow[i].Status != cold[i].Status ||
			slow[i].Chart != cold[i].Chart ||
			!slow[i].DeployedAt.Equal(cold[i].DeployedAt) {
			t.Errorf("row %d differs:\n  fast %+v\n  slow %+v", i, cold[i], slow[i])
		}
	}
}
