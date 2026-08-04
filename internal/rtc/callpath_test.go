package rtc

import (
	"strings"
	"testing"
)

// The production ConfigMap of 2026-08-04, in shape: four files, not one, and not a
// single mention of TURN anywhere in them.
var productionConfig = map[string]string{
	"01-homeserver-underrides.yaml": "presence:\n  enabled: false\n",
	"04-homeserver-overrides.yaml":  "server_name: example.test\nreport_stats: false\n",
	"05-main.yaml":                  "listeners:\n  - port: 8008\n",
	"log_config.yaml":               "version: 1\n",
}

func TestProductionConfigHasNoRelay(t *testing.T) {
	status, uris := TURNFromConfig(productionConfig)
	if status != TURNAbsent {
		t.Fatalf("got %s, want absent", status)
	}
	if len(uris) != 0 {
		t.Errorf("expected no URIs, got %v", uris)
	}

	f := AssessCallPaths(CallPaths{ElementCall: true, TURN: status})
	if f.Level != LevelWarn {
		t.Fatalf("expected warn, got %s (%s)", f.Level, f.Title)
	}
	if f.Action == "" {
		t.Error("a warning with nothing to do about it is a shrug")
	}
	// The whole point of the finding is that the rest of the page is about the
	// other mechanism. If it does not say so, it is just one more green tick.
	if !strings.Contains(f.Detail, "SFU") {
		t.Error("the finding must relate itself to the SFU path the page otherwise reports on")
	}
	// The chart *does* have a `turn` switch — LiveKit's own, enabled by default.
	// An operator who reads "no TURN" here and then finds `turn.enabled: true` in
	// the values concludes the panel is broken. The finding has to name both.
	if !strings.Contains(f.Detail, "LiveKit") {
		t.Error("the finding must distinguish Synapse's relay from LiveKit's own, or it reads as contradicting the values")
	}
}

func TestConfiguredRelayReportsOK(t *testing.T) {
	files := map[string]string{
		"03-turn.yaml": "turn_uris:\n  - turn:relay.example.test:3478?transport=udp\n  - turn:relay.example.test:3478?transport=tcp\n",
	}
	status, uris := TURNFromConfig(files)
	if status != TURNPresent {
		t.Fatalf("got %s, want present", status)
	}
	if len(uris) != 2 {
		t.Fatalf("expected both URIs, got %v", uris)
	}
	if f := AssessCallPaths(CallPaths{TURN: status, TURNURIs: uris}); f.Level != LevelOK {
		t.Fatalf("expected ok, got %s", f.Level)
	}
}

// `turn_uris: []` is present and empty, which is exactly as relayless as absent.
// Reporting it as configured because the key exists would be the worst outcome
// available: a green tick on a deployment that cannot relay.
func TestEmptyListIsNotARelay(t *testing.T) {
	for _, body := range []string{"turn_uris: []\n", "turn_uris:\n", "turn_uris: \"\"\n"} {
		status, _ := TURNFromConfig(map[string]string{"03-turn.yaml": body})
		if status != TURNAbsent {
			t.Errorf("%q: got %s, want absent", body, status)
		}
	}
}

// A commented-out setting is not a setting. String matching would call this
// configured; parsing does not, which is why it parses.
func TestCommentedOutSettingIsNotARelay(t *testing.T) {
	files := map[string]string{"03-turn.yaml": "# turn_uris:\n#   - turn:relay.example.test:3478\npresence:\n  enabled: true\n"}
	if status, _ := TURNFromConfig(files); status != TURNAbsent {
		t.Fatalf("got %s, want absent", status)
	}
}

// Synapse merges its config directory in lexical order and later files win. A
// relay removed by an override must not be reported as present.
func TestLaterFileOverridesEarlier(t *testing.T) {
	files := map[string]string{
		"01-base.yaml":      "turn_uris:\n  - turn:old.example.test:3478\n",
		"04-overrides.yaml": "turn_uris: []\n",
	}
	if status, uris := TURNFromConfig(files); status != TURNAbsent {
		t.Fatalf("got %s (%v), want absent — the override cleared it", status, uris)
	}

	reversed := map[string]string{
		"01-base.yaml":      "turn_uris: []\n",
		"04-overrides.yaml": "turn_uris:\n  - turn:new.example.test:3478\n",
	}
	if status, _ := TURNFromConfig(reversed); status != TURNPresent {
		t.Fatalf("got %s, want present — the override set it", status)
	}
}

// A single URI written without list syntax is valid YAML and a plausible thing to
// hand-write. It is a relay.
func TestSingleURIWithoutListSyntax(t *testing.T) {
	status, uris := TURNFromConfig(map[string]string{"03-turn.yaml": "turn_uris: turn:relay.example.test:3478\n"})
	if status != TURNPresent || len(uris) != 1 {
		t.Fatalf("got %s %v, want one URI", status, uris)
	}
}

// An unreadable config must never render as "no relay": that looks identical to a
// real finding and sends the operator off to set up coturn they may already have.
func TestNoConfigIsUnknownNotAbsent(t *testing.T) {
	for _, files := range []map[string]string{nil, {}} {
		status, _ := TURNFromConfig(files)
		if status != TURNUnknown {
			t.Fatalf("got %s, want unknown", status)
		}
	}

	f := AssessCallPaths(CallPaths{TURN: TURNUnknown})
	if f.Level != LevelUnknown {
		t.Fatalf("got %s, want unknown", f.Level)
	}
	if f.Action != "" {
		t.Error("nothing was measured, so there is nothing to instruct")
	}
}

// One broken file must not silence the others — but a ConfigMap in which nothing
// parses is not evidence of anything.
func TestUnparseableFilesDegradeCorrectly(t *testing.T) {
	mixed := map[string]string{
		"01-broken.yaml": "\tthis: is: not: yaml: at all\n  - nope\n",
		"03-turn.yaml":   "turn_uris:\n  - turn:relay.example.test:3478\n",
	}
	if status, _ := TURNFromConfig(mixed); status != TURNPresent {
		t.Fatalf("got %s, want present — one broken file must not hide a real relay", status)
	}

	allBroken := map[string]string{"01-broken.yaml": "\tnot: yaml:\n  - at all\n"}
	if status, _ := TURNFromConfig(allBroken); status != TURNUnknown {
		t.Fatalf("got %s, want unknown — nothing was readable", status)
	}
}

// A YAML document that is a list, or a scalar, unmarshals into a nil map rather
// than failing. It must not panic and must not claim a relay.
func TestUnexpectedDocumentShapes(t *testing.T) {
	for _, body := range []string{"- a\n- b\n", "just a string\n", "42\n", "null\n", ""} {
		status, uris := TURNFromConfig(map[string]string{"01.yaml": body})
		if status == TURNPresent {
			t.Errorf("%q must not report a relay (uris=%v)", body, uris)
		}
	}
}
