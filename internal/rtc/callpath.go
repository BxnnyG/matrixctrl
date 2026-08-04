package rtc

import (
	"sort"

	"gopkg.in/yaml.v3"
)

// Calling is not one mechanism. Element Call routes media through the SFU this
// package spends most of its time reporting on; a legacy 1:1 call is plain
// peer-to-peer WebRTC that never touches the SFU and needs a TURN relay from
// Synapse's own config instead.
//
// On 2026-08-02 the whole SFU path was repaired and verified end to end, and
// calling still failed — because the calls being made were the other kind. The
// page was green about a component those calls never used. That is what this file
// exists to stop.

// TURNStatus says whether Synapse can offer a relay for legacy calls. Unknown is
// distinct from Absent on purpose: "we could not read the config" and "there is no
// relay" lead to different actions.
type TURNStatus string

const (
	TURNPresent TURNStatus = "present"
	TURNAbsent  TURNStatus = "absent"
	TURNUnknown TURNStatus = "unknown"
)

// CallPaths is the structured answer to "what can this deployment actually do?",
// rendered next to the findings so an operator sees both mechanisms at once rather
// than inferring the second one's absence from its silence.
type CallPaths struct {
	// ElementCall reports whether the SFU exists and exposes media ports. It says
	// nothing about reachability — that stays permanently unknown (E19).
	ElementCall bool       `json:"element_call"`
	TURN        TURNStatus `json:"turn"`
	// TURNURIs is what Synapse would hand to clients. Reported so a configured
	// relay can be checked rather than trusted.
	TURNURIs []string `json:"turn_uris,omitempty"`
}

// TURNFromConfig reads the merged Synapse configuration and reports whether a TURN
// relay is configured.
//
// files is the ConfigMap's data: filename → YAML. Synapse merges a config
// directory in lexical order with later files overriding earlier ones, so the keys
// are sorted and the last definition wins. Getting that backwards would report a
// relay that an override had removed.
//
// Parsing rather than searching for the string: a commented-out `# turn_uris:`
// must not read as configured, and `turn_uris: []` must not either — it is present
// and empty, which is exactly as relayless as absent.
func TURNFromConfig(files map[string]string) (TURNStatus, []string) {
	if len(files) == 0 {
		return TURNUnknown, nil
	}

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	status := TURNAbsent
	var uris []string
	parsedAny := false

	for _, name := range names {
		var doc map[string]any
		if err := yaml.Unmarshal([]byte(files[name]), &doc); err != nil {
			// One unparseable file does not invalidate the others. Synapse itself
			// would refuse to start on it, so it is not a state that survives long,
			// and failing the whole read here would turn a broken log_config into a
			// silent page.
			continue
		}
		parsedAny = true

		raw, present := doc["turn_uris"]
		if !present {
			continue
		}
		uris = asStrings(raw)
		if len(uris) > 0 {
			status = TURNPresent
		} else {
			// Explicitly set to nothing: a later file cleared it, or it was written
			// empty. Either way there is no relay.
			status = TURNAbsent
		}
	}

	if !parsedAny {
		return TURNUnknown, nil
	}
	return status, uris
}

// asStrings tolerates the shapes a hand-written config actually arrives in: a
// list, or a single URI written without the list syntax. Anything else yields
// nothing, which reads as "no relay" — the safe direction, since claiming a relay
// that does not work sends the operator looking somewhere else entirely.
func asStrings(raw any) []string {
	switch v := raw.(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	default:
		return nil
	}
}

// AssessCallPaths states which call path this deployment supports.
//
// The legacy finding warns rather than informs, and that is a deliberate departure
// from E23's "a quiet SFU is not a fault" rule. A quiet SFU is an absence of
// evidence; a missing relay is a measured property of the deployment that stays
// true until someone changes it. It is actionable — just not from inside this
// panel — so the action names all three steps instead of pointing at a manual.
func AssessCallPaths(paths CallPaths) Finding {
	switch paths.TURN {
	case TURNPresent:
		return Finding{
			Level: LevelOK,
			Title: "Beide Anrufwege haben ein Relay",
			Detail: "Element Call läuft über die SFU, klassische 1:1-Anrufe über TURN (" +
				join(paths.TURNURIs, ", ") + "). Ob das Relay von außen erreichbar ist, sagt das nicht.",
		}

	case TURNUnknown:
		return Finding{
			Level:  LevelUnknown,
			Title:  "Ob klassische 1:1-Anrufe ein Relay haben, ist unklar",
			Detail: "Die Synapse-Konfiguration konnte nicht gelesen werden. Das ist keine Aussage über Calling — nur darüber, dass gerade nichts festgestellt werden konnte.",
		}

	default:
		return Finding{
			Level: LevelWarn,
			Title: "Klassische 1:1-Anrufe haben kein Relay",
			Detail: "In Synapse ist kein turn_uris gesetzt. Ein klassischer 1:1-Anruf ist eine direkte " +
				"Peer-to-Peer-Verbindung und braucht ein TURN-Relay, sobald beide Seiten hinter NAT sitzen — bei Mobilfunk " +
				"(CGNAT) ist das der Normalfall, und der Anruf scheitert dann still auf beiden Seiten. " +
				"Nicht zu verwechseln mit dem TURN, den die SFU mitbringt: der gehört zu LiveKit, authentifiziert über " +
				"LiveKit-Tokens und steht deshalb ausschließlich Element Call zur Verfügung. Synapse braucht einen " +
				"eigenen mit der üblichen REST-Authentifizierung. Alles oberhalb dieser Zeile betrifft nur den anderen Weg.",
			Action: "TURN-Server (z. B. coturn) betreiben, dessen Ports weiterleiten (3478 udp+tcp, 5349 tcp, " +
				"Relay-Range udp) und turn_uris samt turn_shared_secret über synapse.additional eintragen. " +
				"Das ESS-Chart hat dafür keine eigene Option — der vorhandene turn-Schalter unter matrixRTC ist der von LiveKit.",
		}
	}
}
