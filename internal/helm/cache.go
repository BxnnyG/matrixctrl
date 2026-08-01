package helm

import (
	"time"
)

// Reading a release is expensive and the cost does not depend on how little of it
// you want: action.NewGet, NewGetMetadata and NewList all fetch the release secret
// and decompress the whole thing — manifest, hooks and every chart file. For the
// ESS release that secret is ~416 KB and all three paths were measured at ~4 s on
// the live cluster, while every Kubernetes list call in the same handler cost under
// a second. GetRelease keeps seven scalars out of all that, and the dashboard asks
// for them every 15 s.
//
// There is no cheaper API to switch to, so the result is cached instead.
//
// releaseCacheTTL only bounds staleness when the release is changed *outside*
// MatrixCtrl — an operator running `helm` directly. Every change made through
// MatrixCtrl calls InvalidateRelease and is visible immediately.
const releaseCacheTTL = 60 * time.Second

type cachedRelease struct {
	info     *ReleaseInfo
	cachedAt time.Time
}

// now is a field rather than a direct time.Now call so the expiry logic is
// testable without sleeping.
func (c *Client) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// cachedReleaseInfo returns a copy of the cached entry if it exists and is still
// fresh. A copy, because callers receive a pointer and must not be able to mutate
// what the next caller sees.
func (c *Client) cachedReleaseInfo(name string) (*ReleaseInfo, bool) {
	c.relMu.Lock()
	defer c.relMu.Unlock()

	entry, ok := c.relCache[name]
	if !ok || c.clock().Sub(entry.cachedAt) >= releaseCacheTTL {
		return nil, false
	}
	cp := *entry.info
	return &cp, true
}

func (c *Client) storeReleaseInfo(name string, info *ReleaseInfo) {
	if info == nil {
		return
	}
	c.relMu.Lock()
	defer c.relMu.Unlock()

	if c.relCache == nil {
		c.relCache = map[string]cachedRelease{}
	}
	cp := *info
	c.relCache[name] = cachedRelease{info: &cp, cachedAt: c.clock()}
}

// InvalidateRelease drops the cached info for a release. Call it after anything
// that changes the release — upgrade, rollback, applying config — so the next read
// reflects the change rather than waiting out the TTL.
func (c *Client) InvalidateRelease(name string) {
	c.relMu.Lock()
	defer c.relMu.Unlock()
	delete(c.relCache, name)
}
