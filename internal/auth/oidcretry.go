package auth

import (
	"context"
	"log"
	"sync"
	"time"
)

// OIDC init used to be a single attempt at startup. On 2026-08-05 the container was
// OOMKilled and restarted before MAS was serving; discovery answered with a proxy
// error page instead of JSON, that one attempt failed, and the panel stayed in
// bootstrap mode for eleven hours while MAS was healthy the whole time. The operator
// could only get back in because someone else had cluster access.
//
// So the init keeps trying. The retry reuses the *effective startup config* rather
// than re-deriving it from one source: this deployment is env-configured and the DB
// holds no OIDC row, so a DB-only retry would have reported success and changed
// nothing — a silent no-op is worse than the original bug, because the logs would
// claim a recovery was in progress.

const (
	retryBase = 2 * time.Second
	retryMax  = 60 * time.Second
	// retryShiftCap bounds the shift *before* it happens. `1 << n` with an unbounded
	// n wraps to zero and turns a patient backoff into a hot loop — the same overflow
	// a test caught in the login throttle (E29). Clamping the exponent rather than
	// the result is what makes that impossible instead of merely unlikely.
	retryShiftCap = 5
	// logEvery keeps a permanently unreachable issuer from writing a line a minute
	// forever. The first failure is always logged; after that, every tenth.
	logEvery = 10
)

// RetryState is the observable state of the retry loop.
//
// Read by the login page through /auth/oidc/available so a password box that appears
// because the IdP is down does not look like the normal way in.
type RetryState struct {
	mu       sync.RWMutex
	active   bool
	attempts int
}

func NewRetryState() *RetryState { return &RetryState{} }

// Active reports whether a retry is in flight.
//
// Deliberately the only thing exposed. /auth/oidc/available is unauthenticated — it
// has to be, the login page calls it — so it answers "a retry is running", never the
// discovery error itself. The detail goes to the log, which already requires access.
func (s *RetryState) Active() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active
}

func (s *RetryState) begin() {
	s.mu.Lock()
	s.active = true
	s.mu.Unlock()
}

func (s *RetryState) end() {
	s.mu.Lock()
	s.active = false
	s.mu.Unlock()
}

func (s *RetryState) attempt() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts++
	return s.attempts
}

// RetryTarget is what the loop acts on. Plain funcs rather than an interface keep it
// testable without a fake OIDC provider or a live MAS.
type RetryTarget struct {
	// Build attempts one construction of the service.
	Build func(context.Context) (*OIDCService, error)
	// Installed reports whether a service is already active. The connect-OIDC setup
	// flow can install one while a retry is in flight, and a person acting
	// deliberately must win over a background loop.
	Installed func() bool
	// Install publishes a successful build.
	Install func(*OIDCService)
	// Wait sleeps for d, returning false if the context ended first. Injectable so
	// tests do not spend real seconds.
	Wait func(ctx context.Context, d time.Duration) bool
}

// retryBackoff returns the wait before the given 1-based attempt.
func retryBackoff(attempt int) time.Duration {
	steps := attempt - 1
	if steps < 0 {
		steps = 0
	}
	if steps > retryShiftCap {
		steps = retryShiftCap
	}
	if d := retryBase << steps; d < retryMax {
		return d
	}
	return retryMax
}

// RetryOIDC rebuilds the OIDC service until it succeeds, the context ends, or someone
// else installs one. It returns when it stops trying.
//
// Never gives up on its own. Giving up after N attempts would restore exactly the bug
// this exists to fix, only on a delay — and an issuer that is down for an hour is not
// meaningfully different from one that is down for a minute, except that the operator
// is more likely to be asleep for it.
func RetryOIDC(ctx context.Context, state *RetryState, t RetryTarget) {
	state.begin()
	defer state.end()

	for {
		n := state.attempt()
		if !t.Wait(ctx, retryBackoff(n)) {
			return
		}
		if t.Installed() {
			log.Printf("OIDC retry: a service was installed elsewhere (setup flow) — stopping after %d attempts", n)
			return
		}
		svc, err := t.Build(ctx)
		if err != nil {
			if n == 1 || n%logEvery == 0 {
				log.Printf("OIDC retry: attempt %d failed (%v) — still in bootstrap mode", n, err)
			}
			continue
		}
		t.Install(svc)
		log.Printf("OIDC recovered after %d attempt(s) — Matrix login is available again", n)
		return
	}
}

// SleepOrDone is the production Wait: it sleeps for d unless the context ends first.
func SleepOrDone(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
