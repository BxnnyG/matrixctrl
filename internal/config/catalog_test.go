package config

import (
	"strings"
	"testing"
)

// The help text under each setting comes from the `##` comments in the section files.
// yaml.v3 hands a key *every* comment line above it — including configuration that is
// commented out (`# port: 5432`) and that configuration's own documentation. Joined
// together, the description of MAS's `additional` became a block about PostgreSQL ports,
// usernames, sslMode and two secrets (etappe 107). These tests pin which lines belong to
// a key: the block directly above it, and nothing past a blank line or a commented-out
// setting.

// The real shape, from the MAS section file.
const masExcerpt = `matrixAuthenticationService:
  ## Synapse - MAS Shared Secret.
  ## It can either be provided inline in the Helm chart e.g.:
  ## synapseSharedSecret:
  ##   value: SecretValue
  ##
  ## Or it can be provided via an existing Secret e.g.:
  ## synapseSharedSecret:
  ##   secret: existing-secret
  # synapseSharedSecret: {}

  ## Additional configuration to provide to Matrix Authentication Service.
  ## Each key under additional is an additional config to merge into mas-config.
  ## e.g.
  ## additional:
  ##   0-customConfig:
  ##     config: |
  ##       <any valid configuration>
  ##
  ## Most settings are configurable but some settings are owned by the chart
  additional: {}
`

func TestACommentBelongsOnlyToItsOwnKey(t *testing.T) {
	c := ExtractComments(masExcerpt)["matrixAuthenticationService.additional"]

	if !strings.Contains(c, "Additional configuration to provide") {
		t.Fatalf("the key's own documentation is missing: %q", c)
	}
	for _, foreign := range []string{"Shared Secret", "existing-secret", "provided inline"} {
		if strings.Contains(c, foreign) {
			t.Errorf("documentation of another setting leaked in (%q): %q", foreign, c)
		}
	}
}

// An example written as YAML inside the docs is code, not prose. Flattened into one
// line it read "e.g. additional: 0-customConfig: config: | <any valid configuration>",
// which is neither.
func TestYAMLExamplesKeepTheirShape(t *testing.T) {
	c := ExtractComments(masExcerpt)["matrixAuthenticationService.additional"]

	if !strings.Contains(c, "\n  0-customConfig:") {
		t.Errorf("the example lost its line breaks or indentation: %q", c)
	}
	// Prose still reads as prose: wrapped lines are joined, not left broken.
	if !strings.Contains(c, "Matrix Authentication Service. Each key") {
		t.Errorf("wrapped prose should be joined into one paragraph: %q", c)
	}
}

// A commented-out setting directly above a key — no blank line in between — also ends
// the block. Everything above it documents that setting, not this one.
func TestACommentedOutSettingEndsTheBlock(t *testing.T) {
	src := `db:
  ## The port Postgres listens on
  # port: 5432
  ## The host to connect to
  host: localhost
`
	c := ExtractComments(src)["db.host"]
	if c != "The host to connect to" {
		t.Errorf("got %q, want only the host's own line", c)
	}
}

// Charts that document with a single `#` keep their documentation: a prose line is not
// a commented-out setting just because it starts with one hash.
func TestSingleHashProseIsStillDocumentation(t *testing.T) {
	src := `app:
  # The number of replicas to run
  replicas: 1
`
	if c := ExtractComments(src)["app.replicas"]; c != "The number of replicas to run" {
		t.Errorf("got %q", c)
	}
}

// "Note: something" is a sentence, not a key.
func TestProseWithAColonIsNotCode(t *testing.T) {
	src := `app:
  ## Note: changing this restarts the service
  ## and cannot be undone.
  mode: a
`
	c := ExtractComments(src)["app.mode"]
	if c != "Note: changing this restarts the service and cannot be undone." {
		t.Errorf("got %q", c)
	}
}

// ESS documents a setting, then shows a commented-out example of it, then the setting:
// the example is stepped over and the documentation above it kept. The first version
// of this fix stopped at the example and left 22 settings with no description at all.
func TestAnExampleOfTheSameKeyKeepsItsDocumentation(t *testing.T) {
	src := `ingress:
  ## How the Service behind this Ingress is constructed
  # service:
  #   type: ClusterIP
  #   annotations: {}
  #   # External IPs addresses of this service.
  #   externalIPs: []
  service: {}
`
	if c := ExtractComments(src)["ingress.service"]; c != "How the Service behind this Ingress is constructed" {
		t.Errorf("got %q", c)
	}
}

// …and when the example is headed by the parent's name, it documents the parent's
// subtree — the Seccomp documentation in every podSecurityContext.
func TestAnExampleOfTheParentKeepsItsDocumentation(t *testing.T) {
	src := `podSecurityContext:
  seccompProfile:
    ## To set the Seccomp profile for a Container, include the seccompProfile field.
    # seccompProfile:
    #  type: RuntimeDefault
    type: RuntimeDefault
`
	c := ExtractComments(src)["podSecurityContext.seccompProfile.type"]
	if c != "To set the Seccomp profile for a Container, include the seccompProfile field." {
		t.Errorf("got %q", c)
	}
}
