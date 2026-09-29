package k8s

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ComponentHealth struct {
	Name string `json:"name"`
	// Image and Version of the workload's main container, as the pod template says
	// (etappe 112: "Versionen von allem auf den ersten Blick"). Version is the tag;
	// empty for an image pinned by digest only.
	Image    string `json:"image,omitempty"`
	Version  string `json:"version,omitempty"`
	Kind     string `json:"kind"` // Deployment | StatefulSet
	Status   string `json:"status"`
	Ready    int32  `json:"ready"`
	Desired  int32  `json:"desired"`
	Restarts int32  `json:"restarts"`
	// RestartsBy names the container carrying most of Restarts, when one does.
	// Empty means the total is the honest answer (P2-8, see podRestarts).
	RestartsBy string `json:"restarts_by,omitempty"`
	// Looping is the present-tense question, and the only field here that answers it.
	// Restarts only ever grows, so it cannot tell a container dying every thirty
	// seconds from one that misbehaved a fortnight ago — the dashboard called both a
	// "Restart-Schleife" until etappe 53. Kubernetes states it outright as
	// CrashLoopBackOff.
	Looping bool `json:"looping"`
	// LastRestart is when the most recent restart happened, absent when nothing has
	// restarted. It is what turns a bare count into something an operator can judge.
	LastRestart *time.Time `json:"last_restart,omitempty"`
	// Unschedulable says *why* a down component is down, when the answer is "the
	// scheduler refused it". Nil for every other kind of failure — a component that is
	// crashing must not be described as unplaceable (etappe 54).
	Unschedulable *Unschedulable `json:"unschedulable,omitempty"`
}

// ComponentHealth reports every ESS workload. StatefulSets matter as much as
// Deployments here — Synapse and Postgres are both StatefulSets, and leaving
// them out hid the two most important components from the dashboard.
func (c *Client) ComponentHealth(ctx context.Context, namespace string) ([]ComponentHealth, error) {
	var result []ComponentHealth

	deps, err := c.Static.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list deployments: %w", err)
	}
	for _, d := range deps.Items {
		desired := int32(1)
		if d.Spec.Replicas != nil {
			desired = *d.Spec.Replicas
		}
		restarts, by, looping, lastRestart := c.podRestarts(ctx, namespace, d.Spec.Selector.MatchLabels)
		var why *Unschedulable
		if workloadStatus(d.Status.ReadyReplicas, desired) == "down" {
			why = c.WhyUnschedulable(ctx, namespace, d.Spec.Selector.MatchLabels)
		}
		result = append(result, ComponentHealth{
			Name:          d.Name,
			Kind:          "Deployment",
			Image:         mainImage(d.Name, d.Spec.Template.Spec.Containers),
			Version:       imageVersion(mainImage(d.Name, d.Spec.Template.Spec.Containers)),
			Status:        workloadStatus(d.Status.ReadyReplicas, desired),
			Ready:         d.Status.ReadyReplicas,
			Desired:       desired,
			Restarts:      restarts,
			RestartsBy:    by,
			Looping:       looping,
			LastRestart:   lastRestart,
			Unschedulable: why,
		})
	}

	stss, err := c.Static.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list statefulsets: %w", err)
	}
	for _, s := range stss.Items {
		desired := int32(1)
		if s.Spec.Replicas != nil {
			desired = *s.Spec.Replicas
		}
		restarts, by, looping, lastRestart := c.podRestarts(ctx, namespace, s.Spec.Selector.MatchLabels)
		var why *Unschedulable
		if workloadStatus(s.Status.ReadyReplicas, desired) == "down" {
			why = c.WhyUnschedulable(ctx, namespace, s.Spec.Selector.MatchLabels)
		}
		result = append(result, ComponentHealth{
			Name:          s.Name,
			Kind:          "StatefulSet",
			Image:         mainImage(s.Name, s.Spec.Template.Spec.Containers),
			Version:       imageVersion(mainImage(s.Name, s.Spec.Template.Spec.Containers)),
			Status:        workloadStatus(s.Status.ReadyReplicas, desired),
			Ready:         s.Status.ReadyReplicas,
			Desired:       desired,
			Restarts:      restarts,
			RestartsBy:    by,
			Looping:       looping,
			LastRestart:   lastRestart,
			Unschedulable: why,
		})
	}

	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func workloadStatus(ready, desired int32) string {
	switch {
	case desired == 0:
		return "scaled-zero"
	case ready == 0:
		return "down"
	case ready < desired:
		return "degraded"
	default:
		return "healthy"
	}
}

// podRestarts returns a component's restart total and, when one container carries
// most of it, that container's name.
//
// This number collapses two levels at once — across containers and across pods —
// and it is the most prominent figure on the dashboard, turning red above 20. On
// 2026-08-15 it read 42 for `ess-postgres`, which invites exactly one conclusion:
// the database is crash-looping. It was not. `postgres` had restarted zero times
// and all 42 belonged to `postgres-exporter`, a monitoring sidecar in the same pod.
// The total was correct and the impression it created was false (P2-8).
//
// Container names are stable across the pods of one workload, so attributing by
// name is meaningful even when the total spans several pods.
// Returns the total, the container carrying most of it, whether anything is looping
// right now, and when the last restart actually happened. The last two exist because
// the first two cannot answer "is this happening now" — see ComponentHealth.Looping.
func (c *Client) podRestarts(ctx context.Context, namespace string, matchLabels map[string]string) (int32, string, bool, *time.Time) {
	pods, err := c.Static.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelsToSelector(matchLabels),
	})
	if err != nil {
		return 0, "", false, nil
	}
	var restarts int32
	var looping bool
	var last *time.Time
	byContainer := map[string]int32{}
	for _, p := range pods.Items {
		for _, cs := range p.Status.ContainerStatuses {
			restarts += cs.RestartCount
			byContainer[cs.Name] += cs.RestartCount
		}
		if isLooping(p.Status.ContainerStatuses) {
			looping = true
		}
		if at := lastRestartAt(p.Status.ContainerStatuses); at != nil && (last == nil || at.After(*last)) {
			last = at
		}
	}
	return restarts, DominantContributor(byContainer, restarts), looping, last
}

func labelsToSelector(labels map[string]string) string {
	parts := make([]string, 0, len(labels))
	for k, v := range labels {
		parts = append(parts, k+"="+v)
	}
	if len(parts) == 0 {
		return ""
	}
	sort.Strings(parts) // deterministic selector string
	result := parts[0]
	for _, p := range parts[1:] {
		result += "," + p
	}
	return result
}

// isLooping reports whether a container is waiting between crashes *right now*.
//
// Kubernetes answers the present-tense question directly, and the dashboard spent
// months inferring it from a restart count instead — a number that only grows and so
// can never say a loop has stopped (etappe 53).
func isLooping(statuses []corev1.ContainerStatus) bool {
	for _, cs := range statuses {
		if w := cs.State.Waiting; w != nil && w.Reason == "CrashLoopBackOff" {
			return true
		}
	}
	return false
}

// lastRestartAt is when the most recent restart happened, or nil if nothing has
// restarted.
//
// nil rather than the zero time: a zero time renders as year 1 and reads as "restarted
// a very long time ago", which is the opposite of what it means.
func lastRestartAt(statuses []corev1.ContainerStatus) *time.Time {
	var last *time.Time
	for _, cs := range statuses {
		t := cs.LastTerminationState.Terminated
		if t == nil || t.FinishedAt.IsZero() {
			continue
		}
		at := t.FinishedAt.Time
		if last == nil || at.After(*last) {
			last = &at
		}
	}
	return last
}

// mainImage is the image of the container the workload exists for: the one whose name
// the workload's name contains ("synapse" in ess-synapse-main, "postgres" in
// ess-postgres — not its exporter), else the first. Sidecars carry versions too, and
// showing the exporter's as "Postgres" would be a confident wrong answer.
func mainImage(workload string, containers []corev1.Container) string {
	if len(containers) == 0 {
		return ""
	}
	best := -1
	for i, c := range containers {
		if strings.Contains(workload, c.Name) && (best < 0 || len(c.Name) > len(containers[best].Name)) {
			best = i
		}
	}
	if best < 0 {
		best = 0
	}
	return containers[best].Image
}

// imageVersion is the tag of an image reference: after the last colon that follows the
// last slash (a registry port is not a tag), before any digest.
func imageVersion(image string) string {
	ref, _, _ := strings.Cut(image, "@")
	name := ref[strings.LastIndex(ref, "/")+1:]
	if i := strings.LastIndex(name, ":"); i >= 0 {
		return name[i+1:]
	}
	return ""
}
