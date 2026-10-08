package k8s

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func synapsePod(name, app string, phase corev1.PodPhase) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"app.kubernetes.io/name": app}},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "synapse", Ports: []corev1.ContainerPort{
			{Name: "synapse-http", ContainerPort: 8008}, {Name: "synapse-metrics", ContainerPort: 9001}}}}},
		Status: corev1.PodStatus{Phase: phase, PodIP: "10.0.0.1",
			ContainerStatuses: []corev1.ContainerStatus{{Ready: true, RestartCount: 2}}},
	}
}

func TestSynapseProcessesNamesMainAndWorkers(t *testing.T) {
	got := synapseProcesses([]corev1.Pod{
		synapsePod("ess-synapse-main-0", "synapse-main", corev1.PodRunning),
		synapsePod("ess-synapse-synchrotron-0", "synapse-synchrotron", corev1.PodRunning),
		synapsePod("ess-synapse-pusher-0", "synapse-pusher", corev1.PodPending),
	})
	if len(got) != 2 || got[0].Worker != "main" || got[1].Worker != "synchrotron" {
		t.Fatalf("got %+v", got)
	}
	if got[0].Port != 9001 || got[0].Restarts != 2 || !got[0].Ready {
		t.Errorf("main: %+v", got[0])
	}
}
