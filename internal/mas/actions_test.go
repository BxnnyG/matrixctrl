package mas

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type capture struct {
	path string
	body map[string]any
}

func actionServer(t *testing.T, status int, out *capture) (*Client, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/token") {
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "expires_in": 300})
			return
		}
		out.path = r.URL.Path
		out.body = nil
		if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
			_ = json.Unmarshal(raw, &out.body)
		}
		w.WriteHeader(status)
	}))
	c := New(srv.URL, srv.URL+"/oauth2/token", "id", "secret")
	c.http = srv.Client()
	return c, srv.Close
}

// The deliberate deviation from MAS's own default, which is skip_erase=false — i.e.
// it asks the homeserver to GDPR-erase the account. A one-click irreversible erasure
// is the wrong default for an admin panel, and it sits oddly beside a Reactivate
// that cannot bring the data back. If this ever flips, calls that look identical
// start destroying data.
func TestDeactivateNeverErases(t *testing.T) {
	var got capture
	c, done := actionServer(t, http.StatusOK, &got)
	defer done()

	if err := c.Deactivate(context.Background(), "01ABC"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got.path, "/api/admin/v1/users/01ABC/deactivate") {
		t.Fatalf("path: %s", got.path)
	}
	if skip, _ := got.body["skip_erase"].(bool); !skip {
		t.Fatalf("skip_erase must be true, got body %+v", got.body)
	}
}

func TestAdminGrantAndRevokeSendTheRightValue(t *testing.T) {
	var got capture
	c, done := actionServer(t, http.StatusOK, &got)
	defer done()

	for _, want := range []bool{true, false} {
		if err := c.SetAdmin(context.Background(), "01ABC", want); err != nil {
			t.Fatal(err)
		}
		if v, _ := got.body["admin"].(bool); v != want {
			t.Errorf("admin=%v, want %v (body %+v)", v, want, got.body)
		}
	}
}

// A password must travel in the body and never in a URL: paths reach logs, proxies
// and the audit table's Resource column.
func TestPasswordGoesInTheBodyNotThePath(t *testing.T) {
	var got capture
	c, done := actionServer(t, http.StatusNoContent, &got)
	defer done()

	const secret = "correct-horse-battery-staple"
	if err := c.SetPassword(context.Background(), "01ABC", secret); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.path, secret) {
		t.Fatal("the password appeared in the request path")
	}
	if got.body["password"] != secret {
		t.Fatalf("body: %+v", got.body)
	}
}

// Rejected before a request is made, so an empty field cannot become an empty
// password on a real account.
func TestEmptyPasswordNeverReachesMAS(t *testing.T) {
	var got capture
	c, done := actionServer(t, http.StatusNoContent, &got)
	defer done()

	for _, pw := range []string{"", "   "} {
		if err := c.SetPassword(context.Background(), "01ABC", pw); err == nil {
			t.Errorf("%q should have been rejected", pw)
		}
	}
	if got.path != "" {
		t.Fatalf("no request should have been sent, got %s", got.path)
	}
}

// The UI has to be able to say *which* failure happened: a rejected password, a
// disabled password login and a vanished account need different next steps.
func TestErrorsAreDistinguishable(t *testing.T) {
	cases := []struct {
		status  int
		check   func(*ActionError) bool
		wantSub string
	}{
		{http.StatusNotFound, (*ActionError).NotFound, "kennt dieses Konto nicht"},
		{http.StatusBadRequest, (*ActionError).Rejected, "Komplexität"},
		{http.StatusForbidden, (*ActionError).Forbidden, "Passwort-Anmeldung deaktiviert"},
	}
	for _, tc := range cases {
		var got capture
		c, done := actionServer(t, tc.status, &got)

		err := c.Lock(context.Background(), "01ABC")
		actErr, ok := err.(*ActionError)
		if !ok {
			t.Fatalf("status %d: got %T", tc.status, err)
		}
		if !tc.check(actErr) {
			t.Errorf("status %d: predicate did not match", tc.status)
		}
		if !strings.Contains(actErr.Msg, tc.wantSub) {
			t.Errorf("status %d: message %q lacks %q", tc.status, actErr.Msg, tc.wantSub)
		}
		done()
	}
}

func TestSuccessStatuses(t *testing.T) {
	// set-password answers 204, the others 200. Treating 204 as a failure would
	// report "did not work" for a password that was in fact changed — the worst of
	// the available outcomes, because the operator sets it again.
	for _, status := range []int{http.StatusOK, http.StatusNoContent, http.StatusCreated} {
		var got capture
		c, done := actionServer(t, status, &got)
		if err := c.Unlock(context.Background(), "01ABC"); err != nil {
			t.Errorf("status %d: %v", status, err)
		}
		done()
	}
}

func TestActionPaths(t *testing.T) {
	ctx := context.Background()
	calls := map[string]func(*Client) error{
		"lock":       func(c *Client) error { return c.Lock(ctx, "01ABC") },
		"unlock":     func(c *Client) error { return c.Unlock(ctx, "01ABC") },
		"deactivate": func(c *Client) error { return c.Deactivate(ctx, "01ABC") },
		"reactivate": func(c *Client) error { return c.Reactivate(ctx, "01ABC") },
		"set-admin":  func(c *Client) error { return c.SetAdmin(ctx, "01ABC", true) },
	}
	for action, call := range calls {
		var got capture
		c, done := actionServer(t, http.StatusOK, &got)
		if err := call(c); err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(got.path, "/users/01ABC/"+action) {
			t.Errorf("%s: path %s", action, got.path)
		}
		done()
	}
}

func TestUnconfiguredClientRefusesWrites(t *testing.T) {
	c := New("https://mas.example", "", "", "")
	if err := c.Lock(context.Background(), "01ABC"); err != ErrNotConfigured {
		t.Fatalf("got %v", err)
	}
}

// The session stores `matrix_user_id` when userinfo provides it and the ULID `sub`
// otherwise. A comparison that handled only one form would protect against
// self-lockout in one deployment and silently not in the next.
func TestResolveUserHandlesBothIdentifierForms(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/token") {
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 300})
			return
		}
		seen = append(seen, r.URL.Path)
		_, _ = w.Write([]byte(`{"data":{"type":"user","id":"01RESOLVED","attributes":{"username":"alice","created_at":"2026-01-01T00:00:00Z","admin":true}}}`))
	}))
	defer srv.Close()

	c := New(srv.URL, srv.URL+"/oauth2/token", "id", "secret")
	c.http = srv.Client()
	ctx := context.Background()

	u, err := c.ResolveUser(ctx, "@alice:example.test")
	if err != nil || u == nil || u.ID != "01RESOLVED" {
		t.Fatalf("mxid form: %+v %v", u, err)
	}
	if !strings.Contains(seen[0], "/users/by-username/alice") {
		t.Errorf("mxid should resolve by username, got %s", seen[0])
	}

	if u, err = c.ResolveUser(ctx, "01ULID"); err != nil || u == nil {
		t.Fatalf("ulid form: %+v %v", u, err)
	}
	if !strings.HasSuffix(seen[1], "/users/01ULID") {
		t.Errorf("ulid should resolve by id, got %s", seen[1])
	}
}

// An unknown identity is (nil, nil) — a definite "MAS does not know this" rather
// than a failure, so the caller decides what that means.
func TestResolveUnknownUser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/token") {
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 300})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := New(srv.URL, srv.URL+"/oauth2/token", "id", "secret")
	c.http = srv.Client()

	for _, id := range []string{"@ghost:example.test", "01GHOST"} {
		u, err := c.ResolveUser(context.Background(), id)
		if err != nil || u != nil {
			t.Errorf("%s: got %+v, %v", id, u, err)
		}
	}
	if u, err := c.ResolveUser(context.Background(), "  "); err != nil || u != nil {
		t.Errorf("empty identifier: %+v %v", u, err)
	}
}

// A rotated secret must cost one retry, not every write until the process restarts.
func TestWriteRetriesOnceAfterUnauthorized(t *testing.T) {
	var writes int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/token") {
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 300})
			return
		}
		writes++
		if writes == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL, srv.URL+"/oauth2/token", "id", "secret")
	c.http = srv.Client()

	if err := c.Lock(context.Background(), "01ABC"); err != nil {
		t.Fatal(err)
	}
	if writes != 2 {
		t.Fatalf("expected exactly one retry, got %d writes", writes)
	}
}
