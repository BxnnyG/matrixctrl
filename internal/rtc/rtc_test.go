package rtc

import (
	"errors"
	"strings"
	"testing"
)

func production() []ServicePort {
	// The real shape on the production cluster, including the one service the
	// built-in hook does not cover.
	return []ServicePort{
		{Service: "ess-matrix-rtc-sfu-tcp", Name: "rtc-tcp", Protocol: "TCP", NodePort: 30001, ExternalTrafficPolicy: "Local"},
		{Service: "ess-matrix-rtc-sfu-muxed-udp", Name: "rtc-muxed-udp", Protocol: "UDP", NodePort: 30002, ExternalTrafficPolicy: "Local"},
		{Service: "ess-matrix-rtc-sfu-turn", Name: "turn-udp", Protocol: "UDP", NodePort: 30004, ExternalTrafficPolicy: "Local"},
		{Service: "ess-matrix-rtc-sfu-turn-tls", Name: "turn-tls-tcp", Protocol: "TCP", NodePort: 31443, ExternalTrafficPolicy: "Cluster"},
	}
}

func TestOnlyRTCServicesAreListed(t *testing.T) {
	// Telling an operator to forward a port that has nothing to do with calling
	// is advice to open their network for no reason.
	svcs := append(production(),
		ServicePort{Service: "ess-synapse-metrics", Name: "metrics", Protocol: "TCP", NodePort: 30099, ExternalTrafficPolicy: "Cluster"},
	)

	for _, p := range SFUPorts(svcs) {
		if p.Port == 30099 {
			t.Errorf("listed %s as a port to forward — it is not an RTC service", p.Service)
		}
	}
	if got := len(SFUPorts(svcs)); got != 4 {
		t.Errorf("got %d RTC ports, want 4", got)
	}
}

func TestProtocolIsCarriedThrough(t *testing.T) {
	// Forwarding TCP 30002 instead of UDP 30002 produces a router rule that
	// looks right and does nothing, which is the worst possible failure here.
	byPort := map[int32]Port{}
	for _, p := range SFUPorts(production()) {
		byPort[p.Port] = p
	}
	for port, want := range map[int32]string{30001: "TCP", 30002: "UDP", 30004: "UDP", 31443: "TCP"} {
		if got := byPort[port].Protocol; got != want {
			t.Errorf("port %d: protocol %q, want %q", port, got, want)
		}
	}
}

func TestClusterPolicyIsReported(t *testing.T) {
	findings := Assess(SFUPorts(production()), "rtc.example.com", []string{"203.0.113.10"}, nil)

	var found *Finding
	for i := range findings {
		if strings.Contains(findings[i].Title, "Client-Adresse") {
			found = &findings[i]
		}
	}
	if found == nil {
		t.Fatal("turn-tls runs with externalTrafficPolicy=Cluster and nothing said so")
	}
	if found.Level != LevelWarn {
		t.Errorf("level = %q, want warn", found.Level)
	}
	if !strings.Contains(found.Detail, "31443") {
		t.Errorf("the finding should name the affected port, got: %s", found.Detail)
	}
}

func TestAllLocalProducesNoPolicyWarning(t *testing.T) {
	svcs := production()
	svcs[3].ExternalTrafficPolicy = "Local"

	for _, f := range Assess(SFUPorts(svcs), "rtc.example.com", []string{"203.0.113.10"}, nil) {
		if strings.Contains(f.Title, "Client-Adresse") {
			t.Error("warned about source-IP preservation while every service preserves it")
		}
	}
}

// The point of the whole package: "not checked" must never be presented as "ok".
func TestReachabilityIsAlwaysReportedAsUnknown(t *testing.T) {
	cases := map[string][]Finding{
		"healthy":     Assess(SFUPorts(production()), "rtc.example.com", []string{"203.0.113.10"}, nil),
		"no ports":    Assess(nil, "rtc.example.com", []string{"203.0.113.10"}, nil),
		"no hostname": Assess(SFUPorts(production()), "", nil, nil),
		"dns broken":  Assess(SFUPorts(production()), "rtc.example.com", nil, errors.New("no such host")),
	}

	for name, findings := range cases {
		var unknown bool
		for _, f := range findings {
			if f.Level == LevelUnknown && strings.Contains(f.Title, "Internet erreichbar") {
				unknown = true
				if f.Action == "" {
					t.Errorf("%s: the unknown finding must say what to do instead", name)
				}
			}
		}
		if !unknown {
			t.Errorf("%s: inbound reachability was not reported as unknown — "+
				"silence next to green ticks reads as 'fine', which is the bug this package exists to fix", name)
		}
	}
}

func TestUnresolvableHostIsAWarningNotAnUnknown(t *testing.T) {
	// This one *is* knowable from inside, so reporting it as "unknown" would
	// hide a real, actionable problem.
	findings := Assess(SFUPorts(production()), "rtc.example.com", nil, errors.New("no such host"))

	for _, f := range findings {
		if strings.Contains(f.Title, "löst nicht auf") {
			if f.Level != LevelWarn {
				t.Errorf("level = %q, want warn", f.Level)
			}
			if !strings.Contains(f.Action, "rtc.example.com") {
				t.Errorf("action should name the record to fix, got %q", f.Action)
			}
			return
		}
	}
	t.Error("an unresolvable announced hostname was not reported")
}
