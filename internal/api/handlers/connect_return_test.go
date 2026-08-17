package handlers

import "testing"

// The return path originates with the browser, so this is the open-redirect guard.
// "Starts with a slash" would not be one: `//evil.example.com` is a protocol-relative
// URL and browsers follow it off-site (etappe 52).
func TestConnectReturnToRefusesAnythingOffTheAllowlist(t *testing.T) {
	for _, in := range []string{
		"//evil.example.com",
		"https://evil.example.com",
		"/rooms/../../etc",
		"/reports/extra",
		"/",
		"",
		"rooms",
		"/Rooms",
		"javascript:alert(1)",
		"/rooms?x=1",
	} {
		if got := connectReturnTo(in); got != "/rooms" {
			t.Errorf("connectReturnTo(%q) = %q, want the default /rooms", in, got)
		}
	}
}

func TestConnectReturnToKeepsTheTwoRealScreens(t *testing.T) {
	for _, in := range []string{"/rooms", "/reports"} {
		if got := connectReturnTo(in); got != in {
			t.Errorf("connectReturnTo(%q) = %q, want it unchanged", in, got)
		}
	}
}
