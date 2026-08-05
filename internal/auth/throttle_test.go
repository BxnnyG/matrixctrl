package auth

import (
	"testing"
	"time"
)

// The shape of the backoff, without a database. What matters is that it is zero
// while people are plausibly mistyping, grows once they are not, and stops growing
// before a request can be held open long enough to exhaust the server's own
// connections — a limiter that becomes the outage has not helped.
func TestBackoffShape(t *testing.T) {
	for i := 0; i < freeAttempts; i++ {
		if d := backoffFor(i); d != 0 {
			t.Errorf("attempt %d should be free, got %v", i, d)
		}
	}

	first := backoffFor(freeAttempts)
	if first <= 0 {
		t.Fatalf("the first attempt past the free ones must cost something, got %v", first)
	}

	prev := first
	for i := freeAttempts + 1; i < freeAttempts+8; i++ {
		d := backoffFor(i)
		if d < prev {
			t.Errorf("attempt %d went backwards: %v after %v", i, d, prev)
		}
		if d > maxBackoff {
			t.Errorf("attempt %d exceeded the cap: %v > %v", i, d, maxBackoff)
		}
		prev = d
	}

	// Far past any plausible attempt count it must still be the cap, not an
	// arithmetic overflow that wraps to something small — or negative.
	for _, n := range []int{40, 62, 63, 64, 100, 1000} {
		d := backoffFor(n)
		if d != maxBackoff {
			t.Errorf("attempt %d: got %v, want the cap %v", n, d, maxBackoff)
		}
	}
}

func TestLockoutMessageNamesAWaitingTime(t *testing.T) {
	e := &ErrLockedOut{RetryAfter: 90 * time.Second}
	if msg := e.Error(); msg == "" {
		t.Fatal("empty message")
	}
	// Rounds up rather than down: telling someone to wait 1 minute when it is 90
	// seconds produces a second failure and a second lockout extension.
	if got := (&ErrLockedOut{RetryAfter: 90 * time.Second}).Error(); got != "zu viele fehlgeschlagene Anmeldeversuche — bitte in 2 Minuten erneut versuchen" {
		t.Fatalf("got %q", got)
	}
}

func TestIntervalArg(t *testing.T) {
	if got := intervalArg(time.Hour); got != "3600 seconds" {
		t.Fatalf("got %q", got)
	}
}
