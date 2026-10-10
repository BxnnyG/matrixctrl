package k8s

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// RUN_LIVE=1, on a single-node playground: the host-network rollout deadlock, real.
// A throwaway Deployment in its own namespace, shaped like the ESS call server — host
// network, one replica, maxUnavailable 0, a declared port. A template change produces a
// replacement that cannot be placed; the test first proves it stays Pending (the
// deadlock exists), then that UnblockHostPortRollouts removes the old pod and the new one
// runs (etappe 119c).
func TestLiveHostPortDeadlockIsResolved(t *testing.T) {
	if os.Getenv("RUN_LIVE") == "" {
		t.Skip("set RUN_LIVE=1")
	}
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	const ns = "matrixctrl-hostport-test"
	cs := c.Static
	if _, err := cs.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cs.CoreV1().Namespaces().Delete(context.Background(), ns, metav1.DeleteOptions{}) }()

	one, zero, surge := int32(1), intstr.FromInt32(0), intstr.FromInt32(1)
	labels := map[string]string{"app": "hostport-test"}
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "fake-sfu"},
		Spec: appsv1.DeploymentSpec{
			Replicas: &one,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Strategy: appsv1.DeploymentStrategy{Type: appsv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDeployment{MaxUnavailable: &zero, MaxSurge: &surge}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{HostNetwork: true, Containers: []corev1.Container{{
					Name: "pause", Image: "docker.io/rancher/mirrored-pause:3.6", ImagePullPolicy: corev1.PullIfNotPresent,
					Ports: []corev1.ContainerPort{{ContainerPort: 39871, HostPort: 39871, Protocol: corev1.ProtocolUDP}},
				}}},
			},
		},
	}
	if _, err := cs.AppsV1().Deployments(ns).Create(ctx, dep, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	running := func() []string {
		pods, _ := cs.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
		var out []string
		for _, p := range pods.Items {
			if p.Status.Phase == corev1.PodRunning && p.DeletionTimestamp == nil {
				out = append(out, p.Name)
			}
		}
		return out
	}
	waitFor := func(what string, cond func() bool) {
		for !cond() {
			select {
			case <-ctx.Done():
				t.Fatalf("timed out waiting for %s", what)
			case <-time.After(2 * time.Second):
			}
		}
	}
	waitFor("the first pod", func() bool { return len(running()) == 1 })
	old := running()[0]

	// A template change: the rollout that deadlocks.
	patch := []byte(`{"spec":{"template":{"metadata":{"annotations":{"test/rev":"2"}}}}}`)
	if _, err := cs.AppsV1().Deployments(ns).Patch(ctx, "fake-sfu", types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		t.Fatal(err)
	}
	var pending string
	waitFor("the replacement to be refused for ports", func() bool {
		pods, _ := cs.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
		for _, p := range pods.Items {
			if p.Status.Phase == corev1.PodPending && strings.Contains(c.schedulerRefusal(ctx, ns, p.Name), "free ports") {
				pending = p.Name
				return true
			}
		}
		return false
	})
	// The deadlock is real: twenty seconds later nothing has moved.
	time.Sleep(20 * time.Second)
	if r := running(); len(r) != 1 || r[0] != old {
		t.Fatalf("expected the old pod alone to keep running, got %v", r)
	}
	t.Logf("deadlock reproduced: %s pending, %s holding the port", pending, old)

	removed, err := c.UnblockHostPortRollouts(ctx, ns)
	if err != nil || len(removed) != 1 || removed[0] != old {
		t.Fatalf("removed %v, err %v; want [%s]", removed, err, old)
	}
	waitFor("the replacement to run", func() bool {
		r := running()
		return len(r) == 1 && r[0] == pending
	})
	t.Logf("resolved: %s removed, %s running", old, pending)

	// Once the rollout is through there is nothing left to remove.
	if again, _ := c.UnblockHostPortRollouts(ctx, ns); len(again) != 0 {
		t.Errorf("removed %v from a finished rollout", again)
	}
}
