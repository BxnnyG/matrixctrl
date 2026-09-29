package config

import (
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// keepComments takes the edited document and puts back every run of lines where only
// comments moved (etappe 114).
//
// yaml.v3 attaches the comment lines between the end of a nested block and the next,
// shallower key to that key, and writes them at *its* indentation. Nothing is lost, but
// every save re-indented and reshuffled documentation far from the edited value: turning
// on one Synapse setting produced thirty lines of moved comments in the pending diff —
// in a view whose whole purpose is to show what a change does.
//
// So the two texts are compared line by line, and each contiguous run of differences
// that consists only of comments and blank lines, on both sides, is replaced by the
// original. A run that touches a real line is kept as written: when a comment move and
// a value change are adjacent, correctness wins over tidiness.
func keepComments(original, edited string) string {
	if original == "" {
		return edited
	}
	dmp := diffmatchpatch.New()
	a, b, lines := dmp.DiffLinesToChars(original, edited)
	diffs := dmp.DiffCharsToLines(dmp.DiffMain(a, b, false), lines)

	var out strings.Builder
	var del, ins strings.Builder
	flush := func() {
		if onlyComments(del.String()) && onlyComments(ins.String()) {
			out.WriteString(del.String())
		} else {
			out.WriteString(ins.String())
		}
		del.Reset()
		ins.Reset()
	}
	for _, d := range diffs {
		switch d.Type {
		case diffmatchpatch.DiffEqual:
			flush()
			out.WriteString(d.Text)
		case diffmatchpatch.DiffDelete:
			del.WriteString(d.Text)
		case diffmatchpatch.DiffInsert:
			ins.WriteString(d.Text)
		}
	}
	flush()
	return out.String()
}

func onlyComments(block string) bool {
	for _, l := range strings.Split(block, "\n") {
		t := strings.TrimSpace(l)
		if t != "" && !strings.HasPrefix(t, "#") {
			return false
		}
	}
	return true
}
