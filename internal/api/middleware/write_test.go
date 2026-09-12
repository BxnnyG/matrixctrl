package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func gated(mayWrite bool, userID string) http.Handler {
	h := RequireWrite(func(string) bool { return mayWrite })(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) }),
	)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), UserIDKey, userID)))
	})
}

func status(t *testing.T, h http.Handler, method, path string) int {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec.Code
}

// Reading is never refused, whatever the role.
func TestReadingIsAlwaysAllowed(t *testing.T) {
	h := gated(false, "01MODERATOR")
	for _, path := range []string{"/api/v1/status", "/api/v1/config/slices", "/api/v1/users"} {
		if got := status(t, h, http.MethodGet, path); got != http.StatusTeapot {
			t.Errorf("GET %s: %d — a read-only session still reads", path, got)
		}
	}
}

// The things that would cost the installation.
func TestWritesAreRefusedWithoutTheRight(t *testing.T) {
	h := gated(false, "01MODERATOR")
	cases := []struct{ method, path string }{
		{http.MethodPost, "/api/v1/status/restore"},
		{http.MethodPost, "/api/v1/setup/deploy-ess"},
		{http.MethodPost, "/api/v1/helm/releases/ess/upgrade"},
		{http.MethodPut, "/api/v1/config/slices/synapse.yaml"},
		{http.MethodDelete, "/api/v1/hooks/3"},
		{http.MethodPost, "/api/v1/config/rename"},
	}
	for _, c := range cases {
		if got := status(t, h, c.method, c.path); got != http.StatusForbidden {
			t.Errorf("%s %s: %d, want 403", c.method, c.path, got)
		}
	}
}

// Refusing these would trap somebody in a panel they may not use, or stop them reading.
func TestTheHarmlessNonGetsStayOpen(t *testing.T) {
	h := gated(false, "01MODERATOR")
	for path := range readOnlySafe {
		if got := status(t, h, http.MethodPost, path); got != http.StatusTeapot {
			t.Errorf("POST %s: %d — this one changes nothing here", path, got)
		}
	}
}

// It leaves the cluster and discloses the public address. Reading does not do that.
func TestTheOutsideCheckIsNotHarmless(t *testing.T) {
	h := gated(false, "01MODERATOR")
	if got := status(t, h, http.MethodPost, "/api/v1/rtc/reachability"); got != http.StatusForbidden {
		t.Errorf("got %d, want 403 — it contacts two third parties with this server's address", got)
	}
}

func TestAnAdminIsRefusedNothing(t *testing.T) {
	h := gated(true, "01ADMIN")
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/status/restore"},
		{http.MethodDelete, "/api/v1/status/evicted-pods"},
	} {
		if got := status(t, h, c.method, c.path); got != http.StatusTeapot {
			t.Errorf("%s %s: %d", c.method, c.path, got)
		}
	}
}
