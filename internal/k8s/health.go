package k8s

import (
	"context"
	"fmt"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ComponentHealth struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"` // Deployment | StatefulSet
	Status   string `json:"status"`
	Ready    int32  `json:"ready"`
	Desired  int32  `json:"desired"`
	Restarts int32  `json:"restarts"`
	// RestartsBy names the container carrying most of Restarts, when one does.
	// Empty means the total is the honest answer (P2-8, see podRestarts).
	RestartsBy string `json:"restarts_by,omitempty"`
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
		restarts, by := c.podRestarts(ctx, namespace, d.Spec.Selector.MatchLabels)
		result = append(result, ComponentHealth{
			Name:       d.Name,
			Kind:       "Deployment",
			Status:     workloadStatus(d.Status.ReadyReplicas, desired),
			Ready:      d.Status.ReadyReplicas,
			Desired:    desired,
			Restarts:   restarts,
			RestartsBy: by,
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
		restarts, by := c.podRestarts(ctx, namespace, s.Spec.Selector.MatchLabels)
		result = append(result, ComponentHealth{
			Name:       s.Name,
			Kind:       "StatefulSet",
			Status:     workloadStatus(s.Status.ReadyReplicas, desired),
			Ready:      s.Status.ReadyReplicas,
			Desired:    desired,
			Restarts:   restarts,
			RestartsBy: by,
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
func (c *Client) podRestarts(ctx context.Context, namespace string, matchLabels map[string]string) (int32, string) {
	pods, err := c.Static.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelsToSelector(matchLabels),
	})
	if err != nil {
		return 0, ""
	}
	var restarts int32
	byContainer := map[string]int32{}
	for _, p := range pods.Items {
		for _, cs := range p.Status.ContainerStatuses {
			restarts += cs.RestartCount
			byContainer[cs.Name] += cs.RestartCount
		}
	}
	return restarts, DominantContributor(byContainer, restarts)
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
