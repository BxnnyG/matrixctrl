package helm

import (
	"strconv"
	"testing"
)

// The memo never touches cfg or the cluster, so a zero-value client is enough.
func newTestClient() *Client { return &Client{} }

func idAt(rev int, status, modifiedAt string) releaseIdentity {
	return releaseIdentity{
		Revision:   rev,
		Status:     status,
		ModifiedAt: modifiedAt,
		SecretName: "sh.helm.release.v1.ess.v" + strconv.Itoa(rev),
	}
}

func TestMemoHitRequiresTheSameSecret(t *testing.T) {
	c := newTestClient()
	id := idAt(22, "deployed", "1769459689")
	c.storeReleaseInfo("ess", id, &ReleaseInfo{Name: "ess", Version: "26.7.2", Revision: 22})

	if _, ok := c.memoisedReleaseInfo("ess", id); !ok {
		t.Fatal("the identity we stored under must be a hit")
	}
}

// A new revision must not be answered from the previous one. This is the whole
// correctness argument for dropping the TTL: freshness is decided by what the
// cluster reports, not by how much time has passed.
func TestMemoMissesOnNewRevision(t *testing.T) {
	c := newTestClient()
	c.storeReleaseInfo("ess", idAt(22, "deployed", "1769459689"),
		&ReleaseInfo{Name: "ess", Version: "26.7.2", Revision: 22})

	if _, ok := c.memoisedReleaseInfo("ess", idAt(23, "deployed", "1769999999")); ok {
		t.Fatal("revision 23 must not be served from revision 22's decode")
	}
}

// An upgrade writes revision N as pending-upgrade and then rewrites the same
// revision as deployed. Keying on the revision alone would leave the dashboard
// stuck on "pending" until the *next* upgrade — a status that is wrong for as long
// as the process lives.
func TestMemoMissesWhenOnlyTheStatusChanged(t *testing.T) {
	c := newTestClient()
	c.storeReleaseInfo("ess", idAt(23, "pending-upgrade", "1769999000"),
		&ReleaseInfo{Name: "ess", Version: "26.7.3", Revision: 23, Status: "pending-upgrade"})

	if _, ok := c.memoisedReleaseInfo("ess", idAt(23, "deployed", "1769999090")); ok {
		t.Fatal("a status transition on the same revision must force a fresh decode")
	}
}

// Helm rewrites the secret without changing revision or status in some flows
// (annotations, a re-applied label set). modifiedAt is the only thing that moves.
func TestMemoMissesWhenOnlyModifiedAtChanged(t *testing.T) {
	c := newTestClient()
	c.storeReleaseInfo("ess", idAt(23, "deployed", "1769999000"),
		&ReleaseInfo{Name: "ess", Revision: 23})

	if _, ok := c.memoisedReleaseInfo("ess", idAt(23, "deployed", "1769999500")); ok {
		t.Fatal("a rewritten secret must not be answered from the old decode")
	}
}

func TestMemoMissForUnknownRelease(t *testing.T) {
	c := newTestClient()
	if _, ok := c.memoisedReleaseInfo("never-stored", idAt(1, "deployed", "1")); ok {
		t.Fatal("unknown release must be a miss")
	}
}

func TestInvalidateRelease(t *testing.T) {
	c := newTestClient()
	id := idAt(22, "deployed", "1769459689")
	c.storeReleaseInfo("ess", id, &ReleaseInfo{Name: "ess", Version: "26.7.1"})
	c.InvalidateRelease("ess")

	if _, ok := c.memoisedReleaseInfo("ess", id); ok {
		t.Fatal("an invalidated entry must not be served even under its own identity")
	}
}

func TestInvalidateUnknownReleaseIsSafe(t *testing.T) {
	newTestClient().InvalidateRelease("does-not-exist") // must not panic on a nil map
}

// The memo hands out pointers. If it handed out *the* pointer, one caller mutating
// the struct would silently change what every later caller sees.
func TestMemoReturnsCopiesNotTheStoredPointer(t *testing.T) {
	c := newTestClient()
	id := idAt(22, "deployed", "1769459689")

	original := &ReleaseInfo{Name: "ess", Version: "26.7.2"}
	c.storeReleaseInfo("ess", id, original)

	// Mutating the caller's original must not affect the memo.
	original.Version = "mutated-after-store"

	got, ok := c.memoisedReleaseInfo("ess", id)
	if !ok {
		t.Fatal("expected a hit")
	}
	if got.Version != "26.7.2" {
		t.Fatalf("memo stored a shared pointer: got %q", got.Version)
	}

	// Mutating what we got back must not affect the next reader either.
	got.Version = "mutated-after-read"
	again, _ := c.memoisedReleaseInfo("ess", id)
	if again.Version != "26.7.2" {
		t.Fatalf("memo returned a shared pointer: got %q", again.Version)
	}
}
