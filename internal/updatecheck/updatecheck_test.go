package updatecheck

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
)

// A registry that answers the two calls the real one does, so the test exercises the
// token dance rather than a stub of it.
func registry(t *testing.T, tags []string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/token"):
			fmt.Fprint(w, `{"token":"anonymous"}`)
		case strings.HasSuffix(r.URL.Path, "/tags/list"):
			if r.Header.Get("Authorization") != "Bearer anonymous" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			fmt.Fprintf(w, `{"name":"x","tags":[%s]}`, `"`+strings.Join(tags, `","`)+`"`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func checker(t *testing.T, current string, tags []string) *Checker {
	c := New(current)
	c.registry = registry(t, tags).URL
	return c
}

func TestNewestReleaseWins(t *testing.T) {
	// Deliberately not in order, and with the tags a real registry carries alongside
	// releases. String sorting would answer "0.1.9" here.
	c := checker(t, "0.1.73", []string{"0.1.9", "latest", "0.1.73", "0.1.100", "0.1.72", "sha-abc"})

	got := c.Check(context.Background())
	if got.Error != "" {
		t.Fatalf("unexpected error: %s", got.Error)
	}
	if got.Latest != "0.1.100" {
		t.Errorf("latest = %q, want 0.1.100 — non-version tags must be skipped and versions compared numerically", got.Latest)
	}
	if !got.Available {
		t.Error("0.1.100 is newer than 0.1.73; an update must be reported")
	}
}

func TestUpToDateIsNotAnUpdate(t *testing.T) {
	c := checker(t, "0.1.73", []string{"0.1.72", "0.1.73"})
	got := c.Check(context.Background())
	if got.Available {
		t.Error("running the newest version must not be reported as an available update")
	}
	if got.Latest != "0.1.73" {
		t.Errorf("latest = %q, want 0.1.73", got.Latest)
	}
}

// A developer's working copy reports version "dev". Telling them their build is out
// of date would be both true and useless.
func TestDevelopmentBuildIsNotCompared(t *testing.T) {
	c := checker(t, "dev", []string{"0.1.73"})
	got := c.Check(context.Background())
	if got.Available {
		t.Error("a development build must not claim an update is available")
	}
	if got.Error == "" {
		t.Error("it should say why there is no answer instead of silently reporting none")
	}
}

// The endpoint this feeds must answer whether or not a third party does.
func TestRegistryFailureIsReportedNotReturned(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := New("0.1.73")
	c.registry = srv.URL

	got := c.Check(context.Background())
	if got.Error == "" {
		t.Error("a failed check must say so")
	}
	if got.Current != "0.1.73" {
		t.Errorf("current = %q — the locally known answer must survive a failed remote call", got.Current)
	}
}

func TestAnswerIsCachedAndStaleAnswerSurvivesAFailure(t *testing.T) {
	var calls int
	var down bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/token") {
			fmt.Fprint(w, `{"token":"anonymous"}`)
			return
		}
		calls++
		fmt.Fprint(w, `{"tags":["0.1.74"]}`)
	}))
	defer srv.Close()

	now := time.Now()
	c := New("0.1.73")
	c.registry = srv.URL
	c.now = func() time.Time { return now }

	if got := c.Check(context.Background()); got.Latest != "0.1.74" {
		t.Fatalf("first check: latest = %q", got.Latest)
	}
	c.Check(context.Background())
	if calls != 1 {
		t.Errorf("tag list fetched %d times; the answer changes once per release and the UI asks on every page load", calls)
	}

	// Past the TTL, with the registry now unreachable: the last known answer is more
	// useful than nothing, and the failure is still reported alongside it.
	now = now.Add(7 * time.Hour)
	down = true
	got := c.Check(context.Background())
	if got.Latest != "0.1.74" {
		t.Errorf("latest = %q, want the last known 0.1.74 to survive an unreachable registry", got.Latest)
	}
	if got.Error == "" {
		t.Error("the stale answer must still admit the refresh failed")
	}
}

// The mock above answers exactly what I told it to, which is the whole problem with
// mocks: it proves the code handles my idea of GHCR. This one asks the real registry.
//
// Guarded like TestDiscoverLive, so CI and offline machines skip it.
func TestAgainstRealRegistry(t *testing.T) {
	if os.Getenv("RUN_LIVE") != "1" {
		t.Skip("set RUN_LIVE=1 to ask ghcr.io")
	}
	c := New("0.0.1") // deliberately ancient: any published release is newer
	got := c.Check(context.Background())
	if got.Error != "" {
		t.Fatalf("live check failed: %s", got.Error)
	}
	if got.Latest == "" {
		t.Fatal("no latest version came back from the real registry")
	}
	if !got.Available {
		t.Errorf("latest=%s is not newer than 0.0.1 — the comparison is wrong", got.Latest)
	}
	// 0.1.118 was the first release past GHCR's first page of 100 tags. A checker
	// that reads one page answers 0.1.117 here for ever (etappe 117).
	if v, err := semver.NewVersion(got.Latest); err != nil || v.LessThan(semver.MustParse("0.1.118")) {
		t.Errorf("latest = %s — the check stopped at the first page of tags", got.Latest)
	}
	t.Logf("ghcr.io says the newest published chart is %s", got.Latest)
}

// "Check now" has to reach the registry although the cached answer is still fresh —
// that is the case it exists for: a release published after the last check, which
// the cache would hide for up to its whole lifetime (etappe 116c).
func TestRefreshSeesAReleaseTheCacheWouldHide(t *testing.T) {
	var calls int
	tags := `"0.1.115"`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/token") {
			fmt.Fprint(w, `{"token":"anonymous"}`)
			return
		}
		calls++
		fmt.Fprintf(w, `{"tags":[%s]}`, tags)
	}))
	defer srv.Close()

	now := time.Now()
	c := New("0.1.115")
	c.registry = srv.URL
	c.now = func() time.Time { return now }

	if got := c.Check(context.Background()); got.Available {
		t.Fatalf("nothing newer yet: %+v", got)
	}

	// Published ten minutes later; the hourly cache is still fresh.
	tags = `"0.1.115","0.1.116"`
	now = now.Add(10 * time.Minute)
	if got := c.Check(context.Background()); got.Available {
		t.Fatalf("the plain check is expected to answer from cache here: %+v", got)
	}
	got := c.Refresh(context.Background())
	if !got.Available || got.Latest != "0.1.116" {
		t.Fatalf("refresh answered from the cache: %+v", got)
	}

	// Pressed again at once: the registry is not asked a third time.
	c.Refresh(context.Background())
	if calls != 2 {
		t.Errorf("tag list fetched %d times, want 2 — a button pressed in a loop must not reach the registry each time", calls)
	}
	now = now.Add(31 * time.Second)
	c.Refresh(context.Background())
	if calls != 3 {
		t.Errorf("tag list fetched %d times after the floor, want 3", calls)
	}
}
