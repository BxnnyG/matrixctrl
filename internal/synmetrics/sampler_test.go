package synmetrics

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type captured struct {
	mu    sync.Mutex
	rates []Rate
}

func (c *captured) Record(_ context.Context, _, _ string, r Rate) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rates = append(c.rates, r)
	return nil
}

// A process whose counters the test moves: CPU seconds and start time.
type fakeSynapse struct {
	mu         sync.Mutex
	cpu, start float64
}

func (f *fakeSynapse) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fmt.Fprintf(w, "# TYPE process_cpu_seconds_total counter\nprocess_cpu_seconds_total %v\n", f.cpu)
	fmt.Fprintf(w, "# TYPE process_start_time_seconds gauge\nprocess_start_time_seconds %v\n", f.start)
	fmt.Fprint(w, "# TYPE process_resident_memory_bytes gauge\nprocess_resident_memory_bytes 2e8\n")
}

func TestTheSamplerRecordsRatesAndSkipsRestarts(t *testing.T) {
	fake := &fakeSynapse{cpu: 100, start: 1000}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	rec := &captured{}
	present := true
	s := newSampler(rec, func(context.Context) ([]Target, error) {
		if !present {
			return nil, nil
		}
		return []Target{{Process: "ess-synapse-main-0", Worker: "main", URL: srv.URL}}, nil
	})
	now := time.Unix(2000, 0)
	s.now = func() time.Time { return now }

	s.Once(context.Background()) // first reading: nothing to pair with
	if len(rec.rates) != 0 {
		t.Fatalf("recorded without a previous reading: %+v", rec.rates)
	}
	fake.cpu, now = 130, now.Add(time.Minute)
	s.Once(context.Background())
	if len(rec.rates) != 1 || math.Abs(rec.rates[0].Cores-0.5) > 1e-9 {
		t.Fatalf("rates: %+v", rec.rates)
	}

	// Restarted: new start time, counters from zero. No rate for this minute.
	fake.cpu, fake.start, now = 2, 2050, now.Add(time.Minute)
	s.Once(context.Background())
	if len(rec.rates) != 1 {
		t.Fatalf("a restart produced a rate: %+v", rec.rates[len(rec.rates)-1])
	}
	if l := s.Latest()["ess-synapse-main-0"]; l.Worker != "main" || l.StartTime != 2050 {
		t.Errorf("latest: %+v", l)
	}

	// The pod is gone: so is its reading.
	present = false
	s.Once(context.Background())
	if len(s.Latest()) != 0 {
		t.Errorf("a vanished process is still reported: %v", s.Latest())
	}
}

// A sampler that cannot reach Synapse must say so; otherwise it is indistinguishable
// from one that has not been running long.
func TestAnUnreadableProcessIsReported(t *testing.T) {
	s := newSampler(&captured{}, func(context.Context) ([]Target, error) {
		return []Target{{Process: "ess-synapse-main-0", Worker: "main", URL: "http://127.0.0.1:1/_synapse/metrics"}}, nil
	})
	s.Once(context.Background())
	if e := s.Errors()["ess-synapse-main-0"]; e == "" {
		t.Fatal("no error reported for a process that cannot be read")
	}
}
