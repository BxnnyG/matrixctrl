package synmetrics

import (
	"strings"
	"testing"
	"time"
)

// minutes builds n one-minute rows for the main process, the cores from f.
func minutes(n int, f func(i int) (float64, map[string]float64)) []Row {
	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	rows := make([]Row, n)
	for i := range rows {
		c, areas := f(i)
		rows[i] = Row{At: t0.Add(time.Duration(i) * time.Minute), Process: "ess-synapse-main-0", Worker: "main", Cores: c, Areas: areas, Memory: 3e8}
	}
	return rows
}

const day = 24 * 60

// The production server on 2026-10-08: well under one percent of a core. The answer
// has to be "no worker", with the number — not silence, and not a suggestion.
func TestASmallServerNeedsNoWorker(t *testing.T) {
	main := Aggregate(minutes(7*day, func(int) (float64, map[string]float64) {
		return 0.004, map[string]float64{"federation-in": 0.001}
	}))
	v := Judge(main, nil, nil, nil)
	if v.Level != "fine" || v.Worker != "" || !strings.Contains(v.Detail, "0 %") {
		t.Fatalf("verdict: %+v", v)
	}
}

// Busy at the peaks, sync carrying most of it: the synchrotron is the answer.
func TestSaturatedBySyncRecommendsTheSynchrotron(t *testing.T) {
	main := Aggregate(minutes(2*day, func(i int) (float64, map[string]float64) {
		return 0.9, map[string]float64{"sync": 0.4, "federation-in": 0.1}
	}))
	v := Judge(main, nil, nil, nil)
	if v.Level != "recommend" || v.Worker != "synchrotron" {
		t.Fatalf("verdict: %+v", v)
	}
	// Already switched on: not recommended twice; the next area is.
	v = Judge(main, nil, nil, map[string]bool{"synchrotron": true})
	if v.Worker == "synchrotron" {
		t.Fatalf("recommended a worker that is already on: %+v", v)
	}
}

// Saturated, but nothing a worker takes over carries a quarter. Recommending the
// largest small slice would be a guess dressed as advice.
func TestSaturatedButSpreadIsSaidSo(t *testing.T) {
	main := Aggregate(minutes(2*day, func(int) (float64, map[string]float64) {
		return 0.9, map[string]float64{"sync": 0.05, "media": 0.04}
	}))
	if v := Judge(main, nil, nil, nil); v.Level != "busy" || v.Worker != "" {
		t.Fatalf("verdict: %+v", v)
	}
}

// The percentile, not the average, decides: a server idle at night and saturated every
// evening averages to "fine".
func TestPeaksCountNotTheAverage(t *testing.T) {
	main := Aggregate(minutes(2*day, func(i int) (float64, map[string]float64) {
		if i%day >= 18*60 && i%day < 22*60 { // four busy hours a day
			return 0.95, map[string]float64{"sync": 0.5}
		}
		return 0.05, nil
	}))
	if main.Avg >= watchAt {
		t.Fatalf("test premise: average %v should look harmless", main.Avg)
	}
	if v := Judge(main, nil, nil, nil); v.Level != "recommend" || v.Worker != "synchrotron" {
		t.Fatalf("verdict: %+v", v)
	}
}

func TestAFederationBacklogRecommendsTheSender(t *testing.T) {
	rows := minutes(2*day, func(int) (float64, map[string]float64) { return 0.1, nil })
	for i := range rows {
		rows[i].FedPendingDest = 120
	}
	if v := Judge(Aggregate(rows), nil, nil, nil); v.Worker != "federation-sender" {
		t.Fatalf("verdict: %+v", v)
	}
}

func TestTooLittleDataSaysSoWithTheLifetimeAverage(t *testing.T) {
	main := Aggregate(minutes(20, func(int) (float64, map[string]float64) { return 0.95, nil }))
	v := Judge(main, &Rate{Cores: 0.03}, nil, nil)
	if v.Level != "measuring" || !strings.Contains(v.Detail, "3 %") {
		t.Fatalf("verdict: %+v", v)
	}
}

func TestAnIdleWorkerIsNamedWithItsMemory(t *testing.T) {
	main := Aggregate(minutes(2*day, func(int) (float64, map[string]float64) { return 0.05, nil }))
	idle := Aggregate(minutes(2*day, func(int) (float64, map[string]float64) { return 0.001, nil }))
	idle.Worker = "media-repository"
	v := Judge(main, nil, []Stats{idle}, map[string]bool{"media-repository": true})
	if len(v.Notes) != 1 || !strings.Contains(v.Notes[0], "media-repository") || !strings.Contains(v.Notes[0], "300 MB") {
		t.Fatalf("notes: %v", v.Notes)
	}
}
