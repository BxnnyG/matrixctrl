package auth

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func newTestTokens(t *testing.T, r Refresher) (*MatrixTokens, func(time.Duration)) {
	t.Helper()
	m := NewMatrixTokens(r)
	now := time.Now()
	m.now = func() time.Time { return now }
	return m, func(d time.Duration) { now = now.Add(d) }
}

func TestFreshTokenIsReturnedWithoutRefreshing(t *testing.T) {
	refreshed := 0
	m, _ := newTestTokens(t, func(context.Context, string) (string, string, int, error) {
		refreshed++
		return "new", "r2", 300, nil
	})
	m.Put("u1", "access", "r1", 300)

	got, err := m.Get(context.Background(), "u1")
	if err != nil || got != "access" {
		t.Fatalf("got %q err=%v", got, err)
	}
	if refreshed != 0 {
		t.Errorf("a valid token must not trigger a refresh, got %d", refreshed)
	}
}

// MAS tokens live 300s. A request starting at 4:55 must not carry one that dies
// mid-flight, so renewal happens before expiry rather than after a failure.
func TestRefreshHappensBeforeExpiry(t *testing.T) {
	refreshed := 0
	m, advance := newTestTokens(t, func(context.Context, string) (string, string, int, error) {
		refreshed++
		return "renewed", "r2", 300, nil
	})
	m.Put("u1", "access", "r1", 300)

	advance(300*time.Second - refreshSkew + time.Second)

	got, err := m.Get(context.Background(), "u1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "renewed" || refreshed != 1 {
		t.Errorf("expected one refresh and the new token, got %q refreshed=%d", got, refreshed)
	}
}

func TestRefreshedTokenIsReused(t *testing.T) {
	refreshed := 0
	m, advance := newTestTokens(t, func(context.Context, string) (string, string, int, error) {
		refreshed++
		return "renewed", "r2", 300, nil
	})
	m.Put("u1", "access", "r1", 300)
	advance(300 * time.Second)

	for i := 0; i < 3; i++ {
		if _, err := m.Get(context.Background(), "u1"); err != nil {
			t.Fatal(err)
		}
	}
	if refreshed != 1 {
		t.Errorf("the renewed token should be reused, refreshed %d times", refreshed)
	}
}

// The normal state after a restart: nothing held, and that is not an error.
func TestUnknownUserIsNotAnError(t *testing.T) {
	m, _ := newTestTokens(t, nil)
	got, err := m.Get(context.Background(), "nobody")
	if err != nil {
		t.Fatalf("an absent session is ordinary, got %v", err)
	}
	if got != "" {
		t.Errorf("expected no token, got %q", got)
	}
}

// A dead MAS session must not become a retry loop.
func TestFailedRefreshForgetsTheSession(t *testing.T) {
	calls := 0
	m, advance := newTestTokens(t, func(context.Context, string) (string, string, int, error) {
		calls++
		return "", "", 0, errors.New("session revoked")
	})
	m.Put("u1", "access", "r1", 300)
	advance(300 * time.Second)

	if _, err := m.Get(context.Background(), "u1"); err == nil {
		t.Fatal("a failed refresh should be reported")
	}
	if m.Has("u1") {
		t.Error("a dead session must be dropped")
	}

	// The next call is the clean "sign in again", not another doomed refresh.
	got, err := m.Get(context.Background(), "u1")
	if err != nil || got != "" {
		t.Errorf("expected a clean empty result, got %q err=%v", got, err)
	}
	if calls != 1 {
		t.Errorf("the refresher should not be called again, got %d", calls)
	}
}

// MAS may or may not rotate the refresh token; keeping the old one when none comes
// back is the difference between working and logging out every five minutes.
func TestRefreshTokenIsKeptWhenNotRotated(t *testing.T) {
	var seen []string
	m, advance := newTestTokens(t, func(_ context.Context, rt string) (string, string, int, error) {
		seen = append(seen, rt)
		return "a", "", 300, nil // no new refresh token
	})
	m.Put("u1", "access", "r1", 300)

	advance(300 * time.Second)
	if _, err := m.Get(context.Background(), "u1"); err != nil {
		t.Fatal(err)
	}
	advance(300 * time.Second)
	if _, err := m.Get(context.Background(), "u1"); err != nil {
		t.Fatal(err)
	}

	if len(seen) != 2 || seen[0] != "r1" || seen[1] != "r1" {
		t.Errorf("the original refresh token should be reused, saw %v", seen)
	}
}

func TestExpiredWithoutRefreshTokenJustEnds(t *testing.T) {
	m, advance := newTestTokens(t, nil)
	m.Put("u1", "access", "", 300)
	advance(300 * time.Second)

	got, err := m.Get(context.Background(), "u1")
	if err != nil || got != "" {
		t.Errorf("expected a clean empty result, got %q err=%v", got, err)
	}
	if m.Has("u1") {
		t.Error("the unusable session should be dropped")
	}
}

func TestForgetRemovesTheSession(t *testing.T) {
	m, _ := newTestTokens(t, nil)
	m.Put("u1", "access", "r1", 300)
	if !m.Has("u1") {
		t.Fatal("expected the session to be stored")
	}
	m.Forget("u1")
	if m.Has("u1") {
		t.Error("logout must drop the Matrix session too")
	}
}

func TestPutIgnoresIncompleteInput(t *testing.T) {
	m, _ := newTestTokens(t, nil)
	m.Put("", "access", "r", 300)
	m.Put("u1", "", "r", 300)
	if m.Has("") || m.Has("u1") {
		t.Error("a session without a user or without a token is not a session")
	}
}

func TestMatrixTokenStoreIsBounded(t *testing.T) {
	m, _ := newTestTokens(t, nil)
	for i := 0; i < maxSessions+20; i++ {
		m.Put(string(rune('a'+i%26))+string(rune('a'+i/26)), "access", "r", 300)
	}
	m.mu.Lock()
	size := len(m.sessions)
	m.mu.Unlock()
	if size > maxSessions {
		t.Fatalf("the store grew past its bound: %d", size)
	}
}

// The refresh is a network call and must not be made under the lock, or one slow MAS
// stalls every other operator.
func TestConcurrentGetsAreSafe(t *testing.T) {
	m, _ := newTestTokens(t, func(context.Context, string) (string, string, int, error) {
		return "a", "r", 300, nil
	})
	for i := 0; i < 20; i++ {
		m.Put(string(rune('a'+i)), "access", "r", 300)
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = m.Get(context.Background(), string(rune('a'+i)))
			m.Has(string(rune('a' + i)))
		}(i)
	}
	wg.Wait()
}
