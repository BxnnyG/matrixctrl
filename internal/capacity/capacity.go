// Package capacity answers "would this configuration fit on this cluster?" before it
// is applied (etappe 55).
//
// It exists because the values that took the managed homeserver down for 37 hours were
// written through this panel, and the panel is the last thing that sees them before
// they reach the cluster (§4.53, §4.54).
package capacity

import (
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	"github.com/bxnnyg/matrixctrl/internal/k8s"
)

// Level separates a config that can never be scheduled from one that merely does not
// fit at this moment. Unknown is a third answer and never means "fine".
type Level string

const (
	LevelOK      Level = "ok"
	LevelWarn    Level = "warn"
	LevelBlocked Level = "blocked"
	LevelUnknown Level = "unknown"
)

// Finding is one workload's verdict.
type Finding struct {
	Level Level `json:"level"`
	// Workload is the Deployment/StatefulSet name from the rendered manifest.
	Workload string `json:"workload,omitempty"`
	Kind     string `json:"kind,omitempty"`
	// CPURequestMillis is the *effective* request of the pod the chart would create —
	// max(sum(containers), max(initContainers)), not the number in the values file.
	CPURequestMillis     int64  `json:"cpu_request_millis,omitempty"`
	CPUAllocatableMillis int64  `json:"cpu_allocatable_millis,omitempty"`
	Message              string `json:"message"`
}

// Node is the capacity to measure against.
type Node struct {
	Name                 string
	CPUAllocatableMillis int64
	CPUUsedMillis        int64
}

// FromNodeInfo adapts what the k8s package already reports.
func FromNodeInfo(in []k8s.NodeInfo) []Node {
	out := make([]Node, 0, len(in))
	for _, n := range in {
		out = append(out, Node{Name: n.Name, CPUAllocatableMillis: n.CPUTotalMillis, CPUUsedMillis: n.CPUUsedMillis})
	}
	return out
}

// podSpecs pulls every pod template out of a rendered manifest.
//
// Only Deployments and StatefulSets: they are what ESS runs long-lived, and a Job that
// cannot be scheduled fails visibly and once rather than taking a homeserver down.
func podSpecs(manifest string) map[string]struct {
	Kind string
	Spec corev1.PodSpec
} {
	out := map[string]struct {
		Kind string
		Spec corev1.PodSpec
	}{}

	for _, doc := range strings.Split(manifest, "\n---") {
		doc = strings.TrimSpace(doc)
		if doc == "" {
			continue
		}
		var probe struct {
			Kind     string `json:"kind"`
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		}
		if err := yaml.Unmarshal([]byte(doc), &probe); err != nil {
			continue
		}
		switch probe.Kind {
		case "Deployment":
			var d appsv1.Deployment
			if yaml.Unmarshal([]byte(doc), &d) == nil && d.Name != "" {
				out[d.Name] = struct {
					Kind string
					Spec corev1.PodSpec
				}{"Deployment", d.Spec.Template.Spec}
			}
		case "StatefulSet":
			var s appsv1.StatefulSet
			if yaml.Unmarshal([]byte(doc), &s) == nil && s.Name != "" {
				out[s.Name] = struct {
					Kind string
					Spec corev1.PodSpec
				}{"StatefulSet", s.Spec.Template.Spec}
			}
		}
	}
	return out
}

// Check measures every workload the manifest would create against the cluster.
//
// An empty manifest or no nodes yields exactly one Unknown finding. "I could not
// check" and "it fits" are different statements, and this whole package exists because
// the second was assumed once already.
func Check(manifest string, nodes []Node) []Finding {
	if strings.TrimSpace(manifest) == "" || len(nodes) == 0 {
		return []Finding{{
			Level:   LevelUnknown,
			Message: "Die Konfiguration konnte nicht gegen die Cluster-Kapazität geprüft werden.",
		}}
	}

	var largest, freeOnLargest int64
	for _, n := range nodes {
		if n.CPUAllocatableMillis > largest {
			largest = n.CPUAllocatableMillis
			freeOnLargest = n.CPUAllocatableMillis - n.CPUUsedMillis
		}
	}

	var findings []Finding
	for name, w := range podSpecs(manifest) {
		cpu, _ := k8s.EffectiveRequest(w.Spec)
		if cpu == 0 {
			continue // no request declared: the scheduler will place it anywhere
		}
		switch {
		case cpu > largest:
			findings = append(findings, Finding{
				Level: LevelBlocked, Workload: name, Kind: w.Kind,
				CPURequestMillis: cpu, CPUAllocatableMillis: largest,
				Message: fmt.Sprintf(
					"%s würde %dm CPU anfordern — mehr als der größte Node überhaupt hat (%dm). "+
						"Dieser Pod kann nach dem Anwenden auf keinem Node laufen.", name, cpu, largest),
			})
		case freeOnLargest > 0 && cpu > freeOnLargest:
			findings = append(findings, Finding{
				Level: LevelWarn, Workload: name, Kind: w.Kind,
				CPURequestMillis: cpu, CPUAllocatableMillis: largest,
				Message: fmt.Sprintf(
					"%s würde %dm CPU anfordern; auf dem größten Node sind derzeit etwa %dm frei. "+
						"Das kann sich von selbst lösen, sobald anderes weicht.", name, cpu, freeOnLargest),
			})
		}
	}
	return findings
}

// Blocking reports whether any finding says a pod could never be scheduled — the case
// worth interrupting an operator for, as opposed to a cluster that is busy right now.
func Blocking(findings []Finding) bool {
	for _, f := range findings {
		if f.Level == LevelBlocked {
			return true
		}
	}
	return false
}
