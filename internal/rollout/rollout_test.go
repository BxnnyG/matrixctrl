package rollout

import (
	"strings"
	"testing"
)

// Verbatim from the 26.8.0 upgrade on 2026-08-05. Seven minutes of
// "(30s elapsed)" were printed while this was the state.
const masLog = `Error: could not connect to the database

Caused by:
    0: error returned from database: password authentication failed for user "matrixauthenticationservice_user"
    1: password authentication failed for user "matrixauthenticationservice_user"
`

func failingMAS() PodState {
	return PodState{
		Name: "ess-matrix-authentication-service-8b8955db7-jkvwf", Phase: "Pending",
		Containers: []ContainerState{
			{Name: "render-config", Init: true},
			{Name: "db-wait", Init: true},
			{Name: "database-migrate", Init: true, Waiting: "CrashLoopBackOff", Terminated: true, LastExitCode: 1, Message: masLog},
			{Name: "matrix-authentication-service", Waiting: "PodInitializing"},
		},
	}
}

// The whole point of the etappe: one tick has to say what seven minutes of elapsed
// time did not.
func TestTheRealFailureIsReported(t *testing.T) {
	blockers, starting := Assess([]PodState{failingMAS()})
	if len(blockers) != 1 {
		t.Fatalf("expected one blocker, got %+v", blockers)
	}
	if starting != 0 {
		t.Errorf("a pod with a failing container is blocked, not starting (%d)", starting)
	}

	line := Describe(blockers, starting)
	for _, want := range []string{
		"matrix-authentication-service", // the operator's name for it
		"init:database-migrate",         // which container, and that it is an init
		"CrashLoopBackOff",
		"password authentication failed", // the line that solves it
	} {
		if !strings.Contains(line, want) {
			t.Errorf("line is missing %q:\n  %s", want, line)
		}
	}
	// The pod hash is noise that pushes the error off the end.
	if strings.Contains(line, "jkvwf") {
		t.Errorf("pod hash should be dropped:\n  %s", line)
	}
}

// The first line says there is a problem; the third says what it is. Printing only
// the first would give the operator the half they could already guess.
func TestSummariseKeepsTheCause(t *testing.T) {
	got := summarise(masLog)
	if !strings.Contains(got, "could not connect to the database") {
		t.Errorf("lost the headline: %q", got)
	}
	if !strings.Contains(got, "password authentication failed") {
		t.Errorf("lost the cause: %q", got)
	}
	if strings.Contains(got, "Caused by") {
		t.Errorf("kept the envelope: %q", got)
	}
	// The chain repeats itself; the repetition adds nothing.
	if strings.Count(got, "password authentication failed") != 1 {
		t.Errorf("repeated the cause: %q", got)
	}
}

// A rollout in progress is the normal case, and narrating it would bury the one
// line that matters when something is actually wrong.
func TestStartingPodsAreCountedNotNarrated(t *testing.T) {
	pods := []PodState{
		{Name: "a-1", Containers: []ContainerState{{Name: "x", Waiting: "ContainerCreating"}}},
		{Name: "b-2", Containers: []ContainerState{{Name: "x", Waiting: "PodInitializing"}}},
		{Name: "c-3", Ready: true},
	}
	blockers, starting := Assess(pods)
	if len(blockers) != 0 {
		t.Fatalf("nothing is failing: %+v", blockers)
	}
	if starting != 2 {
		t.Fatalf("expected 2 starting, got %d", starting)
	}
	if got := Describe(blockers, starting); !strings.Contains(got, "2 Pods") {
		t.Errorf("got %q", got)
	}
}

// Nothing to say means the caller keeps its plain elapsed line rather than this
// package inventing noise.
func TestNothingToSay(t *testing.T) {
	blockers, starting := Assess([]PodState{{Name: "a", Ready: true}})
	if got := Describe(blockers, starting); got != "" {
		t.Fatalf("expected silence, got %q", got)
	}
	if got := Describe(nil, 0); got != "" {
		t.Fatalf("expected silence, got %q", got)
	}
}

// An unknown waiting reason is treated as "still starting", not as a failure. A new
// Kubernetes reason should not turn every rollout into an alarm.
func TestUnknownWaitingReasonIsNotAFailure(t *testing.T) {
	pods := []PodState{{Name: "a-1", Containers: []ContainerState{{Name: "x", Waiting: "SomeFutureReason"}}}}
	blockers, starting := Assess(pods)
	if len(blockers) != 0 || starting != 1 {
		t.Fatalf("blockers=%+v starting=%d", blockers, starting)
	}
}

// A container that exited non-zero between waiting states is failing even before
// Kubernetes labels it — that window is exactly when a poll is likely to land.
func TestNonZeroExitWithoutAWaitingReason(t *testing.T) {
	pods := []PodState{{Name: "a-1", Containers: []ContainerState{
		{Name: "x", Terminated: true, LastExitCode: 2, Message: "boom"},
	}}}
	blockers, _ := Assess(pods)
	if len(blockers) != 1 || blockers[0].Reason != "Exit 2" {
		t.Fatalf("got %+v", blockers)
	}
}

// A clean exit is how init containers finish. Reporting those would flag every
// healthy pod in the namespace.
func TestSuccessfulInitContainersAreNotBlockers(t *testing.T) {
	pods := []PodState{{Name: "a-1", Containers: []ContainerState{
		{Name: "init", Init: true, Terminated: true, LastExitCode: 0},
		{Name: "app", Waiting: "PodInitializing"},
	}}}
	blockers, starting := Assess(pods)
	if len(blockers) != 0 || starting != 1 {
		t.Fatalf("blockers=%+v starting=%d", blockers, starting)
	}
}

func TestShortPodKeepsRealWords(t *testing.T) {
	cases := map[string]string{
		"ess-matrix-authentication-service-8b8955db7-jkvwf": "ess-matrix-authentication-service",
		"ess-synapse-main-0":               "ess-synapse-main-0",
		"ess-postgres-0":                   "ess-postgres-0",
		"ess-element-web-5db8cc4cc5-fmddn": "ess-element-web",
		"plain":                            "plain",
	}
	for in, want := range cases {
		if got := shortPod(in); got != want {
			t.Errorf("%s -> %s, want %s", in, got, want)
		}
	}
}

// Deterministic order: a progress log that reshuffles between ticks cannot be read
// as a sequence.
func TestBlockerOrderIsStable(t *testing.T) {
	pods := []PodState{
		{Name: "z-1", Containers: []ContainerState{{Name: "b", Waiting: "CrashLoopBackOff"}, {Name: "a", Waiting: "CrashLoopBackOff"}}},
		{Name: "a-1", Containers: []ContainerState{{Name: "c", Waiting: "ImagePullBackOff"}}},
	}
	first, _ := Assess(pods)
	second, _ := Assess(pods)
	if len(first) != 3 {
		t.Fatalf("got %d", len(first))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("order differs at %d", i)
		}
	}
	if first[0].Pod != "a-1" || first[1].Container != "a" {
		t.Fatalf("unsorted: %+v", first)
	}
}

func TestSummariseHandlesJunk(t *testing.T) {
	for _, in := range []string{"", "\n\n", "Caused by:", "   ", "0: "} {
		if got := summarise(in); got != "" {
			t.Errorf("%q produced %q", in, got)
		}
	}
	long := strings.Repeat("x", 500)
	if got := summarise(long); len(got) > 210 {
		t.Errorf("not truncated: %d chars", len(got))
	}
}
