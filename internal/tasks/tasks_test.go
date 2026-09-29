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

// Element Web reads its blocks as JSON text directly under the key. Ours is written as
// JSON; a foreign JSON block setting the same key locks the field (counter-probe).
func TestElementWebBlocksAreJSON(t *testing.T) {
	plan, err := Write(values(t, "{}"), fields, map[string]interface{}{"web.brand": "Mein Chat", "web.theme": "dark"})
	if err != nil {
		t.Fatal(err)
	}
	text, _ := plan.Set["elementWeb.additional."+Block].(string)
	if text != `{"brand":"Mein Chat","default_theme":"dark"}` {
		t.Errorf("JSON block: %q (plan %+v)", text, plan)
	}
	foreign := values(t, `elementWeb:
  additional:
    branding: '{"brand": "Firma"}'
`)
	if r := Read(foreign, fields)["web.brand"]; r.SetElsewhere != "branding" || r.Value != "Firma" {
		t.Errorf("foreign JSON block: %+v", r)
	}
	if r := Read(foreign, fields)["web.theme"]; r.SetElsewhere != "" || !r.IsDefault {
		t.Errorf("a key the foreign block does not set stays ours: %+v", r)
	}
}

// Everyone = the key absent; no one = an empty list; some = the list.
func TestFederationAllowList(t *testing.T) {
	v := values(t, "{}")
	blockOf := func(p Plan) map[string]interface{} {
		var doc map[string]interface{}
		_ = yaml.Unmarshal([]byte(p.Set["synapse.additional."+Block+".config"].(string)), &doc)
		return doc
	}
	none, err := Write(v, fields, map[string]interface{}{"synapse.federationAllow": []interface{}{}})
	if err != nil {
		t.Fatal(err)
	}
	if l, ok := blockOf(none)["federation_domain_whitelist"].([]interface{}); !ok || len(l) != 0 {
		t.Errorf("no one must be an empty list: %+v", blockOf(none))
	}
	some, _ := Write(v, fields, map[string]interface{}{"synapse.federationAllow": []interface{}{"matrix.org", "example.org"}})
	if l, _ := blockOf(some)["federation_domain_whitelist"].([]interface{}); len(l) != 2 {
		t.Errorf("list: %+v", blockOf(some))
	}
	own := values(t, "synapse:\n  additional:\n    matrixctrl-tasks:\n      config: |\n        federation_domain_whitelist: []\n")
	all, _ := Write(own, fields, map[string]interface{}{"synapse.federationAllow": nil})
	if len(all.Remove) != 1 || all.Remove[0] != "synapse.additional."+Block {
		t.Errorf("everyone must remove the key (and the emptied block): %+v", all)
	}
	if _, err := Write(v, fields, map[string]interface{}{"synapse.federationAllow": []interface{}{"not a host"}}); err == nil {
		t.Error("a non-host must be refused")
	}
}

func TestNewKindsAreValidated(t *testing.T) {
	v := values(t, "{}")
	for id, bad := range map[string]interface{}{
		"synapse.maxUpload":          "100",
		"synapse.redactionRetention": "7",
		"rtc.portTcp":                float64(70000),
		"web.theme":                  "pink",
	} {
		if _, err := Write(v, fields, map[string]interface{}{id: bad}); err == nil {
			t.Errorf("%s=%v should be refused", id, bad)
		}
	}
	plan, err := Write(v, fields, map[string]interface{}{"rtc.portTcp": float64(30011), "synapse.maxUpload": "200M"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Set["matrixRTC.sfu.exposedServices.rtcTcp.port"] != 30011 {
		t.Errorf("a port is written as a number: %#v", plan.Set)
	}
}

// Seeded installations write defaults out; equal to the default is the default.
func TestAWrittenOutDefaultIsTheDefault(t *testing.T) {
	v := values(t, "synapse:\n  media:\n    maxUploadSize: 100M\nmatrixRTC:\n  enabled: true\n")
	r := Read(v, fields)
	if !r["synapse.maxUpload"].IsDefault || !r["rtc.enabled"].IsDefault {
		t.Errorf("written-out defaults: %+v %+v", r["synapse.maxUpload"], r["rtc.enabled"])
	}
	if r := Read(values(t, "synapse:\n  media:\n    maxUploadSize: 200M\n"), fields)["synapse.maxUpload"]; r.IsDefault {
		t.Error("a different value is not the default")
	}
}

// On and back off: the block is gone again, not holding the default.
func TestSwitchingBackToTheDefaultLeavesNothing(t *testing.T) {
	on, _ := Write(values(t, "{}"), fields, map[string]interface{}{"mas.password_registration": true})
	after := values(t, "matrixAuthenticationService:\n  additional:\n    matrixctrl-tasks:\n      config: |\n"+
		"        account:\n          password_registration_enabled: true\n")
	_ = on
	off, err := Write(after, fields, map[string]interface{}{"mas.password_registration": false})
	if err != nil {
		t.Fatal(err)
	}
	if len(off.Set) != 0 || len(off.Remove) != 1 || off.Remove[0] != "matrixAuthenticationService.additional."+Block {
		t.Errorf("switching back must remove the block: %+v", off)
	}
}
