package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/bxnnyg/matrixctrl/internal/synapse"
)

func ms(v int64) *int64 { return &v }

// The page exists to answer "which ones are failing". Failing first, the longest-failing
// at the top; the rest by name. The next retry is computed here so no screen has to
// know it is last attempt plus interval.
func TestFailingDestinationsComeFirst(t *testing.T) {
	views, failing := destinationViews([]synapse.Destination{
		{Destination: "b.example"},
		{Destination: "recent.example", FailureTS: ms(2000), RetryLastTS: ms(5000), RetryInterval: 600},
		{Destination: "a.example"},
		{Destination: "old.example", FailureTS: ms(1000)},
	})
	got := []string{}
	for _, v := range views {
		got = append(got, v.Destination.Destination)
	}
	want := []string{"old.example", "recent.example", "a.example", "b.example"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
	if failing != 2 {
		t.Errorf("failing = %d", failing)
	}
	if views[1].NextRetryTS != 5600 {
		t.Errorf("next retry = %d, want 5600", views[1].NextRetryTS)
	}
	if views[2].Failing || views[2].NextRetryTS != 0 {
		t.Errorf("a healthy destination reads as failing: %+v", views[2])
	}
}

func TestADestinationParamIsAServerName(t *testing.T) {
	for in, ok := range map[string]bool{
		"matrix.org":             true,
		"example.com:8448":       true,
		"evil%2F..%2Fv2%2Fusers": false,
		"":                       false,
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("destination", in)
		r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
		if _, got := destinationParam(r); got != ok {
			t.Errorf("destinationParam(%q) ok = %v, want %v", in, got, ok)
		}
	}
}
