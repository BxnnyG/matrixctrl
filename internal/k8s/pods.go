package k8s

import (
	"bytes"
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type PodInfo struct {
	Name      string `json:"name"`
	Phase     string `json:"phase"`
	Ready     bool   `json:"ready"`
	Restarts  int32  `json:"restarts"`
	StartedAt string `json:"started_at,omitempty"`
	Node      string `json:"node"`
}

type PVCInfo struct {
	Name         string   `json:"name"`
	Namespace    string   `json:"namespace"`
	Phase        string   `json:"phase"`
	StorageClass string   `json:"storage_class,omitempty"`
	Capacity     string   `json:"capacity,omitempty"`
	AccessModes  []string `json:"access_modes"`
	VolumeName   string   `json:"volume_name,omitempty"`
}

// ListDeploymentPods returns pods managed by the named deployment in the given namespace.
func (c *Client) ListDeploymentPods(ctx context.Context, namespace, deploymentName string) ([]PodInfo, error) {
	dep, err := c.Static.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get deployment %s: %w", deploymentName, err)
	}
	pods, err := c.Static.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelsToSelector(dep.Spec.Selector.MatchLabels),
	})
	if err != nil {
		return nil, err
	}
	return podInfoList(pods.Items), nil
}

// ListNamespacePods returns all pods in a namespace.
func (c *Client) ListNamespacePods(ctx context.Context, namespace string) ([]PodInfo, error) {
	pods, err := c.Static.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return podInfoList(pods.Items), nil
}

func podInfoList(items []corev1.Pod) []PodInfo {
	out := make([]PodInfo, 0, len(items))
	for _, p := range items {
		var restarts int32
		allReady := true
		for _, cs := range p.Status.ContainerStatuses {
			restarts += cs.RestartCount
			if !cs.Ready {
				allReady = false
			}
		}
		if len(p.Status.ContainerStatuses) == 0 {
			allReady = false
		}
		var startedAt string
		if p.Status.StartTime != nil {
			startedAt = p.Status.StartTime.UTC().Format(time.RFC3339)
		}
		out = append(out, PodInfo{
			Name:      p.Name,
			Phase:     string(p.Status.Phase),
			Ready:     allReady,
			Restarts:  restarts,
			StartedAt: startedAt,
			Node:      p.Spec.NodeName,
		})
	}
	return out
}

// GetPodLogs returns the last `tail` lines of logs from the first container of the given pod.
func (c *Client) GetPodLogs(ctx context.Context, namespace, podName string, tail int64) (string, error) {
	opts := &corev1.PodLogOptions{TailLines: &tail}
	req := c.Static.CoreV1().Pods(namespace).GetLogs(podName, opts)
	rc, err := req.Stream(ctx)
	if err != nil {
		return "", fmt.Errorf("stream logs: %w", err)
	}
	defer rc.Close()
	var buf bytes.Buffer
	_, err = buf.ReadFrom(rc)
	return buf.String(), err
}

// DeletePod deletes a pod by name, causing the controller to recreate it.
func (c *Client) DeletePod(ctx context.Context, namespace, podName string) error {
	return c.Static.CoreV1().Pods(namespace).Delete(ctx, podName, metav1.DeleteOptions{})
}

// ListPVCs returns PVCs in the given namespace (or all namespaces if empty).
func (c *Client) ListPVCs(ctx context.Context, namespace string) ([]PVCInfo, error) {
	pvcs, err := c.Static.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]PVCInfo, 0, len(pvcs.Items))
	for _, p := range pvcs.Items {
		var capacity string
		if q, ok := p.Status.Capacity[corev1.ResourceStorage]; ok {
			capacity = q.String()
		}
		modes := make([]string, len(p.Spec.AccessModes))
		for i, m := range p.Spec.AccessModes {
			modes[i] = string(m)
		}
		sc := ""
		if p.Spec.StorageClassName != nil {
			sc = *p.Spec.StorageClassName
		}
		out = append(out, PVCInfo{
			Name:         p.Name,
			Namespace:    p.Namespace,
			Phase:        string(p.Status.Phase),
			StorageClass: sc,
			Capacity:     capacity,
			AccessModes:  modes,
			VolumeName:   p.Spec.VolumeName,
		})
	}
	return out, nil
}

// NodeConditions returns relevant conditions (Ready, MemoryPressure, DiskPressure, PIDPressure) per node.
type NodeConditionInfo struct {
	Name       string            `json:"name"`
	Conditions map[string]string `json:"conditions"`
	KernelVer  string            `json:"kernel_version,omitempty"`
	OSImage    string            `json:"os_image,omitempty"`
	KubeVer    string            `json:"kube_version,omitempty"`
	Arch       string            `json:"arch,omitempty"`
}

func (c *Client) NodeConditions(ctx context.Context) ([]NodeConditionInfo, error) {
	nodes, err := c.Static.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]NodeConditionInfo, 0, len(nodes.Items))
	for _, n := range nodes.Items {
		conds := map[string]string{}
		for _, c := range n.Status.Conditions {
			conds[string(c.Type)] = string(c.Status)
		}
		out = append(out, NodeConditionInfo{
			Name:       n.Name,
			Conditions: conds,
			KernelVer:  n.Status.NodeInfo.KernelVersion,
			OSImage:    n.Status.NodeInfo.OSImage,
			KubeVer:    n.Status.NodeInfo.KubeletVersion,
			Arch:       n.Status.NodeInfo.Architecture,
		})
	}
	return out, nil
}
