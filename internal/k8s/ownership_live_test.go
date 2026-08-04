package k8s

import (
	"context"
	"os"
	"testing"
	"time"
)

// Verifies the one link the unit tests cannot: that a metadata-only list really
// returns managedFields. If the API server stripped them, every report would be
// empty and would look exactly like a clean cluster.
func TestLiveListOwnership(t *testing.T) {
	if os.Getenv("RUN_LIVE") == "" {
		t.Skip("set RUN_LIVE=1")
	}
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if c.Meta == nil {
		t.Fatal("metadata client is nil — the whole feature would be silently empty")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	objs, problems := c.ListOwnership(ctx, "ess", OwnershipTypes)
	for _, p := range problems {
		t.Errorf("problem: %v", p)
	}
	withFields := 0
	for _, o := range objs {
		for _, e := range o.Entries {
			if len(e.FieldsV1) > 0 {
				withFields++
			}
		}
	}
	t.Logf("objects=%d entries-with-fields=%d", len(objs), withFields)
	if withFields == 0 {
		t.Fatal("no fieldsV1 came back — a metadata list that drops them makes every report empty")
	}
}
