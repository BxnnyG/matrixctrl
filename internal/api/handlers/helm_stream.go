// The live log stream behind a running Helm operation: an in-memory buffer that
// late subscribers can catch up on, plus the progress ticker that keeps a silent
// upgrade from looking like a dead one.
package handlers

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// upgradeProgressInterval is how often a long-running Helm operation reports that
// it is still going. It has to be well below the shortest idle timeout in the path
// — Traefik's default is 180 s — and short enough that an operator does not start
// doubting a healthy upgrade.
const upgradeProgressInterval = 30 * time.Second

type upgradeStream struct {
	logs   []string
	status string
	done   bool
	subs   []chan string
	mu     sync.Mutex
}

func (s *upgradeStream) emit(msg interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var line string
	switch v := msg.(type) {
	case string:
		line = v
	default:
		b, _ := json.Marshal(v)
		line = string(b)
	}
	s.logs = append(s.logs, line)
	for _, sub := range s.subs {
		select {
		case sub <- line:
		default:
		}
	}
}

func (s *upgradeStream) finish(status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
	s.done = true
	for _, sub := range s.subs {
		close(sub)
	}
	s.subs = nil
}

// subscribe registers a channel for future log lines. It reports done=true when
// the upgrade already finished, in which case no channel is registered and the
// caller should not wait for one.
func (s *upgradeStream) subscribe() (ch chan string, done bool, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return nil, true, s.status
	}
	ch = make(chan string, 64)
	s.subs = append(s.subs, ch)
	return ch, false, s.status
}

// unsubscribe removes a channel registered by subscribe. It must be called when a
// consumer goes away, otherwise every dropped or reconnecting client leaves a
// dead channel in subs that emit keeps writing to — and reconnects are now the
// normal case, not an exception.
//
// It deliberately does not close the channel: finish may be closing it at the
// same moment, and both paths take s.mu, so whichever runs second finds nothing
// to do.
func (s *upgradeStream) unsubscribe(ch chan string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, sub := range s.subs {
		if sub == ch {
			s.subs = append(s.subs[:i], s.subs[i+1:]...)
			return
		}
	}
}

// snapshot returns the current logs and terminal state under the lock.
func (s *upgradeStream) snapshot() (logs []string, done bool, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	logs = make([]string, len(s.logs))
	copy(logs, s.logs)
	return logs, s.done, s.status
}

// startProgress emits an elapsed-time line every interval until the returned stop
// function is called.
//
// This exists because helm's Upgrade and Install run with Wait=true and block for
// as long as the rollout takes — minutes for ESS — without producing any output.
// The operator saw a stream that stopped after "Loaded 18 config slices…" and had
// no way to tell a working upgrade from a hung one (P1-7). The lines go through
// emit, so they are recorded in the log buffer and a reconnecting client replays
// them.
//
// stop waits for the emitter to have finished, rather than merely signalling it.
// Without that wait, a tick already in flight can land *after* stop returns, so a
// caller that stops the heartbeat and then writes a final line can have the
// heartbeat appear underneath it. It showed up first as a flaky test — the
// assertion "no further lines after stop" failed roughly one run in twenty, and
// only under parallel load, which is exactly the shape of a race that reaches
// production as "the log order is sometimes weird".
// probeFunc returns a line describing what the rollout is currently waiting for,
// or "" when there is nothing to add beyond the elapsed time.
type probeFunc func() string

func (s *upgradeStream) startProgress(label string, interval time.Duration) (stop func()) {
	return s.startProgressWithProbe(label, interval, nil)
}

// startProgressWithProbe is startProgress that can also say *what* it is waiting
// for.
//
// The plain version is a clock: it emits "(30s elapsed)" and never looks at the
// cluster. On 2026-08-05 an operator watched seven minutes of that while one pod sat
// in Init:CrashLoopBackOff with the explanation in its logs — all of it available to
// the process printing the elapsed time (E31).
//
// The probe is strictly additive. It runs on the ticker's goroutine, its failures
// are invisible, and the elapsed line is emitted whether or not it has anything to
// say: a diagnostic that can degrade a running upgrade is worse than one that is
// sometimes silent.
func (s *upgradeStream) startProgressWithProbe(label string, interval time.Duration, probe probeFunc) (stop func()) {
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		started := time.Now()
		t := time.NewTicker(interval)
		defer t.Stop()
		var last string
		for {
			select {
			case <-done:
				return
			case <-t.C:
				s.emit(fmt.Sprintf("%s… (%s elapsed)", label, formatElapsed(time.Since(started))))
				if probe == nil {
					continue
				}
				detail := probe()
				// Repeating an unchanged diagnosis every tick turns the one useful
				// line into the same wallpaper the elapsed counter already was.
				if detail == "" || detail == last {
					continue
				}
				last = detail
				s.emit("  ↳ " + detail)
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() { close(done) })
		<-finished // a second call also blocks here, harmlessly: the channel stays closed
	}
}

// formatElapsed renders a duration the way an operator reads it while waiting:
// seconds below a minute, then minutes and seconds.
func formatElapsed(d time.Duration) string {
	total := int(d.Round(time.Second).Seconds())
	if total < 60 {
		return fmt.Sprintf("%ds", total)
	}
	return fmt.Sprintf("%dm %02ds", total/60, total%60)
}
