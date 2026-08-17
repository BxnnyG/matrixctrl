package rtc

import (
	"fmt"
	"strconv"
	"strings"
)

// The UDP receive buffer the SFU asked for and did not get (etappe 51).
//
// LiveKit logs this once at startup and then never again:
//
//	WARN livekit rtcconfig/rtc_unix.go:31 UDP receive buffer is too small for a
//	     production set-up  {"current": 425984, "suggested": 5000000}
//
// Both numbers are taken from that line rather than from `net.core.rmem_max`, for
// two reasons. The sysctl on this node reads 212992 while LiveKit reports 425984 —
// exactly double, because SO_RCVBUF accounting returns twice what was set — so the
// two sources disagree by construction and the operator's own log shows LiveKit's.
// And the sysctl is network-namespaced: MatrixCtrl does not run with hostNetwork and
// the SFU does, so a value read here is not necessarily the value the SFU got.

// UDPBuffer is what LiveKit reported about its receive buffer.
type UDPBuffer struct {
	// Current is the buffer LiveKit actually has, in bytes, as LiveKit reports it.
	Current int64 `json:"current"`
	// Suggested is what LiveKit wants for a production setup.
	Suggested int64 `json:"suggested"`
}

// Undersized reports whether LiveKit considers the buffer too small. It logs the
// warning only in that case, so a parsed line always means yes — the method exists so
// callers state the question rather than relying on that.
func (b UDPBuffer) Undersized() bool { return b.Current < b.Suggested }

// ParseBufferWarning finds LiveKit's startup warning in a chunk of pod log.
//
// Not found is a real answer and must not be read as "the buffer is fine": LiveKit
// logs this once at startup, so a long or rotated log legitimately no longer contains
// it. The caller turns that into LevelUnknown.
func ParseBufferWarning(podLog string) (UDPBuffer, bool) {
	for _, line := range strings.Split(podLog, "\n") {
		if !strings.Contains(line, "UDP receive buffer is too small") {
			continue
		}
		cur, okCur := jsonInt(line, "current")
		sug, okSug := jsonInt(line, "suggested")
		if !okCur || !okSug {
			// The line is there but its shape changed with a LiveKit version. Half a
			// reading is worse than none — reporting current without suggested would
			// render a comparison against zero.
			return UDPBuffer{}, false
		}
		return UDPBuffer{Current: cur, Suggested: sug}, true
	}
	return UDPBuffer{}, false
}

// jsonInt pulls `"key": 123` out of the structured tail of a log line without
// unmarshalling it: the line is a log record with a JSON *fragment* appended, not a
// JSON document, so a parser would have to be told where the object starts and that
// offset is the fragile part.
func jsonInt(line, key string) (int64, bool) {
	i := strings.Index(line, `"`+key+`"`)
	if i < 0 {
		return 0, false
	}
	rest := line[i+len(key)+2:]
	rest = strings.TrimLeft(rest, ": \t")
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, false
	}
	v, err := strconv.ParseInt(rest[:end], 10, 64)
	return v, err == nil
}

// AssessUDPBuffer turns the two readings into one finding.
//
// A separate entry point rather than another parameter on Assess: the inputs come
// from a different source (the SFU's pod log and its metrics, not the Services), and
// a caller that has neither should be able to omit the finding rather than pass
// zeroes that would be assessed as if measured.
//
// dropped is livekit_node_packet_total{type="dropped"} — the SFU's own count, taken
// from its own network namespace. Reading /proc/net/snmp here instead would count
// MatrixCtrl's traffic: measured at 320 datagrams against the node's 48009, so a drop
// counter from this process would read zero forever whatever the SFU experienced.
func AssessUDPBuffer(buf UDPBuffer, found bool, dropped int, droppedKnown bool) Finding {
	if !found {
		return Finding{
			Level: LevelUnknown,
			Title: "UDP-Puffergröße der SFU unbekannt",
			Detail: "LiveKit meldet die Größe seines Empfangspuffers nur einmal beim Start. " +
				"Im vorliegenden Ausschnitt des Pod-Logs steht diese Zeile nicht — das heißt nicht, " +
				"dass der Puffer ausreicht, sondern nur, dass er hier nicht abgelesen werden konnte.",
			Action: "Nach einem Neustart der SFU erneut prüfen; die Zeile steht dann am Anfang des Logs.",
		}
	}

	if !buf.Undersized() {
		return Finding{
			Level: LevelOK,
			Title: "UDP-Empfangspuffer ausreichend",
			Detail: fmt.Sprintf("LiveKit meldet %s, empfohlen sind %s.",
				bytesShort(buf.Current), bytesShort(buf.Suggested)),
		}
	}

	detail := fmt.Sprintf(
		"LiveKit hat beim Start %s bekommen und hält %s für einen Produktivbetrieb nötig. "+
			"Der Kernel verdoppelt den gesetzten Wert für die Verwaltung, der zugrunde liegende "+
			"net.core.rmem_max ist also halb so groß wie die hier genannte Zahl.",
		bytesShort(buf.Current), bytesShort(buf.Suggested))

	// The half that keeps this from reading as an active fault. A warning about a
	// condition that is not currently costing anything has to say so, or it sends
	// someone hunting a problem that is not happening.
	switch {
	case droppedKnown && dropped == 0:
		detail += " Verworfen wurde bisher nichts (die SFU zählt 0 verworfene Pakete), " +
			"das ist also eine Warnung über die Belastbarkeit unter Last, kein aktueller Fehler."
	case droppedKnown && dropped > 0:
		detail += fmt.Sprintf(" Die SFU zählt bereits %d verworfene Pakete — der Puffer kostet "+
			"hier also bereits Medienqualität.", dropped)
	default:
		detail += " Ob deshalb schon Pakete verworfen wurden, war nicht abzulesen."
	}

	return Finding{
		Level: LevelWarn,
		Title: "Der UDP-Empfangspuffer ist kleiner als die SFU anfordert",
		Detail: detail,
		// Explicitly a host-level change: MatrixCtrl has no privileged access to the
		// node and this is one of the things it deliberately cannot do for you.
		Action: "Auf dem Node setzen und dauerhaft hinterlegen: " +
			"`sysctl -w net.core.rmem_max=5000000` sowie ein Eintrag in /etc/sysctl.d/. " +
			"MatrixCtrl kann das nicht selbst ändern — es ist eine Kernel-Einstellung des Hosts, " +
			"außerhalb des Clusters.",
	}
}

// bytesShort renders a byte count the way the operator will compare it against a
// sysctl value: the exact number, with a MiB hint for the ones that are hard to read.
func bytesShort(n int64) string {
	if n >= 1<<20 {
		return fmt.Sprintf("%d Bytes (~%.1f MiB)", n, float64(n)/(1<<20))
	}
	return fmt.Sprintf("%d Bytes (~%d KiB)", n, n/1024)
}
