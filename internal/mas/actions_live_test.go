package mas

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// Exercises the real write path end to end — token, POST, status mapping — without
// touching a real account. Deliberately targets an ID MAS cannot know: proving the
// request arrives and the 404 is mapped is worth having, and changing somebody's
// account to get it is not.
func TestLiveActionPathAgainstUnknownUser(t *testing.T) {
	issuer, id, secret := os.Getenv("MAS_ISSUER"), os.Getenv("MAS_CLIENT_ID"), os.Getenv("MAS_CLIENT_SECRET")
	if issuer == "" || id == "" || secret == "" {
		t.Skip("set MAS_ISSUER / MAS_CLIENT_ID / MAS_CLIENT_SECRET")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := New(issuer, issuer+"/oauth2/token", id, secret)

	err := c.Lock(ctx, "00000000000000000000000000")
	var actErr *ActionError
	if !errors.As(err, &actErr) {
		t.Fatalf("expected an ActionError, got %T: %v", err, err)
	}
	t.Logf("lock on an unknown id -> status=%d msg=%q", actErr.Status, actErr.Msg)
	if !actErr.NotFound() {
		t.Errorf("expected 404 for an unknown user, got %d", actErr.Status)
	}
}

// Identity resolution is read-only and is what the self-lockout rail depends on. If
// it silently failed, the rail would refuse every action — or worse, allow one.
func TestLiveResolveIdentity(t *testing.T) {
	issuer, id, secret := os.Getenv("MAS_ISSUER"), os.Getenv("MAS_CLIENT_ID"), os.Getenv("MAS_CLIENT_SECRET")
	who := os.Getenv("MAS_TEST_USERNAME")
	if issuer == "" || id == "" || secret == "" || who == "" {
		t.Skip("set MAS_ISSUER / MAS_CLIENT_ID / MAS_CLIENT_SECRET / MAS_TEST_USERNAME")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := New(issuer, issuer+"/oauth2/token", id, secret)

	byMXID, err := c.ResolveUser(ctx, "@"+who+":example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	if byMXID == nil {
		t.Fatal("resolving by MXID localpart found nothing")
	}
	byULID, err := c.ResolveUser(ctx, byMXID.ID)
	if err != nil || byULID == nil {
		t.Fatalf("resolving the same user by ULID failed: %+v %v", byULID, err)
	}
	// Both identifier shapes must land on the same account, or the self-lockout rail
	// protects in one deployment and silently not in the next.
	if byULID.ID != byMXID.ID {
		t.Fatalf("the two forms resolved differently: %s vs %s", byMXID.ID, byULID.ID)
	}
	t.Logf("both identifier forms resolved to the same account (admin=%v, state=%s)", byULID.Admin, byULID.State())
}
