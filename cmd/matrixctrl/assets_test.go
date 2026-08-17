package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// The committed frontend was found sixteen days stale, so a bare `go build` served a
// UI with no moderation screen and said nothing (etappe 50). The assets are no longer
// committed, which turns that silent staleness into an obvious absence — but only if
// the absence is actually detected and reported.
func TestFrontendBuiltDistinguishesPlaceholderFromRealBuild(t *testing.T) {
	// What a clean checkout has: the .gitkeep that keeps //go:embed compiling.
	placeholder := fstest.MapFS{"dist/.gitkeep": {Data: []byte{}}}
	if frontendBuilt(placeholder) {
		t.Error("a dist holding only .gitkeep must not count as a built frontend")
	}

	built := fstest.MapFS{
		"dist/.gitkeep":                {Data: []byte{}},
		"dist/index.html":              {Data: []byte("<!doctype html>")},
		"dist/assets/index-abc1234.js": {Data: []byte("console.log(1)")},
	}
	if !frontendBuilt(built) {
		t.Error("a dist with index.html is a built frontend")
	}
}

// A missing UI answering a bare 404 reads as a routing fault and sends the reader to
// the wrong file. It has to name the actual cause.
func TestStaticHandlerExplainsAMissingFrontend(t *testing.T) {
	h := staticHandler(fstest.MapFS{"dist/.gitkeep": {Data: []byte{}}})

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 — a 404 here is indistinguishable from a routing bug", w.Code)
	}
	body := w.Body.String()
	// The fix must be in the page. Someone hitting this is mid-build, not mid-debug.
	if !strings.Contains(body, "make build") {
		t.Errorf("the page must name the command that fixes it; got: %.200s", body)
	}
}

// The real build must still be served normally — including the SPA fallback, which is
// how every deep link loads.
func TestStaticHandlerServesARealBuild(t *testing.T) {
	h := staticHandler(fstest.MapFS{
		"dist/index.html":              {Data: []byte("<!doctype html>app")},
		"dist/assets/index-abc1234.js": {Data: []byte("console.log(1)")},
	})

	for _, path := range []string{"/", "/assets/index-abc1234.js", "/reports"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, w.Code)
		}
	}
}
