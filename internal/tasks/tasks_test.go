package tasks

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func values(t *testing.T, doc string) map[string]interface{} {
	t.Helper()
	var v map[string]interface{}
	if err := yaml.Unmarshal([]byte(doc), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

var fields = AllFields(Cards())

func TestASwitchWritesOnlyOurBlock(t *testing.T) {
	v := values(t, "matrixAuthenticationService:\n  additional:\n    matrixctrl-upstream:\n      configSecret: s\n")
	plan, err := Write(v, fields, map[string]interface{}{"mas.password_registration": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Set) != 1 || len(plan.Remove) != 0 {
		t.Fatalf("exactly one edit expected: %+v", plan)
	}
	text, _ := plan.Set["matrixAuthenticationService.additional."+Block+".config"].(string)
	var doc map[string]interface{}
	_ = yaml.Unmarshal([]byte(text), &doc)
	if got, _ := get(doc, []string{"account", "password_registration_enabled"}); got != true {
		t.Errorf("block content: %q", text)
	}
	if text != "account:\n  password_registration_enabled: true\n" {
		t.Errorf("indented like the section files: %q", text)
	}
}

// Another block sets the key: shown, not editable, and the block is named. The
// counter-probe is the same key in our own block, which stays editable.
func TestAKeySetElsewhereIsReadOnly(t *testing.T) {
	v := values(t, `matrixAuthenticationService:
  additional:
    0-custom:
      config: |
        account:
          password_registration_enabled: true
`)
	got := Read(v, fields)["mas.password_registration"]
	if got.SetElsewhere != "0-custom" || got.Value != true {
		t.Errorf("read: %+v", got)
	}
	if _, err := Write(v, fields, map[string]interface{}{"mas.password_registration": false}); err == nil || !strings.Contains(err.Error(), "0-custom") {
		t.Errorf("write must refuse and name the block: %v", err)
	}

	own := values(t, `matrixAuthenticationService:
  additional:
    matrixctrl-tasks:
      config: |
        account:
          password_registration_enabled: true
`)
	if r := Read(own, fields)["mas.password_registration"]; r.SetElsewhere != "" || r.Value != true || r.IsDefault {
		t.Errorf("own block: %+v", r)
	}
	if _, err := Write(own, fields, map[string]interface{}{"mas.password_registration": false}); err != nil {
		t.Errorf("own block must stay editable: %v", err)
	}
}

// Back to the default removes the setting, and the last one removes the block.
func TestResetRemovesInsteadOfWritingTheDefault(t *testing.T) {
	v := values(t, `matrixAuthenticationService:
  additional:
    matrixctrl-tasks:
      config: |
        account:
          password_registration_enabled: true
postgres:
  resources:
    requests:
      memory: 1Gi
`)
	plan, err := Write(v, fields, map[string]interface{}{"mas.password_registration": nil, "postgres.resources.requests.memory": nil})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"matrixAuthenticationService.additional.matrixctrl-tasks", "postgres.resources.requests.memory"}
	if strings.Join(plan.Remove, ",") != strings.Join(want, ",") || len(plan.Set) != 0 {
		t.Errorf("plan: %+v", plan)
	}
	if r := Read(values(t, "{}"), fields)["mas.password_registration"]; !r.IsDefault || r.Value != false {
		t.Errorf("unset reads as the documented default: %+v", r)
	}
}

func TestTheServerNameIsLocked(t *testing.T) {
	if _, err := Write(values(t, "serverName: a.org\n"), fields, map[string]interface{}{"serverName": "b.org"}); err == nil {
		t.Error("the server name must not be writable")
	}
}

func TestValuesAreValidated(t *testing.T) {
	v := values(t, "{}")
	for id, bad := range map[string]interface{}{
		"postgres.resources.requests.memory": "viel",
		"host.synapse":                       "https://matrix.example.org",
		"mas.passwords":                      "ja",
	} {
		if _, err := Write(v, fields, map[string]interface{}{id: bad}); err == nil {
			t.Errorf("%s=%v should be refused", id, bad)
		}
	}
	for id, good := range map[string]interface{}{
		"postgres.resources.requests.memory": "1536Mi",
		"synapse.resources.requests.cpu":     "500m",
		"host.synapse":                       "matrix.example.org",
	} {
		if _, err := Write(v, fields, map[string]interface{}{id: good}); err != nil {
			t.Errorf("%s=%v refused: %v", id, good, err)
		}
	}
}
