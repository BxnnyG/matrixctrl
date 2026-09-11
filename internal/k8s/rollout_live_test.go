package k8s

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// The one link no unit test can check: that a crash-looping container's *reason*
// actually reaches the caller.
//
// A container that dies on a bad configuration usually leaves no termination message —
// the reason is in its own output, and only the *previous* run has it, because the
// current one has not started. RolloutState reads that log; whether it arrives is a
// question about the API server, the container runtime and the timing of the probe,
// and answering it from a fixture would only restate the assumption.
//
// It creates a pod that fails the way Synapse fails a bad config — print, exit non-zero
// — waits for CrashLoopBackOff, and removes it again.
func TestLiveCrashLoopReasonReachesTheCaller(t *testing.T) {
	if os.Getenv("RUN_LIVE") == "" {
		t.Skip("set RUN_LIVE=1")
	}
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}

	const ns = "matrixctrl"
	const marker = "Error in configuration at 'server_name': this is a test"
	name := fmt.Sprintf("rollout-probe-%d", time.Now().Unix())

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyAlways,
			Containers: []corev1.Container{{
				Name:    "boom",
				Image:   "busybox:1.36",
				Command: []string{"sh", "-c", "echo \"" + marker + "\"; exit 1"},
			}},
		},
	}
	if _, err := c.Static.CoreV1().Pods(ns).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create probe pod: %v", err)
	}
	t.Cleanup(func() {
		// Its own context: the one above is cancelled by the defer, and a cleanup that
		// reuses a cancelled context leaves the pod behind on every run.
		del, cancelDel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelDel()
		_ = c.Static.CoreV1().Pods(ns).Delete(del, name, metav1.DeleteOptions{})
	})

	// CrashLoopBackOff needs at least one restart, so this waits rather than polls once.
	var found string
	for deadline := time.Now().Add(2 * time.Minute); time.Now().Before(deadline); {
		time.Sleep(5 * time.Second)
		for _, p := range c.RolloutState(ctx, ns) {
			if p.Name != name {
				continue
			}
			for _, container := range p.Containers {
				if strings.Contains(container.Message, marker) {
					found = container.Message
				}
			}
		}
		if found != "" {
			break
		}
	}

	if found == "" {
		t.Fatal("the container's own output never reached RolloutState — a config error " +
			"would surface as a rollout that simply times out")
	}
	t.Logf("reason carried through: %q", strings.TrimSpace(found))
}
