package synapse

import (
	"encoding/json"
	"testing"
)

func TestParseMXC(t *testing.T) {
	cases := []struct {
		in         string
		ok         bool
		server, id string
	}{
		{"mxc://example.org/AbCdEf123", true, "example.org", "AbCdEf123"},
		{"mxc://example.org/a", true, "example.org", "a"},
		// Everything below must be refused rather than half-parsed: each becomes a
		// URL path segment, where a stray slash addresses a different resource.
		{"https://example.org/media/x", false, "", ""},
		{"mxc://example.org", false, "", ""},
		{"mxc://example.org/", false, "", ""},
		{"mxc:///abc", false, "", ""},
		{"mxc://example.org/a/b", false, "", ""},
		{"mxc://example.org/a?x=1", false, "", ""},
		{"mxc://exa/mple.org/abc", false, "", ""},
		{"", false, "", ""},
	}
	for _, c := range cases {
		ref, ok := parseMXC(c.in)
		if ok != c.ok {
			t.Errorf("parseMXC(%q) ok = %v, want %v", c.in, ok, c.ok)
			continue
		}
		if ok && (ref.Server != c.server || ref.ID != c.id) {
			t.Errorf("parseMXC(%q) = %q/%q, want %q/%q", c.in, ref.Server, ref.ID, c.server, c.id)
		}
	}
}

func TestMediaInEventReadsAllThreeSites(t *testing.T) {
	ev := json.RawMessage(`{
		"type": "m.room.message",
		"content": {
			"url": "mxc://example.org/full",
			"info": {"thumbnail_url": "mxc://example.org/thumb"}
		}
	}`)
	got := MediaInEvent(ev)
	if len(got) != 2 {
		t.Fatalf("got %d refs, want 2: %+v", len(got), got)
	}
	// The kinds matter: quarantining a thumbnail while the full image stays
	// available is not what an admin meant to do.
	kinds := map[string]string{}
	for _, r := range got {
		kinds[r.Kind] = r.ID
	}
	if kinds["media"] != "full" || kinds["thumbnail"] != "thumb" {
		t.Errorf("kinds = %+v", kinds)
	}
}

func TestMediaInEventEncryptedSite(t *testing.T) {
	ev := json.RawMessage(`{"type":"m.room.message","content":{"file":{"url":"mxc://example.org/enc"}}}`)
	got := MediaInEvent(ev)
	if len(got) != 1 || got[0].ID != "enc" || got[0].Kind != "encrypted" {
		t.Errorf("got %+v, want one encrypted ref", got)
	}
}

func TestMediaInEventDeduplicates(t *testing.T) {
	// A thumbnail_url equal to the url is legal and common enough; it must not
	// produce two rows that an admin then quarantines twice.
	ev := json.RawMessage(`{"content":{"url":"mxc://example.org/x","info":{"thumbnail_url":"mxc://example.org/x"}}}`)
	if got := MediaInEvent(ev); len(got) != 1 {
		t.Errorf("got %d refs, want 1: %+v", len(got), got)
	}
}

func TestMediaInEventNoMedia(t *testing.T) {
	// The common case, and it must read as "no media" rather than as a parse
	// failure — the screen renders those differently.
	for _, raw := range []string{
		`{"type":"m.room.message","content":{"body":"just text"}}`,
		`{"type":"m.room.encrypted","content":{"ciphertext":"..."}}`,
		`not json`,
		``,
	} {
		if got := MediaInEvent(json.RawMessage(raw)); len(got) != 0 {
			t.Errorf("MediaInEvent(%q) = %+v, want none", raw, got)
		}
	}
}

func TestMXCRoundTrip(t *testing.T) {
	ref, ok := parseMXC("mxc://example.org/abc")
	if !ok || ref.MXC() != "mxc://example.org/abc" {
		t.Errorf("round trip = %q", ref.MXC())
	}
}

// Quarantined() is what every "did it work?" decision reads, and Synapse expresses
// "not quarantined" as an absent/empty string rather than a boolean.
func TestQuarantinedFromQuarantinedBy(t *testing.T) {
	if (MediaInfo{}).Quarantined() {
		t.Error("empty quarantined_by means not quarantined")
	}
	if !(MediaInfo{QuarantinedBy: "@admin:example.org"}).Quarantined() {
		t.Error("a non-empty quarantined_by means quarantined")
	}
}
