package synmetrics

import (
	"context"
	"os"
	"testing"
	"time"
)

// RUN_LIVE=1 SYNAPSE_METRICS_URL=http://localhost:19001/_synapse/metrics
func TestLiveTwoReadings(t *testing.T) {
	url := os.Getenv("SYNAPSE_METRICS_URL")
	if os.Getenv("RUN_LIVE") != "1" || url == "" {
		t.Skip("set RUN_LIVE=1 and SYNAPSE_METRICS_URL")
	}
	s := newSampler(nil, nil)
	read := func() Snapshot {
		snap, err := s.read(context.Background(), url)
		if err != nil {
			t.Fatal(err)
		}
		return snap
	}
	a := read()
	time.Sleep(30 * time.Second)
	s.now = time.Now
	b := read()
	r, ok := Between(a, b)
	if !ok {
		t.Fatal("no rate between two readings 30 s apart")
	}
	lt, _ := Lifetime(b)
	t.Logf("30 s: %.2f %% of a core, %.0f MB; areas %v", r.Cores*100, r.Memory/1e6, r.Areas)
	t.Logf("lifetime: %.3f %% of a core over %.1f days; areas %v", lt.Cores*100, lt.Seconds/86400, lt.Areas)
}
