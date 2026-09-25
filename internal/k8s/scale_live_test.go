package k8s

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Stopping and starting something, on a real cluster, as the account that will do it.
//
// A restore stops Synapse, waits for the pod to be gone, and only then moves its
// database aside — because Postgres refuses to rename a database anything is connected
// to. "The object says zero replicas" and "nothing is connected any more" are different
// facts and the gap between them is where a half-swapped homeserver lives, so what is
// tested here is the *waiting*, not the patch.
//
// It uses a workload of its own, in the managed namespace. Rehearsing this on Synapse
// would mean stopping somebody's homeserver to find out whether stopping it works — and
// it has to be *that* namespace, because the service account's rights live there and
// nowhere else: the first run of this test put the probe in MatrixCtrl's own namespace
// and was refused `get deployments`, which is the scoping etappe 40 built and a fact
// worth having a test notice.
func TestLiveScaleAndWait(t *testing.T) {
	if os.Getenv("RUN_LIVE") == "" {
		t.Skip("set RUN_LIVE=1")
	}
	admin, err := New()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	ns := essNamespace()
	name := fmt.Sprintf("matrixctrl-scale-probe-%d", os.Getpid())
	selector := "app=" + name

	one := int32(1)
	probe := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: appsv1.DeploymentSpec{
			Replicas: &one,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": name}},
				Spec: corev1.PodSpec{
					// Whatever is already on the node: a live test that waits on an
					// image pull is a live test of the registry.
					Containers: []corev1.Container{{
						Name:    "sleep",
						Image:   "postgres:16-alpine",
						Command: []string{"sleep", "600"},
					}},
					TerminationGracePeriodSeconds: new(int64),
				},
			},
		},
	}
	if _, err := admin.Static.AppsV1().Deployments(ns).Create(ctx, probe, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create the probe workload: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := admin.Static.AppsV1().Deployments(ns).Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
			t.Errorf("the probe workload was left behind: %v", err)
		}
	})

	c := asServiceAccount(ctx, t, admin)

	if err := c.WaitReady(ctx, ns, "deployment", name); err != nil {
		t.Fatalf("the probe never became ready: %v", err)
	}
	n, err := c.Replicas(ctx, ns, "deployment", name)
	if err != nil || n != 1 {
		t.Fatalf("replicas: %d, %v — a restore puts back the number it read", n, err)
	}

	start := time.Now()
	if err := c.Scale(ctx, ns, "deployment", name, 0); err != nil {
		t.Fatalf("scale to zero: %v", err)
	}
	if err := c.WaitGone(ctx, ns, selector); err != nil {
		t.Fatalf("the pods never went away: %v", err)
	}
	t.Logf("angehalten und leer nach %s", time.Since(start).Round(time.Millisecond))

	// The point of WaitGone: after it returns there is no pod left that could still hold
	// a database connection. Asked again rather than trusted.
	pods, err := c.PodsByLabel(ctx, ns, selector)
	if err != nil {
		t.Fatal(err)
	}
	if len(pods) != 0 {
		t.Fatalf("WaitGone returned while %d pod(s) are still there: %v", len(pods), pods)
	}

	start = time.Now()
	if err := c.Scale(ctx, ns, "deployment", name, 1); err != nil {
		t.Fatalf("scale back up: %v", err)
	}
	if err := c.WaitReady(ctx, ns, "deployment", name); err != nil {
		t.Fatalf("the probe did not come back: %v", err)
	}
	t.Logf("wieder betriebsbereit nach %s", time.Since(start).Round(time.Millisecond))
}
