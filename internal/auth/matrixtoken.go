package auth

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// The operator's Matrix token, held for the life of the process and no longer.
//
// Rooms and moderation talk to Synapse's admin API, which authenticates with a Matrix
// access token. MatrixCtrl uses the *operator's own* — obtained at login by asking MAS
// for the `urn:synapse:admin:*` scope, which MAS grants only to accounts with
// `can_request_admin`. The panel can therefore do what the person signed into it can
// do, and nothing more (etappe 36).
//
// Measured on the live deployment: MAS access tokens live **300 seconds**. Every one
// of them, without exception. So a page opened ten minutes after signing in needs a
// refresh token — a credential that can keep minting Synapse-admin access tokens for
// as long as the MAS session lasts.
//
// **That refresh token is never written to disk.** Persisting it would leave a
// Synapse-admin-capable credential at rest in Postgres, per operator, to save a
// sign-in. In memory, a restart costs the operator one login and costs an attacker
// with database access exactly nothing, because there is nothing there. The cost is
// visible and has an obvious cause; the alternative is invisible and permanent.
//
// Nothing here is ever logged. These are not identifiers.

const (
	// refreshSkew renews before expiry rather than after a failure. MAS tokens live
	// five minutes, so a request that starts at 4:55 must not carry a token that dies
	// mid-flight.
	refreshSkew = 45 * time.Second
	// maxSessions bounds the store. One entry per signed-in operator; the bound is
	// generous for that and finite for anything else.
	maxSessions = 256
)

type matrixSession struct {
	accessToken  string
	refreshToken string
	expiresAt    time.Time
}

// Refresher exchanges a refresh token for a new access token.
//
// Injected rather than imported so this store can be tested without an OIDC provider,
// and so the OIDC service keeps sole ownership of how it talks to MAS.
type Refresher func(ctx context.Context, refreshToken string) (access, refresh string, expiresIn int, err error)

// MatrixTokens holds one Matrix session per operator, in memory only.
type MatrixTokens struct {
	mu       sync.Mutex
	sessions map[string]*matrixSession
	refresh  Refresher
	now      func() time.Time
}

func NewMatrixTokens(refresh Refresher) *MatrixTokens {
	return &MatrixTokens{
		sessions: map[string]*matrixSession{},
		refresh:  refresh,
		now:      time.Now,
	}
}

// Put stores the tokens obtained at login.
func (m *MatrixTokens) Put(userID, access, refresh string, expiresIn int) {
	if userID == "" || access == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.sessions[userID]; !exists && len(m.sessions) >= maxSessions {
		m.evictExpiredLocked()
		if len(m.sessions) >= maxSessions {
			return // refuse rather than grow; the operator is told to sign in again
		}
	}
	m.sessions[userID] = &matrixSession{
		accessToken:  access,
		refreshToken: refresh,
		expiresAt:    m.now().Add(time.Duration(expiresIn) * time.Second),
	}
}

// Get returns a usable access token, refreshing it if it is about to expire.
//
// Returns ("", nil) — not an error — when this process holds nothing for the user.
// That is the ordinary state after a restart, and the caller renders it as "sign in
// again" rather than as a failure.
func (m *MatrixTokens) Get(ctx context.Context, userID string) (string, error) {
	m.mu.Lock()
	s, ok := m.sessions[userID]
	if !ok {
		m.mu.Unlock()
		return "", nil
	}
	if m.now().Before(s.expiresAt.Add(-refreshSkew)) {
		token := s.accessToken
		m.mu.Unlock()
		return token, nil
	}
	refreshToken := s.refreshToken
	m.mu.Unlock()

	if refreshToken == "" || m.refresh == nil {
		m.Forget(userID)
		return "", nil
	}

	// Refreshed outside the lock: this is a network call to MAS, and holding the
	// mutex across it would stall every other operator's request behind it.
	access, newRefresh, expiresIn, err := m.refresh(ctx, refreshToken)
	if err != nil {
		// A failed refresh means the MAS session is gone — revoked, expired, or the
		// operator signed out elsewhere. Dropping the entry turns the next call into
		// the same clean "sign in again" instead of a retry loop against a dead
		// session.
		m.Forget(userID)
		return "", fmt.Errorf("the Matrix session could not be renewed: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if newRefresh == "" {
		newRefresh = refreshToken // MAS may keep the same one
	}
	m.sessions[userID] = &matrixSession{
		accessToken:  access,
		refreshToken: newRefresh,
		expiresAt:    m.now().Add(time.Duration(expiresIn) * time.Second),
	}
	return access, nil
}

// Forget drops an operator's Matrix session. Called on logout, and whenever a refresh
// fails.
func (m *MatrixTokens) Forget(userID string) {
	m.mu.Lock()
	delete(m.sessions, userID)
	m.mu.Unlock()
}

// Has reports whether this process holds a session for the user, without refreshing.
// Used to tell the UI whether rooms are available before it asks for them.
func (m *MatrixTokens) Has(userID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.sessions[userID]
	return ok
}

func (m *MatrixTokens) evictExpiredLocked() {
	now := m.now()
	for id, s := range m.sessions {
		if now.After(s.expiresAt) && s.refreshToken == "" {
			delete(m.sessions, id)
		}
	}
}
