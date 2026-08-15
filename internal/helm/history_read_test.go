package helm

import (
	"testing"
	"time"
)

// The revision cache never touches cfg or the cluster, so a zero-value client is
// enough — same trade as cache_test.go.

func TestRevisionFactsAreKeyedPerRevision(t *testing.T) {
	c := newTestClient()
	c.storeRevisionFacts("ess", 28, revisionFacts{Chart: "matrix-stack-26.7.1", DeployedAt: time.Unix(100, 0)})
	c.storeRevisionFacts("ess", 29, revisionFacts{Chart: "matrix-stack-26.7.2", DeployedAt: time.Unix(200, 0)})

	if f, ok := c.revisionFacts("ess", 28); !ok || f.Chart != "matrix-stack-26.7.1" {
		t.Fatalf("revision 28 = %+v, ok=%v", f, ok)
	}
	if f, ok := c.revisionFacts("ess", 29); !ok || f.Chart != "matrix-stack-26.7.2" {
		t.Fatalf("revision 29 = %+v, ok=%v", f, ok)
	}
	if _, ok := c.revisionFacts("ess", 30); ok {
		t.Fatal("a revision never stored must miss")
	}
	if _, ok := c.revisionFacts("other", 29); ok {
		t.Fatal("facts must not leak between releases")
	}
}

// Helm deletes revisions beyond --history-max. Facts about revisions that no
// longer exist have to go with them, or a long-lived process accumulates records
// of things that stopped existing months ago.
func TestPruneDropsRevisionsTheClusterNoLongerReports(t *testing.T) {
	c := newTestClient()
	for rev := 20; rev <= 29; rev++ {
		c.storeRevisionFacts("ess", rev, revisionFacts{Chart: "matrix-stack-26.7.2"})
	}

	live := map[int]bool{}
	for rev := 25; rev <= 29; rev++ {
		live[rev] = true
	}
	c.pruneRevisionFacts("ess", live)

	for rev := 20; rev <= 24; rev++ {
		if _, ok := c.revisionFacts("ess", rev); ok {
			t.Errorf("revision %d is gone from the cluster and must be pruned", rev)
		}
	}
	for rev := 25; rev <= 29; rev++ {
		if _, ok := c.revisionFacts("ess", rev); !ok {
			t.Errorf("revision %d still exists and must be kept", rev)
		}
	}
}

// The bug this pins was real and cost 2.958 s where 40 ms was expected.
//
// Pruning against the *truncated* list makes `max` destructive: a call with max=5
// evicts every older revision's facts, so the next call with max=10 re-decodes
// revisions it had already paid for. The fix is ordering — prune against the full
// label list, then truncate — and ordering is exactly what a later refactor loses.
func TestPruneUsesTheFullListNotThePage(t *testing.T) {
	c := newTestClient()
	all := []int{25, 26, 27, 28, 29}
	for _, rev := range all {
		c.storeRevisionFacts("ess", rev, revisionFacts{Chart: "matrix-stack-26.7.2"})
	}

	// What listHistoryFast must do: build `live` from every revision the labels
	// report, regardless of the page size the caller asked for.
	live := map[int]bool{}
	for _, rev := range all {
		live[rev] = true
	}
	c.pruneRevisionFacts("ess", live)

	for _, rev := range all {
		if _, ok := c.revisionFacts("ess", rev); !ok {
			t.Fatalf("revision %d exists in the cluster; a small max must not evict it", rev)
		}
	}
}

// The cache must not round. An earlier version stored Unix seconds, so the fast
// path returned timestamps up to a second away from the fallback's — caught by the
// live cross-check, which is the only place the two are ever compared.
func TestRevisionFactsKeepSubSecondPrecision(t *testing.T) {
	c := newTestClient()
	want := time.Date(2026, 5, 25, 19, 1, 49, 117941691, time.UTC)
	c.storeRevisionFacts("ess", 20, revisionFacts{Chart: "matrix-stack-26.5.1", DeployedAt: want})

	got, ok := c.revisionFacts("ess", 20)
	if !ok {
		t.Fatal("stored revision must be a hit")
	}
	if !got.DeployedAt.Equal(want) {
		t.Errorf("DeployedAt = %v, want %v", got.DeployedAt, want)
	}
}
