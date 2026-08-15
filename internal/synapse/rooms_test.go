package synapse

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func params(t *testing.T, o ListOptions) url.Values {
	t.Helper()
	v, err := url.ParseQuery(o.Query())
	if err != nil {
		t.Fatalf("the options produced an unparseable query: %v", err)
	}
	return v
}

func TestDefaultsAreApplied(t *testing.T) {
	v := params(t, ListOptions{})
	if v.Get("from") != "0" {
		t.Errorf("from should default to 0, got %q", v.Get("from"))
	}
	if v.Get("limit") != "50" {
		t.Errorf("limit should default to %d, got %q", defaultLimit, v.Get("limit"))
	}
	if v.Has("search_term") || v.Has("order_by") || v.Has("dir") {
		t.Errorf("empty options should send no filters, got %v", v)
	}
}

// A negative offset would make Synapse answer 400, turning an off-by-one in the
// frontend into a broken page.
func TestNegativeOffsetIsClamped(t *testing.T) {
	if got := params(t, ListOptions{From: -10}).Get("from"); got != "0" {
		t.Errorf("expected 0, got %q", got)
	}
}

func TestLimitIsBounded(t *testing.T) {
	if got := params(t, ListOptions{Limit: 100000}).Get("limit"); got != "500" {
		t.Errorf("an absurd limit should be capped at %d, got %q", maxLimit, got)
	}
	if got := params(t, ListOptions{Limit: 10}).Get("limit"); got != "10" {
		t.Errorf("a reasonable limit should survive, got %q", got)
	}
}

// Synapse answers 400 for an order_by it does not know, so a typo would break the page
// rather than fall back to a default ordering.
func TestUnknownOrderByIsDropped(t *testing.T) {
	if params(t, ListOptions{OrderBy: "nonsense"}).Has("order_by") {
		t.Error("an unknown order_by must not be sent")
	}
	if got := params(t, ListOptions{OrderBy: "joined_members"}).Get("order_by"); got != "joined_members" {
		t.Errorf("a known order_by should survive, got %q", got)
	}
}

func TestOnlyValidDirectionsAreSent(t *testing.T) {
	for _, bad := range []string{"", "forward", "F", "asc", "x"} {
		if params(t, ListOptions{Dir: bad}).Has("dir") {
			t.Errorf("dir %q should have been dropped", bad)
		}
	}
	for _, ok := range []string{"f", "b"} {
		if got := params(t, ListOptions{Dir: ok}).Get("dir"); got != ok {
			t.Errorf("dir %q should survive, got %q", ok, got)
		}
	}
}

// A search term is user input and goes into a URL.
func TestSearchTermIsEscaped(t *testing.T) {
	o := ListOptions{SearchTerm: "a&b=c d#e"}
	raw := o.Query()
	if strings.Contains(raw, "a&b=c") {
		t.Fatalf("the term was not escaped: %s", raw)
	}
	if got := params(t, o).Get("search_term"); got != "a&b=c d#e" {
		t.Errorf("the term should survive a round trip, got %q", got)
	}
}

func TestBlankSearchTermIsOmitted(t *testing.T) {
	if params(t, ListOptions{SearchTerm: "   "}).Has("search_term") {
		t.Error("whitespace is not a search")
	}
}

func newTestClient(t *testing.T, status int, body string, token string) (*Client, *string) {
	t.Helper()
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, func(context.Context) (string, error) { return token, nil })
	return c, &gotAuth
}

func TestListRoomsParsesAPage(t *testing.T) {
	body := `{"rooms":[{"room_id":"!a:x","name":"Team","joined_members":4,"public":true}],
	          "offset":0,"total_rooms":1,"next_batch":50,"prev_batch":null}`
	c, auth := newTestClient(t, http.StatusOK, body, "tok")

	page, err := c.ListRooms(context.Background(), ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if *auth != "Bearer tok" {
		t.Errorf("the token should travel in the header, got %q", *auth)
	}
	if len(page.Rooms) != 1 || page.Rooms[0].RoomID != "!a:x" || page.Rooms[0].JoinedMembers != 4 {
		t.Fatalf("unexpected page: %+v", page)
	}
	if page.TotalRooms != 1 {
		t.Errorf("the total should survive, got %d", page.TotalRooms)
	}
	if page.NextBatch == nil || *page.NextBatch != 50 {
		t.Errorf("next_batch should be read, got %v", page.NextBatch)
	}
	if page.PrevBatch != nil {
		t.Errorf("a null prev_batch is the first page, got %v", page.PrevBatch)
	}
}

// The distinction the UI depends on: 401 is recoverable by signing in, 403 is not.
func TestUnauthorisedAndForbiddenAreDistinct(t *testing.T) {
	c401, _ := newTestClient(t, http.StatusUnauthorized,
		`{"errcode":"M_UNKNOWN_TOKEN","error":"Invalid access token"}`, "tok")
	_, err := c401.ListRooms(context.Background(), ListOptions{})
	e, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected a *synapse.Error, got %T", err)
	}
	if !e.NeedsLogin() || e.NotAdmin() {
		t.Errorf("401 should mean sign in again: %+v", e)
	}
	if e.Code != "M_UNKNOWN_TOKEN" {
		t.Errorf("the Matrix errcode should be kept, got %q", e.Code)
	}

	c403, _ := newTestClient(t, http.StatusForbidden,
		`{"errcode":"M_FORBIDDEN","error":"You are not a server admin"}`, "tok")
	_, err = c403.ListRooms(context.Background(), ListOptions{})
	e, _ = err.(*Error)
	if e.NeedsLogin() || !e.NotAdmin() {
		t.Errorf("403 should mean this account cannot, ever: %+v", e)
	}
}

// After a restart there is no refresh token in this process, because it is held in
// memory only. That is a normal state, and it must arrive as the same "sign in again"
// the UI already handles rather than as an unexplained failure.
func TestNoTokenReadsAsNeedsLogin(t *testing.T) {
	c, _ := newTestClient(t, http.StatusOK, `{}`, "")
	_, err := c.ListRooms(context.Background(), ListOptions{})

	e, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected a *synapse.Error, got %T (%v)", err, err)
	}
	if !e.NeedsLogin() {
		t.Errorf("an absent token should read as needing login, got %+v", e)
	}
}

func TestNonJSONErrorBodyStillClassifies(t *testing.T) {
	c, _ := newTestClient(t, http.StatusBadGateway, "<html>no healthy upstream</html>", "tok")
	_, err := c.ListRooms(context.Background(), ListOptions{})

	e, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected a *synapse.Error, got %T", err)
	}
	if e.Status != http.StatusBadGateway || e.Message == "" {
		t.Errorf("a proxy error page must still produce a usable error: %+v", e)
	}
}

func TestMalformedSuccessBodyIsAnError(t *testing.T) {
	c, _ := newTestClient(t, http.StatusOK, "{not json", "tok")
	if _, err := c.ListRooms(context.Background(), ListOptions{}); err == nil {
		t.Fatal("a 200 with an unreadable body must not look like an empty room list")
	}
}

// An empty server is a legitimate answer and must not be confused with a failure.
func TestEmptyRoomListIsNotAnError(t *testing.T) {
	c, _ := newTestClient(t, http.StatusOK, `{"rooms":[],"offset":0,"total_rooms":0}`, "tok")
	page, err := c.ListRooms(context.Background(), ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Rooms) != 0 || page.TotalRooms != 0 {
		t.Errorf("unexpected: %+v", page)
	}
}

func TestBaseURLTrailingSlashDoesNotDoubleUp(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"rooms":[]}`))
	}))
	defer srv.Close()

	c := New(srv.URL+"/", func(context.Context) (string, error) { return "t", nil })
	if _, err := c.ListRooms(context.Background(), ListOptions{}); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/_synapse/admin/v1/rooms" {
		t.Errorf("path should not double its slashes, got %q", gotPath)
	}
}

// Guards the JSON contract against a rename that compiles but silently stops reading
// the field.
func TestRoomFieldNamesMatchTheAPI(t *testing.T) {
	var r Room
	raw := `{"room_id":"!r:x","canonical_alias":"#a:x","joined_local_members":2,
	         "state_events":9,"room_type":"m.space","history_visibility":"shared"}`
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}
	if r.RoomID != "!r:x" || r.CanonicalAlias != "#a:x" || r.JoinedLocal != 2 ||
		r.StateEvents != 9 || r.RoomType != "m.space" || r.HistoryVis != "shared" {
		t.Errorf("a field name drifted from the API: %+v", r)
	}
}
