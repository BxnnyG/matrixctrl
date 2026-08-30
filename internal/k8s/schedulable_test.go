package k8s

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func res(cpu, mem string) corev1.ResourceRequirements {
	return corev1.ResourceRequirements{Requests: corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse(cpu),
		corev1.ResourceMemory: resource.MustParse(mem),
	}}
}

// The shape that caused the 37-hour outage. Synapse's own container asked for 1000m
// while two init containers had inherited 4000m each, so the scheduler reserved 4000m
// the whole time Synapse was merely waiting for the database. Summing containers would
// report 1000m and make the diagnosis look wrong (etappe 54, §4.53).
func TestEffectiveRequestIsNotTheSumOfContainers(t *testing.T) {
	spec := corev1.PodSpec{
		InitContainers: []corev1.Container{
			{Name: "render-config", Resources: res("4", "4Gi")},
			{Name: "db-wait", Resources: res("4", "4Gi")},
		},
		Containers: []corev1.Container{
			{Name: "synapse", Resources: res("1", "4Gi")},
		},
	}
	cpu, mem := EffectiveRequest(spec)
	if cpu != 4000 {
		t.Errorf("cpu = %dm, want 4000m — the init container dominates", cpu)
	}
	if mem != 4096 {
		t.Errorf("mem = %dMi, want 4096Mi", mem)
	}
}

// The other multiplier: one resources block covering several containers. Reporting a
// per-container number would understate the postgres pod by half.
func TestEffectiveRequestSumsSiblingContainers(t *testing.T) {
	spec := corev1.PodSpec{
		Containers: []corev1.Container{
			{Name: "postgres", Resources: res("4", "4Gi")},
			{Name: "postgres-ess-updater", Resources: res("4", "4Gi")},
			{Name: "postgres-exporter", Resources: res("500m", "500Mi")},
		},
	}
	cpu, _ := EffectiveRequest(spec)
	if cpu != 8500 {
		t.Errorf("cpu = %dm, want 8500m — three containers run together", cpu)
	}
}

// A small init container must not lower a large container sum.
func TestEffectiveRequestTakesTheLargerOfTheTwo(t *testing.T) {
	spec := corev1.PodSpec{
		InitContainers: []corev1.Container{{Name: "tiny", Resources: res("50m", "32Mi")}},
		Containers: []corev1.Container{
			{Name: "a", Resources: res("600m", "256Mi")},
			{Name: "b", Resources: res("600m", "256Mi")},
		},
	}
	if cpu, _ := EffectiveRequest(spec); cpu != 1200 {
		t.Errorf("cpu = %dm, want 1200m", cpu)
	}
}

func TestEffectiveRequestHandlesUnsetResources(t *testing.T) {
	spec := corev1.PodSpec{Containers: []corev1.Container{{Name: "none"}}}
	if cpu, mem := EffectiveRequest(spec); cpu != 0 || mem != 0 {
		t.Errorf("got %dm/%dMi, want 0/0 — no request is not an error", cpu, mem)
	}
}

// The distinction the summary turns on: a pod bigger than any node will never be
// placed, however long you wait. A cluster that is merely full might place it later.
func TestSummaryNamesTheHopelessCase(t *testing.T) {
	u := &Unschedulable{CPURequestMillis: 8500, CPUAllocatableMillis: 6000, ExceedsNode: true}
	s := u.Summary()
	if s == "" || !contains(s, "8500") || !contains(s, "6000") {
		t.Errorf("summary must carry both numbers: %q", s)
	}
	if !contains(s, "einzelner Node") {
		t.Errorf("summary must say waiting cannot fix it: %q", s)
	}

	full := &Unschedulable{CPURequestMillis: 1000, CPUAllocatableMillis: 6000}
	if contains(full.Summary(), "einzelner Node") {
		t.Error("a merely-full cluster must not be reported as hopeless")
	}
}

// Nothing measured means nothing claimed; the caller then shows the scheduler's own
// words rather than an arithmetic sentence built from zeroes.
func TestSummaryEmptyWithoutNumbers(t *testing.T) {
	if s := (&Unschedulable{}).Summary(); s != "" {
		t.Errorf("summary = %q, want empty", s)
	}
	var nilU *Unschedulable
	if s := nilU.Summary(); s != "" {
		t.Errorf("nil summary = %q, want empty", s)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
