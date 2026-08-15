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
	Name  string `json:"name"`
	Phase string `json:"phase"`
	Ready bool   `json:"ready"`
	// Restarts is the sum across containers, which is what kubectl shows and what
	// an operator compares against. It is also the number that misleads, so it never
	// travels without RestartsBy (P2-8).
	Restarts int32 `json:"restarts"`
	// RestartsBy names the container carrying most of the count, when a pod has more
	// than one container and the restarts are not spread evenly.
	//
	// This is not a nicety. On 2026-08-15 `ess-postgres-0` reported 42 restarts; the
	// database had restarted **zero** times and all 42 belonged to
	// `postgres-exporter`, a monitoring sidecar. The obvious reading of "42" was
	// wrong in the most alarming possible direction, and disproving it took reading
	// per-container state by hand — while the per-container numbers were already in
	// the struct being folded away here.
	RestartsBy string `json:"restarts_by,omitempty"`
	StartedAt  string `json:"started_at,omitempty"`
	Node       string `json:"node"`
}

// dominantRestarter names the container responsible for a pod's restart count, or
// "" when naming one would mislead in its own right.
func dominantRestarter(statuses []corev1.ContainerStatus, total int32) string {
	if len(statuses) < 2 {
		return ""
	}
	byName := make(map[string]int32, len(statuses))
	for _, cs := range statuses {
		byName[cs.Name] += cs.RestartCount
	}
	return DominantContributor(byName, total)
}

// DominantContributor names the entry carrying most of a total, or "" when naming
// one would mislead in its own right.
//
// It stays silent when there is nothing to attribute, when only one candidate
// exists (the name adds nothing the label does not already say), and when no single
// entry holds a clear majority — three containers at 14 each is genuinely "the
// pod", and picking one of them would invent a culprit. Silence means "the total is
// the honest answer", not "unknown".
//
// Two thirds rather than "the largest share": at 22 against 20 the second entry
// matters just as much, and naming only the first hides it.
func DominantContributor(byName map[string]int32, total int32) string {
	if total == 0 || len(byName) < 2 {
		return ""
	}
	var topName string
	var topCount int32
	for name, count := range byName {
		// Ties resolve by name so the answer does not change between two identical
		// reads — map iteration order is randomised, and a value that flickers
		// between refreshes reads as something happening.
		if count > topCount || (count == topCount && name < topName) {
			topCount, topName = count, name
		}
	}
	if topCount*3 < total*2 {
		return ""
	}
	return topName
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

// ContainerInfo carries the per-container state the UI needs to answer
// "this pod restarted N times — why?". The answer lives in lastState.terminated.
type ContainerInfo struct {
	Name     string `json:"name"`
	Image    string `json:"image"`
	Ready    bool   `json:"ready"`
	Restarts int32  `json:"restarts"`

	// Current state
	State        string `json:"state"` // running | waiting | terminated
	StateReason  string `json:"state_reason,omitempty"`
	StateMessage string `json:"state_message,omitempty"`
	StartedAt    string `json:"started_at,omitempty"`

	// Previous run — this is the restart cause (OOMKilled, Error, exit code, …)
	LastExitReason  string `json:"last_exit_reason,omitempty"`
	LastExitCode    int32  `json:"last_exit_code,omitempty"`
	LastExitSignal  int32  `json:"last_exit_signal,omitempty"`
	LastExitAt      string `json:"last_exit_at,omitempty"`
	LastExitMessage string `json:"last_exit_message,omitempty"`
}

// PodDetail is a pod plus everything needed to diagnose it.
type PodDetail struct {
	Name       string            `json:"name"`
	Phase      string            `json:"phase"`
	Ready      bool              `json:"ready"`
	Restarts   int32             `json:"restarts"`
	RestartsBy string            `json:"restarts_by,omitempty"`
	StartedAt  string            `json:"started_at,omitempty"`
	Node       string            `json:"node"`
	PodIP      string            `json:"pod_ip,omitempty"`
	Reason     string            `json:"reason,omitempty"`
	Message    string            `json:"message,omitempty"`
	Containers []ContainerInfo   `json:"containers"`
	Conditions map[string]string `json:"conditions,omitempty"`
}

func containerInfo(cs corev1.ContainerStatus) ContainerInfo {
	ci := ContainerInfo{
		Name:     cs.Name,
		Image:    cs.Image,
		Ready:    cs.Ready,
		Restarts: cs.RestartCount,
	}
	switch {
	case cs.State.Running != nil:
		ci.State = "running"
		ci.StartedAt = fmtTime(cs.State.Running.StartedAt.Time)
	case cs.State.Waiting != nil:
		ci.State = "waiting"
		ci.StateReason = cs.State.Waiting.Reason
		ci.StateMessage = cs.State.Waiting.Message
	case cs.State.Terminated != nil:
		ci.State = "terminated"
		ci.StateReason = cs.State.Terminated.Reason
		ci.StateMessage = cs.State.Terminated.Message
	}
	if t := cs.LastTerminationState.Terminated; t != nil {
		ci.LastExitReason = t.Reason
		ci.LastExitCode = t.ExitCode
		ci.LastExitSignal = t.Signal
		ci.LastExitAt = fmtTime(t.FinishedAt.Time)
		ci.LastExitMessage = t.Message
	}
	return ci
}

func podDetail(p corev1.Pod) PodDetail {
	var restarts int32
	allReady := len(p.Status.ContainerStatuses) > 0
	containers := make([]ContainerInfo, 0, len(p.Status.ContainerStatuses))
	for _, cs := range p.Status.ContainerStatuses {
		restarts += cs.RestartCount
		if !cs.Ready {
			allReady = false
		}
		containers = append(containers, containerInfo(cs))
	}
	conds := map[string]string{}
	for _, c := range p.Status.Conditions {
		conds[string(c.Type)] = string(c.Status)
	}
	var startedAt string
	if p.Status.StartTime != nil {
		startedAt = fmtTime(p.Status.StartTime.Time)
	}
	return PodDetail{
		Name:       p.Name,
		Phase:      string(p.Status.Phase),
		Ready:      allReady,
		Restarts:   restarts,
		RestartsBy: dominantRestarter(p.Status.ContainerStatuses, restarts),
		StartedAt:  startedAt,
		Node:       p.Spec.NodeName,
		PodIP:      p.Status.PodIP,
		Reason:     p.Status.Reason,
		Message:    p.Status.Message,
		Containers: containers,
		Conditions: conds,
	}
}

// ComponentPods returns detailed pods for a workload (Deployment or StatefulSet)
// together with the events involving them — the drill-down behind a dashboard row.
func (c *Client) ComponentPods(ctx context.Context, namespace, workload string) ([]PodDetail, []EventInfo, error) {
	selector, err := c.workloadSelector(ctx, namespace, workload)
	if err != nil {
		return nil, nil, err
	}
	pods, err := c.Static.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, nil, err
	}
	details := make([]PodDetail, 0, len(pods.Items))
	names := make([]string, 0, len(pods.Items))
	for _, p := range pods.Items {
		details = append(details, podDetail(p))
		names = append(names, p.Name)
	}
	events, _ := c.ListEventsForPods(ctx, namespace, names, 40)
	return details, events, nil
}

// workloadSelector resolves a Deployment or StatefulSet name to its pod selector.
func (c *Client) workloadSelector(ctx context.Context, namespace, workload string) (string, error) {
	if dep, err := c.Static.AppsV1().Deployments(namespace).Get(ctx, workload, metav1.GetOptions{}); err == nil {
		return labelsToSelector(dep.Spec.Selector.MatchLabels), nil
	}
	sts, err := c.Static.AppsV1().StatefulSets(namespace).Get(ctx, workload, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("no Deployment or StatefulSet named %q in %s", workload, namespace)
	}
	return labelsToSelector(sts.Spec.Selector.MatchLabels), nil
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
			Name:       p.Name,
			Phase:      string(p.Status.Phase),
			Ready:      allReady,
			Restarts:   restarts,
			RestartsBy: dominantRestarter(p.Status.ContainerStatuses, restarts),
			StartedAt:  startedAt,
			Node:       p.Spec.NodeName,
		})
	}
	return out
}

// GetPodLogs returns the last `tail` lines of logs for a pod. A container name is
// required for multi-container pods (ess-postgres runs three) — Kubernetes errors
// out rather than picking one. Pass "" to let the API server default.
func (c *Client) GetPodLogs(ctx context.Context, namespace, podName, container string, tail int64) (string, error) {
	opts := &corev1.PodLogOptions{TailLines: &tail, Container: container}
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
