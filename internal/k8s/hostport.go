package k8s

import (
	"context"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// The host-network rollout deadlock (P2-23, etappe 119c).
//
// A Deployment whose pods use the host network, with one replica and
// maxUnavailable 0 — the ESS call server — cannot roll on one node: the new pod needs
// the ports the old one holds, and the old one is only removed once the new one is
// ready. The new pod sits Pending ("didn't have free ports"), Helm waits ten minutes and
// fails, and the next attempt does the same. On 2026-10-10 that was three failed
// upgrades to ESS 26.10.0 in a row.
//
// The way out is the one "restart the call server" already takes: remove the old pod so
// the new one can be placed. Done only in exactly that pattern — the scheduler said
// "ports", both pods use the host network, both belong to the same Deployment through
// different ReplicaSets — and never for anything else that happens to be Pending.

// hostPortVictims picks the running pods that block a pending replacement. rsOwner maps
// a ReplicaSet name to its Deployment; refusal returns the scheduler's message for a pod.
func hostPortVictims(pods []corev1.Pod, rsOwner map[string]string, refusal func(pod string) string) []string {
	ownerRS := func(p corev1.Pod) string {
		for _, o := range p.OwnerReferences {
			if o.Kind == "ReplicaSet" {
				return o.Name
			}
		}
		return ""
	}
	var victims []string
	seen := map[string]bool{}
	for _, pending := range pods {
		if pending.Status.Phase != corev1.PodPending || !pending.Spec.HostNetwork || pending.DeletionTimestamp != nil {
			continue
		}
		rs := ownerRS(pending)
		dep := rsOwner[rs]
		if rs == "" || dep == "" {
			continue
		}
		if !strings.Contains(refusal(pending.Name), "didn't have free ports") {
			continue
		}
		for _, old := range pods {
			ors := ownerRS(old)
			if old.Status.Phase != corev1.PodRunning || !old.Spec.HostNetwork || old.DeletionTimestamp != nil ||
				ors == rs || rsOwner[ors] != dep || seen[old.Name] {
				continue
			}
			seen[old.Name] = true
			victims = append(victims, old.Name)
		}
	}
	return victims
}

// UnblockHostPortRollouts removes the pods that keep a host-network replacement from
// being placed, and returns their names. Nothing is removed when the pattern is not
// there; errors leave everything as it was.
func (c *Client) UnblockHostPortRollouts(ctx context.Context, namespace string) ([]string, error) {
	pods, err := c.Static.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	anyPending := false
	for _, p := range pods.Items {
		if p.Status.Phase == corev1.PodPending && p.Spec.HostNetwork {
			anyPending = true
		}
	}
	if !anyPending {
		return nil, nil
	}
	rsList, err := c.Static.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	rsOwner := map[string]string{}
	for _, rs := range rsList.Items {
		for _, o := range rs.OwnerReferences {
			if o.Kind == "Deployment" {
				rsOwner[rs.Name] = o.Name
			}
		}
	}
	victims := hostPortVictims(pods.Items, rsOwner, func(pod string) string { return c.schedulerRefusal(ctx, namespace, pod) })
	var removed []string
	for _, v := range victims {
		if err := c.Static.CoreV1().Pods(namespace).Delete(ctx, v, metav1.DeleteOptions{}); err != nil {
			return removed, err
		}
		removed = append(removed, v)
	}
	return removed, nil
}
