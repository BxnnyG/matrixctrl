package handlers

import (
	"context"
	"os"
	"testing"

	"github.com/bxnnyg/matrixctrl/internal/drift"
	"github.com/bxnnyg/matrixctrl/internal/helm"
	"github.com/bxnnyg/matrixctrl/internal/k8s"
)

// Against a real cluster: every hand-edit is judged, and the verdict is logged so the
// run can be compared with what is known about the installation (etappe 111).
func TestLiveHandEditsAgainstTheChart(t *testing.T) {
	if os.Getenv("RUN_LIVE") == "" {
		t.Skip("set RUN_LIVE=1")
	}
	kc, err := k8s.New()
	if err != nil {
		t.Fatal(err)
	}
	hc, err := helm.New("ess")
	if err != nil {
		t.Fatal(err)
	}
	h := NewDriftHandler(nil, kc, "ess")
	h.SetManifestSource(func() (string, error) { return hc.ReleaseManifest("ess") })
	ctx := context.Background()
	edits, _ := h.manualEdits(ctx, nil)
	h.markChartMatches(ctx, edits)
	for _, e := range edits {
		if e.Kind != drift.Human {
			continue
		}
		t.Logf("%s/%s by %s: matches_chart=%v (%d Felder)", e.Resource, e.Name, e.Manager, e.MatchesChart, len(e.Paths))
	}
	un, _, _, aligned := drift.SummariseManual(edits)
	t.Logf("laut: %d, leise (gleich dem Chart): %d", un, aligned)
}
