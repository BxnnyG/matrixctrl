package mas

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const tokenOne = `{"type":"user-registration_token","id":"T1","attributes":{"token":"abc123","valid":true,"usage_limit":5,"times_used":2,"created_at":"2026-01-02T03:04:05Z","last_used_at":"2026-02-02T03:04:05Z","expires_at":null,"revoked_at":null}}`

type regServer struct {
	method, path, rawQuery string
	body                   map[string]any
	status                 int
	reply                  func(r *http.Request) string
	queries                []string
}

func newRegServer(t *testing.T, rs *regServer) (*Client, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/token") {
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "expires_in": 300})
			return
		}
		rs.method, rs.path, rs.rawQuery = r.Method, r.URL.Path, r.URL.RawQuery
		rs.queries = append(rs.queries, r.URL.RawQuery)
		rs.body = nil
		if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
			_ = json.Unmarshal(raw, &rs.body)
		}
		if rs.status != 0 {
			w.WriteHeader(rs.status)
			return
		}
		_, _ = w.Write([]byte(rs.reply(r)))
	}))
	c := New(srv.URL, srv.URL+"/oauth2/token", "id", "secret")
	c.http = srv.Client()
	return c, srv.Close
}

func TestListFollowsEveryPageAndReadsTheFields(t *testing.T) {
	rs := &regServer{reply: func(r *http.Request) string {
		if r.URL.Query().Get("page[after]") == "" {
			return `{"data":[` + tokenOne + `],"links":{"next":"/api/admin/v1/user-registration-tokens?page[first]=100&page[after]=T1"}}`
		}
		return `{"data":[{"id":"T2","attributes":{"token":"zzz","valid":false,"usage_limit":null,"times_used":0,"created_at":"2026-01-02T03:04:05Z","revoked_at":"2026-03-01T00:00:00Z"}},{"attributes":{"token":"no-id"}}],"links":{}}`
	}}
	c, done := newRegServer(t, rs)
	defer done()

	got, err := c.ListRegistrationTokens(context.Background(), "valid")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 tokens over 2 pages (the id-less row skipped), got %d", len(got))
	}
	a, b := got[0], got[1]
	if a.ID != "T1" || a.Token != "abc123" || a.UsageLimit == nil || *a.UsageLimit != 5 || a.TimesUsed != 2 ||
		a.LastUsedAt == nil || a.ExpiresAt != nil || a.State() != "valid" {
		t.Errorf("first: %+v", a)
	}
	if b.UsageLimit != nil || b.RevokedAt == nil || b.State() != "revoked" {
		t.Errorf("second: %+v", b)
	}
	if len(rs.queries) != 2 || !strings.Contains(rs.queries[0], "filter%5Bvalid%5D=true") || !strings.Contains(rs.queries[1], "page%5Bafter%5D=T1") {
		t.Errorf("queries: %v", rs.queries)
	}
}

func TestListRejectsAnUnknownFilterAndSurfacesAStatus(t *testing.T) {
	rs := &regServer{status: http.StatusInternalServerError}
	c, done := newRegServer(t, rs)
	defer done()
	if _, err := c.ListRegistrationTokens(context.Background(), "bogus"); err == nil {
		t.Error("unknown filter must be refused")
	}
	if _, err := c.ListRegistrationTokens(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("status must surface: %v", err)
	}
	if _, err := (*Client)(nil).ListRegistrationTokens(context.Background(), ""); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("unconfigured: %v", err)
	}
}

func TestCreateSendsOnlyWhatWasAskedFor(t *testing.T) {
	rs := &regServer{reply: func(*http.Request) string { return `{"data":` + tokenOne + `}` }}
	c, done := newRegServer(t, rs)
	defer done()

	if _, err := c.CreateRegistrationToken(context.Background(), "", nil, nil); err != nil {
		t.Fatal(err)
	}
	if rs.method != http.MethodPost || !strings.HasSuffix(rs.path, "/user-registration-tokens") || len(rs.body) != 0 {
		t.Errorf("random token, no limits: %s %s %v", rs.method, rs.path, rs.body)
	}
	n, exp := 3, time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	tok, err := c.CreateRegistrationToken(context.Background(), " hallo ", &n, &exp)
	if err != nil {
		t.Fatal(err)
	}
	if rs.body["token"] != "hallo" || rs.body["usage_limit"] != float64(3) || rs.body["expires_at"] != "2026-12-01T00:00:00Z" {
		t.Errorf("body: %v", rs.body)
	}
	if tok.ID != "T1" {
		t.Errorf("result: %+v", tok)
	}
	zero := 0
	if _, err := c.CreateRegistrationToken(context.Background(), "", &zero, nil); err == nil {
		t.Error("limit 0 must be refused before MAS is asked")
	}
}

func TestEditIsAPutThatCanClearBothFields(t *testing.T) {
	rs := &regServer{reply: func(*http.Request) string { return `{"data":` + tokenOne + `}` }}
	c, done := newRegServer(t, rs)
	defer done()
	if _, err := c.EditRegistrationToken(context.Background(), "T1", nil, nil); err != nil {
		t.Fatal(err)
	}
	v1, ok1 := rs.body["usage_limit"]
	v2, ok2 := rs.body["expires_at"]
	if rs.method != http.MethodPut || !strings.HasSuffix(rs.path, "/user-registration-tokens/T1") || !ok1 || !ok2 || v1 != nil || v2 != nil {
		t.Errorf("%s %s %v", rs.method, rs.path, rs.body)
	}
}

func TestRevokeAndUnrevokePaths(t *testing.T) {
	rs := &regServer{reply: func(*http.Request) string { return `{"data":` + tokenOne + `}` }}
	c, done := newRegServer(t, rs)
	defer done()
	if _, err := c.RevokeRegistrationToken(context.Background(), "T1"); err != nil || !strings.HasSuffix(rs.path, "/T1/revoke") || rs.method != http.MethodPost {
		t.Errorf("revoke: %v %s", err, rs.path)
	}
	if _, err := c.UnrevokeRegistrationToken(context.Background(), "T1"); err != nil || !strings.HasSuffix(rs.path, "/T1/unrevoke") {
		t.Errorf("unrevoke: %v %s", err, rs.path)
	}
}

func TestWriteErrorsSpeakGerman(t *testing.T) {
	for status, want := range map[int]string{404: "kennt diesen Code", 409: "gibt es bereits", 400: "abgelehnt", 500: "500"} {
		rs := &regServer{status: status}
		c, done := newRegServer(t, rs)
		_, err := c.CreateRegistrationToken(context.Background(), "x", nil, nil)
		done()
		var ae *ActionError
		if !errors.As(err, &ae) || !strings.Contains(ae.Msg, want) {
			t.Errorf("status %d: %v", status, err)
		}
	}
}

func TestStatePrecedence(t *testing.T) {
	past, future := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	one := 1
	cases := []struct {
		t    RegistrationToken
		want string
	}{
		{RegistrationToken{Valid: true}, "valid"},
		{RegistrationToken{Valid: true, ExpiresAt: &future}, "valid"},
		{RegistrationToken{ExpiresAt: &past, RevokedAt: &past}, "revoked"},
		{RegistrationToken{Valid: true, ExpiresAt: &past}, "expired"},
		{RegistrationToken{Valid: true, UsageLimit: &one, TimesUsed: 1}, "used"},
	}
	for i, c := range cases {
		if got := c.t.State(); got != c.want {
			t.Errorf("case %d: %s, want %s", i, got, c.want)
		}
	}
}
