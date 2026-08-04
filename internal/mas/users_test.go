package mas

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Shaped after the live spec (PaginatedResponse_for_User): meta.count, data[] of
// SingleResource with id + attributes, links.next/prev carrying full relative URLs.
const livePage = `{
  "meta": {"count": 42},
  "data": [
    {"type":"user","id":"01ABC","attributes":{"username":"alice","created_at":"2026-01-02T03:04:05Z","locked_at":null,"deactivated_at":null,"admin":true,"legacy_guest":false}},
    {"type":"user","id":"01DEF","attributes":{"username":"bob","created_at":"2026-02-03T04:05:06Z","locked_at":"2026-06-01T00:00:00Z","deactivated_at":null,"admin":false,"legacy_guest":false}},
    {"type":"user","id":"01GHI","attributes":{"username":"carol","created_at":"2026-03-04T05:06:07Z","locked_at":null,"deactivated_at":"2026-07-01T00:00:00Z","admin":false,"legacy_guest":true}}
  ],
  "links": {
    "self": "/api/admin/v1/users?page[first]=25",
    "next": "/api/admin/v1/users?page[first]=25&page[after]=01GHI",
    "prev": "/api/admin/v1/users?page[last]=25&page[before]=01ABC"
  }
}`

func TestParsesTheLiveEnvelope(t *testing.T) {
	page, err := parseUserPage([]byte(livePage))
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 42 {
		t.Errorf("total: got %d", page.Total)
	}
	if len(page.Users) != 3 {
		t.Fatalf("users: got %d", len(page.Users))
	}
	if page.Next != "01GHI" || page.Prev != "01ABC" {
		t.Errorf("cursors: next=%q prev=%q", page.Next, page.Prev)
	}

	alice := page.Users[0]
	if alice.Username != "alice" || !alice.Admin || alice.State() != "active" {
		t.Errorf("alice: %+v", alice)
	}
	if !alice.CreatedAt.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Errorf("created_at: %v", alice.CreatedAt)
	}
}

// The distinction the UI depends on. Collapsing these into "disabled" would throw
// away what the operator needs to decide what to do: locked is reversible and
// usually temporary, deactivated is the account being gone.
func TestLockedAndDeactivatedAreDifferentStates(t *testing.T) {
	page, err := parseUserPage([]byte(livePage))
	if err != nil {
		t.Fatal(err)
	}
	if got := page.Users[1].State(); got != "locked" {
		t.Errorf("bob should be locked, got %s", got)
	}
	if got := page.Users[2].State(); got != "deactivated" {
		t.Errorf("carol should be deactivated, got %s", got)
	}
	if page.Users[1].LockedAt == nil || page.Users[2].DeactivatedAt == nil {
		t.Error("both timestamps must survive to the caller, not just the state word")
	}
}

// A user deactivated *and* locked is deactivated: the stronger fact wins, because
// telling an operator an account is merely locked when it is gone is the wrong
// direction to be wrong in.
func TestDeactivatedOutranksLocked(t *testing.T) {
	now := time.Now()
	u := User{LockedAt: &now, DeactivatedAt: &now}
	if u.State() != "deactivated" {
		t.Fatalf("got %s", u.State())
	}
}

// The link is a full relative URL with the whole query on it. Replaying it verbatim
// would let the caller ask MAS for whatever that link happened to contain; only the
// cursor is taken.
func TestCursorIsExtractedNotReplayed(t *testing.T) {
	got := cursorFromLink("/api/admin/v1/users?filter[admin]=true&page[first]=25&page[after]=01XYZ", "page[after]")
	if got != "01XYZ" {
		t.Fatalf("got %q", got)
	}
	for _, link := range []string{"", "://nope", "/api/admin/v1/users?page[first]=25"} {
		if c := cursorFromLink(link, "page[after]"); c != "" {
			t.Errorf("%q produced %q", link, c)
		}
	}
}

// A MAS version that renames an attribute should cost that attribute, not the page.
// But a row with no ID is skipped: a user the operator cannot act on is worse than
// one that is not shown.
func TestPartialAndJunkData(t *testing.T) {
	body := `{"data":[
	  {"id":"","attributes":{"username":"ghost"}},
	  {"id":"01OK","attributes":{"username":"real","created_at":"nonsense","locked_at":"also-nonsense"}},
	  {"id":"01MIN"}
	],"links":{}}`
	page, err := parseUserPage([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Users) != 2 {
		t.Fatalf("expected the two with IDs, got %d: %+v", len(page.Users), page.Users)
	}
	if page.Users[0].Username != "real" {
		t.Errorf("got %+v", page.Users[0])
	}
	// An unparseable timestamp must not become "locked".
	if page.Users[0].State() != "active" {
		t.Errorf("a junk timestamp must not create a state: %s", page.Users[0].State())
	}
	if !page.Users[0].CreatedAt.IsZero() {
		t.Error("a junk created_at must stay zero rather than becoming now")
	}
}

func TestEmptyAndBrokenResponses(t *testing.T) {
	page, err := parseUserPage([]byte(`{"data":[],"links":{},"meta":{"count":0}}`))
	if err != nil || len(page.Users) != 0 || page.Next != "" {
		t.Fatalf("an empty page is valid: %+v %v", page, err)
	}
	// `data: null` is in the spec as a legal shape.
	if page, err := parseUserPage([]byte(`{"data":null,"links":{}}`)); err != nil || len(page.Users) != 0 {
		t.Fatalf("null data is legal per the spec: %+v %v", page, err)
	}
	for _, bad := range []string{"", "not json", "[1,2]"} {
		if _, err := parseUserPage([]byte(bad)); err == nil {
			t.Errorf("%q should have failed", bad)
		}
	}
}

// Bootstrap mode has no client credentials. It must say so rather than fail at the
// first request with something nobody can act on.
func TestUnconfiguredClientSaysSo(t *testing.T) {
	c := New("https://mas.example", "", "", "")
	if c.Configured() {
		t.Fatal("no credentials means not configured")
	}
	if _, err := c.ListUsers(context.Background(), UserQuery{}); err != ErrNotConfigured {
		t.Fatalf("got %v", err)
	}
}

func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, func()) {
	t.Helper()
	srv := httptest.NewServer(handler)
	c := New(srv.URL, srv.URL+"/oauth2/token", "id", "secret")
	c.http = srv.Client()
	return c, srv.Close
}

func TestQueryTranslationAndTokenReuse(t *testing.T) {
	var tokenCalls int
	var lastQuery string

	c, done := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/token") {
			tokenCalls++
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "expires_in": 300})
			return
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("missing bearer: %q", r.Header.Get("Authorization"))
		}
		lastQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(livePage))
	})
	defer done()

	ctx := context.Background()
	if _, err := c.ListUsers(ctx, UserQuery{Search: "ali", Status: "active", AdminOnly: true, Limit: 10}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"filter%5Bsearch%5D=ali", "filter%5Bstatus%5D=active", "filter%5Badmin%5D=true", "page%5Bfirst%5D=10", "count=true"} {
		if !strings.Contains(lastQuery, want) {
			t.Errorf("query %q missing %q", lastQuery, want)
		}
	}

	// Paging backwards must use last/before and never send first as well: MAS
	// answers 400 to both rather than merging them.
	if _, err := c.ListUsers(ctx, UserQuery{Before: "01ABC", Limit: 10}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(lastQuery, "page%5Bbefore%5D=01ABC") || !strings.Contains(lastQuery, "page%5Blast%5D=10") {
		t.Errorf("backwards paging: %q", lastQuery)
	}
	if strings.Contains(lastQuery, "page%5Bfirst%5D") {
		t.Errorf("first and last must not both be sent: %q", lastQuery)
	}

	// The token is minted once, not per request — the old implementation minted one
	// per call, which doubles the requests for every page.
	if tokenCalls != 1 {
		t.Fatalf("expected one token mint, got %d", tokenCalls)
	}
}

// A revoked or rotated secret must cost one retry, not every request until the
// process restarts.
func TestUnauthorizedRefreshesTheTokenOnce(t *testing.T) {
	var tokenCalls, listCalls int

	c, done := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/token") {
			tokenCalls++
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "expires_in": 300})
			return
		}
		listCalls++
		if listCalls == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(livePage))
	})
	defer done()

	page, err := c.ListUsers(context.Background(), UserQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Users) != 3 {
		t.Fatalf("the retry should have succeeded: %+v", page)
	}
	if tokenCalls != 2 || listCalls != 2 {
		t.Fatalf("expected exactly one retry: tokens=%d lists=%d", tokenCalls, listCalls)
	}
}

// Caching a token past its life turns one expiry into a feature that stays broken
// until someone restarts the process.
func TestTokenWithoutExpiryIsNotCachedForever(t *testing.T) {
	c, done := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok"})
	})
	defer done()

	if _, err := c.adminToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.tokenTill.IsZero() || c.tokenTill.After(time.Now().Add(10*time.Minute)) {
		t.Fatalf("a response without expires_in must get a short life, got %v", c.tokenTill)
	}
}

func TestTokenErrorsSurface(t *testing.T) {
	c, done := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_client", "error_description": "bad secret"})
	})
	defer done()

	_, err := c.ListUsers(context.Background(), UserQuery{})
	if err == nil || !strings.Contains(err.Error(), "invalid_client") {
		t.Fatalf("got %v", err)
	}
}

func TestLimitIsBounded(t *testing.T) {
	var lastQuery string
	c, done := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/token") {
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 300})
			return
		}
		lastQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(livePage))
	})
	defer done()

	_, _ = c.ListUsers(context.Background(), UserQuery{Limit: 100000})
	if !strings.Contains(lastQuery, "page%5Bfirst%5D=100") {
		t.Errorf("an unbounded limit must be clamped: %q", lastQuery)
	}
}
