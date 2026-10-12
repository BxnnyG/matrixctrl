package k8s

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LabelledPod is a pod as a status line needs it: ready, restarts, and the image that
// runs — the version a component actually is, not the one its values ask for.
type LabelledPod struct {
	Name     string `json:"name"`
	Ready    bool   `json:"ready"`
	Restarts int32  `json:"restarts"`
	Image    string `json:"image"`
}

// LabelledPods lists the pods matching a selector (etappe 119).
func (c *Client) LabelledPods(ctx context.Context, namespace, selector string) ([]LabelledPod, error) {
	if c == nil || c.Static == nil {
		return nil, fmt.Errorf("no cluster connection")
	}
	list, err := c.Static.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, err
	}
	var out []LabelledPod
	for _, p := range list.Items {
		lp := LabelledPod{Name: p.Name, Ready: len(p.Status.ContainerStatuses) > 0}
		if len(p.Spec.Containers) > 0 {
			lp.Image = p.Spec.Containers[0].Image
		}
		for _, cs := range p.Status.ContainerStatuses {
			lp.Restarts += cs.RestartCount
			if !cs.Ready {
				lp.Ready = false
			}
		}
		out = append(out, lp)
	}
	return out, nil
}

// GetConfigMap returns a ConfigMap's data.
func (c *Client) GetConfigMap(ctx context.Context, namespace, name string) (map[string]string, error) {
	if c == nil || c.Static == nil {
		return nil, fmt.Errorf("no cluster connection")
	}
	cm, err := c.Static.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return cm.Data, nil
}
