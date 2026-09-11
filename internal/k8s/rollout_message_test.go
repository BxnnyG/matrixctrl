package k8s

import "testing"

// The values are copied from a live crash-looping pod rather than invented — the whole
// defect lived in the difference between a message that explains and one that restates.
func TestWaitingMessageIgnoresTheBackOffText(t *testing.T) {
	const backOff = "back-off 10s restarting failed container=synapse pod=ess-synapse-main-0_ess(abc)"

	if got := waitingMessage("CrashLoopBackOff", backOff); got != "" {
		t.Errorf("got %q, want empty — a non-empty message stops the container's own "+
			"output from ever being read, which is the only thing that says why it died", got)
	}
}

func TestWaitingMessageKeepsReasonsThatExplainThemselves(t *testing.T) {
	cases := map[string]string{
		"ImagePullBackOff":           `Back-off pulling image "ghcr.io/example/nope:1.2.3"`,
		"CreateContainerConfigError": `secret "matrixctrl-secret" not found`,
		"ContainerCreating":          "",
	}
	for reason, message := range cases {
		if got := waitingMessage(reason, message); got != message {
			t.Errorf("%s: got %q, want %q — this one names the thing that is wrong", reason, got, message)
		}
	}
}
