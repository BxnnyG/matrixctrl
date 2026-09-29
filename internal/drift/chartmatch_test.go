package drift

import (
	"encoding/json"
	"testing"
)

const manifest = `---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: ess-postgres
spec:
  template:
    spec:
      containers:
      - name: postgres
        resources:
          requests: {cpu: 500m, memory: 1Gi}
          limits: {cpu: 1000m, memory: 2Gi}
      - name: postgres-exporter
        resources:
          limits: {memory: 500Mi}
`

// The live object as the API server returns it after `kubectl set resources`:
// cpu "1" where the values file says 1000m.
const liveJSON = `{"spec":{"template":{"spec":{"containers":[
 {"name":"postgres-exporter","resources":{"limits":{"memory":"500Mi"}}},
 {"name":"postgres","resources":{"requests":{"cpu":"500m","memory":"1Gi"},"limits":{"cpu":"1","memory":"2Gi"}}}]}}}}`

var pgPaths = []string{
	"spec.template.spec.containers.{name=postgres}.resources.limits.cpu",
	"spec.template.spec.containers.{name=postgres}.resources.limits.memory",
	"spec.template.spec.containers.{name=postgres}.resources.requests.cpu",
	"spec.template.spec.containers.{name=postgres}.resources.requests.memory",
}

func live(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestHandSetValuesTheChartAlsoSetsMatch(t *testing.T) {
	chart := ManifestObject(manifest, "statefulset", "ess-postgres")
	if chart == nil {
		t.Fatal("object not found in manifest")
	}
	if !MatchesChart(live(t, liveJSON), chart, pgPaths) {
		t.Error("1 and 1000m are the same CPU; the fields match the chart")
	}
}

// Counter-probe: the emergency value before it went into the settings.
func TestAHandSetValueTheChartDoesNotWantStillCounts(t *testing.T) {
	chart := ManifestObject(manifest, "StatefulSet", "ess-postgres")
	diff := live(t, `{"spec":{"template":{"spec":{"containers":[{"name":"postgres","resources":{"requests":{"cpu":"500m","memory":"4Gi"},"limits":{"cpu":"1","memory":"2Gi"}}}]}}}}`)
	if MatchesChart(diff, chart, pgPaths) {
		t.Error("4Gi live against 1Gi in the chart is a real exception")
	}
	// A field the chart does not set at all is the hand-set value alone.
	if MatchesChart(live(t, liveJSON), chart, []string{"spec.template.spec.hostNetwork"}) {
		t.Error("a path the chart does not set cannot match it")
	}
}

func TestKeysWithDotsResolve(t *testing.T) {
	obj := map[string]any{"metadata": map[string]any{"annotations": map[string]any{"example.org/x": "y"}}}
	if v, ok := ValueAt(obj, "metadata.annotations.example.org/x"); !ok || v != "y" {
		t.Errorf("got %v %v", v, ok)
	}
}

func TestSameValue(t *testing.T) {
	for _, c := range [][2]any{{"1", "1000m"}, {"1536Mi", "1.5Gi"}, {true, true}, {"Local", "Local"}} {
		if !SameValue(c[0], c[1]) {
			t.Errorf("%v should equal %v", c[0], c[1])
		}
	}
	if SameValue("1Gi", "2Gi") || SameValue("Local", "Cluster") {
		t.Error("different values compared equal")
	}
}
