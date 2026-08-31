package capacity

import (
	"strings"
	"testing"
)

// The real shape: one `resources` block in the values file becomes three containers in
// the manifest. Reading postgres.yaml would see 4000m; the pod is 8500m. This is the
// case that proves the check looks through the chart rather than at the values
// (etappe 55, §4.53).
const postgresManifest = `
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: ess-postgres
spec:
  template:
    spec:
      containers:
        - name: postgres
          resources: {requests: {cpu: "4", memory: 4Gi}}
        - name: postgres-ess-updater
          resources: {requests: {cpu: "4", memory: 4Gi}}
        - name: postgres-exporter
          resources: {requests: {cpu: 500m, memory: 500Mi}}
`

// The other half: the value lands in init containers too, and a pod's request is
// max(sum(containers), max(initContainers)).
const synapseManifest = `
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: ess-synapse-main
spec:
  template:
    spec:
      initContainers:
        - name: render-config
          resources: {requests: {cpu: "4", memory: 4Gi}}
        - name: db-wait
          resources: {requests: {cpu: "4", memory: 4Gi}}
      containers:
        - name: synapse
          resources: {requests: {cpu: "1", memory: 4Gi}}
`

func node(alloc, used int64) []Node {
	return []Node{{Name: "n1", CPUAllocatableMillis: alloc, CPUUsedMillis: used}}
}

func TestSeesThroughTheChartToTheWholePod(t *testing.T) {
	f := Check(postgresManifest, node(6000, 1000))
	if len(f) != 1 {
		t.Fatalf("want one finding, got %d: %+v", len(f), f)
	}
	if f[0].CPURequestMillis != 8500 {
		t.Errorf("request = %dm, want 8500m — the values file says 4000m", f[0].CPURequestMillis)
	}
	if f[0].Level != LevelBlocked {
		t.Errorf("level = %q, want blocked on a 6000m node", f[0].Level)
	}
	if !Blocking(f) {
		t.Error("Blocking() must be true")
	}
}

func TestCountsInitContainersThatOnlyWait(t *testing.T) {
	// A 3000m node: the synapse *container* asks 1000m and would fit, so anything that
	// summed containers would stay silent here. The init containers ask 4000m, which
	// is what the scheduler reserves — and does not fit.
	f := Check(synapseManifest, node(3000, 0))
	if len(f) != 1 {
		t.Fatalf("want one finding — summing containers would report 1000m and say nothing: %+v", f)
	}
	if f[0].CPURequestMillis != 4000 {
		t.Errorf("request = %dm, want 4000m from the init container", f[0].CPURequestMillis)
	}
	if f[0].Level != LevelBlocked {
		t.Errorf("level = %q, want blocked", f[0].Level)
	}
}

// A node big enough: nothing to say. Silence is the correct output.
func TestNothingToReportWhenItFits(t *testing.T) {
	if f := Check(postgresManifest, node(32000, 1000)); len(f) != 0 {
		t.Errorf("want no findings on a 32-core node, got %+v", f)
	}
}

// "Does not fit right now" is a different sentence from "can never fit", because only
// the first one can resolve itself.
func TestFullClusterIsWarnNotBlocked(t *testing.T) {
	f := Check(synapseManifest, node(6000, 5000)) // 1000m free, pod wants 4000m
	if len(f) != 1 {
		t.Fatalf("want one finding, got %+v", f)
	}
	if f[0].Level != LevelWarn {
		t.Errorf("level = %q, want warn — the pod fits the node, just not right now", f[0].Level)
	}
	if Blocking(f) {
		t.Error("a full cluster must not be reported as impossible")
	}
}

// Could not check is never "fine".
func TestUnknownWhenThereIsNothingToMeasure(t *testing.T) {
	for _, c := range []struct {
		manifest string
		nodes    []Node
	}{
		{"", node(6000, 0)},
		{postgresManifest, nil},
	} {
		f := Check(c.manifest, c.nodes)
		if len(f) != 1 || f[0].Level != LevelUnknown {
			t.Errorf("want a single unknown finding, got %+v", f)
		}
		if Blocking(f) {
			t.Error("unknown must not read as blocked")
		}
	}
}

// Manifests carry Services, ConfigMaps and Jobs too; only long-lived workloads matter.
func TestIgnoresNonWorkloadDocuments(t *testing.T) {
	m := postgresManifest + "\n---\napiVersion: v1\nkind: Service\nmetadata:\n  name: svc\n"
	if f := Check(m, node(6000, 0)); len(f) != 1 {
		t.Errorf("a Service must not produce a finding: %+v", f)
	}
}

func TestMessageCarriesBothNumbers(t *testing.T) {
	f := Check(postgresManifest, node(6000, 1000))
	if !strings.Contains(f[0].Message, "8500m") || !strings.Contains(f[0].Message, "6000m") {
		t.Errorf("message must name request and capacity: %q", f[0].Message)
	}
}

// --- memory (etappe 56) ---

func nodeMem(cpuAlloc, cpuUsed, memAlloc int64) []Node {
	return []Node{{Name: "n1", CPUAllocatableMillis: cpuAlloc, CPUUsedMillis: cpuUsed, MemAllocatableMi: memAlloc}}
}

const hungryMemManifest = `
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: ess-postgres
spec:
  template:
    spec:
      containers:
        - name: postgres
          resources: {requests: {cpu: 500m, memory: 64Gi}}
`

// Arithmetic, not tuning: no node has it, so no waiting places it.
func TestMemoryLargerThanAnyNodeBlocks(t *testing.T) {
	f := Check(hungryMemManifest, nodeMem(6000, 1000, 36086))
	if len(f) != 1 || f[0].Level != LevelBlocked {
		t.Fatalf("want one blocked finding, got %+v", f)
	}
	if f[0].MemRequestMi != 65536 {
		t.Errorf("mem = %dMi, want 65536Mi", f[0].MemRequestMi)
	}
	if !strings.Contains(f[0].Message, "Speicher") {
		t.Errorf("message must name memory: %q", f[0].Message)
	}
	if strings.Contains(f[0].Message, "CPU") {
		t.Errorf("500m CPU is fine here and must not be mentioned: %q", f[0].Message)
	}
}

// The case E55 refused to warn about, and still refuses: a node with 36 GiB and most
// of it requested is ordinary Kubernetes. Only "larger than the node" counts.
func TestMemoryPressureIsNotReported(t *testing.T) {
	m := `
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: ess-postgres
spec:
  template:
    spec:
      containers:
        - name: postgres
          resources: {requests: {cpu: 500m, memory: 30Gi}}
`
	if f := Check(m, nodeMem(6000, 500, 36086)); len(f) != 0 {
		t.Errorf("30Gi on a 36Gi node must produce nothing, got %+v", f)
	}
}

// Both over: one finding naming both, not two the reader has to correlate.
func TestBothResourcesOverIsOneFinding(t *testing.T) {
	m := `
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: ess-postgres
spec:
  template:
    spec:
      containers:
        - name: postgres
          resources: {requests: {cpu: "40", memory: 64Gi}}
`
	f := Check(m, nodeMem(6000, 0, 36086))
	if len(f) != 1 {
		t.Fatalf("want exactly one finding, got %d: %+v", len(f), f)
	}
	if !strings.Contains(f[0].Message, "CPU") || !strings.Contains(f[0].Message, "Speicher") {
		t.Errorf("message must name both resources: %q", f[0].Message)
	}
}

// A node with no memory reading must not make every pod look impossible.
func TestUnknownMemoryCapacityBlocksNothing(t *testing.T) {
	if f := Check(hungryMemManifest, node(6000, 0)); len(f) != 0 {
		t.Errorf("without a memory reading nothing may be blocked on memory: %+v", f)
	}
}
