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
	CPURequestMillis     int64 `json:"cpu_request_millis,omitempty"`
	CPUAllocatableMillis int64 `json:"cpu_allocatable_millis,omitempty"`
	// Memory is measured only against the largest node, never against what is free —
	// see Check for why the two resources get different treatment (etappe 56).
	MemRequestMi     int64  `json:"mem_request_mi,omitempty"`
	MemAllocatableMi int64  `json:"mem_allocatable_mi,omitempty"`
	Message          string `json:"message"`
}

// Node is the capacity to measure against.
type Node struct {
	Name                 string
	CPUAllocatableMillis int64
	CPUUsedMillis        int64
	MemAllocatableMi     int64
}

// FromNodeInfo adapts what the k8s package already reports.
func FromNodeInfo(in []k8s.NodeInfo) []Node {
	out := make([]Node, 0, len(in))
	for _, n := range in {
		out = append(out, Node{
			Name:                 n.Name,
			CPUAllocatableMillis: n.CPUTotalMillis,
			CPUUsedMillis:        n.CPUUsedMillis,
			MemAllocatableMi:     n.MemTotalMi,
		})
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

	var largestCPU, freeCPUOnLargest, largestMem int64
	for _, n := range nodes {
		if n.CPUAllocatableMillis > largestCPU {
			largestCPU = n.CPUAllocatableMillis
			freeCPUOnLargest = n.CPUAllocatableMillis - n.CPUUsedMillis
		}
		if n.MemAllocatableMi > largestMem {
			largestMem = n.MemAllocatableMi
		}
	}

	var findings []Finding
	for name, w := range podSpecs(manifest) {
		cpu, mem := k8s.EffectiveRequest(w.Spec)
		if cpu == 0 && mem == 0 {
			continue // no request declared: the scheduler will place it anywhere
		}

		// Larger than any node, in either resource. This is arithmetic rather than
		// tuning — no eviction and no waiting places such a pod — so memory belongs
		// here and nowhere else (etappe 56).
		overCPU := largestCPU > 0 && cpu > largestCPU
		overMem := largestMem > 0 && mem > largestMem
		if overCPU || overMem {
			findings = append(findings, Finding{
				Level: LevelBlocked, Workload: name, Kind: w.Kind,
				CPURequestMillis: cpu, CPUAllocatableMillis: largestCPU,
				MemRequestMi: mem, MemAllocatableMi: largestMem,
				Message: blockedMessage(name, cpu, largestCPU, mem, largestMem, overCPU, overMem),
			})
			continue
		}

		// Merely more than is free right now. CPU only, deliberately: a node with
		// 36 GiB and 30 GiB requested is ordinary Kubernetes, and a memory warning
		// tuned like this one would fire constantly and be ignored — which is worse
		// than not warning, because it teaches the operator to skip the whole panel.
		if freeCPUOnLargest > 0 && cpu > freeCPUOnLargest {
			findings = append(findings, Finding{
				Level: LevelWarn, Workload: name, Kind: w.Kind,
				CPURequestMillis: cpu, CPUAllocatableMillis: largestCPU,
				Message: fmt.Sprintf(
					"%s würde %dm CPU anfordern; auf dem größten Node sind derzeit etwa %dm frei. "+
						"Das kann sich von selbst lösen, sobald anderes weicht.", name, cpu, freeCPUOnLargest),
			})
		}
	}
	return findings
}

// blockedMessage names whichever resources are actually over, so a pod that exceeds
// both is one sentence rather than two findings the reader has to correlate.
func blockedMessage(name string, cpu, largestCPU, mem, largestMem int64, overCPU, overMem bool) string {
	var parts []string
	if overCPU {
		parts = append(parts, fmt.Sprintf("%dm CPU (größter Node: %dm)", cpu, largestCPU))
	}
	if overMem {
		parts = append(parts, fmt.Sprintf("%d Mi Speicher (größter Node: %d Mi)", mem, largestMem))
	}
	return fmt.Sprintf("%s würde %s anfordern — mehr, als ein einzelner Node bereitstellen kann. "+
		"Dieser Pod kann nach dem Anwenden auf keinem Node laufen.", name, strings.Join(parts, " und "))
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
