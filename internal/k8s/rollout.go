package k8s

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/api/core/v1"
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
