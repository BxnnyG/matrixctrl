package k8s

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Stopping and starting a workload (etappe 106).
//
// A restore has to stop Synapse before its database can be moved aside — Postgres
// refuses to rename a database anything is connected to, which is the right refusal and
// makes "stop it first" a step rather than a warning in a document.
//
// Scaling goes through a patch on `spec.replicas` rather than the `scale` subresource:
// `patch statefulsets` is already in the role (it is how the SFU patches are applied),
// while `statefulsets/scale` would be another permission to grant for the same effect.
// Fewer rights for the same result is the whole argument of §4.37.

// Replicas reads what a workload is currently asked to run.
//
// Read before stopping and used when starting again: a restore must put back the number
// that was there, not the 1 it assumed. An install with two Synapse workers that comes
// back with one is a restore that quietly halved the server.
func (c *Client) Replicas(ctx context.Context, namespace, kind, name string) (int32, error) {
	switch kind {
	case "deployment":
		d, err := c.Static.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return 0, err
		}
		if d.Spec.Replicas == nil {
			return 1, nil
		}
		return *d.Spec.Replicas, nil
	case "statefulset":
		s, err := c.Static.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return 0, err
		}
		if s.Spec.Replicas == nil {
			return 1, nil
		}
		return *s.Spec.Replicas, nil
	}
	return 0, fmt.Errorf("unknown workload kind %q", kind)
}

// Scale sets the replica count and returns once the API server has accepted it.
//
// Accepted is not the same as done — the pods take their time — which is what WaitGone
// and WaitReady are for. Keeping the two apart means a caller cannot accidentally treat
// "the object says 0" as "nothing is writing to that database any more".
func (c *Client) Scale(ctx context.Context, namespace, kind, name string, replicas int32) error {
	patch := fmt.Appendf(nil, `{"spec":{"replicas":%d}}`, replicas)
	if err := c.Patch(ctx, kind, namespace, name, types.MergePatchType, patch); err != nil {
		return fmt.Errorf("scale %s/%s to %d: %w", kind, name, replicas, err)
	}
	return nil
}

// WaitGone blocks until no pod matches the selector any more.
//
// This is the one that matters before a rename: a StatefulSet at 0 replicas still has a
// pod for as long as it takes to terminate, and that pod still holds its database
// connections. Asking the workload object would answer about the intent; this asks
// about the pods.
func (c *Client) WaitGone(ctx context.Context, namespace, selector string) error {
	return poll(ctx, time.Second, func() (bool, error) {
		pods, err := c.PodsByLabel(ctx, namespace, selector)
		if err != nil {
			return false, err
		}
		return len(pods) == 0, nil
	})
}

// WaitReady blocks until a workload's replicas are all ready.
//
// Both kinds, because ESS is both: Synapse and Postgres are StatefulSets and the
// authentication service is a Deployment. Handling only Deployments is how a check ends
// up silently skipping the two most important components (CLAUDE.md).
func (c *Client) WaitReady(ctx context.Context, namespace, kind, name string) error {
	return poll(ctx, time.Second, func() (bool, error) {
		switch kind {
		case "deployment":
			d, err := c.Static.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return false, err
			}
			want := int32(1)
			if d.Spec.Replicas != nil {
				want = *d.Spec.Replicas
			}
			return d.Status.ReadyReplicas == want && d.Status.UpdatedReplicas == want, nil
		case "statefulset":
			s, err := c.Static.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return false, err
			}
			want := int32(1)
			if s.Spec.Replicas != nil {
				want = *s.Spec.Replicas
			}
			return s.Status.ReadyReplicas == want, nil
		}
		return false, fmt.Errorf("unknown workload kind %q", kind)
	})
}

// poll runs a condition on an interval until it is true, it errors, or the context ends.
//
// With a sleep. WaitForRollout next door has polled in a tight loop since etappe 12,
// which asks the API server as fast as the network allows for as long as a rollout
// takes; this one does not copy that.
func poll(ctx context.Context, every time.Duration, cond func() (bool, error)) error {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		ok, err := cond()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
}
