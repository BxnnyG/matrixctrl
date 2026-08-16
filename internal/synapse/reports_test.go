package synapse

import (
	"encoding/json"
	"strings"
	"testing"
)

// Synapse's report object carries two user IDs: `user_id` is whoever filed the
// report, `sender` is whoever sent the reported event. Swapping them accuses the
// wrong person, and the names give no hint which is which — this test is the
// translation's proof.
func TestReporterAndSenderAreNotSwapped(t *testing.T) {
	var wire reportWire
	raw := `{
		"id": 7,
		"event_id": "$abc",
		"room_id": "!room:example.org",
		"user_id": "@complainant:example.org",
		"sender": "@accused:example.org",
		"reason": "spam",
		"received_ts": 1770000000000
	}`
	if err := json.Unmarshal([]byte(raw), &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := wire.toReport()

	if got.Reporter != "@complainant:example.org" {
		t.Errorf("Reporter = %q, want the account that filed it (Synapse's user_id)", got.Reporter)
	}
	if got.Sender != "@accused:example.org" {
		t.Errorf("Sender = %q, want the reported event's author (Synapse's sender)", got.Sender)
	}
}

func TestReportOptionsDefaults(t *testing.T) {
	q := ReportOptions{}.query()
	// Newest first, unlike Synapse's own default: a moderation queue is read from
	// the top.
	if !strings.Contains(q, "dir=b") {
		t.Errorf("query = %q, want dir=b", q)
	}
	if !strings.Contains(q, "limit=50") {
		t.Errorf("query = %q, want a bounded default limit", q)
	}
	// from=0 is the first page and is Synapse's own default, so sending it is noise.
	if strings.Contains(q, "from=") {
		t.Errorf("query = %q, should not send from=0", q)
	}

	over := ReportOptions{Limit: 5000}.query()
	if !strings.Contains(over, "limit=50") {
		t.Errorf("an unbounded limit must be clamped: %q", over)
	}
}

func TestListReportsNeverReturnsNilSlice(t *testing.T) {
	// A nil slice reaches the frontend as `null`, where every .map on it crashes the
	// page — the same defect E41 fixed for room members.
	var wire struct {
		EventReports []reportWire `json:"event_reports"`
	}
	if err := json.Unmarshal([]byte(`{"event_reports": []}`), &wire); err != nil {
		t.Fatal(err)
	}
	page := &ReportPage{Reports: make([]Report, 0, len(wire.EventReports))}
	if page.Reports == nil {
		t.Error("an empty queue must marshal as [], not null")
	}
	out, _ := json.Marshal(page)
	if !strings.Contains(string(out), `"reports":[]`) {
		t.Errorf("marshalled empty page = %s", out)
	}
}

func TestReportedBodyAndType(t *testing.T) {
	msg := json.RawMessage(`{"type":"m.room.message","content":{"body":"hello"}}`)
	if got := ReportedBody(msg); got != "hello" {
		t.Errorf("body = %q", got)
	}
	if got := ReportedEventType(msg); got != "m.room.message" {
		t.Errorf("type = %q", got)
	}

	// An encrypted event has no readable body, and that is the answer — not an
	// error, and not something to paper over with an invented description.
	enc := json.RawMessage(`{"type":"m.room.encrypted","content":{"ciphertext":"..."}}`)
	if got := ReportedBody(enc); got != "" {
		t.Errorf("encrypted body = %q, want empty", got)
	}
	if got := ReportedEventType(enc); got != "m.room.encrypted" {
		t.Errorf("encrypted type = %q", got)
	}

	// A non-string body (some clients send objects) must not panic or half-decode.
	odd := json.RawMessage(`{"type":"m.reaction","content":{"body":{"nested":true}}}`)
	if got := ReportedBody(odd); got != "" {
		t.Errorf("non-string body = %q, want empty", got)
	}
	if got := ReportedBody(nil); got != "" {
		t.Errorf("nil = %q", got)
	}
}

func TestValidStateRefusesOpen(t *testing.T) {
	// "open" is the *absence* of a decision, expressed by the absence of a row.
	// Accepting it as a stored value would create two ways to say the same thing.
	if ValidState(StateOpen) {
		t.Error("open must not be writable as a disposition")
	}
	if !ValidState(StateHandled) || !ValidState(StateDismissed) {
		t.Error("handled and dismissed must both be writable")
	}
	if ValidState("resolved") {
		t.Error("unknown states must be refused")
	}
}
