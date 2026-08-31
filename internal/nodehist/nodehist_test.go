package nodehist

import (
	"testing"
	"time"
)

func s(node string, mins int, cpuAl, memAl int64) Sample {
	return Sample{
		At:   time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC).Add(time.Duration(mins) * time.Minute),
		Node: node, CPU: 700, CPUAl: cpuAl, Mem: 3000, MemAl: memAl,
	}
}

// The outage, in miniature: 32 cores become 6 and nothing said so (§4.53). This is the
// one thing recording `allocatable` is for.
func TestDetectsTheNodeShrinking(t *testing.T) {
	got := DetectCapacityChanges([]Sample{
		s("n1", 0, 32000, 36086),
		s("n1", 1, 32000, 36086),
		s("n1", 2, 6000, 36086),
		s("n1", 3, 6000, 36086),
	})
	if len(got) != 1 {
		t.Fatalf("want one change, got %+v", got)
	}
	if got[0].FromCPUMillis != 32000 || got[0].ToCPUMillis != 6000 {
		t.Errorf("got %dm → %dm, want 32000 → 6000", got[0].FromCPUMillis, got[0].ToCPUMillis)
	}
	// Reported against the newest sample, so it is still visible hours later rather
	// than only during the single interval it happened in.
	if !got[0].At.Equal(s("n1", 3, 0, 0).At) {
		t.Errorf("At = %v, want the newest sample's time", got[0].At)
	}
}

// Usage moves constantly; that is not a capacity change and must stay silent.
func TestUsageChangesAreNotCapacityChanges(t *testing.T) {
	a, b := s("n1", 0, 6000, 36086), s("n1", 1, 6000, 36086)
	a.CPU, b.CPU = 500, 5800
	a.Mem, b.Mem = 1000, 30000
	if got := DetectCapacityChanges([]Sample{a, b}); len(got) != 0 {
		t.Errorf("usage swings must not be reported: %+v", got)
	}
}

func TestMemoryChangeAlsoCounts(t *testing.T) {
	got := DetectCapacityChanges([]Sample{s("n1", 0, 6000, 36086), s("n1", 1, 6000, 16384)})
	if len(got) != 1 || got[0].FromMemMi != 36086 || got[0].ToMemMi != 16384 {
		t.Errorf("want a memory change, got %+v", got)
	}
}

// A shrinking node next to a steady one must not be averaged away.
func TestPerNodeNotAggregated(t *testing.T) {
	got := DetectCapacityChanges([]Sample{
		s("n1", 0, 32000, 36086), s("n2", 0, 8000, 16384),
		s("n1", 1, 6000, 36086), s("n2", 1, 8000, 16384),
	})
	if len(got) != 1 || got[0].Node != "n1" {
		t.Errorf("only n1 changed: %+v", got)
	}
}

func TestNothingToCompare(t *testing.T) {
	if got := DetectCapacityChanges(nil); len(got) != 0 {
		t.Errorf("no samples, no claims: %+v", got)
	}
	if got := DetectCapacityChanges([]Sample{s("n1", 0, 6000, 36086)}); len(got) != 0 {
		t.Errorf("a single sample cannot show a change: %+v", got)
	}
}
