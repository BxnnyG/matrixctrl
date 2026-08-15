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
	found, err := Discover("ess")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	t.Logf("discovered %d ESS release(s):", len(found.Releases))
	for _, r := range found.Releases {
		t.Logf("  - %s/%s  version=%s  status=%s", r.Namespace, r.Name, r.Version, r.Status)
	}
	if len(found.Releases) == 0 {
		t.Errorf("expected at least one matrix-stack release")
	}

	// Validate the adopt source: read the release's user-supplied values.
	if len(found.Releases) > 0 {
		c, err := New(found.Releases[0].Namespace)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		vals, err := c.GetReleaseValues(found.Releases[0].Name)
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
