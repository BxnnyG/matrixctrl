package rtc

import (
	"testing"
	"time"
)

func ev(rooms, seconds, quality, live, participants int) MediaEvidence {
	return MediaEvidence{
		RoomsCompleted: rooms, RoomSeconds: seconds, QualitySamples: quality,
		Live: LiveGauges{Rooms: live, Participants: participants},
	}
}

var t0 = time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)

// The first sample has nothing to compare against. Treating the counters as this
// interval's activity would invent history at the one moment nothing can check it.
func TestFirstSampleClaimsNothing(t *testing.T) {
	s := NewSample(t0, ev(7, 1200, 40, 1, 2), nil)
	if s.DRoomsCompleted != 0 || s.DRoomSeconds != 0 || s.DQualitySamples != 0 {
		t.Errorf("deltas = %d/%d/%d, want 0/0/0", s.DRoomsCompleted, s.DRoomSeconds, s.DQualitySamples)
	}
	if s.SFURestarted {
		t.Error("no previous sample is not a restart")
	}
	// The gauges are still reported: they describe now, not an interval.
	if s.RoomsLive != 1 || s.ParticipantsLive != 2 {
		t.Errorf("live = %d/%d, want 1/2", s.RoomsLive, s.ParticipantsLive)
	}
}

func TestNormalDelta(t *testing.T) {
	prev := Counters{RoomsCompleted: 7, RoomSeconds: 1200, QualitySamples: 40}
	s := NewSample(t0, ev(9, 1500, 55, 0, 0), &prev)
	if s.SFURestarted {
		t.Fatal("counters rose; this is not a restart")
	}
	if s.DRoomsCompleted != 2 || s.DRoomSeconds != 300 || s.DQualitySamples != 15 {
		t.Errorf("deltas = %d/%d/%d, want 2/300/15", s.DRoomsCompleted, s.DRoomSeconds, s.DQualitySamples)
	}
}

// The case this file exists for. The SFU pod is deleted on every ESS upgrade, so a
// negative delta is not an edge case here — it is a weekly event.
func TestRestartYieldsTheNewValueNotANegative(t *testing.T) {
	prev := Counters{RoomsCompleted: 9, RoomSeconds: 1500, QualitySamples: 55}
	s := NewSample(t0, ev(1, 90, 4, 0, 0), &prev)

	if !s.SFURestarted {
		t.Fatal("counters fell; that is a restart")
	}
	if s.DRoomsCompleted != 1 || s.DRoomSeconds != 90 || s.DQualitySamples != 4 {
		t.Errorf("deltas = %d/%d/%d, want 1/90/4 — the new process's own count",
			s.DRoomsCompleted, s.DRoomSeconds, s.DQualitySamples)
	}
}

// The normal shape of a restart on this deployment: nothing had happened before it
// and nothing has happened since, so rooms_completed reads 0 on both sides. Watching
// only that counter would miss the restart entirely.
func TestRestartDetectedWhenTheHeadlineCounterIsZeroBothSides(t *testing.T) {
	prev := Counters{RoomsCompleted: 0, RoomSeconds: 0, QualitySamples: 31}
	s := NewSample(t0, ev(0, 0, 0, 0, 0), &prev)
	if !s.SFURestarted {
		t.Error("quality_samples fell from 31 to 0 — the process restarted")
	}
}

// A restart between two samples must not lose the completed calls that the previous
// process had already contributed, nor double-count them.
func TestTotalsAreExactAcrossARestart(t *testing.T) {
	var prev *Counters
	var samples []Sample

	// Three reads of one process: 0 → 2 → 5 completed rooms.
	for i, m := range []MediaEvidence{ev(0, 0, 0, 0, 0), ev(2, 300, 10, 0, 0), ev(5, 900, 25, 1, 3)} {
		s := NewSample(t0.Add(time.Duration(i)*time.Minute), m, prev)
		samples = append(samples, s)
		c := countersOf(m)
		prev = &c
	}
	// Restart, then two more rooms in the new process.
	for i, m := range []MediaEvidence{ev(0, 0, 0, 0, 0), ev(2, 240, 8, 0, 0)} {
		s := NewSample(t0.Add(time.Duration(3+i)*time.Minute), m, prev)
		samples = append(samples, s)
		c := countersOf(m)
		prev = &c
	}

	got := Sum(samples)
	// 5 from the first process (0→2→5, first sample claims nothing) + 2 from the
	// second. 900 + 240 seconds.
	if got.Calls != 7 {
		t.Errorf("calls = %d, want 7", got.Calls)
	}
	if got.Seconds != 1140 {
		t.Errorf("seconds = %d, want 1140", got.Seconds)
	}
	if got.Restarts != 1 {
		t.Errorf("restarts = %d, want 1", got.Restarts)
	}
	if got.Samples != 5 {
		t.Errorf("samples = %d, want 5", got.Samples)
	}
	if !got.Since.Equal(t0) {
		t.Errorf("since = %v, want %v", got.Since, t0)
	}
}

func TestSumOfNothing(t *testing.T) {
	got := Sum(nil)
	if got.Calls != 0 || got.Samples != 0 || !got.Since.IsZero() {
		t.Errorf("empty sum = %+v", got)
	}
}
