package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The dashboard's status response must never answer `null` where it promises a list.
//
// It did. On a fresh install ComponentHealth fails (there is no ESS namespace yet),
// the error was discarded, the nil slice marshalled as `null`, and the dashboard ran
// `data.components.filter(...)` on it. React unmounted the route and the operator got
// TanStack Router's default screen: "Something went wrong!" — no cause, no way on.
// They logged into a brand-new server and had to type /setup to reach anything.
//
// A zero-value handler is exactly the fresh-install shape: no cluster client, no Helm
// client, nothing deployed, nothing reachable. The response still has to be a valid
// dashboard payload.
func TestStatusResponseNeverSaysNullForAList(t *testing.T) {
	h := &StatusHandler{}
	rec := httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest(http.MethodGet, "/api/v1/status", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200 — an empty cluster is a state, not a failure", rec.Code)
	}
	body := rec.Body.String()
	for _, field := range []string{`"components":null`, `"nodes":null`} {
		if strings.Contains(body, field) {
			t.Errorf("response contains %s; a list must serialise as [] so the client can iterate it", field)
		}
	}

	// Decoded, not just grepped: the client's own reading is what matters.
	var got struct {
		Release    *struct{}                 `json:"release"`
		Components *[]map[string]interface{} `json:"components"`
		Nodes      *[]map[string]interface{} `json:"nodes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v — body was %s", err, body)
	}
	if got.Components == nil {
		t.Error("components decoded as null; the dashboard calls .filter() on it")
	}
	if got.Nodes == nil {
		t.Error("nodes decoded as null")
	}
	// A missing release is the honest answer on a fresh install, and the dashboard
	// reads it as "nothing deployed yet" — that one may stay null.
	if got.Release != nil {
		t.Error("release should be null when there is no ESS")
	}
}
