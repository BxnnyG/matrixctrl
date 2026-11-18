package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/bxnnyg/matrixctrl/internal/mas"
)

func masStub(t *testing.T, status int, body string) *mas.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/token") {
			_, _ = w.Write([]byte(`{"access_token":"t","expires_in":300}`))
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return mas.New(srv.URL, srv.URL+"/oauth2/token", "id", "secret")
}

const oneToken = `{"data":{"id":"T1","attributes":{"token":"abc","valid":true,"usage_limit":2,"times_used":1,"created_at":"2026-01-02T03:04:05Z","expires_at":null,"revoked_at":null}}}`

func TestInvitesWithoutMASAre503WithGermanText(t *testing.T) {
	h := NewInvitesHandler(func() *mas.Client { return nil })
	for name, fn := range map[string]http.HandlerFunc{"list": h.List, "create": h.Create, "update": h.Update, "revoke": h.Revoke(false)} {
		rec := httptest.NewRecorder()
		fn(rec, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader("{}")))
		if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "MAS") {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
}

func TestInviteResponseCarriesTheFrontendFields(t *testing.T) {
	h := NewInvitesHandler(func() *mas.Client { return masStub(t, 200, oneToken) })
	rec := httptest.NewRecorder()
	h.Create(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"usage_limit":2,"expires_at":"2026-12-01T00:00:00Z"}`)))
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	for _, k := range []string{"id", "token", "valid", "state", "usage_limit", "times_used", "created_at", "last_used_at", "expires_at", "revoked_at"} {
		if _, ok := got[k]; !ok {
			t.Errorf("field %q missing: %v", k, got)
		}
	}
	if got["token"] != "abc" || got["state"] != "valid" {
		t.Errorf("%v", got)
	}

	bad := httptest.NewRecorder()
	h.Create(bad, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"expires_at":"morgen"}`)))
	if bad.Code != http.StatusBadRequest {
		t.Errorf("bad date: %d", bad.Code)
	}
}

func TestInviteMASErrorsMapToStatuses(t *testing.T) {
	for mas1, want := range map[int]int{404: 404, 409: 409, 500: 502} {
		h := NewInvitesHandler(func() *mas.Client { return masStub(t, mas1, "") })
		r := chi.NewRouter()
		r.Post("/{id}/revoke", h.Revoke(false))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/T1/revoke", nil))
		if rec.Code != want {
			t.Errorf("MAS %d: got %d, want %d", mas1, rec.Code, want)
		}
	}
}
