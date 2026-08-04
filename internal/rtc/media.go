package rtc

import (
	"bufio"
	"strconv"
	"strings"
)

// MediaEvidence answers "has a call ever carried media through this SFU?" — which
// is a different question from "can it", and unlike that one, it is answerable from
// inside the cluster.
//
// Two days were spent asking whether calling worked. Every component reported on
// itself and none of them answered. These counters were on the SFU's own metrics
// port the whole time, and they move only when media actually flows.
type MediaEvidence struct {
	// RoomsCompleted counts rooms that opened and closed since the SFU started.
	RoomsCompleted int `json:"rooms_completed"`
	// QualitySamples and ForwardSamples are only ever incremented while media is
	// being carried. Either being above zero proves media flowed.
	QualitySamples int `json:"quality_samples"`
	ForwardSamples int `json:"forward_samples"`
	// PacketsOut is reported for context only. It rises without any call — it is
	// not evidence of a working session and must not be read as such.
	PacketsOut int `json:"packets_out"`
}

// MediaFlowed reports whether any sample exists. Two sources rather than one
// because a call can end before a quality sample is taken, but forwarding latency
// is recorded per forwarded packet.
func (m MediaEvidence) MediaFlowed() bool {
	return m.QualitySamples > 0 || m.ForwardSamples > 0
}

// metricsWanted maps the Prometheus metric name to where its value lands. Keeping
// it a table rather than a chain of ifs means adding a counter is one line, and it
// documents exactly which four numbers this package depends on.
var metricsWanted = map[string]func(*MediaEvidence, int){
	"livekit_room_duration_seconds_count": func(m *MediaEvidence, v int) { m.RoomsCompleted = v },
	"livekit_quality_score_count":         func(m *MediaEvidence, v int) { m.QualitySamples = v },
	"livekit_forward_latency_ns_count":    func(m *MediaEvidence, v int) { m.ForwardSamples = v },
}

// ParseMetrics reads the counters out of a Prometheus exposition body.
//
// Deliberately tolerant: an unknown line, a missing metric or a malformed value is
// skipped rather than failing the whole read. A LiveKit upgrade that renames one
// counter should cost that one number, not the page. What it must never do is
// invent a zero that looks like a measurement — an absent counter leaves the field
// at zero, and the caller distinguishes "zero rooms" from "no evidence" using
// RoomsCompleted, so a parse that finds nothing reports nothing rather than
// "no media flowed".
func ParseMetrics(body string) (MediaEvidence, bool) {
	var m MediaEvidence
	found := false

	sc := bufio.NewScanner(strings.NewReader(body))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		name, value, ok := splitMetric(line)
		if !ok {
			continue
		}
		if setter, want := metricsWanted[name]; want {
			setter(&m, value)
			found = true
			continue
		}
		// Packets out carries a label selecting the direction.
		if name == "livekit_node_packet_total" && strings.Contains(line, `type="out"`) {
			m.PacketsOut = value
			found = true
		}
	}
	return m, found
}

// splitMetric turns `name{labels} 42` or `name 42` into its name and integer value.
// Float exposition (`42.0`, `1.5e+04`) is truncated to an integer because every
// counter read here is a count; a fractional sample count is not a thing.
func splitMetric(line string) (string, int, bool) {
	sp := strings.LastIndex(line, " ")
	if sp < 0 {
		return "", 0, false
	}
	name := line[:sp]
	if brace := strings.IndexByte(name, '{'); brace >= 0 {
		// A label set that never closes means the line is truncated. Taking the
		// name anyway would let a half-delivered body set a real counter — and the
		// counter this reads decides whether the page says media flowed, so a
		// truncated read could produce a false all-clear.
		if !strings.HasSuffix(name, "}") {
			return "", 0, false
		}
		name = name[:brace]
	}

	raw := strings.TrimSpace(line[sp+1:])
	if v, err := strconv.Atoi(raw); err == nil {
		return name, v, true
	}
	if f, err := strconv.ParseFloat(raw, 64); err == nil {
		return name, int(f), true
	}
	return "", 0, false
}

// AssessMedia turns the counters into a finding.
//
// The distinction that matters: zero samples with zero rooms means **nobody
// called**, which is not a fault. Zero samples *after* rooms were created is one.
// Collapsing those would produce an alarm that fires on every quiet night, and an
// alarm that fires when nothing is wrong is switched off before it ever fires when
// something is.
func AssessMedia(ev MediaEvidence, ok bool, uptime string) Finding {
	if !ok {
		return Finding{
			Level:  LevelUnknown,
			Title:  "Ob je ein Anruf Medien übertragen hat, ist nicht abrufbar",
			Detail: "Die Metriken der SFU konnten nicht gelesen werden. Das ist keine Aussage über Calling — nur darüber, dass gerade nichts gemessen werden konnte.",
		}
	}

	switch {
	case ev.MediaFlowed():
		return Finding{
			Level: LevelOK,
			Title: "Es sind Medien geflossen",
			Detail: "Seit dem Start der SFU (" + uptime + ") wurden " +
				strconv.Itoa(ev.QualitySamples+ev.ForwardSamples) + " Medien-Messpunkte aufgezeichnet. " +
				"Der Medienweg funktioniert also mindestens für einen Teil der Clients.",
		}

	case ev.RoomsCompleted > 0:
		return Finding{
			Level: LevelWarn,
			Title: "Anrufe erreichen die SFU, aber es fließen keine Medien",
			Detail: "Seit dem Start der SFU (" + uptime + ") wurden " + strconv.Itoa(ev.RoomsCompleted) +
				" Räume eröffnet und wieder geschlossen, ohne dass ein einziges Mediensample entstanden ist. " +
				"Signalisierung und Token funktionieren damit — der Medienweg nicht.",
			Action: "Die Medienports von außen prüfen (UDP 30002 zuerst) und die angekündigte Adresse gegenprüfen. " +
				"Signalisierung läuft über 443 und beweist nichts über UDP.",
		}

	default:
		return Finding{
			Level: LevelUnknown,
			Title: "Seit dem Start der SFU hat noch niemand angerufen",
			Detail: "Keine Räume, keine Mediensamples seit " + uptime + ". Das ist kein Befund über den " +
				"Medienweg — es wurde schlicht nichts versucht.",
			Action: "Einen Testanruf führen; danach sagt diese Zeile, ob Medien angekommen sind.",
		}
	}
}
