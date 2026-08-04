// Package rtc answers one question: can a call actually connect?
//
// The honest answer has two halves, and only one of them is knowable from inside
// the cluster. Element Call was broken on the production instance while every
// signal MatrixCtrl produced was green — pods healthy, patches applied — because
// the deciding half (are the node ports reachable from the internet?) was never
// looked at, and its absence read as "fine" rather than "unknown".
//
// So this package classifies findings into what is verified, what is a problem,
// and what **cannot be checked from here** — and the last category is a first-
// class result, not an omission.
package rtc

import "fmt"

// Level says how much weight a finding carries. Unknown is deliberately distinct
// from OK: "I did not check this" and "this is fine" are different statements,
// and conflating them is the bug this package exists to fix.
type Level string

const (
	LevelOK      Level = "ok"
	LevelWarn    Level = "warn"
	LevelUnknown Level = "unknown"
)

type Finding struct {
	Level  Level  `json:"level"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	// Action is what the operator should do about it. Empty when there is
	// nothing to do.
	Action string `json:"action,omitempty"`
}

// Port is one node port that must be reachable from the internet.
type Port struct {
	Protocol string `json:"protocol"`
	Port     int32  `json:"port"`
	Service  string `json:"service"`
	Purpose  string `json:"purpose"`
	// SourceIPPreserved reports externalTrafficPolicy=Local. The SFU needs the
	// real client address for STUN; behind a Cluster policy it sees a node
	// address instead.
	SourceIPPreserved bool `json:"source_ip_preserved"`
}

// purposes maps the chart's port names to something an operator can act on. A
// name this map does not know falls through to the raw name rather than to a
// guess.
var purposes = map[string]string{
	"rtc-muxed-udp": "Medien (der wichtigste Port — ohne ihn kein Ton und kein Bild)",
	"rtc-tcp":       "Medien-Fallback für Netze, die UDP blockieren",
	"turn-udp":      "TURN — Relay für Clients hinter striktem NAT",
	"turn-tls-tcp":  "TURN über TLS — Fallback, wenn nur 443-artiger Verkehr durchkommt",
}

func purposeOf(portName string) string {
	if p, ok := purposes[portName]; ok {
		return p
	}
	return portName
}

// SFUPorts turns the live NodePort services into the list an operator has to
// forward. Only services whose name marks them as RTC are included: everything
// else in the namespace is a node port for some other reason and forwarding it
// would be advice to open ports nobody asked about.
func SFUPorts(services []ServicePort) []Port {
	out := make([]Port, 0, len(services))
	for _, s := range services {
		if !isRTC(s.Service) {
			continue
		}
		out = append(out, Port{
			Protocol:          s.Protocol,
			Port:              s.NodePort,
			Service:           s.Service,
			Purpose:           purposeOf(s.Name),
			SourceIPPreserved: s.ExternalTrafficPolicy == "Local",
		})
	}
	return out
}

// ServicePort mirrors k8s.ExposedPort without importing it, so this package —
// which holds the reasoning — stays testable with no cluster and no client-go.
type ServicePort struct {
	Service               string
	Name                  string
	Protocol              string
	NodePort              int32
	ExternalTrafficPolicy string
}

func isRTC(service string) bool {
	for _, marker := range []string{"rtc-sfu", "matrix-rtc"} {
		if len(service) >= len(marker) && contains(service, marker) {
			return true
		}
	}
	return false
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// Assess produces the findings shown next to the port list.
//
// The unknown finding is always present. It is not a fallback for when something
// goes wrong — it is the permanent, correct statement that inbound reachability
// cannot be tested from inside the network it terminates in.
func Assess(ports []Port, announcedHost string, resolvedIPs []string, resolveErr error) []Finding {
	return AssessWithFreshness(ports, announcedHost, resolvedIPs, resolveErr, FreshnessUnknown, "")
}

// AssessWithFreshness is Assess plus the answer to "is the address the SFU announces
// still the current one?" — see address.go. Kept as a separate entry point so the
// existing callers and tests that have no freshness input keep working unchanged,
// and so a caller that cannot determine freshness passes Unknown explicitly rather
// than having it defaulted somewhere out of sight.
func AssessWithFreshness(ports []Port, announcedHost string, resolvedIPs []string, resolveErr error,
	freshness Freshness, freshnessDetail string) []Finding {
	var findings []Finding

	switch freshness {
	case FreshnessStale:
		findings = append(findings, Finding{
			Level: LevelWarn,
			Title: "Die SFU kündigt eine veraltete Adresse an",
			Detail: freshnessDetail + " LiveKit ermittelt seine öffentliche Adresse einmal beim Start und behält sie. " +
				"Nach einer Zwangstrennung bekommen Clients per DNS die richtige Adresse, die SFU nennt in ihren " +
				"ICE-Candidates aber die alte — Medien laufen ins Leere, während Signalisierung und Pods gesund aussehen.",
			Action: "SFU-Pod ersetzen. Nicht per rollout restart: hostNetwork mit maxUnavailable=0 blockiert " +
				"sich selbst, der neue Pod bekommt die Host-Ports nie und bleibt Pending.",
		})
	case FreshnessOK:
		findings = append(findings, Finding{
			Level:  LevelOK,
			Title:  "Angekündigte Adresse ist aktuell",
			Detail: freshnessDetail + " Die SFU wurde nach der letzten Adressänderung gestartet.",
		})
	case FreshnessUnknown:
		if freshnessDetail != "" {
			findings = append(findings, Finding{
				Level:  LevelUnknown,
				Title:  "Ob die angekündigte Adresse aktuell ist, steht noch nicht fest",
				Detail: freshnessDetail + " Beobachtet wird ab jetzt; ein Urteil braucht mindestens eine gesehene Änderung.",
				Action: "Nichts zu tun — der Hinweis verschwindet, sobald eine Adressänderung beobachtet wurde.",
			})
		}
	}

	if len(ports) == 0 {
		findings = append(findings, Finding{
			Level:  LevelWarn,
			Title:  "Keine RTC-NodePorts gefunden",
			Detail: "Im ESS-Namespace existiert kein NodePort-Service für die SFU. Ohne den kann kein Medienverkehr den Cluster erreichen.",
			Action: "Prüfen, ob matrixRTC in der Config aktiviert ist und das Deployment durchgelaufen ist.",
		})
	}

	var clusterPolicy []Port
	for _, p := range ports {
		if !p.SourceIPPreserved {
			clusterPolicy = append(clusterPolicy, p)
		}
	}
	if len(clusterPolicy) > 0 {
		names := ""
		for i, p := range clusterPolicy {
			if i > 0 {
				names += ", "
			}
			names += fmt.Sprintf("%s (%s %d)", p.Service, p.Protocol, p.Port)
		}
		findings = append(findings, Finding{
			Level: LevelWarn,
			Title: "Nicht alle RTC-Services erhalten die Client-Adresse",
			Detail: "externalTrafficPolicy=Cluster bei: " + names +
				". Die SFU sieht dann eine Node-Adresse statt der echten Client-Adresse, was STUN unbrauchbar machen kann. " +
				"Der eingebaute Hook setzt bewusst nur drei der vier Services auf Local.",
			Action: "Prüfen, ob dieser Service die Client-Adresse braucht — und wenn ja, den Hook erweitern statt einmalig zu patchen.",
		})
	}

	switch {
	case announcedHost == "":
		findings = append(findings, Finding{
			Level:  LevelUnknown,
			Title:  "Kein RTC-Hostname in der Config",
			Detail: "matrixRTC.ingress.host ist leer, also lässt sich nicht prüfen, was Clients überhaupt angeboten bekommt.",
		})
	case resolveErr != nil:
		findings = append(findings, Finding{
			Level:  LevelWarn,
			Title:  "Der angekündigte RTC-Hostname löst nicht auf",
			Detail: announcedHost + " ist nicht auflösbar: " + resolveErr.Error() + ". Clients bekommen einen Namen genannt, den sie nicht erreichen.",
			Action: "DNS-Record für " + announcedHost + " prüfen.",
		})
	default:
		findings = append(findings, Finding{
			Level:  LevelOK,
			Title:  "RTC-Hostname löst auf",
			Detail: announcedHost + " → " + join(resolvedIPs, ", "),
		})
	}

	findings = append(findings, Finding{
		Level: LevelUnknown,
		Title: "Ob die Ports aus dem Internet erreichbar sind, kann MatrixCtrl nicht prüfen",
		Detail: "Dafür bräuchte es einen Aussichtspunkt außerhalb deines Netzes. Genau diese Hälfte entscheidet aber, " +
			"ob ein Anruf zustande kommt — Pods gesund und Patches gesetzt sagen darüber nichts aus.",
		Action: "Die oben gelisteten Ports im Router auf diesen Node weiterleiten (Protokoll beachten: UDP ist nicht TCP) " +
			"und von außen testen. Bei CGNAT beim Provider ist eingehender Verkehr grundsätzlich nicht möglich.",
	})

	return findings
}

func join(items []string, sep string) string {
	out := ""
	for i, s := range items {
		if i > 0 {
			out += sep
		}
		out += s
	}
	return out
}
