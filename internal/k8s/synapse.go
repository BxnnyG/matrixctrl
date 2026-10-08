package k8s

import (
	"context"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SynapseProcess is one running Synapse process — the main one or a worker — and where
// it reports its metrics (etappe 118).
type SynapseProcess struct {
	Pod      string `json:"pod"`
	Worker   string `json:"worker"` // "main" or the ESS worker type
	IP       string `json:"-"`
	Port     int32  `json:"-"`
	Ready    bool   `json:"ready"`
	Restarts int32  `json:"restarts"`
}

// SynapseProcesses lists the Synapse pods of one ESS release.
//
// Selected by exact instance label: the chart's config-check job carries a different
// one (`<release>-synapse-check-config`) and is not a process anyone wants measured.
func (c *Client) SynapseProcesses(ctx context.Context, namespace, release string) ([]SynapseProcess, error) {
	pods, err := c.Static.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "k8s.element.io/synapse-instance=" + release + "-synapse",
	})
	if err != nil {
		return nil, err
	}
	return synapseProcesses(pods.Items), nil
}

func synapseProcesses(items []corev1.Pod) []SynapseProcess {
	var out []SynapseProcess
	for _, p := range items {
		if p.DeletionTimestamp != nil || p.Status.Phase != corev1.PodRunning {
			continue
		}
		worker := strings.TrimPrefix(p.Labels["app.kubernetes.io/name"], "synapse-")
		if worker == "" || worker == p.Labels["app.kubernetes.io/name"] {
			continue // not a synapse-* process
		}
		sp := SynapseProcess{Pod: p.Name, Worker: worker, IP: p.Status.PodIP, Ready: len(p.Status.ContainerStatuses) > 0}
		for _, ctr := range p.Spec.Containers {
			for _, port := range ctr.Ports {
				if port.Name == "synapse-metrics" {
					sp.Port = port.ContainerPort
				}
			}
		}
		for _, cs := range p.Status.ContainerStatuses {
			sp.Restarts += cs.RestartCount
			if !cs.Ready {
				sp.Ready = false
			}
		}
		if sp.Port == 0 || sp.IP == "" {
			continue
		}
		out = append(out, sp)
	}
	return out
}
