package k8s

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func rsPod(name, rs string, phase corev1.PodPhase, hostNet bool) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: rs}}},
		Spec:       corev1.PodSpec{HostNetwork: hostNet},
		Status:     corev1.PodStatus{Phase: phase},
	}
}

const portRefusal = "0/1 nodes are available: 1 node(s) didn't have free ports for the requested pod ports."

var rsOwner = map[string]string{"sfu-new": "ess-matrix-rtc-sfu", "sfu-old": "ess-matrix-rtc-sfu", "other-rs": "ess-synapse"}

func refuse(msg string) func(string) string { return func(string) string { return msg } }

// Production, 2026-10-10: the new call-server pod Pending on ports, the old one running.
func TestTheProductionDeadlockIsRecognised(t *testing.T) {
	pods := []corev1.Pod{
		rsPod("sfu-new-x", "sfu-new", corev1.PodPending, true),
		rsPod("sfu-old-y", "sfu-old", corev1.PodRunning, true),
	}
	got := hostPortVictims(pods, rsOwner, refuse(portRefusal))
	if len(got) != 1 || got[0] != "sfu-old-y" {
		t.Fatalf("victims = %v", got)
	}
}

func TestNothingElseIsTouched(t *testing.T) {
	cases := map[string]struct {
		pods    []corev1.Pod
		refusal string
	}{
		// Pending for another reason — not enough memory — is not this deadlock.
		"refused for memory": {[]corev1.Pod{rsPod("sfu-new-x", "sfu-new", corev1.PodPending, true), rsPod("sfu-old-y", "sfu-old", corev1.PodRunning, true)},
			"0/1 nodes are available: 1 Insufficient memory."},
		// Without the host network there is no shared port to free.
		"no host network": {[]corev1.Pod{rsPod("sfu-new-x", "sfu-new", corev1.PodPending, false), rsPod("sfu-old-y", "sfu-old", corev1.PodRunning, false)}, portRefusal},
		// An old pod off the host network holds no host port; removing it frees nothing.
		"old pod off the host network": {[]corev1.Pod{rsPod("sfu-new-x", "sfu-new", corev1.PodPending, true), rsPod("sfu-old-y", "sfu-old", corev1.PodRunning, false)}, portRefusal},
		// Two pods of the same ReplicaSet are replicas, not old and new.
		"same replicaset": {[]corev1.Pod{rsPod("sfu-new-x", "sfu-new", corev1.PodPending, true), rsPod("sfu-new-z", "sfu-new", corev1.PodRunning, true)}, portRefusal},
		// Another Deployment holding the port is a conflict to report, not a pod to kill.
		"other deployment": {[]corev1.Pod{rsPod("sfu-new-x", "sfu-new", corev1.PodPending, true), rsPod("syn-a", "other-rs", corev1.PodRunning, true)}, portRefusal},
	}
	for name, tc := range cases {
		if got := hostPortVictims(tc.pods, rsOwner, refuse(tc.refusal)); len(got) != 0 {
			t.Errorf("%s: would delete %v", name, got)
		}
	}
}
