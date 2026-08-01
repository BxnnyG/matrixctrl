package k8s

import (
	"context"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ExposedPort is one port that must be reachable from the internet for calling
// to work — read from the live Service, never from documentation, because a
// written-down port list goes stale the moment the chart changes one.
type ExposedPort struct {
	Service               string `json:"service"`
	Name                  string `json:"name"`
	Protocol              string `json:"protocol"`
	NodePort              int32  `json:"node_port"`
	ExternalTrafficPolicy string `json:"external_traffic_policy"`
}

// NodePorts lists every NodePort service in a namespace, sorted so the output is
// stable between calls — an operator comparing two screenshots should not have to
// wonder whether the order means something.
func (c *Client) NodePorts(ctx context.Context, namespace string) ([]ExposedPort, error) {
	svcs, err := c.Static.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	var out []ExposedPort
	for _, s := range svcs.Items {
		if string(s.Spec.Type) != "NodePort" {
			continue
		}
		for _, p := range s.Spec.Ports {
			if p.NodePort == 0 {
				continue
			}
			out = append(out, ExposedPort{
				Service:               s.Name,
				Name:                  p.Name,
				Protocol:              string(p.Protocol),
				NodePort:              p.NodePort,
				ExternalTrafficPolicy: string(s.Spec.ExternalTrafficPolicy),
			})
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].NodePort < out[j].NodePort })
	return out, nil
}
