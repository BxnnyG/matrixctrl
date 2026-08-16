package rtc

import "time"

// Turning a series of process-lifetime counters into a history that survives the
// process (etappe 44).
//
// The whole difficulty is one case: the SFU restarts, its counters go back to zero,
// and a naive subtraction produces a large negative number that would corrupt every
// total computed from it. That is not a rare edge — the post-upgrade hook deletes
// the SFU pod on every ESS upgrade, so it happens several times a week on this
// instance, and it is the reason this table exists at all.

// Sample is one observation of the SFU, with the deltas already resolved.
type Sample struct {
	ObservedAt time.Time `json:"observed_at"`

	RoomsLive        int `json:"rooms_live"`
	ParticipantsLive int `json:"participants_live"`

	// Raw counter values as read.
	RoomsCompleted int `json:"rooms_completed"`
	RoomSeconds    int `json:"room_seconds"`
	QualitySamples int `json:"quality_samples"`

	// Deltas since the previous sample, with a reset resolved.
	DRoomsCompleted int `json:"d_rooms_completed"`
	DRoomSeconds    int `json:"d_room_seconds"`
	DQualitySamples int `json:"d_quality_samples"`

	SFURestarted bool `json:"sfu_restarted"`
}

// Counters is the subset of a read that accumulates.
type Counters struct {
	RoomsCompleted int
	RoomSeconds    int
	QualitySamples int
}

func countersOf(m MediaEvidence) Counters {
	return Counters{
		RoomsCompleted: m.RoomsCompleted,
		RoomSeconds:    m.RoomSeconds,
		QualitySamples: m.QualitySamples,
	}
}

// NewSample builds the row to store from a fresh read and the previous counters.
//
// `prev` is nil for the very first sample ever taken. That case is deliberately not
// treated as a reset: there is no earlier observation, so the counters describe
// whatever the SFU did before MatrixCtrl started watching, and claiming those as
// activity "in this interval" would invent history at exactly the moment there is
// none to check it against. The deltas are zero and the accounting starts here.
func NewSample(now time.Time, m MediaEvidence, prev *Counters) Sample {
	cur := countersOf(m)
	s := Sample{
		ObservedAt:       now,
		RoomsLive:        m.Live.Rooms,
		ParticipantsLive: m.Live.Participants,
		RoomsCompleted:   cur.RoomsCompleted,
		RoomSeconds:      cur.RoomSeconds,
		QualitySamples:   cur.QualitySamples,
	}
	if prev == nil {
		return s
	}

	// Any counter going backwards means the process restarted; they all reset
	// together. Checking all three rather than one avoids a false negative when the
	// counter being watched happened to be zero on both sides of the restart —
	// which is the *normal* case here, because rooms_completed is zero far more
	// often than not.
	s.SFURestarted = cur.RoomsCompleted < prev.RoomsCompleted ||
		cur.RoomSeconds < prev.RoomSeconds ||
		cur.QualitySamples < prev.QualitySamples

	if s.SFURestarted {
		// The new value *is* the delta: everything the previous process counted was
		// already recorded by the samples taken while it ran.
		s.DRoomsCompleted = cur.RoomsCompleted
		s.DRoomSeconds = cur.RoomSeconds
		s.DQualitySamples = cur.QualitySamples
		return s
	}

	s.DRoomsCompleted = cur.RoomsCompleted - prev.RoomsCompleted
	s.DRoomSeconds = cur.RoomSeconds - prev.RoomSeconds
	s.DQualitySamples = cur.QualitySamples - prev.QualitySamples
	return s
}

// Totals is the aggregate over a window of samples.
type Totals struct {
	Calls    int `json:"calls"`
	Seconds  int `json:"seconds"`
	Quality  int `json:"quality_samples"`
	Restarts int `json:"sfu_restarts"`
	// Samples is how many observations the totals are made of. It is reported so a
	// small number can be read as "not much has been observed yet" rather than as
	// "not much has happened".
	Samples int `json:"samples"`
	// Since is the oldest observation in the window — the honest start of the
	// accounting, which is not the same as the start of the window asked for.
	Since time.Time `json:"since,omitempty"`
}

// Sum adds up resolved deltas. Exact regardless of the sampling interval: both
// underlying counters are cumulative, so a call that began and ended entirely
// between two samples is still counted, with only its *timing* lost to the gap.
func Sum(samples []Sample) Totals {
	var t Totals
	for _, s := range samples {
		t.Calls += s.DRoomsCompleted
		t.Seconds += s.DRoomSeconds
		t.Quality += s.DQualitySamples
		if s.SFURestarted {
			t.Restarts++
		}
		t.Samples++
		if t.Since.IsZero() || s.ObservedAt.Before(t.Since) {
			t.Since = s.ObservedAt
		}
	}
	return t
}
