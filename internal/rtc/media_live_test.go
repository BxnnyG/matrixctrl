package rtc

import (
	"context"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

// TestSFUMetricsLive reads the real SFU and checks that every metric this package
// depends on is actually present. Skipped unless RUN_LIVE=1.
//
// The unit tests parse a fixture, which proves the parser and nothing about the
// server. What this catches is the failure mode the parser is deliberately tolerant
// of: a LiveKit upgrade that renames or drops a counter costs that number silently,
// and the page then reports a confident zero. ESS moved LiveKit from 1.9 to 1.10
// during this project without anyone checking.
//
// Requires a route to the SFU metrics port — run it inside the cluster or with a
// port-forward, and set SFU_METRICS_URL.
func TestSFUMetricsLive(t *testing.T) {
	if os.Getenv("RUN_LIVE") == "" {
		t.Skip("set RUN_LIVE=1 to run against the live SFU")
	}
	url := os.Getenv("SFU_METRICS_URL")
	if url == "" {
		t.Skip("set SFU_METRICS_URL to the SFU's /metrics endpoint")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	ev, ok := ParseMetrics(string(body))
	if !ok {
		t.Fatal("no known metric found — every counter this page reports has been renamed or removed")
	}
	t.Logf("live=%d rooms/%d participants · completed=%d · seconds=%d · quality=%d · forward=%d · packets=%d",
		ev.Live.Rooms, ev.Live.Participants, ev.RoomsCompleted, ev.RoomSeconds,
		ev.QualitySamples, ev.ForwardSamples, ev.PacketsOut)

	// Each name asserted individually. A single "found something" check passes while
	// five of six counters are missing, which is exactly the silent degradation this
	// test exists to catch.
	for _, name := range []string{
		"livekit_room_total",
		"livekit_participant_total",
		"livekit_room_duration_seconds_count",
		"livekit_room_duration_seconds_sum",
		"livekit_quality_score_count",
		"livekit_forward_latency_ns_count",
	} {
		if !hasMetric(string(body), name) {
			t.Errorf("metric %q is not exposed by this SFU — whatever the page derives from it now reads zero", name)
		}
	}

	// PacketsOut rises on an idle server, so it is the one number that can be
	// asserted as non-zero without waiting for a call. A zero here means the label
	// match broke, not that the SFU is idle.
	if ev.PacketsOut == 0 {
		t.Errorf(`packets_out = 0; the type="out" label match has probably broken`)
	}
}

func hasMetric(body, name string) bool {
	for _, line := range splitLines(body) {
		if n, _, ok := splitMetric(line); ok && n == name {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}
