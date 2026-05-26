package k8s

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ComponentHealth struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Ready    int32  `json:"ready"`
	Desired  int32  `json:"desired"`
	Restarts int32  `json:"restarts"`
}

func (c *Client) ComponentHealth(ctx context.Context, namespace string) ([]ComponentHealth, error) {
	deps, err := c.Static.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list deployments: %w", err)
	}

	var result []ComponentHealth
	for _, d := range deps.Items {
		var restarts int32
		pods, err := c.Static.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: labelsToSelector(d.Spec.Selector.MatchLabels),
		})
		if err == nil {
			for _, p := range pods.Items {
				for _, cs := range p.Status.ContainerStatuses {
					restarts += cs.RestartCount
				}
			}
		}

		desired := int32(1)
		if d.Spec.Replicas != nil {
			desired = *d.Spec.Replicas
		}

		status := "healthy"
		if d.Status.ReadyReplicas == 0 && desired > 0 {
			status = "down"
		} else if d.Status.ReadyReplicas < desired {
			status = "degraded"
		}

		result = append(result, ComponentHealth{
			Name:    d.Name,
			Status:  status,
			Ready:   d.Status.ReadyReplicas,
			Desired: desired,
			Restarts: restarts,
		})
	}

	return result, nil
}

func labelsToSelector(labels map[string]string) string {
	var parts []string
	for k, v := range labels {
		parts = append(parts, k+"="+v)
	}
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for _, p := range parts[1:] {
		result += "," + p
	}
	return result
}
