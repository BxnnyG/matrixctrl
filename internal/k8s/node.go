package k8s

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

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
	n := 0
	for _, p := range pods.Items {
		if p.Status.Reason == "Evicted" {
			n++
		}
	}
	return n
}

// DeleteEvictedPods deletes all evicted pods in the given namespace and returns the count deleted.
func (c *Client) DeleteEvictedPods(ctx context.Context, namespace string) (int, error) {
	pods, err := c.Static.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: "status.phase=Failed",
	})
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, p := range pods.Items {
		if p.Status.Reason != "Evicted" {
			continue
		}
		if err := c.Static.CoreV1().Pods(namespace).Delete(ctx, p.Name, metav1.DeleteOptions{}); err == nil {
			deleted++
		}
	}
	return deleted, nil
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
