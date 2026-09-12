package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bxnnyg/matrixctrl/internal/api/handlers"
	"github.com/bxnnyg/matrixctrl/internal/auth"
)

// anySession accepts every token and calls the holder by one name.
type anySession struct{ user string }

func (a anySession) Login(context.Context, string, string, string, string) (string, error) {
	return "", nil
}
func (a anySession) ValidateToken(string) (string, error)        { return a.user, nil }
func (a anySession) RevokeSession(context.Context, string) error { return nil }

// The middleware has its own tests. This one asks the question those cannot: is it
// actually installed?
//
// Six times in this series the right thing was built, named and reasoned about, and
// never wired — a linter nobody ran, a type that was `string`, a state nobody read, a
// rollback nothing called, an entry nobody checked, and a log read behind a condition
// that was never true. A guard that protects a homeserver is a poor place to make it
// seven.
func TestTheWriteGateIsActuallyInstalled(t *testing.T) {
	deps := Deps{
		Auth:   handlers.NewAuthHandler(anySession{user: "01MODERATOR"}, nil, nil, []byte("k")),
		Status: handlers.NewStatusHandler(nil, nil, "ess", "ess", http.NotFoundHandler()),
		// Somebody else is the admin, so this session may only look.
		Roles: auth.ParseRoles("01SOMEBODY-ELSE"),
	}
	r := NewRouter(deps)

	call := func(method, path string) int {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer whatever")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}

	// Reading still works — and the status endpoint answers without a cluster.
	if got := call(http.MethodGet, "/api/v1/status"); got == http.StatusForbidden {
		t.Error("GET /api/v1/status was refused; a read-only session reads")
	}

	// Writing does not. The route exists and would otherwise reach a handler.
	if got := call(http.MethodDelete, "/api/v1/status/evicted-pods"); got != http.StatusForbidden {
		t.Errorf("DELETE /api/v1/status/evicted-pods answered %d, want 403 — "+
			"the gate is not in the chain", got)
	}
}

// With nobody named, nothing changes — which is every installation that upgrades into
// this without touching a value.
func TestWithoutNamedAdminsNothingIsRefused(t *testing.T) {
	deps := Deps{
		Auth:   handlers.NewAuthHandler(anySession{user: "01ANYONE"}, nil, nil, []byte("k")),
		Status: handlers.NewStatusHandler(nil, nil, "ess", "ess", http.NotFoundHandler()),
		Roles:  auth.ParseRoles(""),
	}
	r := NewRouter(deps)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/status/evicted-pods", nil)
	req.Header.Set("Authorization", "Bearer whatever")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code == http.StatusForbidden {
		t.Error("a deployment that named nobody must keep behaving exactly as before")
	}
}
