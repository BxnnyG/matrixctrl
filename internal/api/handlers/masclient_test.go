package handlers

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Verbatim from the operator's instance on 2026-08-05: written by a version of the
// generator that predates client_name, which is why MAS asked "Continue to
// 01KSPV9ZMR7NB4B2BBWMPYSD1P?".
const storedFragment = `clients:
  - client_id: "01KSPV9ZMR7NB4B2BBWMPYSD1P"
    client_auth_method: client_secret_basic
    client_secret: "s3cr3t"
    redirect_uris:
      - https://panel.example.test/api/v1/auth/oidc/callback
policy:
  data:
    admin_clients:
      - "01KSPV9ZMR7NB4B2BBWMPYSD1P"
`

func TestAddsTheMissingClientName(t *testing.T) {
	got, err := repairMASClientConfig(storedFragment, "MatrixCtrl")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Changed) != 1 || got.Changed[0] != "client_name" {
		t.Fatalf("changed: %v", got.Changed)
	}
	if !strings.Contains(got.Config, `client_name: "MatrixCtrl"`) {
		t.Fatalf("config:\n%s", got.Config)
	}
}

// The credential the instance is currently authenticating with must survive. A
// repair that rotated it would log the operator out of the panel they were using to
// run the repair.
func TestRepairPreservesIdSecretAndRedirects(t *testing.T) {
	got, err := repairMASClientConfig(storedFragment, "MatrixCtrl")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"01KSPV9ZMR7NB4B2BBWMPYSD1P",
		"s3cr3t",
		"client_secret_basic",
		"https://panel.example.test/api/v1/auth/oidc/callback",
	} {
		if !strings.Contains(got.Config, want) {
			t.Errorf("lost %q:\n%s", want, got.Config)
		}
	}

	// The admin_clients policy is what makes the panel's admin check work at all.
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(got.Config), &doc); err != nil {
		t.Fatal(err)
	}
	policy, _ := doc["policy"].(map[string]any)
	data, _ := policy["data"].(map[string]any)
	admins, _ := data["admin_clients"].([]any)
	if len(admins) != 1 || admins[0] != "01KSPV9ZMR7NB4B2BBWMPYSD1P" {
		t.Fatalf("admin_clients did not survive: %+v", doc["policy"])
	}
}

// Idempotent: running it twice is a normal thing to do, and the second run must be
// a no-op rather than a second insertion.
func TestAlreadyCorrectIsANoOp(t *testing.T) {
	once, err := repairMASClientConfig(storedFragment, "MatrixCtrl")
	if err != nil {
		t.Fatal(err)
	}
	twice, err := repairMASClientConfig(once.Config, "MatrixCtrl")
	if err != nil {
		t.Fatal(err)
	}
	if len(twice.Changed) != 0 {
		t.Fatalf("second run changed %v", twice.Changed)
	}
	if twice.Config != once.Config {
		t.Fatal("second run rewrote a config it had nothing to change")
	}
	if strings.Count(twice.Config, "client_name") != 1 {
		t.Fatalf("client_name appears %d times", strings.Count(twice.Config, "client_name"))
	}
}

// An operator who set their own display name keeps it. The repair fills gaps; it
// does not impose the generator's opinion on a value someone chose.
func TestExistingNameIsNotOverwritten(t *testing.T) {
	custom := strings.Replace(storedFragment,
		`client_id: "01KSPV9ZMR7NB4B2BBWMPYSD1P"`,
		`client_id: "01KSPV9ZMR7NB4B2BBWMPYSD1P"`+"\n    client_name: \"Admin-Panel\"", 1)

	got, err := repairMASClientConfig(custom, "MatrixCtrl")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Changed) != 0 {
		t.Fatalf("changed %v on a config that already had a name", got.Changed)
	}
	if strings.Contains(got.Config, "MatrixCtrl") {
		t.Fatal("the operator's own name was replaced")
	}
}

// A fragment this cannot read may have been hand-edited. Overwriting it would
// destroy that edit while reporting success, which is the worst available outcome.
func TestUnreadableFragmentIsRefusedNotReplaced(t *testing.T) {
	for _, bad := range []string{
		"\tnot: yaml:\n  - at all\n",
		"- a\n- b\n",              // a sequence, not a mapping
		"policy:\n  data: {}\n",   // no clients
		"clients: []\n",           // empty clients
		"clients: \"a string\"\n", // clients of the wrong kind
		"",
	} {
		if _, err := repairMASClientConfig(bad, "MatrixCtrl"); err == nil {
			t.Errorf("%q should have been refused", bad)
		}
	}
}

// Several clients in one fragment is legal. Each gets the name it is missing, and
// one that already has a name is left alone.
func TestSeveralClients(t *testing.T) {
	frag := `clients:
  - client_id: "AAA"
    client_secret: "x"
  - client_id: "BBB"
    client_name: "Something Else"
    client_secret: "y"
`
	got, err := repairMASClientConfig(frag, "MatrixCtrl")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Changed) != 1 {
		t.Fatalf("expected one change, got %v", got.Changed)
	}
	if !strings.Contains(got.Config, "Something Else") {
		t.Error("the client that already had a name lost it")
	}
	if strings.Count(got.Config, "client_name") != 2 {
		t.Fatalf("expected both to end up named:\n%s", got.Config)
	}
}

// A display name is text. Left unquoted, YAML would read `Yes` back as a boolean
// and a date-looking name as a timestamp, and MAS would reject the config.
func TestNameIsQuoted(t *testing.T) {
	for _, name := range []string{"Yes", "2026-01-01", "null", "123"} {
		got, err := repairMASClientConfig(storedFragment, name)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Clients []struct {
				ClientName string `yaml:"client_name"`
			} `yaml:"clients"`
		}
		if err := yaml.Unmarshal([]byte(got.Config), &doc); err != nil {
			t.Fatalf("%q produced unparseable YAML: %v", name, err)
		}
		if doc.Clients[0].ClientName != name {
			t.Errorf("%q round-tripped as %q", name, doc.Clients[0].ClientName)
		}
	}
}

func TestEmptyNameIsRejected(t *testing.T) {
	if _, err := repairMASClientConfig(storedFragment, ""); err == nil {
		t.Fatal("an empty name should be refused")
	}
}

// The inserted key belongs next to client_id, not appended after the redirect list —
// a generated block that grows an appendix stops looking generated.
func TestNameIsInsertedAfterClientId(t *testing.T) {
	got, err := repairMASClientConfig(storedFragment, "MatrixCtrl")
	if err != nil {
		t.Fatal(err)
	}
	idAt := strings.Index(got.Config, "client_id")
	nameAt := strings.Index(got.Config, "client_name")
	authAt := strings.Index(got.Config, "client_auth_method")
	if !(idAt < nameAt && nameAt < authAt) {
		t.Fatalf("order is id=%d name=%d auth=%d:\n%s", idAt, nameAt, authAt, got.Config)
	}
}
