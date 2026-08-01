package db

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The failure this guards against is not a bug in the UPDATE — it is someone
// adding a new in-flight status to the upgrade handler and not adding it here.
// The row would then sit unreconciled forever, which is exactly what P2-16 was:
// a status nothing ever moved off.
//
// So the test reads the handler's source and asserts that every status it
// writes is accounted for as either in-flight or terminal. A pure unit test on
// the SQL could not catch this, and an integration test would need a database
// this suite deliberately does not require.
func TestEveryUpgradeStatusIsClassified(t *testing.T) {
	const handler = "../api/handlers/helm_upgrade.go"

	src, err := os.ReadFile(handler)
	if err != nil {
		t.Fatalf("read %s: %v", handler, err)
	}

	terminal := map[string]bool{
		"success":      true,
		"failed":       true,
		"hooks-failed": true,
		"interrupted":  true, // written here, not by the handler
	}
	inFlight := map[string]bool{}
	for _, s := range nonTerminalUpgradeStates {
		inFlight[s] = true
	}

	// status='x' in SQL, and the Go literals assigned to finalStatus.
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`status\s*=\s*'([a-z-]+)'`),
		regexp.MustCompile(`VALUES\([^)]*'([a-z-]+)'\)`),
		regexp.MustCompile(`finalStatus\s*:?=\s*"([a-z-]+)"`),
	}

	seen := map[string]bool{}
	for _, re := range patterns {
		for _, m := range re.FindAllStringSubmatch(string(src), -1) {
			seen[m[1]] = true
		}
	}

	if len(seen) == 0 {
		t.Fatalf("found no status literals in %s — the patterns stopped matching, "+
			"which makes this test silently useless", handler)
	}

	var unclassified []string
	for status := range seen {
		if !terminal[status] && !inFlight[status] {
			unclassified = append(unclassified, status)
		}
	}
	sort.Strings(unclassified)

	if len(unclassified) > 0 {
		t.Errorf("upgrade status(es) %s are written by the handler but are neither "+
			"terminal nor listed in nonTerminalUpgradeStates.\n"+
			"A row in an unlisted in-flight status is never reconciled after a restart "+
			"and stays there forever (BACKLOG P2-16).",
			strings.Join(unclassified, ", "))
	}
}

func TestInterruptedMessageSaysWhatIsUnknown(t *testing.T) {
	// The point of the message is that it does not claim the upgrade failed —
	// the Helm revision may well have gone through. If someone shortens it to
	// "upgrade failed", the record starts lying in the other direction.
	if !strings.Contains(interruptedMessage, "restarted") {
		t.Error("message should say the process restarted")
	}
	for _, wrong := range []string{"failed", "error"} {
		if strings.Contains(strings.ToLower(interruptedMessage), wrong) {
			t.Errorf("message must not imply the upgrade %s — the outcome is unknown, not bad", wrong)
		}
	}
}
