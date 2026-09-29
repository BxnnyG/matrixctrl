package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The shape that moved thirty lines on 2026-09-29: comments at the end of a nested
// block, followed by a shallower key. yaml.v3 re-indents them to that key's level.
const footComments = `synapse:
  media:
    storage:
      size: 10Gi

      ## Whether to keep the PVC
      # resourcePolicy: keep
    ephemeralStorages: {}
  ## Additional configuration
  additional: {}
  appservices: []
`

func TestSavingOneValueMovesNoComments(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "synapse.yaml"), []byte(footComments), 0o644)
	s := &Store{path: dir}
	if err := s.SetSectionValues(context.Background(), map[string]interface{}{
		"synapse.additional.matrixctrl-tasks.config": "federation_domain_whitelist: []\n",
	}, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "synapse.yaml"))
	want := strings.Replace(footComments, "  additional: {}\n",
		"  additional:\n    matrixctrl-tasks:\n      config: |\n        federation_domain_whitelist: []\n", 1)
	if string(got) != want {
		t.Errorf("only the edited lines may change.\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// Counter-probe: without the repair the same save does move them — otherwise the test
// above would prove nothing.
func TestWithoutTheRepairCommentsMove(t *testing.T) {
	n, _ := ParseYAMLNode(footComments)
	_ = SetNodeValue(n, []string{"synapse", "additional", "matrixctrl-tasks", "config"}, "federation_domain_whitelist: []\n")
	raw, _ := MarshalNode(n)
	if strings.Contains(raw, "      ## Whether to keep the PVC\n") && strings.Contains(raw, "\n\n      ## Whether") {
		t.Skip("this yaml.v3 no longer moves foot comments; the repair is then a no-op")
	}
	if keepComments(footComments, raw) == raw {
		t.Error("the repair changed nothing, so the fixture does not reproduce the move")
	}
}

func TestARealChangeIsNeverReverted(t *testing.T) {
	orig := "a:\n  # note\n  b: 1\n"
	edited := "a:\n  # note\n  b: 2\n"
	if got := keepComments(orig, edited); got != edited {
		t.Errorf("value change lost: %q", got)
	}
}
