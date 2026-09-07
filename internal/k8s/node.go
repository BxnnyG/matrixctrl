package k8s

import (
	"context"
	"encoding/json"
	"net"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type NodeInfo struct {
	Name           string `json:"name"`
	CPUUsedMillis  int64  `json:"cpu_used_millis"`
	CPUTotalMillis int64  `json:"cpu_total_millis"`
	MemUsedMi      int64  `json:"mem_used_mi"`
	MemTotalMi     int64  `json:"mem_total_mi"`
}

func (c *Client) NodeInfo(ctx context.Context) ([]NodeInfo, error) {
	nodes, err := c.Static.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	// Fetch live usage from metrics-server via raw REST
	type metricsItem struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Usage struct {
			CPU    string `json:"cpu"`
			Memory string `json:"memory"`
		} `json:"usage"`
	}
	type metricsList struct {
		Items []metricsItem `json:"items"`
	}

	metricsMap := map[string]metricsItem{}
	metricsCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if raw, err := c.Static.RESTClient().Get().
		AbsPath("/apis/metrics.k8s.io/v1beta1/nodes").
		DoRaw(metricsCtx); err == nil {
		var ml metricsList
		if json.Unmarshal(raw, &ml) == nil {
			for _, m := range ml.Items {
				metricsMap[m.Metadata.Name] = m
			}
		}
	}

	result := make([]NodeInfo, 0, len(nodes.Items))
	for _, node := range nodes.Items {
		info := NodeInfo{Name: node.Name}

		if cpu, ok := node.Status.Allocatable["cpu"]; ok {
			info.CPUTotalMillis = cpuToMillis(cpu.String())
		}
		if mem, ok := node.Status.Allocatable["memory"]; ok {
			info.MemTotalMi = memToMi(mem.String())
		}
		if m, ok := metricsMap[node.Name]; ok {
			info.CPUUsedMillis = cpuToMillis(m.Usage.CPU)
			info.MemUsedMi = memToMi(m.Usage.Memory)
		}

		result = append(result, info)
	}
	return result, nil
}

// EvictedPodCount returns the number of evicted pods in the given namespace.
func (c *Client) EvictedPodCount(ctx context.Context, namespace string) int {
	pods, err := c.Static.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: "status.phase=Failed",
	})
	if err != nil {
		return 0
	}
	// Every pod in phase Failed is terminal — it will never run again, whatever put it
	// there. The filter used to insist on Reason == "Evicted", which is one way among
	// several: this cluster carried two cert-manager pods in ContainerStatusUnknown for
	// 105 days, with running replacements beside them, invisible to a cleanup that only
	// knew the word "Evicted".
	return len(pods.Items)
}

// DeleteEvictedPods deletes every terminal pod in the namespace and returns how many.
//
// Terminal, not "evicted": see EvictedPodCount. The name is kept because it is the
// route's name and an audit-log entry that already exists.
func (c *Client) DeleteEvictedPods(ctx context.Context, namespace string) (int, error) {
	pods, err := c.Static.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: "status.phase=Failed",
	})
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, p := range pods.Items {
		if err := c.Static.CoreV1().Pods(namespace).Delete(ctx, p.Name, metav1.DeleteOptions{}); err == nil {
			deleted++
		}
	}
	return deleted, nil
}

// DeadPod is a pod that has finished failing, with the two facts that decide whether
// anyone still wants it: why, and how long ago.
type DeadPod struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Reason    string `json:"reason"`
	AgeHours  int    `json:"age_hours"`
}

// DeadPods lists terminal pods across the given namespaces.
//
// Shown before anything is deleted, because "dead" is a judgement and the operator is
// entitled to see what it is being applied to.
func (c *Client) DeadPods(ctx context.Context, namespaces ...string) ([]DeadPod, error) {
	out := []DeadPod{}
	for _, ns := range namespaces {
		pods, err := c.Static.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
			FieldSelector: "status.phase=Failed",
		})
		if err != nil {
			continue
		}
		for _, p := range pods.Items {
			reason := p.Status.Reason
			if reason == "" {
				reason = "Failed"
			}
			out = append(out, DeadPod{
				Namespace: ns,
				Name:      p.Name,
				Reason:    reason,
				AgeHours:  int(time.Since(p.CreationTimestamp.Time).Hours()),
			})
		}
	}
	return out, nil
}

func cpuToMillis(s string) int64 {
	switch {
	case strings.HasSuffix(s, "n"): // nanocores
		v, _ := strconv.ParseInt(strings.TrimSuffix(s, "n"), 10, 64)
		return v / 1_000_000
	case strings.HasSuffix(s, "m"): // millicores
		v, _ := strconv.ParseInt(strings.TrimSuffix(s, "m"), 10, 64)
		return v
	default: // whole cores
		v, _ := strconv.ParseFloat(s, 64)
		return int64(v * 1000)
	}
}

func memToMi(s string) int64 {
	switch {
	case strings.HasSuffix(s, "Ki"):
		v, _ := strconv.ParseInt(strings.TrimSuffix(s, "Ki"), 10, 64)
		return v / 1024
	case strings.HasSuffix(s, "Mi"):
		v, _ := strconv.ParseInt(strings.TrimSuffix(s, "Mi"), 10, 64)
		return v
	case strings.HasSuffix(s, "Gi"):
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "Gi"), 64)
		return int64(v * 1024)
	default:
		v, _ := strconv.ParseInt(s, 10, 64)
		return v / (1024 * 1024)
	}
}

// NodeAddress is what a DNS record has to point at.
//
// Kept separate from NodeInfo, which answers a different question (how loaded is this
// node) and is polled every fifteen seconds by the dashboard. Addresses change when a
// node is rebuilt, not when it gets busy.
type NodeAddress struct {
	Node     string `json:"node"`
	External string `json:"external,omitempty"`
	Internal string `json:"internal,omitempty"`
}

// NodeAddresses lists the addresses Kubernetes knows for each node.
//
// On a rented VM with a public address this gives the right answer directly. Behind
// NAT — a home server, a router, most self-hosting — the node only knows its private
// address, and the public one is genuinely unknowable from in here. The caller is
// expected to say so rather than present a 192.168 address as the record's target.
func (c *Client) NodeAddresses(ctx context.Context) ([]NodeAddress, error) {
	nodes, err := c.Static.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]NodeAddress, 0, len(nodes.Items))
	for _, n := range nodes.Items {
		a := NodeAddress{Node: n.Name}
		for _, addr := range n.Status.Addresses {
			switch addr.Type {
			case corev1.NodeExternalIP:
				if a.External == "" {
					a.External = addr.Address
				}
			case corev1.NodeInternalIP:
				if a.Internal == "" {
					a.Internal = addr.Address
				}
			}
		}
		out = append(out, a)
	}
	return out, nil
}

// IsPrivate reports whether an address is one the public internet cannot reach, which
// is what decides whether MatrixCtrl may present it as a DNS target.
func IsPrivate(addr string) bool {
	ip := net.ParseIP(addr)
	if ip == nil {
		return true // not an address at all; certainly not one to publish
	}
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()
}
