package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Knowing which version is running must not depend on reaching a registry.
//
// With the check turned off — or unreachable — the endpoint still has to answer the
// question it exists for. The dashboard learned this the hard way when an optional
// lookup took the whole page down (§4.78).
func TestVersionIsReportedWithoutAnUpdateCheck(t *testing.T) {
	h := NewVersionHandler("0.1.74", "abc1234", nil, nil)
	rec := httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest(http.MethodGet, "/api/v1/version", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	var got struct {
		Version  string          `json:"version"`
		Commit   string          `json:"commit"`
		Update   json.RawMessage `json:"update"`
		MayWrite bool            `json:"may_write"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v — body was %s", err, rec.Body.String())
	}
	if got.Version != "0.1.74" || got.Commit != "abc1234" {
		t.Errorf("got %s (%s), want 0.1.74 (abc1234)", got.Version, got.Commit)
	}
	// Absent, not false: "not checked" and "up to date" are different answers and the
	// UI must be able to tell them apart.
	// No roles configured is the state every installation was in before they existed,
	// and it must not read as "you may only look".
	if !got.MayWrite {
		t.Error("may_write is false with no role restriction configured")
	}
	if got.Update != nil {
		t.Errorf("update = %s; with the check disabled the field must be absent", got.Update)
	}
}
