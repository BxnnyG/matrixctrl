package handlers

import (
	"testing"
	"time"
)

func TestFormatElapsed(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{0, "0s"},
		{5 * time.Second, "5s"},
		{59 * time.Second, "59s"},
		{60 * time.Second, "1m 00s"},
		{95 * time.Second, "1m 35s"},
		{10 * time.Minute, "10m 00s"},
		// Rounding, not truncation: 89.6s reads as 1m 30s, not 1m 29s.
		{89600 * time.Millisecond, "1m 30s"},
	}
	for _, c := range cases {
		if got := formatElapsed(c.in); got != c.want {
			t.Errorf("formatElapsed(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSubscribeReceivesEmittedLines(t *testing.T) {
	s := &upgradeStream{status: "running"}

	ch, done, _ := s.subscribe()
	if done {
		t.Fatal("a running stream must not report done")
	}

	s.emit("hello")

	select {
	case line := <-ch:
		if line != "hello" {
			t.Fatalf("got %q, want %q", line, "hello")
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive the emitted line")
	}
}

// The bug this guards: before unsubscribe existed, every dropped or reconnecting
// client left its channel in subs forever, and emit kept writing to it. With
// reconnects now being normal behaviour that would grow without bound.
func TestUnsubscribeRemovesTheChannel(t *testing.T) {
	s := &upgradeStream{status: "running"}

	a, _, _ := s.subscribe()
	b, _, _ := s.subscribe()

	s.mu.Lock()
	got := len(s.subs)
	s.mu.Unlock()
	if got != 2 {
		t.Fatalf("expected 2 subscribers, got %d", got)
	}

	s.unsubscribe(a)

	s.mu.Lock()
	got = len(s.subs)
	remaining := s.subs[0]
	s.mu.Unlock()
	if got != 1 {
		t.Fatalf("expected 1 subscriber after unsubscribe, got %d", got)
	}
	if remaining != b {
		t.Fatal("unsubscribe removed the wrong channel")
	}
}

func TestUnsubscribeAfterFinishIsSafe(t *testing.T) {
	s := &upgradeStream{status: "running"}
	ch, _, _ := s.subscribe()

	s.finish("success") // closes and clears subs
	s.unsubscribe(ch)   // must not panic or double-close

	if _, open := <-ch; open {
		t.Fatal("channel should be closed after finish")
	}
}

// Racing an upgrade that finishes just before a client subscribes: the client
// must be told the outcome instead of waiting on a channel nobody will ever
// write to.
func TestSubscribeAfterFinishReportsDone(t *testing.T) {
	s := &upgradeStream{status: "running"}
	s.finish("success")

	ch, done, status := s.subscribe()
	if !done {
		t.Fatal("subscribe must report done for a finished stream")
	}
	if status != "success" {
		t.Fatalf("status = %q, want %q", status, "success")
	}
	if ch != nil {
		t.Fatal("no channel should be registered for a finished stream")
	}
}

func TestSnapshotCopiesLogs(t *testing.T) {
	s := &upgradeStream{status: "running"}
	s.emit("one")

	logs, done, status := s.snapshot()
	if len(logs) != 1 || logs[0] != "one" {
		t.Fatalf("unexpected snapshot: %v", logs)
	}
	if done || status != "running" {
		t.Fatalf("unexpected state: done=%v status=%q", done, status)
	}

	// Mutating the snapshot must not corrupt the stream's own buffer.
	logs[0] = "tampered"
	again, _, _ := s.snapshot()
	if again[0] != "one" {
		t.Fatalf("snapshot shares the underlying array: %q", again[0])
	}
}

func TestStartProgressEmitsUntilStopped(t *testing.T) {
	s := &upgradeStream{status: "running"}

	stop := s.startProgress("Waiting", 10*time.Millisecond)
	time.Sleep(55 * time.Millisecond)
	stop()

	logs, _, _ := s.snapshot()
	if len(logs) == 0 {
		t.Fatal("expected progress lines while running — this is the whole point " +
			"of P1-7: the stream must not go silent during helm's wait")
	}

	// After stop, no further lines.
	countAtStop := len(logs)
	time.Sleep(40 * time.Millisecond)
	logs, _, _ = s.snapshot()
	if len(logs) != countAtStop {
		t.Fatalf("progress kept emitting after stop: %d -> %d", countAtStop, len(logs))
	}
}

func TestStopProgressIsIdempotent(t *testing.T) {
	s := &upgradeStream{status: "running"}
	stop := s.startProgress("Waiting", time.Hour)
	stop()
	stop() // a second call must not panic on a closed channel
}
