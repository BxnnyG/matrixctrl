package helm

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestListVersionsLive proves the two things the unit tests cannot: that the tag
// walk still finds the real releases, and that the dates the list renders are
// actually there. Skipped unless RUN_LIVE=1 (needs outbound HTTPS).
//
// The date half is the point. The version list has rendered a date column since it
// was written and had nothing to put in it, for every version, for the life of the
// project — a defect no unit test can see, because the unit tests never call the
// registry.
func TestListVersionsLive(t *testing.T) {
	if os.Getenv("RUN_LIVE") == "" {
		t.Skip("set RUN_LIVE=1 to run against the live registry")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	start := time.Now()
	versions, err := ListVersions(ctx)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	t.Logf("%d versions in %s", len(versions), time.Since(start).Round(time.Millisecond))

	if len(versions) < 50 {
		t.Errorf("got %d versions, expected the full set (67 at the time of writing)", len(versions))
	}

	var dated, prerelease int
	for _, v := range versions {
		if !v.PublishedAt.IsZero() {
			dated++
		}
		if v.Prerelease {
			prerelease++
			t.Logf("prerelease: %s", v.Version)
		}
	}

	// Not "all", deliberately: a version present in the registry but not in the
	// GitHub release index legitimately has no date, and the list renders fine
	// without one. What would be a defect is the state this replaced — none at all.
	if dated < len(versions)/2 {
		t.Errorf("only %d of %d versions have a date; the index join is not working", dated, len(versions))
	}
	t.Logf("%d of %d dated", dated, len(versions))

	// Every suffixed tag this chart has ever published is a `0.x.y-dev` build tag,
	// and those are now dropped in parseReleaseTag. A non-zero count here means ESS
	// has started publishing real pre-releases — at which point the UI's
	// pre-release badge starts earning its place, and this expectation is the thing
	// that should be updated.
	if prerelease != 0 {
		t.Logf("NOTE: %d genuine pre-releases now published — see devTagRe", prerelease)
	}

	// Sorted newest-first, so index 0 is what the page calls "latest".
	if len(versions) > 1 && compareVersions(versions[0].Version, versions[1].Version) <= 0 {
		t.Errorf("not sorted newest-first: %s before %s", versions[0].Version, versions[1].Version)
	}
	t.Logf("latest: %s (%s)", versions[0].Version, versions[0].PublishedAt.Format(time.DateOnly))
}
