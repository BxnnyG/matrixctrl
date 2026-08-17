package rtc

import (
	"strings"
	"testing"
)

// The real line, copied from the running SFU rather than invented.
const sfuBufferLine = `2026-08-16T00:50:15.636Z	WARN	livekit	rtcconfig/rtc_unix.go:31	UDP receive buffer is too small for a production set-up	{"current": 425984, "suggested": 5000000}`

func TestParseBufferWarningReadsLiveKitsOwnNumbers(t *testing.T) {
	buf, ok := ParseBufferWarning("some earlier line\n" + sfuBufferLine + "\nlater line")
	if !ok {
		t.Fatal("the real SFU line must parse")
	}
	// 425984, not the node's net.core.rmem_max of 212992: the kernel doubles it, and
	// the panel must agree with the log the operator is looking at.
	if buf.Current != 425984 || buf.Suggested != 5000000 {
		t.Errorf("got current=%d suggested=%d, want 425984/5000000", buf.Current, buf.Suggested)
	}
	if !buf.Undersized() {
		t.Error("425984 < 5000000 is undersized")
	}
}

func TestParseBufferWarningAbsenceIsNotHealth(t *testing.T) {
	for _, in := range []string{
		"",
		"starting livekit\nlistening on 7880\n",
		// Present but reshaped by a LiveKit version change: half a reading would
		// compare against a zero suggestion and report "sufficient".
		`WARN UDP receive buffer is too small for a production set-up {"current": 425984}`,
	} {
		if _, ok := ParseBufferWarning(in); ok {
			t.Errorf("must not claim a reading from %q", in)
		}
	}
}

func TestAssessUDPBufferSaysWhetherItIsBitingYet(t *testing.T) {
	buf := UDPBuffer{Current: 425984, Suggested: 5000000}

	// The live case: undersized, nothing dropped. Must warn *and* say it is latent,
	// or it reads as an active fault.
	f := AssessUDPBuffer(buf, true, 0, true)
	if f.Level != LevelWarn {
		t.Errorf("level = %q, want warn", f.Level)
	}
	if !strings.Contains(f.Detail, "0 verworfene Pakete") {
		t.Errorf("a latent fault must say nothing has been dropped yet: %q", f.Detail)
	}
	if !strings.Contains(f.Action, "rmem_max") {
		t.Errorf("the action must name the sysctl: %q", f.Action)
	}

	// Actually dropping now — a different sentence, not the same warning.
	f = AssessUDPBuffer(buf, true, 1234, true)
	if !strings.Contains(f.Detail, "1234") {
		t.Errorf("must report the real drop count: %q", f.Detail)
	}
	if strings.Contains(f.Detail, "kein aktueller Fehler") {
		t.Error("must not call it latent while packets are being dropped")
	}

	// Drop counter missing from the exposition: neither reassure nor invent.
	f = AssessUDPBuffer(buf, true, 0, false)
	if strings.Contains(f.Detail, "0 verworfene Pakete") {
		t.Error("an absent counter must not be reported as zero drops")
	}
}

// A log without the line means "not read", never "fine". LiveKit logs it once at
// startup, so a long-running or rotated log legitimately lacks it.
func TestAssessUDPBufferUnknownWhenLineAbsent(t *testing.T) {
	f := AssessUDPBuffer(UDPBuffer{}, false, 0, false)
	if f.Level != LevelUnknown {
		t.Errorf("level = %q, want unknown — absence is not health", f.Level)
	}
}

func TestAssessUDPBufferOKWhenLargeEnough(t *testing.T) {
	f := AssessUDPBuffer(UDPBuffer{Current: 5000000, Suggested: 5000000}, true, 0, true)
	if f.Level != LevelOK {
		t.Errorf("level = %q, want ok", f.Level)
	}
}

// The metric was in this package's fixture from the start and only type="out" was
// ever read.
func TestParseMetricsReadsDroppedPackets(t *testing.T) {
	m, found := ParseMetrics(`livekit_node_packet_total{node_id="ND_x",node_type="SERVER",type="dropped"} 7
livekit_node_packet_total{node_id="ND_x",node_type="SERVER",type="out"} 18633`)
	if !found {
		t.Fatal("metrics must parse")
	}
	if !m.DroppedKnown || m.PacketsDropped != 7 {
		t.Errorf("dropped = %d (known=%v), want 7/true", m.PacketsDropped, m.DroppedKnown)
	}
	if m.PacketsOut != 18633 {
		t.Errorf("out = %d, want 18633 — the existing reading must not regress", m.PacketsOut)
	}
}

func TestParseMetricsDroppedAbsentIsNotZero(t *testing.T) {
	m, _ := ParseMetrics(`livekit_node_packet_total{type="out"} 5`)
	if m.DroppedKnown {
		t.Error("DroppedKnown must be false when the metric is not in the exposition")
	}
}
