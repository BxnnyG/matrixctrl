package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authmw "github.com/bxnnyg/matrixctrl/internal/api/middleware"
	"github.com/bxnnyg/matrixctrl/internal/auth"
)

func asUser(r *http.Request, userID string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), authmw.UserIDKey, userID))
}

// Rooms and moderation act with a Matrix account. An emergency-login session is not
// one: the authorization succeeded, filed the token under the Matrix account, and the
// session kept asking under its own name — a connect button that went round a loop on
// the new server with nothing on screen to say why (etappe 116d).
func TestTheEmergencyLoginIsToldToSignInWithMatrix(t *testing.T) {
	started := 0
	h := NewRoomsHandler(nil,
		func(string) bool { return true }, // whatever is stored, it is not this session's
		func(context.Context, string) (string, error) { started++; return "https://mas.example/authorize", nil },
	)

	rec := httptest.NewRecorder()
	h.State(rec, asUser(httptest.NewRequest(http.MethodGet, "/api/v1/rooms/state", nil), auth.BootstrapUserID))
	var st roomsState
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.Connected || st.Session != "bootstrap" || !strings.Contains(st.Reason, "über Matrix anmelden") {
		t.Fatalf("state for the emergency login: %+v", st)
	}

	rec = httptest.NewRecorder()
	h.Connect(rec, asUser(httptest.NewRequest(http.MethodPost, "/api/v1/rooms/connect", strings.NewReader(`{}`)), auth.BootstrapUserID))
	if rec.Code != http.StatusConflict || started != 0 {
		t.Fatalf("connect for the emergency login: status %d, authorizations started %d — want 409 and none", rec.Code, started)
	}

	// A Matrix session is unaffected.
	rec = httptest.NewRecorder()
	h.Connect(rec, asUser(httptest.NewRequest(http.MethodPost, "/api/v1/rooms/connect", strings.NewReader(`{}`)), "@op:example.com"))
	if rec.Code != http.StatusOK || started != 1 {
		t.Fatalf("connect for a Matrix session: status %d, started %d", rec.Code, started)
	}
}
