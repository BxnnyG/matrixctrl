package helm

import (
	"os"
	"testing"
)

// TestDiscoverLive validates ESS discovery against a real cluster. Skipped unless
// RUN_LIVE=1 (needs KUBECONFIG). Not a CI test.
func TestDiscoverLive(t *testing.T) {
	if os.Getenv("RUN_LIVE") == "" {
		t.Skip("set RUN_LIVE=1 to run against a live cluster")
	}
	found, err := Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	t.Logf("discovered %d ESS release(s):", len(found))
	for _, r := range found {
		t.Logf("  - %s/%s  version=%s  status=%s", r.Namespace, r.Name, r.Version, r.Status)
	}
	if len(found) == 0 {
		t.Errorf("expected at least one matrix-stack release")
	}

	// Validate the adopt source: read the release's user-supplied values.
	if len(found) > 0 {
		c, err := New(found[0].Namespace)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		vals, err := c.GetReleaseValues(found[0].Name)
		if err != nil {
			t.Fatalf("GetReleaseValues: %v", err)
		}
		keys := make([]string, 0, len(vals))
		for k := range vals {
			keys = append(keys, k)
		}
		t.Logf("release values top-level keys (%d): %v", len(keys), keys)
		if len(keys) == 0 {
			t.Errorf("expected non-empty release values")
		}
	}
}
