// Package rollout answers "what is this upgrade waiting for?".
//
// It exists because the answer used to be a clock. `startProgress` emitted
// "Waiting for Helm rollout… (30s elapsed)" every thirty seconds and never looked
// at the cluster, so on 2026-08-05 an operator watched seven minutes of that while
// one pod sat in Init:CrashLoopBackOff with a one-line explanation in its logs:
//
//	database-migrate: password authentication failed for user "…"
//
// Every byte of that was available to the process printing the elapsed time.
package rollout

import (
	"fmt"
	"sort"
	"strings"
)

// PodState is the subset of a pod this package reasons about, declared here rather
// than importing client-go so the logic stays testable with no cluster.
type PodState struct {
	Name  string
	Ready bool
	Phase string
	// Containers covers both init and regular containers: today's failure was in an
	// init container, and a check that only looked at the main one would have
	// reported the pod as merely "not ready yet" for seven minutes.
	Containers []ContainerState
}

type ContainerState struct {
	Name string
	Init bool
	// Waiting is the waiting reason, e.g. CrashLoopBackOff, ImagePullBackOff.
	Waiting string
	// LastExitCode is the exit status of the previous run, when there was one.
	LastExitCode int32
	Terminated   bool
	// Message is the best available explanation — a termination message or a log
	// tail. Filled by the caller, which is the part that needs a cluster.
	Message string
}

// failing reasons are the ones that mean "this is not going to fix itself".
// Anything else — ContainerCreating, PodInitializing, Pending — is what a healthy
// rollout looks like halfway through, and narrating it would bury the real thing.
var failingReasons = map[string]bool{
	"CrashLoopBackOff":           true,
	"ImagePullBackOff":           true,
	"ErrImagePull":               true,
	"CreateContainerError":       true,
	"CreateContainerConfigError": true,
	"InvalidImageName":           true,
	"RunContainerError":          true,
}

// classify decides whether a container is failing, and what to call it.
//
// A waiting reason that is not in the failing set — ContainerCreating,
// PodInitializing, Pending — is what a healthy rollout looks like halfway through.
// Narrating those would bury the one line that matters.
func classify(c ContainerState) (reason string, failing bool) {
	if failingReasons[c.Waiting] {
		return c.Waiting, true
	}
	if c.Waiting != "" {
		return c.Waiting, false // known-benign or unknown: still starting
	}
	if c.Terminated && c.LastExitCode != 0 {
		// Between waiting states when the poll landed: a container that exited
		// non-zero and is being retried is failing even if Kubernetes has not
		// labelled it yet.
		return fmt.Sprintf("Exit %d", c.LastExitCode), true
	}
	return "", false
}

// Blocker is one pod that is not going to become ready without intervention.
type Blocker struct {
	Pod       string
	Container string
	Init      bool
	Reason    string
	Message   string
}

// Assess splits the not-ready pods into ones that are failing and ones that are
// merely still starting.
//
// The count of the second group is kept because it is the difference between "this
// is progressing" and "this is stuck", and an operator watching a log needs to know
// which they are looking at.
func Assess(pods []PodState) (blockers []Blocker, starting int) {
	for _, p := range pods {
		if p.Ready {
			continue
		}

		found := false
		for _, c := range p.Containers {
			reason, failing := classify(c)
			if !failing {
				continue
			}
			blockers = append(blockers, Blocker{
				Pod: p.Name, Container: c.Name, Init: c.Init,
				Reason: reason, Message: strings.TrimSpace(c.Message),
			})
			found = true
		}
		if !found {
			starting++
		}
	}

	// Stable order: a log that reorders between ticks is one nobody can follow.
	sort.SliceStable(blockers, func(i, j int) bool {
		if blockers[i].Pod != blockers[j].Pod {
			return blockers[i].Pod < blockers[j].Pod
		}
		return blockers[i].Container < blockers[j].Container
	})
	return blockers, starting
}

// Describe renders one progress line.
//
// Returns "" when there is nothing worth saying beyond the elapsed time, so the
// caller keeps its existing behaviour rather than this package inventing noise.
func Describe(blockers []Blocker, starting int) string {
	if len(blockers) == 0 {
		if starting == 0 {
			return ""
		}
		return fmt.Sprintf("%d Pod%s startet noch", starting, plural(starting, "", "s"))
	}

	parts := make([]string, 0, len(blockers))
	for _, b := range blockers {
		where := b.Container
		if b.Init {
			where = "init:" + b.Container
		}
		line := fmt.Sprintf("%s [%s] %s", shortPod(b.Pod), where, b.Reason)
		if b.Message != "" {
			line += " — " + summarise(b.Message)
		}
		parts = append(parts, line)
	}

	out := strings.Join(parts, " · ")
	if starting > 0 {
		out += fmt.Sprintf(" (%d weitere starten noch)", starting)
	}
	return out
}

// shortPod drops the ReplicaSet and pod hashes. `ess-matrix-authentication-service`
// is the name an operator recognises; the two hashes after it are noise that pushes
// the actual error off the line.
func shortPod(name string) string {
	parts := strings.Split(name, "-")
	// At most two: a ReplicaSet hash and a pod suffix. Bounded so a component whose
	// own name happens to look like a hash cannot be eaten segment by segment.
	for i := 0; i < 2 && len(parts) > 1 && looksLikeHash(parts[len(parts)-1]); i++ {
		parts = parts[:len(parts)-1]
	}
	return strings.Join(parts, "-")
}

// looksLikeHash distinguishes `jkvwf` and `8b8955db7` from `service` and `main`.
//
// The discriminator is vowels. Kubernetes generates these suffixes from an alphabet
// that deliberately omits a, e, i, o and u (and other easily-confused characters),
// so a generated segment has none while an English component name essentially always
// does. An earlier version required a digit instead, which was wrong the first time
// it met a real pod name: `jkvwf` has no digit either, and the test caught it.
func looksLikeHash(s string) bool {
	if len(s) < 5 || len(s) > 10 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		default:
			return false
		}
		if strings.ContainsRune("aeiou", r) {
			return false
		}
	}
	return true
}

// summarise condenses a log tail to something that fits on a progress line.
//
// Not just the first line. The failure that prompted this etappe read:
//
//	Error: could not connect to the database
//	Caused by:
//	    0: error returned from database: password authentication failed for user "…"
//
// The first line says there is a problem; the third says what it is. Taking only
// the first would have printed the half an operator can already guess. So the
// first two meaningful lines survive, within a character budget — a progress log
// that scrolls is a progress log nobody reads.
func summarise(s string) string {
	const budget = 200

	var kept []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		// Bare envelope lines carry no information of their own.
		if line == "" || line == "Caused by:" || line == "Caused by" {
			continue
		}
		// Rust/Go error chains number their causes; the number is not the message.
		line = strings.TrimLeft(line, "0123456789: ")
		if line == "" {
			continue
		}
		// A cause that merely repeats the line before it adds nothing.
		if len(kept) > 0 && strings.Contains(kept[len(kept)-1], line) {
			continue
		}
		kept = append(kept, line)
		if len(kept) == 2 {
			break
		}
	}

	out := strings.Join(kept, ": ")
	if len(out) > budget {
		return out[:budget] + "…"
	}
	return out
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
