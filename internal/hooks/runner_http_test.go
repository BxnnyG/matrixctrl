package hooks

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Declared, offered in the hook editor with a full form, and unimplemented — saving
// worked and the hook failed the first time it ran, during an upgrade (etappe 62).
func TestHTTPActionPerformsTheRequest(t *testing.T) {
	var gotMethod, gotBody, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	r := &Runner{}
	err := r.runHTTP(context.Background(), HookAction{
		Type: ActionHTTPRequest, URL: srv.URL, Method: "post", Body: `{"ok":true}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != "POST" {
		t.Errorf("method = %q, want POST — a lowercase method from the form must still work", gotMethod)
	}
	if gotBody != `{"ok":true}` {
		t.Errorf("body = %q", gotBody)
	}
	if gotCT != "application/json" {
		t.Errorf("content-type = %q", gotCT)
	}
	// 204 is a 2xx and must count as success.
}

// A notification that silently 404s is not a notification.
func TestHTTPActionFailsOnNon2xx(t *testing.T) {
	for _, code := range []int{400, 404, 500} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(code)
		}))
		err := (&Runner{}).runHTTP(context.Background(), HookAction{URL: srv.URL})
		srv.Close()
		if err == nil {
			t.Errorf("HTTP %d must fail the hook", code)
			continue
		}
		// The status is the one thing that says what to fix.
		if !strings.Contains(err.Error(), http.StatusText(code)) && !strings.Contains(err.Error(), itoa(code)) {
			t.Errorf("error must name the status: %v", err)
		}
	}
}

// POST is the default because a hook action is a notification, not a fetch.
func TestHTTPActionDefaultsToPost(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = r.Method }))
	defer srv.Close()
	if err := (&Runner{}).runHTTP(context.Background(), HookAction{URL: srv.URL}); err != nil {
		t.Fatal(err)
	}
	if got != "POST" {
		t.Errorf("method = %q, want POST", got)
	}
}

// An empty GET must not carry a Content-Type it has no content for.
func TestHTTPActionOmitsContentTypeWithoutBody(t *testing.T) {
	var ct string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct = r.Header.Get("Content-Type")
	}))
	defer srv.Close()
	if err := (&Runner{}).runHTTP(context.Background(), HookAction{URL: srv.URL, Method: "GET"}); err != nil {
		t.Fatal(err)
	}
	if ct != "" {
		t.Errorf("content-type = %q, want none", ct)
	}
}

func TestHTTPActionRefusesWithoutURL(t *testing.T) {
	if err := (&Runner{}).runHTTP(context.Background(), HookAction{}); err == nil {
		t.Error("an action with no URL must fail rather than request nothing quietly")
	}
}

// An unreachable host must fail rather than hang: hooks run inside an upgrade's hook
// phase, which an operator is watching.
func TestHTTPActionFailsOnUnreachableHost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing is listening now
	if err := (&Runner{}).runHTTP(context.Background(), HookAction{URL: url}); err == nil {
		t.Error("a dead endpoint must fail the hook")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
