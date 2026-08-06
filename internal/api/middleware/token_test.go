package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// `?token=` used to be accepted on every route. chi's request logger writes the
// full URL, so any log line, link or Referer carrying one was a usable session
// (P0-5). It survives only where a browser genuinely cannot set a header.
func TestQueryTokenIsRejectedOnNormalRoutes(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/status?token=stolen", nil)
	if got := extractToken(r); got != "" {
		t.Fatalf("a plain request must not accept ?token=, got %q", got)
	}
}

// E29 kept `?token=` for the WebSocket handshake, the one place a browser cannot set
// a header. That was still enough to put a valid session JWT in the container log,
// once per upgrade stream — the exposure E29's own comments described. Handshakes now
// use single-use tickets, so no route accepts a session token from a URL (E35).
func TestQueryTokenIsRejectedEvenForWebSockets(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/helm/x/logs?token=legit", nil)
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Connection", "Upgrade")

	if got := extractToken(r); got != "" {
		t.Fatalf("a session token in a URL must never authenticate, got %q", got)
	}
}

// Browsers send `Connection: keep-alive, Upgrade`, so a whole-string comparison would
// reject a real handshake and break the upgrade log stream. Now tested directly:
// isWebSocketUpgrade decides which requests authenticate by ticket.
func TestConnectionHeaderIsParsedAsAList(t *testing.T) {
	for _, conn := range []string{"Upgrade", "upgrade", "keep-alive, Upgrade", "Upgrade, keep-alive"} {
		r := httptest.NewRequest(http.MethodGet, "/x", nil)
		r.Header.Set("Upgrade", "websocket")
		r.Header.Set("Connection", conn)
		if !isWebSocketUpgrade(r) {
			t.Errorf("Connection: %q should count as an upgrade", conn)
		}
	}
}

// The mirror of the above: these are not handshakes, so they must not reach the
// ticket path.
func TestPartialUpgradeHeadersAreNotHandshakes(t *testing.T) {
	cases := []map[string]string{
		{"Upgrade": "websocket"},
		{"Connection": "Upgrade"},
		{"Upgrade": "h2c", "Connection": "Upgrade"},
		{"Upgrade": "websocket", "Connection": "keep-alive"},
	}
	for _, headers := range cases {
		r := httptest.NewRequest(http.MethodGet, "/x", nil)
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		if isWebSocketUpgrade(r) {
			t.Errorf("%v must not count as a handshake", headers)
		}
	}
}

// Setting only one of the two headers is not a handshake, and must not be a way to
// re-open the query fallback.
func TestPartialUpgradeHeadersDoNotCount(t *testing.T) {
	cases := []map[string]string{
		{"Upgrade": "websocket"},
		{"Connection": "Upgrade"},
		{"Upgrade": "h2c", "Connection": "Upgrade"},
		{"Upgrade": "websocket", "Connection": "keep-alive"},
	}
	for _, headers := range cases {
		r := httptest.NewRequest(http.MethodGet, "/x?token=stolen", nil)
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		if got := extractToken(r); got != "" {
			t.Errorf("%v must not accept a query token, got %q", headers, got)
		}
	}
}

// The header path is unchanged and still wins.
func TestBearerHeaderStillWorks(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/status?token=stolen", nil)
	r.Header.Set("Authorization", "Bearer real")
	if got := extractToken(r); got != "real" {
		t.Fatalf("got %q", got)
	}
}

// The wiring, not the parts: a handshake authenticates by ticket, and a session token
// in the URL gets nowhere even though that used to be the supported way in.
func TestWebSocketAuthenticatesByTicketOnly(t *testing.T) {
	redeem := func(ticket string) (string, bool) {
		if ticket == "good-ticket" {
			return "user-1", true
		}
		return "", false
	}
	validate := func(string) (string, error) { return "user-1", nil }

	var reached bool
	h := RequireAuthWithTickets(validate, redeem)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			reached = true
			if got := UserIDFromContext(r.Context()); got != "user-1" {
				t.Errorf("the ticket's user must reach the handler, got %q", got)
			}
		}))

	handshake := func(query string) *httptest.ResponseRecorder {
		reached = false
		r := httptest.NewRequest(http.MethodGet, "/api/v1/x/logs?"+query, nil)
		r.Header.Set("Upgrade", "websocket")
		r.Header.Set("Connection", "Upgrade")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	if w := handshake("ticket=good-ticket"); w.Code != http.StatusOK || !reached {
		t.Errorf("a valid ticket should authenticate, got %d reached=%v", w.Code, reached)
	}
	if w := handshake("token=a.valid.jwt"); w.Code != http.StatusUnauthorized || reached {
		t.Errorf("a session token in the URL must be refused, got %d reached=%v", w.Code, reached)
	}
	if w := handshake("ticket=wrong"); w.Code != http.StatusUnauthorized || reached {
		t.Errorf("an unknown ticket must be refused, got %d", w.Code)
	}
	if w := handshake(""); w.Code != http.StatusUnauthorized || reached {
		t.Errorf("no credential must be refused, got %d", w.Code)
	}
}

// A ticket must not be a way to authenticate ordinary requests: those still need the
// header, and a ticket in a query string is not one.
func TestTicketDoesNotAuthenticatePlainRequests(t *testing.T) {
	redeem := func(string) (string, bool) { return "user-1", true }
	validate := func(string) (string, error) { return "user-1", nil }

	reached := false
	h := RequireAuthWithTickets(validate, redeem)(http.HandlerFunc(
		func(http.ResponseWriter, *http.Request) { reached = true }))

	r := httptest.NewRequest(http.MethodGet, "/api/v1/users?ticket=good-ticket", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized || reached {
		t.Fatalf("a ticket on a normal route must not authenticate, got %d reached=%v", w.Code, reached)
	}
}
