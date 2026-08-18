package k8s

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// The live case that produced the defect: postgres at 0 restarts, its exporter at 64,
// nothing looping. The banner said "postgres in Restart-Schleife" (E53).
func TestDominantRestarterNamesTheExporterNotThePod(t *testing.T) {
	statuses := []corev1.ContainerStatus{
		{Name: "postgres", RestartCount: 0},
		{Name: "postgres-ess-updater", RestartCount: 0},
		{Name: "postgres-exporter", RestartCount: 64},
	}
	if got := dominantRestarter(statuses, 64); got != "postgres-exporter" {
		t.Errorf("dominantRestarter = %q, want postgres-exporter", got)
	}
}

// The distinction the whole etappe rests on: a large count is not a present-tense
// loop, and CrashLoopBackOff is the only thing that says one is happening now.
func TestLoopingIsAStateNotACount(t *testing.T) {
	stable := corev1.ContainerStatus{
		Name: "postgres-exporter", RestartCount: 64,
		State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
	}
	looping := corev1.ContainerStatus{
		Name: "flaky", RestartCount: 3,
		State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
	}

	if isLooping([]corev1.ContainerStatus{stable}) {
		t.Error("64 restarts while Running is history, not a loop")
	}
	if !isLooping([]corev1.ContainerStatus{looping}) {
		t.Error("CrashLoopBackOff is a loop even at 3 restarts")
	}
}

func TestLastRestartTakesTheMostRecent(t *testing.T) {
	older := metav1.NewTime(time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC))
	newer := metav1.NewTime(time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC))
	statuses := []corev1.ContainerStatus{
		{Name: "a", RestartCount: 1, LastTerminationState: corev1.ContainerState{
			Terminated: &corev1.ContainerStateTerminated{FinishedAt: older}}},
		{Name: "b", RestartCount: 1, LastTerminationState: corev1.ContainerState{
			Terminated: &corev1.ContainerStateTerminated{FinishedAt: newer}}},
	}
	got := lastRestartAt(statuses)
	if got == nil || !got.Equal(newer.Time) {
		t.Errorf("lastRestartAt = %v, want %v", got, newer.Time)
	}

	// Nothing has ever restarted: absent, not the zero time, which would render as
	// 1 January year 1 and read as "restarted a very long time ago".
	if lastRestartAt([]corev1.ContainerStatus{{Name: "a"}}) != nil {
		t.Error("no termination means no last restart")
	}
}
