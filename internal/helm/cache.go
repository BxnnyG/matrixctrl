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
