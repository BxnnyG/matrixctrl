package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bxnnyg/matrixctrl/internal/api/handlers"
)

// The SPA fallback used to catch unmatched API paths too, so `GET
// /api/v1/definitely-not-a-route` answered `200 text/html` — indistinguishable from a
// working endpoint until JSON.parse failed on the HTML (etappe 48, P2-31).
//
// The two halves are tested together on purpose: making the API return 404 is easy,
// and quietly breaking the SPA's deep links while doing it is easier. Every frontend
// route is served by index.html, so a NotFound that stops doing that logs the operator
// out of a page reload.
func TestAPIPathsGet404WhileSPAStillServes(t *testing.T) {
	const spaBody = "<!doctype html><title>spa</title>"
	spa := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(spaBody))
	})
	r := NewRouter(Deps{Status: handlers.NewStatusHandler(nil, nil, "ess", "ess", spa)})

	cases := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantSPA    bool
	}{
		{"unknown api path", "GET", "/api/v1/definitely-not-a-route", http.StatusNotFound, false},
		{"unknown api path with method", "PUT", "/api/v1/media/x/y/nonesuch", http.StatusNotFound, false},
		{"unknown nested api path", "GET", "/api/v1/reports/users/nope/extra", http.StatusNotFound, false},
		// Frontend routes must still load. These are real screens.
		{"spa root", "GET", "/", http.StatusOK, true},
		{"spa deep link", "GET", "/reports", http.StatusOK, true},
		{"spa deep link with id", "GET", "/rooms/!abc:example.org", http.StatusOK, true},
		// A path that merely starts with "api" is not an API path; only "/api/" is.
		{"not actually an api path", "GET", "/apidocs", http.StatusOK, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(c.method, c.path, nil))

			if w.Code != c.wantStatus {
				t.Errorf("status = %d, want %d (body: %s)", w.Code, c.wantStatus, w.Body.String())
			}
			if got := w.Body.String() == spaBody; got != c.wantSPA {
				t.Errorf("served SPA = %v, want %v (body: %.60s)", got, c.wantSPA, w.Body.String())
			}
			if !c.wantSPA {
				// The frontend branches on the JSON error shape; HTML here is the
				// original bug.
				var body map[string]any
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					t.Errorf("body is not JSON: %v (%s)", err, w.Body.String())
				}
			}
		})
	}
}

// A registered path with the wrong verb is a different mistake from an unknown path,
// and reporting it as "no such endpoint" sends the reader looking for a typo that is
// not there.
func TestWrongMethodOnKnownAPIPathIs405(t *testing.T) {
	spa := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("spa"))
	})
	r := NewRouter(Deps{Status: handlers.NewStatusHandler(nil, nil, "ess", "ess", spa)})

	w := httptest.NewRecorder()
	// /api/v1/auth/oidc/available is registered as GET and needs no auth.
	r.ServeHTTP(w, httptest.NewRequest("DELETE", "/api/v1/auth/oidc/available", nil))

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405 (body: %s)", w.Code, w.Body.String())
	}
	if w.Body.String() == "spa" {
		t.Error("a wrong verb on an API path served the SPA")
	}
}
