package helm

import "testing"

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want string // "newer" | "older" | "equal"
	}{
		// A plain string sort ranked 26.5.1 above 26.10.0, hiding newer releases.
		{"26.10.0", "26.5.1", "newer"},
		{"26.5.1", "26.10.0", "older"},
		{"0.10.0", "0.9.0", "newer"},
		{"26.7.2", "26.7.1", "newer"},
		{"26.7.0", "26.6.2", "newer"},
		{"26.5.1", "26.5.1", "equal"},
		{"v26.5.1", "26.5.1", "equal"},
		// A final release outranks its own prereleases.
		{"1.0.0", "1.0.0-rc.1", "newer"},
		{"1.0.0-rc.1", "1.0.0", "older"},
	}
	for _, c := range cases {
		got := compareVersions(c.a, c.b)
		var label string
		switch {
		case got > 0:
			label = "newer"
		case got < 0:
			label = "older"
		default:
			label = "equal"
		}
		if label != c.want {
			t.Errorf("compareVersions(%q, %q) = %s, want %s", c.a, c.b, label, c.want)
		}
	}
}

func TestParseReleaseTag(t *testing.T) {
	cases := []struct {
		tag        string
		ok         bool
		prerelease bool
	}{
		{"26.5.1", true, false},
		{"v26.5.1", true, false},
		{"26.5.1-rc.1", true, true},
		{"26.5.1-beta.2", true, true},
		// Per-commit build tags must never appear as upgrade targets.
		{"26.7.3-shaee15011deaf0f883aeb6ec8e4179ef96aa7d3844", false, false},
		{"0.2.1-sha7e8aed427374dd19f83d2ec7391832ac1f1b1cc7", false, false},
		{"latest", false, false},
		{"26.5", false, false},
		// The older build-tag convention. All twelve suffixed tags the registry
		// holds are of this shape, and none of them is an upgrade target — see
		// devTagRe. The real ones from the registry, not invented examples:
		{"0.0.0-dev", false, false},
		{"0.7.2-dev", false, false},
		// `-dev` is matched whole, so a genuine pre-release that merely starts with
		// those letters survives. Nothing hinges on it today; the point is that the
		// rule is "the suffix is exactly dev", not "the suffix contains dev".
		{"26.9.0-dev.1", true, true},
		{"26.9.0-developer", true, true},
	}
	for _, c := range cases {
		v, ok := parseReleaseTag(c.tag)
		if ok != c.ok {
			t.Errorf("parseReleaseTag(%q) ok = %v, want %v", c.tag, ok, c.ok)
			continue
		}
		if ok && v.Prerelease != c.prerelease {
			t.Errorf("parseReleaseTag(%q) prerelease = %v, want %v", c.tag, v.Prerelease, c.prerelease)
		}
	}
}

func TestNextPageURL(t *testing.T) {
	link := `</v2/element-hq/ess-helm/matrix-stack/tags/list?last=0.10.1-shac00c&n=1000>; rel="next"`
	got := nextPageURL(link)
	want := "https://ghcr.io/v2/element-hq/ess-helm/matrix-stack/tags/list?last=0.10.1-shac00c&n=1000"
	if got != want {
		t.Errorf("nextPageURL() = %q, want %q", got, want)
	}
	if got := nextPageURL(""); got != "" {
		t.Errorf("nextPageURL(\"\") = %q, want empty", got)
	}
}
