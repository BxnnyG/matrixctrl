package helm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// The publication date of every ESS chart version, in one request.
//
// The version list has rendered a date column since it was written and has never
// had a date to put in it: ListVersions reads the GHCR *tag list*, which is a list
// of strings and nothing else. So the page showed 25 monospace version numbers and
// no way to tell last week's release from 2024's (E43).
//
// The date could come from each version's OCI manifest, but that is one round trip
// per version — 67 of them for a list nobody scrolls. GitHub's release index
// answers the whole question at once: 67 releases, single page, no pagination.
//
// This is decoration, and decoration is held to a stricter rule than the feature it
// decorates: it must never be able to break the thing next to it. GitHub's
// unauthenticated limit is 60 requests an hour *per address*, shared with the
// per-version release-notes fetch that the upgrade screen depends on — and the
// remaining budget was already down to 52 while this was being investigated. Hence
// a long TTL, stale-on-error, and a single flight.

const (
	releaseIndexURL = "https://api.github.com/repos/element-hq/ess-helm/releases?per_page=100"
	// releaseIndexTTL is long because the underlying facts barely move: ESS ships
	// roughly one chart release a week, and a date that is six hours stale is a
	// date that is right.
	releaseIndexTTL = 6 * time.Hour
	// releaseIndexRetry is how soon a *failed* fetch may be retried. Without it a
	// GitHub outage would mean one upstream request per page load, which is the
	// fastest possible way to spend a 60/hour budget.
	releaseIndexRetry = 10 * time.Minute
	indexTimeout      = 10 * time.Second
	// indexBodyLimit bounds the read. 67 releases with bodies is ~200 KB; an
	// unbounded read from a third party is how a version list becomes an outage.
	indexBodyLimit = 4 << 20
)

type releaseIndexEntry struct {
	TagName     string    `json:"tag_name"`
	PublishedAt time.Time `json:"published_at"`
}

var (
	indexMu      sync.Mutex
	indexCache   map[string]time.Time
	indexFetched time.Time
	indexFailed  time.Time
)

// releaseDates returns tag → publication time, or nil when it cannot be had.
//
// Never returns an error. A caller that has no dates renders a list without dates,
// which is what it did before this file existed; a caller that fails outright
// renders nothing, which would be a worse page than the one being improved.
func releaseDates(ctx context.Context) map[string]time.Time {
	indexMu.Lock()
	fresh := indexCache != nil && time.Since(indexFetched) < releaseIndexTTL
	backoff := time.Since(indexFailed) < releaseIndexRetry
	cached := indexCache
	indexMu.Unlock()

	if fresh || (cached != nil && backoff) {
		return cached
	}
	if cached == nil && backoff {
		return nil
	}

	got, err := fetchReleaseIndex(ctx)

	indexMu.Lock()
	defer indexMu.Unlock()
	if err != nil {
		indexFailed = time.Now()
		// Deliberately keeps serving the old map. A version's publication date does
		// not change, so an expired entry is not a wrong entry — it is a complete
		// answer about every version that existed when it was fetched, missing only
		// the ones released since.
		return indexCache
	}
	indexCache = got
	indexFetched = time.Now()
	indexFailed = time.Time{}
	return got
}

func fetchReleaseIndex(ctx context.Context) (map[string]time.Time, error) {
	reqCtx, cancel := context.WithTimeout(ctx, indexTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, releaseIndexURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("release index: status %d", resp.StatusCode)
	}

	var entries []releaseIndexEntry
	if err := json.NewDecoder(io.LimitReader(resp.Body, indexBodyLimit)).Decode(&entries); err != nil {
		return nil, err
	}

	out := make(map[string]time.Time, len(entries))
	for _, e := range entries {
		if e.TagName == "" || e.PublishedAt.IsZero() {
			continue
		}
		out[e.TagName] = e.PublishedAt
		// The registry tags carry no `v`, the GitHub tags might. Indexing both
		// spellings costs a map entry and removes a class of silent mismatch.
		out["v"+e.TagName] = e.PublishedAt
	}
	return out, nil
}

// withDates fills PublishedAt where the index knows it, leaving the rest alone.
func withDates(ctx context.Context, versions []VersionInfo) []VersionInfo {
	dates := releaseDates(ctx)
	if dates == nil {
		return versions
	}
	for i := range versions {
		if t, ok := dates[versions[i].Version]; ok {
			versions[i].PublishedAt = t
		}
	}
	return versions
}
