// Package changelog cuts the sections of CHANGELOG.md a "what's new" dialog shows.
package changelog

import (
	"regexp"
	"strconv"
	"strings"
)

// Section is one `## [X.Y.Z] — date` block, body unchanged.
type Section struct {
	Version string `json:"version"`
	Date    string `json:"date"`
	Body    string `json:"body"`
}

var head = regexp.MustCompile(`^##\s+\[(\d+\.\d+\.\d+)[^\]]*\]\s*(?:[—–-]+\s*(.*))?$`)

type ver [3]int

// parse reads X.Y.Z with an optional leading v and a -prerelease/+build suffix, which
// is dropped: 0.1.130-rc.2 counts as 0.1.130.
func parse(s string) (ver, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return ver{}, false
	}
	var v ver
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return ver{}, false
		}
		v[i] = n
	}
	return v, true
}

func less(a, b ver) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// Since returns the sections with since < version <= current, newest first. An empty or
// unreadable since yields only current's own section; an unknown current (a dev build)
// yields nothing.
func Since(md, since, current string) []Section {
	cur, ok := parse(current)
	if !ok {
		return []Section{}
	}
	from, haveFrom := parse(since)
	type sec struct {
		v ver
		s Section
	}
	var all []sec
	var open *Section
	var v ver
	var buf []string
	flush := func() {
		if open != nil {
			open.Body = strings.TrimSpace(strings.Join(buf, "\n"))
			all = append(all, sec{v, *open})
		}
		open, buf = nil, nil
	}
	for _, line := range strings.Split(md, "\n") {
		if m := head.FindStringSubmatch(strings.TrimRight(line, "\r")); m != nil {
			flush()
			v, _ = parse(m[1])
			open = &Section{Version: m[1], Date: strings.TrimSpace(m[2])}
			continue
		}
		if strings.HasPrefix(line, "## ") {
			flush() // e.g. [Unreleased]
			continue
		}
		if open != nil {
			buf = append(buf, line)
		}
	}
	flush()
	out := []Section{}
	for _, s := range all {
		if less(cur, s.v) {
			continue
		}
		if haveFrom && !less(from, s.v) {
			continue
		}
		if !haveFrom && s.v != cur {
			continue
		}
		out = append(out, s.s)
	}
	for i := 0; i < len(out); i++ { // newest first, whatever the file order
		for j := i + 1; j < len(out); j++ {
			a, _ := parse(out[i].Version)
			b, _ := parse(out[j].Version)
			if less(a, b) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}
