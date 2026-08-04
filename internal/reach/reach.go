// Package reach answers the question E19 declared permanently unanswerable: are the
// media ports open from the internet?
//
// E19 was right that it cannot be tested from inside the network it terminates in,
// and wrote that into the product as an honest `unknown`. What it got wrong was the
// implication that therefore nothing could be done. On 2026-08-04, after three days
// of measuring from the inside — ICE statistics, packet counters, conntrack — one
// request to a public port checker answered it in seconds: nothing inbound reached
// the node at all. That single fact explained every measurement taken since.
//
// So this package steps outside. It is the only thing in MatrixCtrl that talks to a
// third party, which is why it never runs without a click.
package reach

import (
	"fmt"
	"sort"
	"strings"
)

type Status string

const (
	Open   Status = "open"
	Closed Status = "closed"
	// Unknown is the answer whenever the check itself could not be trusted. A
	// checker that is blocked, rate-limited or broken reports everything as closed,
	// and an operator who believes it goes and reconfigures a router that was
	// already correct.
	Unknown Status = "unknown"
)

// PortResult is one port as seen from outside.
type PortResult struct {
	Protocol string `json:"protocol"`
	Port     int32  `json:"port"`
	Status   Status `json:"status"`
	Purpose  string `json:"purpose,omitempty"`
}

// Result is one run of the check.
type Result struct {
	// Address is what was tested — the node's own egress address, discovered the
	// same way LiveKit discovers it. Never the announced hostname: where that
	// resolves to a proxy or tunnel, testing it would test the proxy.
	Address string       `json:"address"`
	Ports   []PortResult `json:"ports"`
	// ControlOK reports that a port known to be open on an unrelated host came back
	// open. Without it, "closed" is not a measurement — it is the absence of one.
	ControlOK bool `json:"control_ok"`
	// UDPSkipped counts ports that could not be tested because free checkers speak
	// TCP. Reported rather than silently dropped: the most important port on the
	// list is UDP, and a result that quietly omits it invites the reader to
	// generalise from the ones it did test.
	UDPSkipped int    `json:"udp_skipped"`
	Error      string `json:"error,omitempty"`
}

// Verdict is the sentence shown to the operator.
type Verdict struct {
	Level  string `json:"level"` // ok | warn | unknown
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Action string `json:"action,omitempty"`
}

// Assess turns a run into that sentence.
//
// The ordering matters: an untrustworthy control outranks every port result,
// because a wrong "closed" is worse than no answer. It sends someone to change a
// configuration that was already right, and the next three things they try will all
// be on top of that mistake.
func Assess(r Result) Verdict {
	switch {
	case r.Error != "":
		return Verdict{
			Level:  "unknown",
			Title:  "Die Prüfung von außen konnte nicht durchgeführt werden",
			Detail: r.Error + " Das ist keine Aussage über die Ports — nur darüber, dass gerade nichts gemessen werden konnte.",
		}

	case !r.ControlOK:
		return Verdict{
			Level: "unknown",
			Title: "Das Ergebnis ist nicht belastbar",
			Detail: "Die Kontrollprüfung ist fehlgeschlagen: ein Port, der bei einem fremden Host offen sein muss, " +
				"kam als geschlossen zurück. Damit ist der Prüfdienst blockiert oder gestört, und jedes „geschlossen“ " +
				"in diesem Lauf wäre geraten.",
			Action: "Später erneut versuchen. Auf keinen Fall aufgrund dieses Laufs am Router etwas ändern.",
		}
	}

	var closed, open []string
	for _, p := range r.Ports {
		switch p.Status {
		case Closed:
			closed = append(closed, fmt.Sprintf("%s %d", p.Protocol, p.Port))
		case Open:
			open = append(open, fmt.Sprintf("%s %d", p.Protocol, p.Port))
		}
	}
	sort.Strings(closed)
	sort.Strings(open)

	udpNote := ""
	if r.UDPSkipped > 0 {
		udpNote = fmt.Sprintf(" %d UDP-Port%s %s nicht geprüft werden — der Prüfdienst kann nur TCP. "+
			"Für Medien ist UDP der wichtigere Weg.",
			r.UDPSkipped, plural(r.UDPSkipped, "", "s"), plural(r.UDPSkipped, "konnte", "konnten"))
	}

	switch {
	case len(closed) > 0:
		return Verdict{
			Level: "warn",
			Title: "Von außen geschlossen: " + strings.Join(closed, ", "),
			Detail: "Von einem Punkt außerhalb deines Netzes ist " + strings.Join(closed, ", ") +
				" auf " + r.Address + " nicht erreichbar." + udpNote +
				" Ohne eingehenden Verkehr auf diesen Ports kann kein Anruf Medien übertragen, egal wie gesund alles andere aussieht.",
			Action: "Im Router eine Portweiterleitung (DNAT) auf die lokale Adresse dieses Nodes einrichten. " +
				"Eine Firewall-Regel, die den Port „erlaubt“, ist etwas anderes: sie lässt Verkehr durch, " +
				"der bereits an den Host adressiert ist, und leitet nichts um, was an die öffentliche Adresse geht.",
		}

	case len(open) > 0:
		return Verdict{
			Level: "ok",
			Title: "Von außen erreichbar: " + strings.Join(open, ", "),
			Detail: "Geprüft von außerhalb deines Netzes gegen " + r.Address + "." + udpNote +
				" Ein offener TCP-Port beweist, dass die Weiterleitung grundsätzlich greift — für UDP sagt er es nicht mit.",
		}

	default:
		return Verdict{
			Level:  "unknown",
			Title:  "Es gab nichts zu prüfen",
			Detail: "Es sind keine TCP-Ports gelistet, die von außen erreichbar sein müssten." + udpNote,
		}
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
