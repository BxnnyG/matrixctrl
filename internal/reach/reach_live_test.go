package reach

import (
	"context"
	"os"
	"testing"
	"time"
)

// Runs the real check against the real services. Off by default: every other test
// in this repo is offline, and a suite that reaches the internet fails for reasons
// that have nothing to do with the code.
func TestLiveCheck(t *testing.T) {
	if os.Getenv("RUN_LIVE") == "" {
		t.Skip("set RUN_LIVE=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	res := NewClient().Check(ctx, []PortResult{
		{Protocol: "TCP", Port: 30001, Purpose: "media fallback"},
		{Protocol: "UDP", Port: 30002, Purpose: "media"},
		{Protocol: "UDP", Port: 30004, Purpose: "turn"},
	})

	t.Logf("control_ok=%v udp_skipped=%d error=%q", res.ControlOK, res.UDPSkipped, res.Error)
	for _, p := range res.Ports {
		t.Logf("  %s %d -> %s", p.Protocol, p.Port, p.Status)
	}
	v := Assess(res)
	t.Logf("verdict [%s] %s", v.Level, v.Title)
	t.Logf("  %s", v.Detail)
	if v.Action != "" {
		t.Logf("  action: %s", v.Action)
	}

	if res.Error == "" && !res.ControlOK {
		t.Error("the control failed with no transport error — the checker is not usable")
	}
}
