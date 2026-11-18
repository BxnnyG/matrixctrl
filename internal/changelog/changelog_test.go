package changelog

import "testing"

const md = `# Changelog

## [Unreleased]

nothing yet

## [0.1.130] — 2026-11-20

### Added
- thirty

## [0.1.129] — 2026-11-17

- twentynine

## [0.1.128] — 2026-11-17

- twentyeight

## [0.1.127] — 2026-11-12

- twentyseven
`

func versions(s []Section) string {
	out := ""
	for _, x := range s {
		out += x.Version + " "
	}
	return out
}

func TestSince(t *testing.T) {
	cases := []struct{ name, since, cur, want string }{
		{"skipped versions", "0.1.127", "0.1.130", "0.1.130 0.1.129 0.1.128 "},
		{"rc counts as target", "0.1.129", "0.1.130-rc.2", "0.1.130 "},
		{"same version", "0.1.130", "0.1.130", ""},
		{"since empty", "", "0.1.129", "0.1.129 "},
		{"since garbage", "abc", "0.1.129", "0.1.129 "},
		{"dev build", "0.1.127", "dev", ""},
		{"current older than file", "0.1.127", "0.1.128", "0.1.128 "},
		{"v prefix", "v0.1.128", "v0.1.129", "0.1.129 "},
	}
	for _, c := range cases {
		if got := versions(Since(md, c.since, c.cur)); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
	s := Since(md, "0.1.128", "0.1.129")
	if len(s) != 1 || s[0].Date != "2026-11-17" || s[0].Body != "- twentynine" {
		t.Errorf("section content: %+v", s)
	}
	if Since(md, "", "dev") == nil {
		t.Error("must not be nil")
	}
}
