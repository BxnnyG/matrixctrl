package mas

import (
	"context"
	"os"
	"testing"
	"time"
)

// Proves the one link the unit tests cannot: that the shared client, with real
// credentials, actually mints an admin token and reads a page from the live MAS.
// If the envelope assumptions were wrong the list would render empty and look
// exactly like a deployment with no users.
func TestLiveListUsers(t *testing.T) {
	issuer, id, secret := os.Getenv("MAS_ISSUER"), os.Getenv("MAS_CLIENT_ID"), os.Getenv("MAS_CLIENT_SECRET")
	if issuer == "" || id == "" || secret == "" {
		t.Skip("set MAS_ISSUER / MAS_CLIENT_ID / MAS_CLIENT_SECRET")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := New(issuer, issuer+"/oauth2/token", id, secret)
	page, err := c.ListUsers(ctx, UserQuery{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("total=%d returned=%d next=%q", page.Total, len(page.Users), page.Next)
	for _, u := range page.Users {
		t.Logf("  %-20s %-12s admin=%v created=%s", u.Username, u.State(), u.Admin, u.CreatedAt.Format("2006-01-02"))
	}
	if page.Total == 0 && len(page.Users) == 0 {
		t.Fatal("no users came back at all — the envelope assumptions are probably wrong")
	}

	// Search must actually narrow, or the filter is a control that does nothing.
	if len(page.Users) > 0 {
		name := page.Users[0].Username
		got, err := c.ListUsers(ctx, UserQuery{Search: name, Limit: 5})
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("search %q -> %d", name, len(got.Users))
		if len(got.Users) == 0 {
			t.Error("searching for a username that exists returned nothing")
		}
	}
}
