package helm

import (
	"context"
	"os"
	"testing"
	"time"
)

// memStore is a RevisionStore that survives being handed to a second Client, which
// is what makes it a stand-in for the database: the point of E42's persistence is
// that a *new process* does not repeat the decode, and a store held outside both
// Clients models exactly that.
type memStore struct {
	facts  map[int]RevisionFact
	loads  int
	writes int
}

func (m *memStore) LoadRevisionFacts(_ context.Context, _ string) (map[int]RevisionFact, error) {
	m.loads++
	out := make(map[int]RevisionFact, len(m.facts))
	for k, v := range m.facts {
		out[k] = v
	}
	return out, nil
}

func (m *memStore) SaveRevisionFacts(_ context.Context, _ string, facts map[int]RevisionFact) error {
	m.writes++
	for k, v := range facts {
		m.facts[k] = v
	}
	return nil
}

// TestHistoryPersistenceAcrossProcessesLive is the measurement E42 claimed and never
// took: that a *restarted* process reads the history cheaply.
//
// E39's in-memory cache made the second read in a process fast and left the first one
// at 3–7 s. The framing "cold once per process" hid how often that is — this project
// deploys several times a day, so the operator met the cold path constantly and
// measured 7.7 s. This test runs two independent Clients against the same store, which
// is the shape of a pod restart.
func TestHistoryPersistenceAcrossProcessesLive(t *testing.T) {
	if os.Getenv("RUN_LIVE") == "" {
		t.Skip("set RUN_LIVE=1 to run against a live cluster")
	}
	ns := envOr("MATRIXCTRL_ESS_NAMESPACE", "ess")
	name := envOr("MATRIXCTRL_ESS_RELEASE", "ess")

	store := &memStore{facts: map[int]RevisionFact{}}

	first, err := New(ns)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if first.meta == nil {
		t.Fatal("no metadata client — this run would only measure the fallback")
	}
	first.SetRevisionStore(store)

	start := time.Now()
	cold, err := first.ListHistory(name, 20)
	coldCost := time.Since(start)
	if err != nil {
		t.Fatalf("cold ListHistory: %v", err)
	}
	t.Logf("process 1, empty store: %v for %d revisions", coldCost.Round(time.Millisecond), len(cold))

	if store.writes == 0 {
		t.Fatal("nothing was persisted — the store would never fill, and every restart would pay the full decode")
	}
	if len(store.facts) != len(cold) {
		t.Errorf("persisted %d facts for %d revisions", len(store.facts), len(cold))
	}

	// A second Client with no in-memory cache of its own: a restarted pod.
	second, err := New(ns)
	if err != nil {
		t.Fatalf("New (second): %v", err)
	}
	second.SetRevisionStore(store)

	start = time.Now()
	warm, err := second.ListHistory(name, 20)
	warmCost := time.Since(start)
	if err != nil {
		t.Fatalf("ListHistory after restart: %v", err)
	}
	t.Logf("process 2, populated store: %v for %d revisions", warmCost.Round(time.Millisecond), len(warm))

	if len(warm) != len(cold) {
		t.Fatalf("restarted process saw %d revisions, first saw %d", len(warm), len(cold))
	}

	// The whole claim, stated as an assertion rather than a log line.
	if warmCost >= coldCost {
		t.Errorf("a restarted process cost %v against the first process's %v — persistence bought nothing",
			warmCost.Round(time.Millisecond), coldCost.Round(time.Millisecond))
	}

	// Row-for-row equality. A cache that is fast and disagrees with the source is
	// worse than a slow one: the first version of this stored Unix seconds and
	// produced timestamps up to a second away from the fallback's.
	for i := range cold {
		if cold[i].Revision != warm[i].Revision || cold[i].Chart != warm[i].Chart ||
			!cold[i].DeployedAt.Equal(warm[i].DeployedAt) {
			t.Errorf("row %d differs:\n  fresh: %+v\n  store: %+v", i, cold[i], warm[i])
		}
	}
}
