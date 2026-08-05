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

func TestQueryTokenSurvivesForWebSockets(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/helm/x/logs?token=legit", nil)
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Connection", "Upgrade")

	if got := extractToken(r); got != "legit" {
		t.Fatalf("the WebSocket handshake still needs the query token, got %q", got)
	}
}

// Browsers send `Connection: keep-alive, Upgrade`, so a whole-string comparison
// would reject a real handshake and break the upgrade log stream.
func TestConnectionHeaderIsParsedAsAList(t *testing.T) {
	for _, conn := range []string{"Upgrade", "upgrade", "keep-alive, Upgrade", "Upgrade, keep-alive"} {
		r := httptest.NewRequest(http.MethodGet, "/x?token=t", nil)
		r.Header.Set("Upgrade", "websocket")
		r.Header.Set("Connection", conn)
		if got := extractToken(r); got != "t" {
			t.Errorf("Connection: %q should be an upgrade, got %q", conn, got)
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
