package helm

import (
	"testing"
	"time"
)

// newTestClient builds a Client with a controllable clock. The cache never
// touches cfg, so a zero-value client is enough.
func newTestClient(now *time.Time) *Client {
	return &Client{now: func() time.Time { return *now }}
}

func TestReleaseCacheHitAndExpiry(t *testing.T) {
	clock := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	c := newTestClient(&clock)

	c.storeReleaseInfo("ess", &ReleaseInfo{Name: "ess", Version: "26.7.2", Revision: 22})

	if _, ok := c.cachedReleaseInfo("ess"); !ok {
		t.Fatal("freshly stored entry should be a hit")
	}

	// Just inside the TTL.
	clock = clock.Add(releaseCacheTTL - time.Second)
	if _, ok := c.cachedReleaseInfo("ess"); !ok {
		t.Fatal("entry should still be fresh one second before the TTL")
	}

	// Exactly at the TTL counts as expired — the boundary is a real case, since a
	// dashboard polling on a fixed interval can land on it.
	clock = clock.Add(time.Second)
	if _, ok := c.cachedReleaseInfo("ess"); ok {
		t.Fatal("entry should be expired at exactly the TTL")
	}
}

func TestReleaseCacheMissForUnknownRelease(t *testing.T) {
	clock := time.Now()
	c := newTestClient(&clock)
	if _, ok := c.cachedReleaseInfo("never-stored"); ok {
		t.Fatal("unknown release must be a miss")
	}
}

func TestInvalidateRelease(t *testing.T) {
	clock := time.Now()
	c := newTestClient(&clock)

	c.storeReleaseInfo("ess", &ReleaseInfo{Name: "ess", Version: "26.7.1"})
	c.InvalidateRelease("ess")

	if _, ok := c.cachedReleaseInfo("ess"); ok {
		t.Fatal("invalidated entry must not be served — this is what makes an " +
			"upgrade visible immediately instead of after the TTL")
	}
}

// The cache hands out pointers. If it handed out *the* pointer, one caller
// mutating the struct would silently change what every later caller sees.
func TestCacheReturnsCopiesNotTheStoredPointer(t *testing.T) {
	clock := time.Now()
	c := newTestClient(&clock)

	original := &ReleaseInfo{Name: "ess", Version: "26.7.2"}
	c.storeReleaseInfo("ess", original)

	// Mutating the caller's original must not affect the cache.
	original.Version = "mutated-after-store"

	got, ok := c.cachedReleaseInfo("ess")
	if !ok {
		t.Fatal("expected a cache hit")
	}
	if got.Version != "26.7.2" {
		t.Fatalf("cache stored a shared pointer: got %q", got.Version)
	}

	// Mutating what we got back must not affect the next reader either.
	got.Version = "mutated-after-read"
	again, _ := c.cachedReleaseInfo("ess")
	if again.Version != "26.7.2" {
		t.Fatalf("cache returned a shared pointer: got %q", again.Version)
	}
}

func TestInvalidateUnknownReleaseIsSafe(t *testing.T) {
	clock := time.Now()
	c := newTestClient(&clock)
	c.InvalidateRelease("does-not-exist") // must not panic on a nil map
}
