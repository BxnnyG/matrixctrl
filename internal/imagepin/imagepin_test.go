package imagepin

import (
	"strings"
	"testing"
)

// The real state of the operator's config against chart 26.8.0 on 2026-08-05. The
// MAS row is the one that made the upgrade fail; the others had been quietly
// holding components back for weeks.
func TestTheRealMismatch(t *testing.T) {
	config := map[string]string{
		"matrixAuthenticationService": "1.15.0",
		"elementWeb":                  "v1.12.14",
		"elementAdmin":                "0.1.11",
		"matrixRTC":                   "0.4.4",
		"synapse":                     "v1.151.0-ess.1",
	}
	chart := map[string]string{
		"matrixAuthenticationService": "1.22.0",
		"elementWeb":                  "v1.12.25",
		"elementAdmin":                "0.1.12",
		"matrixRTC":                   "0.4.4",
		"synapse":                     "v1.151.0-ess.1",
	}

	got := Compare(config, chart)
	if len(got) != 3 {
		t.Fatalf("expected 3 findings, got %+v", got)
	}
	// Sorted, so the report does not reshuffle between runs.
	if got[0].Component != "elementAdmin" || got[2].Component != "matrixAuthenticationService" {
		t.Fatalf("unsorted: %+v", got)
	}

	line := Describe(got)
	for _, want := range []string{"1.15.0", "1.22.0", "matrixAuthenticationService"} {
		if !strings.Contains(line, want) {
			t.Errorf("line missing %q: %s", want, line)
		}
	}
}

// Equal is the normal state and must be silent, or the warning fires on every
// healthy deployment and gets ignored.
func TestEqualIsSilent(t *testing.T) {
	got := Compare(map[string]string{"a": "1.2.3"}, map[string]string{"a": "1.2.3"})
	if len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
	if Describe(nil) != "" {
		t.Fatal("nothing to report should render as nothing")
	}
}

// An operator running ahead of the chart made a decision. Telling them it is a
// mistake is how a check loses its credibility.
func TestNewerThanChartIsNotReported(t *testing.T) {
	got := Compare(map[string]string{"a": "2.0.0"}, map[string]string{"a": "1.9.9"})
	if len(got) != 0 {
		t.Fatalf("running ahead is not a finding: %+v", got)
	}
}

// A wrong "you are behind" costs an upgrade nobody needed. Anything not confidently
// orderable is left alone.
func TestUncomparableTagsAreLeftAlone(t *testing.T) {
	cases := []struct{ cfg, chart string }{
		{"latest", "1.2.3"},
		{"sha256:abc", "1.2.3"},
		{"main", "1.2.3"},
		{"v1.2.3", "1.2.4"},   // different prefix scheme
		{"1.2.3", "v1.2.4"},   // ditto, the other way
		{"", "1.2.3"},         // absent in config
		{"1.2.3", ""},         // absent in chart
		{"2026-01-01", "1.2"}, // a date is not a semver
	}
	for _, c := range cases {
		if got := Compare(map[string]string{"a": c.cfg}, map[string]string{"a": c.chart}); len(got) != 0 {
			t.Errorf("cfg=%q chart=%q should be silent, got %+v", c.cfg, c.chart, got)
		}
	}
}

// A component the chart does not know is not a finding — it may be something the
// operator added.
func TestUnknownComponentIsIgnored(t *testing.T) {
	if got := Compare(map[string]string{"mything": "1.0.0"}, map[string]string{"a": "1.0.0"}); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestVersionOrdering(t *testing.T) {
	cases := []struct {
		a, b           string
		older, compare bool
	}{
		{"1.15.0", "1.22.0", true, true},
		{"1.22.0", "1.15.0", false, true},
		{"v1.12.14", "v1.12.25", true, true},
		{"0.1.11", "0.1.12", true, true},
		{"1.2", "1.2.1", true, true},     // shorter is older when the prefix matches
		{"1.10.0", "1.9.0", false, true}, // numeric, not lexical: 10 > 9
		{"v1.151.0-ess.1", "v1.151.0-ess.2", false, true},
		{"latest", "1.0.0", false, false},
	}
	for _, c := range cases {
		older, comparable := isOlder(c.a, c.b)
		if older != c.older || comparable != c.compare {
			t.Errorf("%s vs %s: older=%v comparable=%v, want %v/%v", c.a, c.b, older, comparable, c.older, c.compare)
		}
	}
}

// A string sort would rank 1.9.0 above 1.10.0 — the same class of bug that once
// made the ESS version list show ancient builds.
func TestNumericNotLexical(t *testing.T) {
	got := Compare(map[string]string{"a": "1.9.0"}, map[string]string{"a": "1.10.0"})
	if len(got) != 1 {
		t.Fatalf("1.9.0 is older than 1.10.0: %+v", got)
	}
}

func TestNoInput(t *testing.T) {
	if got := Compare(nil, nil); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestExtractTags(t *testing.T) {
	values := map[string]interface{}{
		"matrixAuthenticationService": map[string]interface{}{
			"image": map[string]interface{}{"registry": "ghcr.io", "tag": "1.15.0"},
		},
		"postgres": map[string]interface{}{
			// A nested image belongs to a component rather than being one; listing
			// it beside the components an operator recognises makes the report long
			// instead of actionable.
			"postgresExporter": map[string]interface{}{
				"image": map[string]interface{}{"tag": "v0.18.1"},
			},
		},
		"serverName":   "example.test", // not a section
		"emptySection": map[string]interface{}{},
		"noTag":        map[string]interface{}{"image": map[string]interface{}{"registry": "x"}},
	}
	got := ExtractTags(values)
	if len(got) != 1 || got["matrixAuthenticationService"] != "1.15.0" {
		t.Fatalf("got %+v", got)
	}
}

// A diagnostic that cannot conjugate reads as one nobody maintains — badly timed,
// since it appears at the moment an upgrade is about to go wrong.
func TestDescribeAgreesInNumber(t *testing.T) {
	one := Describe([]Finding{{Component: "a", Config: "1.0.0", Chart: "1.1.0"}})
	if !strings.Contains(one, "1 Image-Tag in der Config ist älter") {
		t.Errorf("singular: %s", one)
	}
	if !strings.Contains(one, "Diese Komponente wird") {
		t.Errorf("singular tail: %s", one)
	}

	two := Describe([]Finding{
		{Component: "a", Config: "1.0.0", Chart: "1.1.0"},
		{Component: "b", Config: "2.0.0", Chart: "2.1.0"},
	})
	if !strings.Contains(two, "2 Image-Tags in der Config sind älter") {
		t.Errorf("plural: %s", two)
	}
	// Asserted whole, and by suffix. The earlier version checked
	// Contains(…, "Diese Komponenten werden"), which is a prefix of the broken
	// "Diese Komponenten werden wird vom Upgrade nicht mit aktualisiert." — so the
	// bug shipped past a test that reads as though it covers exactly this (E43).
	const wantTail = "Diese Komponenten werden vom Upgrade nicht mit aktualisiert."
	if !strings.HasSuffix(two, wantTail) {
		t.Errorf("plural tail: got %q, want suffix %q", two, wantTail)
	}
}
