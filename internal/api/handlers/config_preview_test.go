package handlers

import (
	"context"
	"testing"
)

// Without cluster access at startup the Helm client is nil. The preview then says it
// cannot tell — it does not crash, and it does not block (§4.55: unknown is not "no").
func TestVerdictWithoutAClusterIsUnknownNotBlocking(t *testing.T) {
	h := &HelmHandler{}
	v := h.verdictFor(context.Background(), "ess", map[string]interface{}{})
	if v.Rendered || v.Blocking || v.Note == "" {
		t.Errorf("want unrendered, not blocking, with a reason: %+v", v)
	}
}
