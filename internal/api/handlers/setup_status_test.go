package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bxnnyg/matrixctrl/internal/auth"
)

// Connecting the Matrix login switches over at runtime. Setup has to see that switch
// on its next request, not on the next start of the process: it used to remember the
// answer from startup, kept offering the connect card after a successful connect, and
// that card's button then answered "already registered" to nobody (etappe 116b).
func TestSetupSeesALoginConnectedAtRuntime(t *testing.T) {
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"token_endpoint":"http://mas.invalid/oauth2/token"}`))
	}))
	defer issuer.Close()

	authH := NewAuthHandler(nil, nil, nil, []byte("0123456789abcdef0123456789abcdef"))
	setup := NewSetupHandler(nil, nil, "ess", "ess", authH.OIDCConfigured)

	status := func() map[string]any {
		rec := httptest.NewRecorder()
		setup.Status(rec, httptest.NewRequest(http.MethodGet, "/api/v1/setup/status", nil))
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("status body: %v", err)
		}
		return body
	}

	if got := status(); got["oidc_configured"] != false || got["bootstrap_active"] != true {
		t.Fatalf("before connecting: %v", got)
	}

	svc, err := auth.NewOIDCService(auth.OIDCConfig{Issuer: issuer.URL, ClientID: "c", ClientSecret: "s"}, nil, nil)
	if err != nil {
		t.Fatalf("oidc service: %v", err)
	}
	authH.InstallOIDC(svc)

	if got := status(); got["oidc_configured"] != true || got["bootstrap_active"] != false {
		t.Fatalf("after connecting, Setup still reports the startup state: %v", got)
	}
}
