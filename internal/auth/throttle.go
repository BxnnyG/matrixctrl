package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Login throttling.
//
// bcrypt makes each attempt slow, which is a cost rather than a limit: an admin
// login reachable from the internet with no ceiling on attempts is an open door with
// a heavy handle (P1-17).
//
// Counted in Postgres, not in memory, for one reason: the pod restarts. An in-memory
// counter would make "restart it and try again" the attack, and MatrixCtrl restarts
// on every deploy.

const (
	// freeAttempts before any delay. People mistype passwords; a limiter that
	// punishes the third attempt gets disabled by the person it protects.
	freeAttempts = 5
	// lockoutAfter refuses outright. Chosen well above the number of tries a human
	// makes and far below the number a script needs.
	lockoutAfter = 15
	// lockoutFor is how long a locked key stays refused, measured from the last
	// failure — so continuing to hammer it keeps it locked.
	lockoutFor = 15 * time.Minute
	// forgetAfter clears the counter for someone who simply got it wrong and then
	// walked away.
	forgetAfter = time.Hour
	// maxBackoff caps the delay so a request cannot be held open indefinitely.
	maxBackoff = 4 * time.Second
)

type Throttle struct {
	db *pgxpool.Pool
}

func NewThrottle(db *pgxpool.Pool) *Throttle { return &Throttle{db: db} }

// ErrLockedOut is returned when a key has failed too often too recently.
type ErrLockedOut struct{ RetryAfter time.Duration }

func (e *ErrLockedOut) Error() string {
	return fmt.Sprintf("zu viele fehlgeschlagene Anmeldeversuche — bitte in %d Minuten erneut versuchen",
		int(e.RetryAfter.Minutes())+1)
}

// Check is called before verifying a password. It returns a delay the caller should
// wait, or ErrLockedOut.
//
// A database that cannot be reached returns an error, and the caller refuses the
// login. That is deliberate and it is the uncomfortable direction: the alternative
// is that taking the database down disables the rate limit, which turns a
// availability problem into an authentication one. The login already needs Postgres
// to verify a password, so nothing is lost that was working.
func (t *Throttle) Check(ctx context.Context, key string) (time.Duration, error) {
	var failures int
	var lastFailed time.Time

	err := t.db.QueryRow(ctx, `
		SELECT failures, last_failed FROM login_attempts
		WHERE key = $1 AND last_failed > NOW() - $2::interval`,
		key, intervalArg(forgetAfter),
	).Scan(&failures, &lastFailed)
	if err != nil {
		// No row is the common case: nobody has failed recently.
		if isNoRows(err) {
			return 0, nil
		}
		return 0, err
	}

	if failures >= lockoutAfter {
		remaining := lockoutFor - time.Since(lastFailed)
		if remaining > 0 {
			return 0, &ErrLockedOut{RetryAfter: remaining}
		}
		// The lockout has expired. The counter stays until the next success or the
		// forget window; one more failure locks it again immediately, which is the
		// intent — a slow attacker should not get a fresh budget every 15 minutes.
	}

	return backoffFor(failures), nil
}

// backoffFor grows the delay geometrically past the free attempts and then stops.
// The cap exists because an unbounded sleep is a way to exhaust the server's own
// connections, which would make the limiter the outage.
func backoffFor(failures int) time.Duration {
	if failures < freeAttempts {
		return 0
	}

	// The shift is clamped before it happens, not the result afterwards. Left
	// unclamped, `1 << 57` overflows the multiplication and the delay wraps to
	// zero — so the attacker who has failed the most would be the one waiting the
	// least. That is reachable: Check keeps returning a backoff after a lockout
	// window expires without resetting the counter, so `failures` grows without
	// bound. Found by a test, not in production.
	steps := failures - freeAttempts
	if steps > 8 {
		steps = 8
	}

	d := time.Duration(1<<uint(steps)) * 250 * time.Millisecond
	if d > maxBackoff {
		return maxBackoff
	}
	return d
}

// Failed records one failed attempt.
func (t *Throttle) Failed(ctx context.Context, key string) error {
	_, err := t.db.Exec(ctx, `
		INSERT INTO login_attempts(key, failures, first_failed, last_failed)
		VALUES($1, 1, NOW(), NOW())
		ON CONFLICT (key) DO UPDATE SET
			failures = CASE
				-- A counter older than the forget window starts over rather than
				-- resuming, so someone who mistyped last week is not near a lockout.
				WHEN login_attempts.last_failed < NOW() - $2::interval THEN 1
				ELSE login_attempts.failures + 1
			END,
			first_failed = CASE
				WHEN login_attempts.last_failed < NOW() - $2::interval THEN NOW()
				ELSE login_attempts.first_failed
			END,
			last_failed = NOW()`,
		key, intervalArg(forgetAfter),
	)
	return err
}

// Succeeded clears the counter. Only a correct password does this — anything else
// would give an attacker a way to reset their own budget.
func (t *Throttle) Succeeded(ctx context.Context, key string) error {
	_, err := t.db.Exec(ctx, `DELETE FROM login_attempts WHERE key = $1`, key)
	return err
}

func intervalArg(d time.Duration) string {
	return fmt.Sprintf("%d seconds", int(d.Seconds()))
}

// isNoRows compares against pgx's sentinel rather than the message text: an error
// string is not an API, and matching on one breaks silently on a driver upgrade.
func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
