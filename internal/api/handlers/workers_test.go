package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bxnnyg/matrixctrl/internal/k8s"
	"github.com/bxnnyg/matrixctrl/internal/synmetrics"
)

// A chart point is a bucket's average and its busiest minute. The busiest minute is the
// half that matters: an hour averaging 20 % can hide ten minutes at 100 %.
func TestHistoryKeepsTheBusiestMinute(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	var rows []synmetrics.Row
	for i := 0; i < 10; i++ {
		c := 0.1
		if i == 3 {
			c = 0.9
		}
		rows = append(rows, synmetrics.Row{At: t0.Add(time.Duration(i) * time.Minute), Process: "p", Cores: c})
	}
	h := history(map[string][]synmetrics.Row{"p": rows}, t0.Add(-time.Hour), 24*time.Hour)
	pts := h["p"]
	if len(pts) != 2 {
		t.Fatalf("10 minutes in 5-minute buckets = 2 points, got %d", len(pts))
	}
	if pts[0][2] != 0.9 || pts[0][1] >= 0.5 {
		t.Errorf("first bucket = %v, want max 0.9 with a low average", pts[0])
	}
}

// Before the first recorded minute the page still has something true to say: the
// lifetime average from the latest reading.
func TestBeforeAnyHistoryTheLifetimeIsShown(t *testing.T) {
	now := time.Now()
	h := NewWorkersHandler(nil,
		func() map[string]synmetrics.Latest {
			return map[string]synmetrics.Latest{"ess-synapse-main-0": {Worker: "main", Snapshot: synmetrics.Snapshot{
				At: now, StartTime: float64(now.Add(-time.Hour).Unix()), CPU: 36, Memory: 3e8,
				Servlets: map[string]float64{"FederationSendServlet": 18}, Background: map[string]float64{}}}}
		},
		func(context.Context) ([]k8s.SynapseProcess, error) {
			return []k8s.SynapseProcess{{Pod: "ess-synapse-main-0", Worker: "main", Ready: true}}, nil
		}, nil)
	rec := httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest(http.MethodGet, "/api/v1/workers", nil))
	var body struct {
		Verdict     synmetrics.Verdict `json:"verdict"`
		AreasSource string             `json:"areas_source"`
		MainCores   float64            `json:"main_cores"`
		Areas       []struct {
			ID    string  `json:"id"`
			Cores float64 `json:"cores"`
		} `json:"areas"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err, rec.Body.String())
	}
	if body.Verdict.Level != "measuring" || body.AreasSource != "lifetime" {
		t.Fatalf("verdict %+v, source %q", body.Verdict, body.AreasSource)
	}
	if body.MainCores < 0.009 || body.MainCores > 0.011 || len(body.Areas) != 1 || body.Areas[0].ID != "federation-in" {
		t.Errorf("main %v, areas %+v", body.MainCores, body.Areas)
	}
}
