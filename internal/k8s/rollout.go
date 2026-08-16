package k8s

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Reading what a rollout is stuck on.
//
// This is diagnostics attached to a running Helm operation, so every choice here
// bends towards "never make things worse": short timeouts, a bounded number of log
// reads, and errors that degrade to no information rather than to a failed upgrade.

// RolloutPod mirrors internal/rollout.PodState without that package importing
// client-go, keeping the reasoning testable with no cluster.
type RolloutPod struct {
	Name       string
	Ready      bool
	Phase      string
	Containers []RolloutContainer
}

type RolloutContainer struct {
	Name         string
	Init         bool
	Waiting      string
	LastExitCode int32
	Terminated   bool
	Message      string
}

const (
	// rolloutProbeTimeout bounds the whole probe. It runs between progress ticks
	// during an upgrade, so it must finish long before the next one rather than
	// queue up behind a struggling API server.
	rolloutProbeTimeout = 5 * time.Second
	// maxLogReads caps how many failing containers get a log tail. Three is more
	// than an operator can read on one line anyway, and it stops a namespace that
	// is failing wholesale from turning the probe into a log storm.
	maxLogReads = 3
	// logTailLines is enough for an error and its cause, and little enough that a
	// chatty container cannot flood the stream.
	logTailLines = 12
	// maxPullEvents bounds the Pulling-event list. An ESS upgrade rolls eight
	// workloads; a hundred is far past what any real upgrade produces and still a
	// bounded read.
	maxPullEvents = 100
)

// RolloutState reports pods that are not ready, with an explanation for the ones
// that are failing.
//
// Never returns an error: the caller is a progress ticker inside a live upgrade,
// and a diagnostic that can abort a deploy is worse than one that occasionally says
// nothing. A failed read yields an empty slice and the caller falls back to the
// plain elapsed line.
func (c *Client) RolloutState(ctx context.Context, namespace string) []RolloutPod {
	if c == nil || c.Static == nil {
		return nil
	}

	probeCtx, cancel := context.WithTimeout(ctx, rolloutProbeTimeout)
	defer cancel()

	list, err := c.Static.CoreV1().Pods(namespace).List(probeCtx, metav1.ListOptions{})
	if err != nil {
		return nil
	}

	var out []RolloutPod
	logsRead := 0

	for i := range list.Items {
		pod := &list.Items[i]
		if pod.DeletionTimestamp != nil {
			continue // on its way out; not something the rollout is waiting for
		}

		rp := RolloutPod{Name: pod.Name, Phase: string(pod.Status.Phase), Ready: podReady(pod)}
		if rp.Ready {
			continue // only the interesting ones travel
		}

		collect := func(statuses []corev1.ContainerStatus, init bool) {
			for _, cs := range statuses {
				rc := RolloutContainer{Name: cs.Name, Init: init}
				if w := cs.State.Waiting; w != nil {
					rc.Waiting = w.Reason
					// The waiting message is free and often enough on its own —
					// an image pull failure explains itself here.
					rc.Message = w.Message
				}
				if t := cs.LastTerminationState.Terminated; t != nil {
					rc.Terminated = true
					rc.LastExitCode = t.ExitCode
					if rc.Message == "" {
						rc.Message = t.Message
					}
				} else if t := cs.State.Terminated; t != nil {
					rc.Terminated = true
					rc.LastExitCode = t.ExitCode
					if rc.Message == "" {
						rc.Message = t.Message
					}
				}

				// A crash loop usually leaves no termination message — the reason
				// is in the container's own output, which is exactly the line that
				// was missing for seven minutes on 2026-08-05.
				if rc.Message == "" && rc.Waiting == "CrashLoopBackOff" && logsRead < maxLogReads {
					logsRead++
					if logs, err := c.previousLogs(probeCtx, namespace, pod.Name, cs.Name); err == nil {
						rc.Message = logs
					}
				}
				rp.Containers = append(rp.Containers, rc)
			}
		}
		collect(pod.Status.InitContainerStatuses, true)
		collect(pod.Status.ContainerStatuses, false)

		out = append(out, rp)
	}
	return out
}

// RolloutWorkload is one Deployment or StatefulSet mid-rollout.
//
// This is the denominator the upgrade screen needed. A pod count churns — old pods
// terminate while new ones start, so "4 of 9 ready" can go *down* while everything
// is going right — whereas the workload set is fixed for the duration of an upgrade
// and is what `helm --wait` is itself waiting on.
type RolloutWorkload struct {
	Kind    string // Deployment | StatefulSet
	Name    string
	Desired int32
	Updated int32
	Ready   int32
	// Generation and Observed are how a workload Helm has *just* written is told
	// apart from one it has not touched yet.
	//
	// Without them the replica counters alone report a freshly-patched Deployment as
	// fully ready, because its controller has not yet acted and every old replica
	// still matches the old spec. The upgrade screen would open at "8 of 8 ready",
	// fall to 5, and climb back — a progress bar that goes backwards is worse than
	// none. While Generation > Observed the controller has not seen the new spec,
	// which is precisely "not started".
	Generation int64
	Observed   int64
}

// Done reports the condition Helm's own ReadyChecker applies: the controller has
// seen the current spec, and every replica exists, runs it, and is ready.
func (w RolloutWorkload) Done() bool {
	return w.Desired > 0 && w.Observed >= w.Generation &&
		w.Updated == w.Desired && w.Ready == w.Desired
}

// WorkloadRollout lists the workloads in a namespace with their rollout counters.
//
// Same contract as RolloutState: never errors, because it runs inside a live
// upgrade. A partial read yields a partial list, and a caller that cannot describe
// progress falls back to describing elapsed time.
func (c *Client) WorkloadRollout(ctx context.Context, namespace string) []RolloutWorkload {
	if c == nil || c.Static == nil {
		return nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, rolloutProbeTimeout)
	defer cancel()

	var out []RolloutWorkload

	if deps, err := c.Static.AppsV1().Deployments(namespace).List(probeCtx, metav1.ListOptions{}); err == nil {
		for i := range deps.Items {
			d := &deps.Items[i]
			desired := int32(1)
			if d.Spec.Replicas != nil {
				desired = *d.Spec.Replicas
			}
			// A deliberately scaled-to-zero component is not something the rollout
			// waits for, and counting it as "not ready" would park the progress bar
			// short of the end on a perfectly finished upgrade.
			if desired == 0 {
				continue
			}
			out = append(out, RolloutWorkload{
				Kind: "Deployment", Name: d.Name, Desired: desired,
				Updated: d.Status.UpdatedReplicas, Ready: d.Status.ReadyReplicas,
				Generation: d.Generation, Observed: d.Status.ObservedGeneration,
			})
		}
	}

	// StatefulSets are not optional here: ess-synapse-main and ess-postgres are
	// both StatefulSets, and they are the two components an operator most wants to
	// see during an upgrade (CLAUDE.md).
	if stss, err := c.Static.AppsV1().StatefulSets(namespace).List(probeCtx, metav1.ListOptions{}); err == nil {
		for i := range stss.Items {
			s := &stss.Items[i]
			desired := int32(1)
			if s.Spec.Replicas != nil {
				desired = *s.Spec.Replicas
			}
			if desired == 0 {
				continue
			}
			out = append(out, RolloutWorkload{
				Kind: "StatefulSet", Name: s.Name, Desired: desired,
				Updated: s.Status.UpdatedReplicas, Ready: s.Status.ReadyReplicas,
				Generation: s.Generation, Observed: s.Status.ObservedGeneration,
			})
		}
	}
	return out
}

// PullingPods returns the pods the kubelet is currently pulling an image for.
//
// The pod's own status cannot answer this. During a pull the container's waiting
// reason is `ContainerCreating`, which is the same thing it says while mounting a
// volume or attaching a network — so "lädt Image" and "startet gleich" are
// indistinguishable from the pod alone, and they have very different expected
// durations on a first-time pull of an ESS image.
//
// The kubelet does say so, as an event. The field selector keeps the filtering on
// the API server; without it this would list every event in the namespace.
func (c *Client) PullingPods(ctx context.Context, namespace string, since time.Time) map[string]bool {
	if c == nil || c.Static == nil {
		return nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, rolloutProbeTimeout)
	defer cancel()

	limit := int64(maxPullEvents)
	list, err := c.Static.CoreV1().Events(namespace).List(probeCtx, metav1.ListOptions{
		FieldSelector: "reason=Pulling",
		Limit:         limit,
	})
	if err != nil {
		return nil
	}

	pulling := map[string]bool{}
	for i := range list.Items {
		e := &list.Items[i]
		if e.InvolvedObject.Kind != "Pod" || e.InvolvedObject.Name == "" {
			continue
		}
		// Only this upgrade's pulls. Events live for an hour, so without the cut a
		// pod that pulled fifty minutes ago would be reported as pulling now.
		if at := eventTime(e); at.IsZero() || at.Before(since) {
			continue
		}
		pulling[e.InvolvedObject.Name] = true
	}
	return pulling
}

// previousLogs reads the last run's output. `Previous: true` is the point: a
// container in CrashLoopBackOff is not running, so its current log is empty and the
// reason it died is only in the previous instance.
func (c *Client) previousLogs(ctx context.Context, namespace, pod, container string) (string, error) {
	tail := int64(logTailLines)
	req := c.Static.CoreV1().Pods(namespace).GetLogs(pod, &corev1.PodLogOptions{
		Container: container, Previous: true, TailLines: &tail,
	})
	raw, err := req.DoRaw(ctx)
	if err != nil {
		// The very first crash has no previous instance yet; try the current one
		// before giving up.
		req = c.Static.CoreV1().Pods(namespace).GetLogs(pod, &corev1.PodLogOptions{
			Container: container, TailLines: &tail,
		})
		raw, err = req.DoRaw(ctx)
		if err != nil {
			return "", err
		}
	}
	return string(raw), nil
}

func podReady(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady {
			return cond.Status == corev1.ConditionTrue
		}
	}
	return false
}
