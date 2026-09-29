package handlers

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bxnnyg/matrixctrl/internal/capacity"
	"github.com/bxnnyg/matrixctrl/internal/preview"
)

// The lists of a verdict are lists, also when empty — the pending-changes bar reads
// `restarts.filter(...)` and crashed on null.
func TestVerdictListsAreNeverNull(t *testing.T) {
	v := configVerdict{Restarts: []preview.Restart{}, Findings: []capacity.Finding{}, StaleAliases: []preview.StaleAlias{}}
	if r := preview.Restarts("", ""); r != nil {
		v.Restarts = r
	}
	b, _ := json.Marshal(v)
	for _, k := range []string{`"restarts":null`, `"findings":null`, `"stale_aliases":null`} {
		if strings.Contains(string(b), k) {
			t.Errorf("%s in %s", k, b)
		}
	}
}
