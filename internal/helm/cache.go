package helm

// Memoisation for GetRelease, keyed by the identity of the release secret rather
// than by a clock.
//
// The previous version of this file cached for 60 seconds and explained itself
// like this: "There is no cheaper API to switch to, so the result is cached
// instead." The measurement behind that was real — action.NewGet, NewGetMetadata
// and NewList all cost ~4 s — but the conclusion was wrong. All three are slow for
// the same reason: they ask the storage layer for Last(), which fetches and decodes
// *every* revision (11 secrets, 2.93 MB on the production release) to sort them and
// return the newest. The cost was not the read. It was the question.
//
// release_read.go now asks a cheaper question, and this cache changes shape with
// it: an entry is valid because the cluster still reports the same release secret,
// not because a timer has not run out yet. That removes the staleness window
// entirely — a returned value is either confirmed current or freshly decoded —
// which is a stronger guarantee than the TTL gave, not merely a faster one.

// releaseIdentity is everything the release secret's labels reveal without
// decoding the payload. Any change to the secret changes at least one of these,
// so equality is a safe basis for reusing a decoded release.
//
// modifiedAt is included because a status transition rewrites the secret without
// bumping the revision: an upgrade writes revision N as `pending-upgrade` and then
// updates that same revision to `deployed`. Keying on revision alone would pin the
// dashboard to "pending" until the next upgrade.
type releaseIdentity struct {
	Revision   int
	Status     string
	ModifiedAt string
	SecretName string
}

type memoisedRelease struct {
	identity releaseIdentity
	info     *ReleaseInfo
}

// memoisedReleaseInfo returns a copy of the stored entry if it was decoded from
// exactly the secret the probe just saw. A copy, because callers receive a pointer
// and must not be able to mutate what the next caller sees.
func (c *Client) memoisedReleaseInfo(name string, id releaseIdentity) (*ReleaseInfo, bool) {
	c.relMu.Lock()
	defer c.relMu.Unlock()

	entry, ok := c.relCache[name]
	if !ok || entry.identity != id {
		return nil, false
	}
	cp := *entry.info
	return &cp, true
}

func (c *Client) storeReleaseInfo(name string, id releaseIdentity, info *ReleaseInfo) {
	if info == nil {
		return
	}
	c.relMu.Lock()
	defer c.relMu.Unlock()

	if c.relCache == nil {
		c.relCache = map[string]memoisedRelease{}
	}
	cp := *info
	c.relCache[name] = memoisedRelease{identity: id, info: &cp}
}

// InvalidateRelease drops the memoised info for a release.
//
// It is no longer required for correctness: an upgrade or rollback writes a new
// release secret, the probe sees a different identity, and the next read decodes
// afresh whether or not this was called. It is kept because the call sites express
// something true and cheap — we just changed this, so stop claiming to know it —
// and because it is the one thing that still works if the probe is ever wrong.
func (c *Client) InvalidateRelease(name string) {
	c.relMu.Lock()
	defer c.relMu.Unlock()
	delete(c.relCache, name)
}

// Per-revision facts for the history page (etappe 39).
//
// Keyed by (release, revision) and never invalidated, because the two fields it
// holds — chart version and deployment time — are fixed when Helm writes the
// revision and are never rewritten. A rollback does not edit an old revision; it
// appends a new highest one. So this is not a cache with a staleness window, it is
// a record of something that already happened.
//
// The mutable field, status, is deliberately absent: it is read from the secret's
// labels on every call, which costs nothing extra because the label list is
// already being made.

func (c *Client) revisionFacts(name string, rev int) (revisionFacts, bool) {
	c.relMu.Lock()
	defer c.relMu.Unlock()

	byRev, ok := c.revCache[name]
	if !ok {
		return revisionFacts{}, false
	}
	f, ok := byRev[rev]
	return f, ok
}

func (c *Client) storeRevisionFacts(name string, rev int, f revisionFacts) {
	c.relMu.Lock()
	defer c.relMu.Unlock()

	if c.revCache == nil {
		c.revCache = map[string]map[int]revisionFacts{}
	}
	if c.revCache[name] == nil {
		c.revCache[name] = map[int]revisionFacts{}
	}
	c.revCache[name][rev] = f
}

// pruneRevisionFacts drops revisions the cluster no longer reports.
//
// Helm keeps only `--history-max` revisions and deletes the rest, so without this
// a long-running process would hold facts about revisions that stopped existing
// months ago. Bounded either way, but unbounded-looking growth invites someone to
// add an eviction policy that is not needed.
func (c *Client) pruneRevisionFacts(name string, live map[int]bool) {
	c.relMu.Lock()
	defer c.relMu.Unlock()

	byRev, ok := c.revCache[name]
	if !ok {
		return
	}
	for rev := range byRev {
		if !live[rev] {
			delete(byRev, rev)
		}
	}
}
