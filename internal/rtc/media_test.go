package rtc

import (
	"strings"
	"testing"
)

// Verbatim shape of the production body, including the labels and the HELP/TYPE
// lines a real exposition carries.
const liveBody = `# HELP livekit_room_total
# TYPE livekit_room_total gauge
livekit_room_total{node_id="ND_x",node_type="SERVER"} 0
livekit_participant_total{node_id="ND_x",node_type="SERVER"} 0
livekit_node_packet_total{node_id="ND_x",node_type="SERVER",type="dropped"} 0
livekit_node_packet_total{node_id="ND_x",node_type="SERVER",type="out"} 18633
livekit_room_duration_seconds_count{node_id="ND_x",node_type="SERVER"} 0
livekit_quality_score_count{node_id="ND_x",node_type="SERVER"} 0
livekit_forward_latency_ns_count{node_id="ND_x",node_type="SERVER"} 0
`

func TestParsesTheCountersOutOfARealBody(t *testing.T) {
	ev, ok := ParseMetrics(liveBody)
	if !ok {
		t.Fatal("expected the parse to find something")
	}
	if ev.PacketsOut != 18633 {
		t.Errorf("packets out: got %d, want 18633", ev.PacketsOut)
	}
	if ev.RoomsCompleted != 0 || ev.QualitySamples != 0 || ev.ForwardSamples != 0 {
		t.Errorf("expected zeros, got %+v", ev)
	}
	if ev.MediaFlowed() {
		t.Error("no samples must not read as media having flowed")
	}
}

// Packets out rises constantly with no call in progress — it was 16208 and 18633
// two minutes apart on an idle SFU. Treating it as evidence of a working session
// would produce a permanent all-clear.
func TestPacketsOutIsNotEvidenceOfMedia(t *testing.T) {
	ev, _ := ParseMetrics(liveBody)
	if ev.PacketsOut == 0 {
		t.Fatal("fixture should have packets out")
	}
	if ev.MediaFlowed() {
		t.Fatal("packets out must not count as media")
	}
	f := AssessMedia(ev, true, "1h")
	if f.Level == LevelOK {
		t.Fatalf("an idle SFU with outbound packets must not report ok: %+v", f)
	}
}

// The production state of 2026-08-03/04: rooms were created, no media ever arrived.
// This is the one sentence that would have redirected two days of work.
func TestRoomsWithoutMediaIsAWarning(t *testing.T) {
	ev := MediaEvidence{RoomsCompleted: 3}
	f := AssessMedia(ev, true, "2h")

	if f.Level != LevelWarn {
		t.Fatalf("expected warn, got %s (%s)", f.Level, f.Title)
	}
	if f.Action == "" {
		t.Error("a warning with nothing to do about it is a shrug")
	}
	if !strings.Contains(f.Detail, "3") {
		t.Error("the finding must say how many rooms, or it cannot be checked")
	}
}

// A quiet night is not a fault. An alarm that fires when nothing is wrong gets
// switched off before it ever fires when something is.
func TestNoRoomsIsUnknownNotAWarning(t *testing.T) {
	f := AssessMedia(MediaEvidence{}, true, "6h")
	if f.Level != LevelUnknown {
		t.Fatalf("nothing attempted must be unknown, got %s (%s)", f.Level, f.Title)
	}
}

func TestMediaSamplesReportOK(t *testing.T) {
	for _, ev := range []MediaEvidence{
		{RoomsCompleted: 1, QualitySamples: 12},
		{RoomsCompleted: 1, ForwardSamples: 400}, // a call too short for a quality sample
	} {
		if f := AssessMedia(ev, true, "3h"); f.Level != LevelOK {
			t.Fatalf("%+v should report ok, got %s", ev, f.Level)
		}
	}
}

// A failed read must never render as "no media flowed", which would look identical
// to a real fault and send the operator back to their router for nothing.
func TestUnreadableMetricsAreUnknown(t *testing.T) {
	f := AssessMedia(MediaEvidence{}, false, "3h")
	if f.Level != LevelUnknown {
		t.Fatalf("an unreadable body must be unknown, got %s", f.Level)
	}
	if strings.Contains(f.Title, "niemand angerufen") {
		t.Error("a failed read must not be reported as 'nobody called'")
	}
}

func TestMalformedBodyDoesNotPanicAndFindsNothing(t *testing.T) {
	for _, body := range []string{
		"", "garbage", "livekit_quality_score_count", "livekit_quality_score_count{unclosed 5",
		"# only a comment\n", "livekit_quality_score_count{} notanumber",
	} {
		ev, ok := ParseMetrics(body)
		if ok && ev.MediaFlowed() {
			t.Fatalf("malformed body %q must not claim media flowed", body)
		}
	}
}

// Prometheus may render a counter as a float. A count is a count.
func TestFloatValuesAreAccepted(t *testing.T) {
	ev, ok := ParseMetrics("livekit_quality_score_count{node_id=\"x\"} 12.0\n")
	if !ok || ev.QualitySamples != 12 {
		t.Fatalf("expected 12, got %+v (ok=%v)", ev, ok)
	}
}

// One renamed counter after a LiveKit upgrade should cost that number, not the page.
func TestUnknownMetricsAreIgnored(t *testing.T) {
	body := "livekit_something_new{a=\"b\"} 99\nlivekit_quality_score_count{a=\"b\"} 5\n"
	ev, ok := ParseMetrics(body)
	if !ok || ev.QualitySamples != 5 {
		t.Fatalf("expected the known counter to survive an unknown neighbour, got %+v", ev)
	}
}
