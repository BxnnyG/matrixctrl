package config

import "testing"

func TestSetNodeValuePreservesComments(t *testing.T) {
	src := `# top comment
synapse:
  ## How many workers to run
  workers: 1
  ## The public server name
  serverName: old.example.com
`
	doc, err := ParseYAMLNode(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := SetNodeValue(doc, []string{"synapse", "serverName"}, "new.example.com"); err != nil {
		t.Fatal(err)
	}
	// New nested key that didn't exist before.
	if err := SetNodeValue(doc, []string{"synapse", "logging", "level"}, "DEBUG"); err != nil {
		t.Fatal(err)
	}
	out, err := MarshalNode(doc)
	if err != nil {
		t.Fatal(err)
	}
	// Comment must survive the edit.
	if !contains(out, "The public server name") {
		t.Errorf("comment lost:\n%s", out)
	}
	if !contains(out, "How many workers") {
		t.Errorf("sibling comment lost:\n%s", out)
	}
	if !contains(out, "new.example.com") {
		t.Errorf("value not updated:\n%s", out)
	}
	if !contains(out, "DEBUG") {
		t.Errorf("new nested key not added:\n%s", out)
	}
}

func TestDeleteNodeValuePrunes(t *testing.T) {
	src := "a:\n  b:\n    c: 1\n"
	doc, _ := ParseYAMLNode(src)
	DeleteNodeValue(doc, []string{"a", "b", "c"})
	out, _ := MarshalNode(doc)
	if contains(out, "c:") || contains(out, "b:") || contains(out, "a:") {
		t.Errorf("expected fully pruned, got:\n%q", out)
	}
}

func TestSplitTopLevel(t *testing.T) {
	src := `## server
serverName: x.de
synapse:
  ## workers
  workers: 2
`
	doc, _ := ParseYAMLNode(src)
	parts := SplitTopLevel(doc)
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	syn, _ := MarshalNode(parts["synapse"])
	if !contains(syn, "workers") || !contains(syn, "## workers") {
		t.Errorf("synapse split lost content/comment:\n%s", syn)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
