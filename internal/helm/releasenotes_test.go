package helm

import (
	"context"
	"testing"
)

func resetNotesCache() {
	notesMu.Lock()
	notesCache = map[string]ReleaseNotes{}
	notesMu.Unlock()
}

// The version becomes a URL path segment. A value with a slash or a dot-dot would
// address a different GitHub resource entirely, so it is refused rather than escaped
// — refusing is simpler to be certain of.
func TestVersionCannotEscapeThePath(t *testing.T) {
	for _, bad := range []string{
		"", "../../../etc/passwd", "26.8.0/../../other", "a b", "26.8.0?x=1",
		"../releases", "/26.8.0", "26.8.0#frag",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", // 72 chars
	} {
		if _, err := FetchReleaseNotes(context.Background(), bad); err == nil {
			t.Errorf("%q should have been refused", bad)
		}
	}
}

func TestRealisticVersionsAreAccepted(t *testing.T) {
	for _, ok := range []string{"26.8.0", "26.8.0-rc1", "0.10.1-shac00c", "26.5.1"} {
		if !safeVersion.MatchString(ok) {
			t.Errorf("%q should be accepted", ok)
		}
	}
}

// A published version's notes do not change, and GitHub's unauthenticated limit is
// 60 requests an hour. A page that refetched on every render would exhaust it and
// then show nothing at all.
func TestSecondCallIsServedFromCache(t *testing.T) {
	resetNotesCache()
	notesMu.Lock()
	notesCache["26.8.0"] = ReleaseNotes{Version: "26.8.0", Available: true, Body: "cached"}
	notesMu.Unlock()

	got, err := FetchReleaseNotes(context.Background(), "26.8.0")
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "cached" {
		t.Fatalf("expected the cached entry, got %+v", got)
	}
}

// Unbounded is unbounded: the number of versions grows even though each entry is
// immutable.
func TestCacheIsBounded(t *testing.T) {
	resetNotesCache()
	notesMu.Lock()
	for i := 0; i < notesCacheMax; i++ {
		notesCache[string(rune('a'+i%26))+string(rune('a'+i/26))] = ReleaseNotes{}
	}
	notesMu.Unlock()

	// One more entry past the bound clears rather than growing.
	_, _ = FetchReleaseNotes(context.Background(), "26.99.0")

	notesMu.Lock()
	size := len(notesCache)
	notesMu.Unlock()
	if size > notesCacheMax {
		t.Fatalf("cache grew past the bound: %d", size)
	}
}

// An air-gapped install must read "could not be fetched", not "this version has no
// notes" — the two lead to different actions.
func TestUnavailableCarriesAReason(t *testing.T) {
	resetNotesCache()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // no network attempt will succeed

	got, err := FetchReleaseNotes(ctx, "26.8.0")
	if err != nil {
		t.Fatalf("a failed fetch is not an error for the caller: %v", err)
	}
	if got.Available {
		t.Fatal("a cancelled request cannot be available")
	}
	if got.Reason == "" {
		t.Fatal("an unavailable result must say why")
	}
	if got.Version != "26.8.0" {
		t.Errorf("the version should survive: %+v", got)
	}
}
