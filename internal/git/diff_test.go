package git

import (
	"strings"
	"testing"
)

// The old positional implementation emitted no @@ headers at all, so any
// consumer parsing hunks saw an empty diff.
func TestUnifiedDiffEmitsHunkHeader(t *testing.T) {
	old := "a\nb\nc\n"
	next := "a\nB\nc\n"
	got := unifiedDiff(old, next)
	if !strings.Contains(got, "@@") {
		t.Fatalf("diff has no hunk header:\n%s", got)
	}
	if !strings.Contains(got, "-b") || !strings.Contains(got, "+B") {
		t.Errorf("diff missing the changed lines:\n%s", got)
	}
}

// A single insertion near the top used to mark every following line as changed.
func TestUnifiedDiffSingleInsertion(t *testing.T) {
	old := "line1\nline2\nline3\nline4\nline5\n"
	next := "line1\ninserted\nline2\nline3\nline4\nline5\n"
	got := unifiedDiff(old, next)

	adds, removes := 0, 0
	for _, l := range strings.Split(got, "\n") {
		switch {
		case strings.HasPrefix(l, "+"):
			adds++
		case strings.HasPrefix(l, "-"):
			removes++
		}
	}
	if adds != 1 {
		t.Errorf("expected exactly 1 added line, got %d:\n%s", adds, got)
	}
	if removes != 0 {
		t.Errorf("expected 0 removed lines, got %d:\n%s", removes, got)
	}
}

func TestUnifiedDiffNoChange(t *testing.T) {
	same := "a\nb\nc\n"
	if got := unifiedDiff(same, same); got != "" {
		t.Errorf("expected empty diff for identical input, got:\n%s", got)
	}
}

func TestUnifiedDiffContextIsLimited(t *testing.T) {
	// 40 identical lines with one change in the middle should not dump the
	// whole file into the hunk.
	var oldB, newB strings.Builder
	for i := 0; i < 40; i++ {
		oldB.WriteString("same\n")
		if i == 20 {
			newB.WriteString("changed\n")
		} else {
			newB.WriteString("same\n")
		}
	}
	got := unifiedDiff(oldB.String(), newB.String())
	// 1 header + 3 leading context + 1 removed + 1 added + 3 trailing context.
	lines := strings.Count(got, "\n")
	if lines > 9 {
		t.Errorf("hunk should stay near the change (got %d lines):\n%s", lines, got)
	}
	if !strings.Contains(got, "+changed") {
		t.Errorf("missing the change:\n%s", got)
	}
}

func TestUnifiedDiffPureAppend(t *testing.T) {
	old := "a\nb\n"
	next := "a\nb\nc\n"
	got := unifiedDiff(old, next)
	if !strings.Contains(got, "+c") {
		t.Errorf("append not reported:\n%s", got)
	}
	if strings.Contains(got, "-a") || strings.Contains(got, "-b") {
		t.Errorf("append should not delete existing lines:\n%s", got)
	}
}
