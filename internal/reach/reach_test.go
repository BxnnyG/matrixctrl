package reach

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The production state of 2026-08-04: TCP 30001 closed from outside, control open.
// This is the sentence three days of inside-out measurement never produced.
func TestClosedPortIsReportedWithTheRouterDistinction(t *testing.T) {
	v := Assess(Result{
		Address:   "198.51.100.7",
		ControlOK: true,
		Ports:     []PortResult{{Protocol: "TCP", Port: 30001, Status: Closed}},
		// Ports: 30002/udp and 30004/udp could not be tested.
		UDPSkipped: 2,
	})

	if v.Level != "warn" {
		t.Fatalf("expected warn, got %s (%s)", v.Level, v.Title)
	}
	// The distinction that cost three days: an allow rule is not a port forward.
	if !strings.Contains(v.Action, "DNAT") || !strings.Contains(v.Action, "Firewall-Regel") {
		t.Errorf("the action must separate a port forward from a firewall rule: %q", v.Action)
	}
	// The UDP gap must be in the result, not only in the plan.
	if !strings.Contains(v.Detail, "UDP") {
		t.Errorf("the untested UDP ports must be stated: %q", v.Detail)
	}
}

// A checker that is blocked or broken reports everything as closed. Believing it
// sends the operator to reconfigure a router that was already correct — and the
// next three things they try are built on that mistake.
func TestFailedControlMakesEverythingUnknown(t *testing.T) {
	v := Assess(Result{
		Address:   "198.51.100.7",
		ControlOK: false,
		Ports:     []PortResult{{Protocol: "TCP", Port: 30001, Status: Closed}},
	})
	if v.Level != "unknown" {
		t.Fatalf("a failed control must not produce a verdict, got %s", v.Level)
	}
	if strings.Contains(v.Title, "geschlossen:") {
		t.Error("it must not announce closed ports from an untrusted run")
	}
	// The safety-critical half: an untrusted run must actively tell the operator not
	// to change anything, not merely fail to recommend it.
	if !strings.Contains(v.Action, "Auf keinen Fall") {
		t.Errorf("it must warn against acting on this run: %q", v.Action)
	}
}

func TestTransportErrorIsUnknownNotClosed(t *testing.T) {
	v := Assess(Result{Error: "Der Prüfdienst antwortet nicht (timeout).", ControlOK: false})
	if v.Level != "unknown" {
		t.Fatalf("got %s", v.Level)
	}
	if strings.Contains(strings.ToLower(v.Title), "geschlossen") {
		t.Error("a failed request is not a closed port")
	}
}

// An open TCP port is real news but does not settle UDP, and saying so is the
// difference between narrowing the question and closing it wrongly.
func TestOpenPortDoesNotClaimUDP(t *testing.T) {
	v := Assess(Result{
		Address: "198.51.100.7", ControlOK: true, UDPSkipped: 2,
		Ports: []PortResult{{Protocol: "TCP", Port: 30001, Status: Open}},
	})
	if v.Level != "ok" {
		t.Fatalf("got %s", v.Level)
	}
	if !strings.Contains(v.Detail, "UDP") {
		t.Errorf("an ok verdict must still name the untested UDP ports: %q", v.Detail)
	}
}

// One closed port outranks any number of open ones: the operator needs the problem,
// not the score.
func TestClosedOutranksOpen(t *testing.T) {
	v := Assess(Result{
		Address: "198.51.100.7", ControlOK: true,
		Ports: []PortResult{
			{Protocol: "TCP", Port: 30001, Status: Closed},
			{Protocol: "TCP", Port: 8448, Status: Open},
		},
	})
	if v.Level != "warn" || !strings.Contains(v.Title, "30001") {
		t.Fatalf("got %s / %s", v.Level, v.Title)
	}
}

func TestSplitByProtocolCountsWhatItCannotTest(t *testing.T) {
	split := SplitByProtocol([]PortResult{
		{Protocol: "UDP", Port: 30002}, {Protocol: "TCP", Port: 30001}, {Protocol: "UDP", Port: 30004},
	})
	if len(split.Ports) != 1 || split.Ports[0].Port != 30001 {
		t.Fatalf("only TCP is testable: %+v", split.Ports)
	}
	if split.UDPSkipped != 2 {
		t.Fatalf("UDP ports must be counted, got %d", split.UDPSkipped)
	}
	if split.Ports[0].Status != Unknown {
		t.Error("a port that has not been tested yet is unknown, not closed")
	}
}

func TestParsePortResponse(t *testing.T) {
	ok, err := parsePortResponse([]byte(`{"error":false,"check":[{"port":30001,"status":false},{"port":443,"status":true}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if ok[30001] || !ok[443] {
		t.Fatalf("got %+v", ok)
	}

	// Anything unreadable must be an error rather than an empty map, because an
	// empty map read as "nothing is open" is a false alarm on every port.
	for _, bad := range []string{"", "not json", `{"error":true}`, `{"error":false,"check":[]}`, `{}`} {
		if _, err := parsePortResponse([]byte(bad)); err == nil {
			t.Errorf("%q should have failed", bad)
		}
	}
}

// End to end against local stand-ins, so the sequencing is covered: address, then
// control, then ports — and a bad control stops before the ports are ever sent.
func TestCheckFlow(t *testing.T) {
	var portCalls int
	controlOpen := true

	addrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("198.51.100.7"))
	}))
	defer addrSrv.Close()

	portSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		portCalls++
		var req struct {
			Host  string  `json:"host"`
			Ports []int32 `json:"ports"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		out := map[string]any{"error": false}
		var checks []map[string]any
		for _, p := range req.Ports {
			status := false
			if req.Host == controlHost {
				status = controlOpen
			}
			checks = append(checks, map[string]any{"port": p, "status": status})
		}
		out["check"] = checks
		_ = json.NewEncoder(w).Encode(out)
	}))
	defer portSrv.Close()

	c := &Client{http: portSrv.Client(), addressURL: addrSrv.URL, portURL: portSrv.URL}
	ports := []PortResult{{Protocol: "TCP", Port: 30001}, {Protocol: "UDP", Port: 30002}}

	res := c.Check(context.Background(), ports)
	if res.Address != "198.51.100.7" || !res.ControlOK {
		t.Fatalf("got %+v", res)
	}
	if len(res.Ports) != 1 || res.Ports[0].Status != Closed {
		t.Fatalf("expected the TCP port closed: %+v", res.Ports)
	}
	if res.UDPSkipped != 1 {
		t.Fatalf("UDP must be counted as skipped, got %d", res.UDPSkipped)
	}
	if portCalls != 2 {
		t.Fatalf("expected control + ports, got %d calls", portCalls)
	}

	// With a failing control the ports must never be asked about at all.
	controlOpen = false
	portCalls = 0
	res = c.Check(context.Background(), ports)
	if res.ControlOK {
		t.Fatal("control should have failed")
	}
	if portCalls != 1 {
		t.Fatalf("a failed control must stop before testing ports, got %d calls", portCalls)
	}
	if Assess(res).Level != "unknown" {
		t.Fatal("and the verdict must be unknown")
	}
}

// No outbound internet is the air-gapped case, and it must read as "could not
// measure" rather than "your ports are shut".
func TestUnreachableServicesAreUnknown(t *testing.T) {
	c := &Client{http: &http.Client{}, addressURL: "http://127.0.0.1:1/x", portURL: "http://127.0.0.1:1/y"}
	res := c.Check(context.Background(), []PortResult{{Protocol: "TCP", Port: 30001}})
	if res.Error == "" {
		t.Fatal("expected an error")
	}
	if v := Assess(res); v.Level != "unknown" {
		t.Fatalf("got %s: %s", v.Level, v.Title)
	}
}

func TestServicesAreNamedForTheUI(t *testing.T) {
	if len(Services()) == 0 {
		t.Fatal("the UI has to name who receives the address before the click")
	}
}

// A status page that cannot conjugate reads as one nobody maintains, which is
// exactly the impression a diagnostic tool cannot afford.
func TestUDPNoteAgreesInNumber(t *testing.T) {
	one := Assess(Result{Address: "198.51.100.7", ControlOK: true, UDPSkipped: 1,
		Ports: []PortResult{{Protocol: "TCP", Port: 30001, Status: Closed}}})
	if !strings.Contains(one.Detail, "1 UDP-Port konnte nicht") {
		t.Errorf("singular: %q", one.Detail)
	}

	many := Assess(Result{Address: "198.51.100.7", ControlOK: true, UDPSkipped: 2,
		Ports: []PortResult{{Protocol: "TCP", Port: 30001, Status: Closed}}})
	if !strings.Contains(many.Detail, "2 UDP-Ports konnten nicht") {
		t.Errorf("plural: %q", many.Detail)
	}
}
