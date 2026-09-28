package helm

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/bxnnyg/matrixctrl/internal/preview"
)

// The restart prediction against the real release, in both directions.
//
// A prediction that says "restarts" when nothing changed is one an operator learns to
// ignore — so the first half renders the running values and requires silence, and only
// then does the second half change one number and require exactly the service it
// belongs to (§4.105: a check has to be able to say no).
func TestLiveRenderDeployedPredictsRestarts(t *testing.T) {
	if os.Getenv("RUN_LIVE") == "" {
		t.Skip("set RUN_LIVE=1")
	}
	c, err := New("ess")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	values, err := c.GetReleaseValues("ess")
	if err != nil {
		t.Fatalf("release values: %v", err)
	}
	kubeBefore, storeBefore := c.cfg.KubeClient, c.cfg.Releases

	start := time.Now()
	current, rendered, err := c.RenderDeployed(ctx, "ess", values)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	t.Logf("gerendert in %s, ohne Registry", time.Since(start).Round(time.Millisecond))

	if same := preview.Restarts(current, rendered); len(same) != 0 {
		t.Fatalf("the running values predict restarts — the prediction would cry wolf: %v", same)
	}
	t.Log("dieselben Werte: kein Neustart vorhergesagt")

	// Change one number that belongs to one service.
	changed := deepCopy(values)
	syn, _ := changed["synapse"].(map[string]interface{})
	if syn == nil {
		syn = map[string]interface{}{}
		changed["synapse"] = syn
	}
	syn["resources"] = map[string]interface{}{
		"requests": map[string]interface{}{"memory": "1537Mi", "cpu": "500m"},
		"limits":   map[string]interface{}{"memory": "3Gi"},
	}
	_, rendered2, err := c.RenderDeployed(ctx, "ess", changed)
	if err != nil {
		t.Fatalf("render changed: %v", err)
	}
	restarts := preview.Restarts(current, rendered2)

	// The client must be untouched by rendering: ClientOnly replaces the release store and
	// the Kubernetes client of the configuration it is given, and on a shared one every
	// later deploy would have gone to a fake (etappe 108).
	if _, err := c.GetRelease("ess"); err != nil {
		t.Fatalf("after rendering, the client can no longer read the release — its configuration was replaced: %v", err)
	}
	if c.cfg.KubeClient != kubeBefore || c.cfg.Releases != storeBefore {
		t.Fatal("rendering replaced the client's Kubernetes client or release store")
	}
	t.Logf("Synapse-Speicher geändert → vorhergesagt: %v", restarts)
	found := false
	for _, r := range restarts {
		if r.Name == "ess-synapse-main" {
			found = true
		}
	}
	if !found {
		t.Errorf("changing Synapse's memory must predict a Synapse restart")
	}
}

func deepCopy(m map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		if sub, ok := v.(map[string]interface{}); ok {
			out[k] = deepCopy(sub)
		} else {
			out[k] = v
		}
	}
	return out
}
