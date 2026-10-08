// Package synmetrics reads what a Synapse process says about its own load and turns two
// readings into rates: how much of a core it uses, and roughly where that goes
// (etappe 118).
//
// "Roughly" is a finding, not modesty. Synapse accounts CPU time per HTTP endpoint and per
// background job, and on the production server on 2026-10-08 those together explained
// about 15 % of the process's CPU time; the rest is replication, the event loop and the
// database driver, which nothing labels. So the total per process is the number every
// judgement rests on, and the split is shown with its unattributed remainder rather than
// scaled up to look complete.
package synmetrics

import (
	"fmt"
	"io"
	"strings"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

// Snapshot is one reading of one process. Counters are cumulative since the process
// started; StartTime tells two readings of different lives apart.
type Snapshot struct {
	At        time.Time
	StartTime float64 // process_start_time_seconds
	CPU       float64 // process_cpu_seconds_total
	Memory    float64 // process_resident_memory_bytes

	// CPU seconds (user + system) per HTTP servlet and per background job.
	Servlets   map[string]float64
	Background map[string]float64

	// Federation queues — gauges, so they are read, not differenced.
	FedPendingDestinations float64
	FedPendingPDUs         float64
	InboundStaging         float64
}

// Parse reads Synapse's Prometheus text output.
func Parse(r io.Reader, at time.Time) (Snapshot, error) {
	var p expfmt.TextParser
	fams, err := p.TextToMetricFamilies(r)
	if err != nil {
		return Snapshot{}, fmt.Errorf("metrics: %w", err)
	}
	s := Snapshot{At: at, Servlets: map[string]float64{}, Background: map[string]float64{}}
	s.StartTime = single(fams["process_start_time_seconds"])
	s.CPU = single(fams["process_cpu_seconds_total"])
	s.Memory = single(fams["process_resident_memory_bytes"])
	if s.CPU == 0 && s.StartTime == 0 {
		return Snapshot{}, fmt.Errorf("metrics: no process counters — not a Synapse metrics endpoint")
	}
	for _, name := range []string{"synapse_http_server_response_ru_utime_seconds_total", "synapse_http_server_response_ru_stime_seconds_total"} {
		sumBy(fams[name], "servlet", s.Servlets)
	}
	for _, name := range []string{"synapse_background_process_ru_utime_seconds_total", "synapse_background_process_ru_stime_seconds_total"} {
		sumBy(fams[name], "name", s.Background)
	}
	s.FedPendingDestinations = total(fams["synapse_federation_transaction_queue_pending_destinations"])
	s.FedPendingPDUs = total(fams["synapse_federation_transaction_queue_pending_pdus"])
	s.InboundStaging = total(fams["synapse_federation_server_number_inbound_pdu_in_staging"])
	return s, nil
}

func value(m *dto.Metric) float64 {
	switch {
	case m.Counter != nil:
		return m.Counter.GetValue()
	case m.Gauge != nil:
		return m.Gauge.GetValue()
	case m.Untyped != nil:
		return m.Untyped.GetValue()
	}
	return 0
}

func single(f *dto.MetricFamily) float64 {
	if f == nil || len(f.Metric) == 0 {
		return 0
	}
	return value(f.Metric[0])
}

func total(f *dto.MetricFamily) float64 {
	if f == nil {
		return 0
	}
	var t float64
	for _, m := range f.Metric {
		t += value(m)
	}
	return t
}

func sumBy(f *dto.MetricFamily, label string, into map[string]float64) {
	if f == nil {
		return
	}
	for _, m := range f.Metric {
		key := ""
		for _, l := range m.Label {
			if l.GetName() == label {
				key = l.GetValue()
			}
		}
		into[key] += value(m)
	}
}

// Rate is what happened between two readings of the same process.
type Rate struct {
	Seconds float64            `json:"seconds"`
	Cores   float64            `json:"cores"`  // share of one CPU core, 1.0 = one core busy
	Memory  float64            `json:"memory"` // bytes, at the later reading
	Areas   map[string]float64 `json:"areas"`  // cores per area; the remainder is unattributed

	FedPendingDestinations float64 `json:"fed_pending_destinations"`
	FedPendingPDUs         float64 `json:"fed_pending_pdus"`
	InboundStaging         float64 `json:"inbound_staging"`
}

// Between computes the rate from a to b. It refuses — false — when the two readings are
// from different lives of the process (a restart resets every counter, and the
// difference would be a large negative number read as idle) or not in order.
func Between(a, b Snapshot) (Rate, bool) {
	secs := b.At.Sub(a.At).Seconds()
	if secs <= 0 || a.StartTime != b.StartTime || b.CPU < a.CPU {
		return Rate{}, false
	}
	r := Rate{
		Seconds: secs,
		Cores:   (b.CPU - a.CPU) / secs,
		Memory:  b.Memory,
		Areas:   map[string]float64{},

		FedPendingDestinations: b.FedPendingDestinations,
		FedPendingPDUs:         b.FedPendingPDUs,
		InboundStaging:         b.InboundStaging,
	}
	for name, v := range b.Servlets {
		if d := v - a.Servlets[name]; d > 0 {
			r.Areas[ServletArea(name)] += d / secs
		}
	}
	for name, v := range b.Background {
		if d := v - a.Background[name]; d > 0 {
			r.Areas[BackgroundArea(name)] += d / secs
		}
	}
	return r, true
}

// Lifetime is the average since the process started, from one reading. It is what can be
// said at once, before the sampler has a history — an average, so it hides peaks, and
// it is labelled that way wherever it is shown.
func Lifetime(s Snapshot) (Rate, bool) {
	secs := float64(s.At.Unix()) - s.StartTime
	if s.StartTime == 0 || secs < 60 {
		return Rate{}, false
	}
	r := Rate{Seconds: secs, Cores: s.CPU / secs, Memory: s.Memory, Areas: map[string]float64{},
		FedPendingDestinations: s.FedPendingDestinations, FedPendingPDUs: s.FedPendingPDUs, InboundStaging: s.InboundStaging}
	for name, v := range s.Servlets {
		r.Areas[ServletArea(name)] += v / secs
	}
	for name, v := range s.Background {
		r.Areas[BackgroundArea(name)] += v / secs
	}
	return r, true
}

// Area is a part of Synapse's work, and the worker type that would take it over.
type Area struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Worker string `json:"worker,omitempty"` // "" — no worker takes this
}

// Areas in display order. The worker names are ESS's (`synapse.workers.<name>`).
var Areas = []Area{
	{"sync", "Sync — Apps holen Neuigkeiten ab", "synchrotron"},
	{"sliding-sync", "Sliding Sync — neue Element-Apps", "sliding-sync"},
	{"federation-in", "Föderation empfangen", "federation-inbound"},
	{"federation-read", "Föderation beantworten — Abfragen anderer Server", "federation-reader"},
	{"federation-out", "Föderation senden", "federation-sender"},
	{"media", "Medien — Bilder, Dateien, Vorschauen", "media-repository"},
	{"send", "Nachrichten und Raumänderungen senden", "event-creator"},
	{"read", "Verlauf, Mitglieder und Profile lesen", "client-reader"},
	{"keys", "Verschlüsselung — Schlüssel austauschen", "encryption"},
	{"receipts", "Lesebestätigungen", "receipts"},
	{"typing", "Tipp-Anzeige", "typing-persister"},
	{"account-data", "Kontodaten der Apps", "account-data"},
	{"user-dir", "Nutzerverzeichnis durchsuchen", "user-dir"},
	{"push", "Push-Benachrichtigungen", "pusher"},
	{"background", "Hintergrundaufgaben", "background"},
	{"health", "Healthchecks von Kubernetes", ""},
	{"other", "Sonstige Endpunkte, auch die Verwaltung", ""},
}

// AreaByID looks an area up; unknown IDs come back as "other".
func AreaByID(id string) Area {
	for _, a := range Areas {
		if a.ID == id {
			return a
		}
	}
	return Areas[len(Areas)-1]
}

// servletAreas maps Synapse's servlet class names, as they appear in the `servlet`
// label, onto areas. Exact names first; the prefix rules below catch the families.
var servletAreas = map[string]string{
	"SyncRestServlet":            "sync",
	"EventStreamRestServlet":     "sync",
	"InitialSyncRestServlet":     "sync",
	"RoomInitialSyncRestServlet": "sync",
	"SlidingSyncRestServlet":     "sliding-sync",

	// PUT /_matrix/federation/v1/send/{txn}: other servers delivering to this one.
	"FederationSendServlet": "federation-in",

	"ThumbnailResource":   "media",
	"DownloadResource":    "media",
	"UploadResource":      "media",
	"CreateResource":      "media",
	"PreviewUrlResource":  "media",
	"MediaConfigResource": "media",

	"RoomSendEventRestServlet":   "send",
	"RoomStateEventRestServlet":  "send",
	"RoomRedactEventRestServlet": "send",
	"RoomMembershipRestServlet":  "send",
	"JoinRoomAliasServlet":       "send",
	"RoomCreateRestServlet":      "send",
	"SendDelayedEventServlet":    "send",
	"RestartDelayedEventServlet": "send",

	"RoomMessageListRestServlet":      "read",
	"RoomEventServlet":                "read",
	"RoomEventContextServlet":         "read",
	"RelationPaginationServlet":       "read",
	"RoomMemberListRestServlet":       "read",
	"JoinedRoomMemberListRestServlet": "read",
	"ProfileRestServlet":              "read",
	"ProfileFieldRestServlet":         "read",
	"RoomHierarchyRestServlet":        "read",
	"PublicRoomListRestServlet":       "read",
	"RoomStateRestServlet":            "read",
	"ThreadsServlet":                  "read",
	"SearchRestServlet":               "read",
	"UserDirectorySearchRestServlet":  "user-dir",
	"KeyQueryServlet":                 "keys",
	"KeyUploadServlet":                "keys",
	"OneTimeKeyServlet":               "keys",
	"KeyClaimServlet":                 "keys",
	"KeyChangesServlet":               "keys",
	"SigningKeyUploadServlet":         "keys",
	"SignaturesUploadServlet":         "keys",
	"RoomKeysServlet":                 "keys",
	"RoomKeysVersionServlet":          "keys",
	"RoomKeysNewVersionServlet":       "keys",
	"SendToDeviceRestServlet":         "keys",
	"ReceiptRestServlet":              "receipts",
	"ReadMarkerRestServlet":           "receipts",
	"RoomTypingRestServlet":           "typing",
	"AccountDataServlet":              "account-data",
	"RoomAccountDataServlet":          "account-data",
	"PushersSetRestServlet":           "push",
	"PushersRestServlet":              "push",
	"PushRuleRestServlet":             "push",
	"HealthResource":                  "health",
}

// ServletArea names the area a servlet belongs to.
func ServletArea(servlet string) string {
	if a, ok := servletAreas[servlet]; ok {
		return a
	}
	switch {
	// Every other federation endpoint is another server asking something: keys,
	// devices, state, backfill, joins.
	case strings.HasPrefix(servlet, "Federation"), strings.Contains(servlet, "KeyResource"),
		servlet == "LocalKey", servlet == "RemoteKey":
		return "federation-read"
	case strings.Contains(servlet, "Thumbnail"), strings.Contains(servlet, "Download"),
		strings.Contains(servlet, "Upload") && !strings.Contains(servlet, "Key"):
		return "media"
	}
	return "other"
}

// BackgroundArea names the area a background job belongs to.
func BackgroundArea(name string) string {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "federation_transaction"), strings.Contains(n, "wake_destinations"),
		strings.Contains(n, "catch_up"), strings.Contains(n, "catchup"):
		return "federation-out"
	case strings.Contains(n, "incoming_pdu"), strings.Contains(n, "federation_staging"),
		strings.Contains(n, "federation_inbox"):
		return "federation-in"
	case strings.Contains(n, "media"), strings.Contains(n, "url_cache"):
		return "media"
	case strings.Contains(n, "httppush"), strings.Contains(n, "pusher"):
		return "push"
	}
	return "background"
}
