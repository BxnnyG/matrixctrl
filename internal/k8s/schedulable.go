package k8s

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Why a component cannot be placed (etappe 54).
//
// `down` is not a diagnosis. During the outage of 2026-08-16…18 the panel reported
// four components down, correctly and within seconds, for 37 hours — and never said
// that postgres was simply asking for more CPU than the node had. The scheduler had
// been saying so in a FailedScheduling event the whole time.

// Unschedulable explains a Pending pod.
type Unschedulable struct {
	// Pod is the pod the scheduler refused.
	Pod string `json:"pod"`
	// Reason is the scheduler's own message, unedited.
	Reason string `json:"reason"`
	// CPURequestMillis and MemRequestMi are the *effective* request — see
	// EffectiveRequest. Zero when it could not be computed.
	CPURequestMillis int64 `json:"cpu_request_millis,omitempty"`
	MemRequestMi     int64 `json:"mem_request_mi,omitempty"`
	// CPUAllocatableMillis and MemAllocatableMi are the largest node's capacity, which
	// is what makes the request judgeable.
	CPUAllocatableMillis int64 `json:"cpu_allocatable_millis,omitempty"`
	MemAllocatableMi     int64 `json:"mem_allocatable_mi,omitempty"`
	// ExceedsNode is true when the pod asks for more than any single node has, which
	// is a different problem from a cluster that is merely full: no eviction, no
	// scaling down of neighbours and no waiting will ever place it.
	ExceedsNode bool `json:"exceeds_node"`
}

// EffectiveRequest is the request the scheduler actually uses for a pod.
//
// **Not** the sum of the containers. Init containers run one at a time and before the
// others, so the pod must fit the larger of "everything running together" and "the
// greediest init container alone":
//
//	max(sum(containers), max(initContainers))
//
// This is not a detail. In the outage that produced this file, Synapse's `render-config`
// and `db-wait` had inherited 4000m each while its own container asked for 1000m — so
// Synapse reserved 4000m the entire time it was merely waiting for the database, and a
// naive sum would have reported 1000m and made the diagnosis look wrong.
func EffectiveRequest(spec corev1.PodSpec) (cpuMillis, memMi int64) {
	var sumCPU, sumMem int64
	for _, c := range spec.Containers {
		sumCPU += quantityMillis(c.Resources.Requests, corev1.ResourceCPU)
		sumMem += quantityMi(c.Resources.Requests, corev1.ResourceMemory)
	}
	var maxInitCPU, maxInitMem int64
	for _, c := range spec.InitContainers {
		if v := quantityMillis(c.Resources.Requests, corev1.ResourceCPU); v > maxInitCPU {
			maxInitCPU = v
		}
		if v := quantityMi(c.Resources.Requests, corev1.ResourceMemory); v > maxInitMem {
			maxInitMem = v
		}
	}
	return maxInt64(sumCPU, maxInitCPU), maxInt64(sumMem, maxInitMem)
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func quantityMillis(list corev1.ResourceList, name corev1.ResourceName) int64 {
	if q, ok := list[name]; ok {
		return q.MilliValue()
	}
	return 0
}

func quantityMi(list corev1.ResourceList, name corev1.ResourceName) int64 {
	if q, ok := list[name]; ok {
		return q.Value() / (1 << 20)
	}
	return 0
}

// schedulerRefusal returns the scheduler's message for a pod, or "" if it has not
// complained. Absent is not "fine" — the caller reports only what it found.
func (c *Client) schedulerRefusal(ctx context.Context, namespace, pod string) string {
	list, err := c.Static.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: "involvedObject.name=" + pod + ",reason=FailedScheduling",
		Limit:         10,
	})
	if err != nil || len(list.Items) == 0 {
		return ""
	}
	newest := &list.Items[0]
	for i := range list.Items {
		if eventTime(&list.Items[i]).After(eventTime(newest)) {
			newest = &list.Items[i]
		}
	}
	return strings.TrimSpace(newest.Message)
}

// WhyUnschedulable explains the first Pending pod of a workload, or nil when none is
// Pending — a component that is down for another reason must not be described as
// unplaceable.
func (c *Client) WhyUnschedulable(ctx context.Context, namespace string, matchLabels map[string]string) *Unschedulable {
	pods, err := c.Static.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelsToSelector(matchLabels),
	})
	if err != nil {
		return nil
	}
	for i := range pods.Items {
		p := &pods.Items[i]
		// Bound but not started is a different fault (image pull, init container
		// waiting on a dependency). Only an unplaced pod is a scheduling problem.
		if p.Status.Phase != corev1.PodPending || p.Spec.NodeName != "" {
			continue
		}
		reason := c.schedulerRefusal(ctx, namespace, p.Name)
		if reason == "" {
			continue
		}
		u := &Unschedulable{Pod: p.Name, Reason: reason}
		u.CPURequestMillis, u.MemRequestMi = EffectiveRequest(p.Spec)

		if nodes, err := c.NodeInfo(ctx); err == nil {
			for _, n := range nodes {
				if n.CPUTotalMillis > u.CPUAllocatableMillis {
					u.CPUAllocatableMillis = n.CPUTotalMillis
				}
				if n.MemTotalMi > u.MemAllocatableMi {
					u.MemAllocatableMi = n.MemTotalMi
				}
			}
		}
		// Larger than any single node: waiting cannot fix this one, which is worth
		// separating from a cluster that is merely full right now.
		u.ExceedsNode = (u.CPUAllocatableMillis > 0 && u.CPURequestMillis > u.CPUAllocatableMillis) ||
			(u.MemAllocatableMi > 0 && u.MemRequestMi > u.MemAllocatableMi)
		return u
	}
	return nil
}

// Summary is the one sentence the panel shows. Empty when there is nothing measured to
// say, so the caller falls back to the scheduler's own words.
func (u *Unschedulable) Summary() string {
	if u == nil || u.CPURequestMillis == 0 || u.CPUAllocatableMillis == 0 {
		return ""
	}
	s := fmt.Sprintf("Der Pod fordert %dm CPU an, der größte Node hat %dm.",
		u.CPURequestMillis, u.CPUAllocatableMillis)
	if u.ExceedsNode {
		s += " Das ist mehr, als ein einzelner Node überhaupt bereitstellen kann — " +
			"kein Abwarten und kein Verschieben anderer Pods behebt das."
	}
	return s
}
