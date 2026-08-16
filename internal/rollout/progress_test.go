package rollout

import "testing"

// wl is a workload whose controller has already seen its spec, which is the normal
// case. The generation-lag cases set the fields explicitly.
func wl(name string, desired, updated, ready int32) Workload {
	return Workload{
		Kind: "Deployment", Name: name,
		Desired: desired, Updated: updated, Ready: ready,
		Generation: 3, Observed: 3,
	}
}

func find(p Progress, name string) (Component, bool) {
	for _, c := range p.Components {
		if c.Name == name {
			return c, true
		}
	}
	return Component{}, false
}

func TestBuildProgressCountsReadyWorkloads(t *testing.T) {
	p := BuildProgress(PhaseRollout, []Workload{
		wl("ess-synapse-main", 1, 1, 1),
		wl("ess-haproxy", 1, 1, 0),
		wl("ess-element-web", 2, 2, 2),
	}, nil, nil)

	if p.Total != 3 || p.Ready != 2 {
		t.Errorf("ready/total = %d/%d, want 2/3", p.Ready, p.Total)
	}
	if p.Phase != PhaseRollout {
		t.Errorf("phase = %q", p.Phase)
	}
}

// A workload Helm has just written, whose controller has not reacted yet, still has
// every old replica matching the old spec — so the counters alone say "ready".
//
// This is not a hypothetical: it is every workload in the first moment of an
// upgrade, and without the generation check the screen opens at 100 %, falls, and
// climbs back.
func TestFreshlyPatchedWorkloadIsNotReady(t *testing.T) {
	w := wl("ess-synapse-main", 1, 1, 1)
	w.Generation = 4 // Helm just bumped it
	w.Observed = 3   // the controller has not seen it

	p := BuildProgress(PhaseRollout, []Workload{w}, nil, nil)
	if p.Ready != 0 {
		t.Errorf("ready = %d, want 0 — the controller has not seen the new spec", p.Ready)
	}
	c, _ := find(p, "ess-synapse-main")
	if c.State != StateWaiting {
		t.Errorf("state = %q, want %q", c.State, StateWaiting)
	}
}

func TestPullingBeatsStarting(t *testing.T) {
	// ContainerCreating is what a pod reports while pulling *and* while mounting, so
	// the pod alone cannot tell them apart — the kubelet event can.
	pods := []PodState{{
		Name:       "ess-synapse-main-0",
		Containers: []ContainerState{{Name: "synapse", Waiting: "ContainerCreating"}},
	}}
	pulling := map[string]bool{"ess-synapse-main-0": true}

	p := BuildProgress(PhaseRollout, []Workload{wl("ess-synapse-main", 1, 1, 0)}, pods, pulling)
	c, _ := find(p, "ess-synapse-main")
	if c.State != StatePulling {
		t.Errorf("state = %q, want %q", c.State, StatePulling)
	}
}

func TestFailingBeatsPulling(t *testing.T) {
	pods := []PodState{{
		Name: "ess-synapse-main-0",
		Containers: []ContainerState{
			{Name: "db-migrate", Init: true, Waiting: "CrashLoopBackOff", Message: "password authentication failed"},
		},
	}}
	// Both signals present at once: a pod can be pulling one container's image while
	// another has already crashed. The crash is the one worth a row.
	pulling := map[string]bool{"ess-synapse-main-0": true}

	p := BuildProgress(PhaseRollout, []Workload{wl("ess-synapse-main", 1, 1, 0)}, pods, pulling)
	c, _ := find(p, "ess-synapse-main")
	if c.State != StateFailing {
		t.Fatalf("state = %q, want %q", c.State, StateFailing)
	}
	if c.Detail == "" {
		t.Error("a failing component with no detail is the bug this whole package exists to fix")
	}
}

// Pod names carry a ReplicaSet hash for Deployments and an ordinal for
// StatefulSets. Both are suffixes, so one prefix match covers both — but it must
// not let one component's failure land on another whose name it happens to prefix.
func TestBlockersAttachToTheirOwnWorkload(t *testing.T) {
	pods := []PodState{{
		Name:       "ess-element-web-568c8fbb7-lc7tj",
		Containers: []ContainerState{{Name: "element-web", Waiting: "ImagePullBackOff"}},
	}}
	p := BuildProgress(PhaseRollout, []Workload{
		wl("ess-element-web", 2, 2, 1),
		wl("ess-element-admin", 1, 1, 1),
	}, pods, nil)

	web, _ := find(p, "ess-element-web")
	if web.State != StateFailing {
		t.Errorf("element-web state = %q, want %q", web.State, StateFailing)
	}
	admin, _ := find(p, "ess-element-admin")
	if admin.State != StateReady {
		t.Errorf("element-admin state = %q, want %q — it has no failing pod", admin.State, StateReady)
	}
}

// A table that reshuffles every three seconds is one nobody can read.
func TestComponentOrderIsStable(t *testing.T) {
	in := []Workload{wl("zeta", 1, 1, 1), wl("alpha", 1, 1, 1), wl("mid", 1, 1, 1)}
	for i := 0; i < 20; i++ {
		p := BuildProgress(PhaseRollout, in, nil, nil)
		if p.Components[0].Name != "alpha" || p.Components[2].Name != "zeta" {
			t.Fatalf("order = %v", p.Components)
		}
	}
}

func TestEmptyWorkloadsIsNotAFinishedUpgrade(t *testing.T) {
	p := BuildProgress(PhaseConfig, nil, nil, nil)
	if p.Total != 0 || p.Ready != 0 {
		t.Errorf("ready/total = %d/%d, want 0/0", p.Ready, p.Total)
	}
	if len(p.Components) != 0 {
		t.Errorf("components = %v, want none", p.Components)
	}
}

// Jobs and old ReplicaSets leave finished pods behind. They are never Ready, so
// before Phase was consulted they counted as "starting" forever — six of them on the
// settled production namespace, reported as "6 Pods startet noch" on a cluster with
// nothing left to start.
func TestSucceededPodsAreNotStarting(t *testing.T) {
	pods := []PodState{
		{Name: "ess-init-secrets-bqbkx", Phase: "Succeeded",
			Containers: []ContainerState{{Name: "init-secrets", Terminated: true, LastExitCode: 0}}},
		{Name: "ess-synapse-check-config-g8lrx", Phase: "Succeeded",
			Containers: []ContainerState{{Name: "synapse", Terminated: true, LastExitCode: 0}}},
		{Name: "ess-haproxy-57c946f6cd-8j979", Phase: "Pending",
			Containers: []ContainerState{{Name: "haproxy", Waiting: "ContainerCreating"}}},
	}

	blockers, starting := Assess(pods)
	if len(blockers) != 0 {
		t.Errorf("blockers = %v, want none", blockers)
	}
	if starting != 1 {
		t.Errorf("starting = %d, want 1 — only the haproxy pod is actually starting", starting)
	}
	if got := Describe(blockers, starting); got != "1 Pod startet noch" {
		t.Errorf("Describe = %q", got)
	}
}

// A pod that exited non-zero stays visible even though it has also finished: nothing
// is retrying it, which is exactly why nobody would otherwise notice.
func TestFailedPodsAreStillReported(t *testing.T) {
	pods := []PodState{{
		Name: "ess-synapse-check-config-x", Phase: "Failed",
		Containers: []ContainerState{{Name: "render-config", Terminated: true, LastExitCode: 2,
			Message: "config is not valid"}},
	}}
	blockers, _ := Assess(pods)
	if len(blockers) != 1 {
		t.Fatalf("blockers = %v, want 1", blockers)
	}
	if blockers[0].Reason != "Exit 2" {
		t.Errorf("reason = %q, want %q", blockers[0].Reason, "Exit 2")
	}
}
