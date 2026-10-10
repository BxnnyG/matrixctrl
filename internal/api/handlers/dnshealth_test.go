package handlers

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The login page used to show this verbatim. The operator cannot act on "10.43.0.10:53";
// they can act on "the server cannot resolve names" — and must not be told their
// account is the problem (etappe 119a).
func TestALoginBrokenByDNSIsSaidInWords(t *testing.T) {
	err := fmt.Errorf("token exchange: Post \"https://mas.example.com/oauth2/token\": dial tcp: %w",
		&net.DNSError{Err: "server misbehaving", Name: "mas.example.com", Server: "10.43.0.10:53", IsTemporary: true})
	got := loginError(err)
	if !strings.Contains(got, "keine Namen auflösen") || strings.Contains(got, "10.43.0.10") {
		t.Fatalf("got %q", got)
	}
	// Anything else keeps its own message.
	if got := loginError(errors.New("user is not in the MatrixCtrl allowlist")); !strings.Contains(got, "allowlist") {
		t.Fatalf("an unrelated error was rewritten: %q", got)
	}
}

// Public endpoint: one word when resolution is down, nothing at all otherwise.
func TestAvailabilityCarriesTheDNSWordOnlyWhenFailing(t *testing.T) {
	h := NewAuthHandler(nil, nil, nil, []byte("0123456789abcdef0123456789abcdef"))
	failing := false
	h.SetDNSFailing(func() bool { return failing })
	body := func() string {
		rec := httptest.NewRecorder()
		h.OIDCAvailable(rec, httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/available", nil))
		return rec.Body.String()
	}
	if strings.Contains(body(), "dns") {
		t.Fatalf("healthy resolution still mentioned: %s", body())
	}
	failing = true
	if b := body(); !strings.Contains(b, `"dns":"failing"`) {
		t.Fatalf("failing resolution not said: %s", b)
	}
}

// The sidebar showed a ULID where the operator expected their name: MAS gives no Matrix
// ID in userinfo, so the session falls back to the `sub` (etappe 119c).
func TestASessionIDIsShownAsAName(t *testing.T) {
	h := NewAuthHandler(nil, nil, nil, []byte("0123456789abcdef0123456789abcdef"))
	calls := 0
	h.lookupName = func(_ context.Context, id string) (string, error) {
		calls++
		if id == "01KFY1JRA76V3JFYY0000000AB" {
			return "alice", nil
		}
		return "", errors.New("unknown")
	}
	if got := h.displayName(context.Background(), "01KFY1JRA76V3JFYY0000000AB"); got != "alice" {
		t.Fatalf("name = %q", got)
	}
	h.displayName(context.Background(), "01KFY1JRA76V3JFYY0000000AB")
	if calls != 1 {
		t.Errorf("MAS asked %d times for the same ID; the name is kept", calls)
	}
	// A Matrix ID and the emergency login are names already — MAS is not asked.
	for _, id := range []string{"@bob:example.com", "admin"} {
		if got := h.displayName(context.Background(), id); got != "" {
			t.Errorf("displayName(%q) = %q", id, got)
		}
	}
	if calls != 1 {
		t.Errorf("MAS was asked about a Matrix ID or the emergency login")
	}
}
