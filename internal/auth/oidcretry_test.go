package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

// The login throttle shipped with an overflow that made the most persistent attacker
// wait the least: `1 << (failures-5)` wrapped to zero. The same shape appears here,
// so the same test does.
func TestBackoffNeverCollapses(t *testing.T) {
	for _, attempt := range []int{1, 2, 5, 10, 100, 1000, 62, 63, 64, 65, 1 << 20} {
		d := retryBackoff(attempt)
		if d <= 0 {
			t.Fatalf("attempt %d produced a non-positive wait (%v) — that is a hot loop", attempt, d)
		}
		if d > retryMax {
			t.Errorf("attempt %d exceeded the cap: %v", attempt, d)
		}
	}
}

func TestBackoffGrowsThenHolds(t *testing.T) {
	if got := retryBackoff(1); got != retryBase {
		t.Errorf("first wait should be the base, got %v", got)
	}
	if retryBackoff(2) <= retryBackoff(1) {
		t.Error("backoff should grow")
	}
	if got := retryBackoff(50); got != retryMax {
		t.Errorf("a late attempt should sit at the cap, got %v", got)
	}
}

// nowait runs the loop without spending real time.
func nowait(context.Context, time.Duration) bool { return true }

func TestRecoversAndInstallsOnce(t *testing.T) {
	var installs int
	attempts := 0
	st := NewRetryState()

	RetryOIDC(context.Background(), st, RetryTarget{
		Wait:      nowait,
		Installed: func() bool { return false },
		Install:   func(*OIDCService) { installs++ },
		Build: func(context.Context) (*OIDCService, error) {
			attempts++
			if attempts < 3 {
				return nil, errors.New("no healthy upstream")
			}
			return &OIDCService{}, nil
		},
	})

	if attempts != 3 {
		t.Errorf("expected to keep trying until success, got %d attempts", attempts)
	}
	if installs != 1 {
		t.Errorf("expected exactly one install, got %d", installs)
	}
	if st.Active() {
		t.Error("the state must not stay active after the loop returns")
	}
}

// The connect-OIDC setup flow is a person acting deliberately. A background loop must
// not overwrite what they just installed.
func TestSetupFlowWinsOverAnInFlightRetry(t *testing.T) {
	var installs int

	RetryOIDC(context.Background(), NewRetryState(), RetryTarget{
		Wait:      nowait,
		Installed: func() bool { return true },
		Install:   func(*OIDCService) { installs++ },
		Build: func(context.Context) (*OIDCService, error) {
			t.Fatal("must not build once a service is already installed")
			return nil, nil
		},
	})

	if installs != 0 {
		t.Errorf("nothing should have been installed, got %d", installs)
	}
}

// No goroutine may outlive the process context.
func TestStopsWhenContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		RetryOIDC(ctx, NewRetryState(), RetryTarget{
			Wait:      func(c context.Context, _ time.Duration) bool { return c.Err() == nil },
			Installed: func() bool { return false },
			Install:   func(*OIDCService) {},
			Build: func(context.Context) (*OIDCService, error) {
				return nil, errors.New("still down")
			},
		})
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the loop ignored a cancelled context")
	}
}

// A permanently unreachable issuer must not be given up on — that would restore the
// lockout this package exists to prevent, only on a delay.
func TestKeepsTryingWhileTheIssuerStaysDown(t *testing.T) {
	const cutoff = 200
	attempts := 0
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	RetryOIDC(ctx, NewRetryState(), RetryTarget{
		Wait:      func(c context.Context, _ time.Duration) bool { return c.Err() == nil },
		Installed: func() bool { return false },
		Install:   func(*OIDCService) {},
		Build: func(context.Context) (*OIDCService, error) {
			attempts++
			if attempts >= cutoff {
				cancel() // only the context stops it, never an attempt budget
			}
			return nil, errors.New("still down")
		},
	})

	if attempts < cutoff {
		t.Errorf("the loop gave up on its own after %d attempts", attempts)
	}
}

func TestStateReportsActiveWhileRunning(t *testing.T) {
	st := NewRetryState()
	if st.Active() {
		t.Error("a fresh state is not active")
	}

	seen := false
	RetryOIDC(context.Background(), st, RetryTarget{
		Wait:      nowait,
		Installed: func() bool { return false },
		Install:   func(*OIDCService) {},
		Build: func(context.Context) (*OIDCService, error) {
			seen = st.Active() // the login page must be able to see this mid-flight
			return &OIDCService{}, nil
		},
	})

	if !seen {
		t.Error("the retry must report itself active while it runs")
	}
}

// A nil state is what a caller passes when OIDC was never configured at all.
func TestNilStateIsNotActive(t *testing.T) {
	var st *RetryState
	if st.Active() {
		t.Error("a nil state must read as not retrying")
	}
}
