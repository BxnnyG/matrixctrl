package handlers

import (
	"sort"
	"strings"
	"testing"
)

// The greenfield deploy failed on every attempt because this map contained
// wellKnownDelegation.ingress.host, which matrix-stack's schema rejects — its
// ingress block has no `host` property and sets additionalProperties:false.
// helm install refuses the whole release, so the headline feature was broken
// from the first day and nobody noticed: our own instance already has ESS and
// never reaches this code path.
//
// The keys below were checked against matrix-stack 26.7.2's values.schema.json.
// Adding one without checking breaks greenfield deploy again, silently, until
// someone runs it on an empty cluster.
func TestGreenfieldHostnameKeys(t *testing.T) {
	got := greenfieldHostnames("example.com")

	want := []string{
		"elementAdmin.ingress.host",
		"elementWeb.ingress.host",
		"matrixAuthenticationService.ingress.host",
		"matrixRTC.ingress.host",
		"serverName",
		"synapse.ingress.host",
	}

	var keys []string
	for k := range got {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	if len(keys) != len(want) {
		t.Fatalf("key set changed:\n got %v\nwant %v", keys, want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("key set changed:\n got %v\nwant %v", keys, want)
		}
	}
}

func TestGreenfieldRejectsWellKnownHost(t *testing.T) {
	for k := range greenfieldHostnames("example.com") {
		if strings.HasPrefix(k, "wellKnownDelegation") {
			t.Fatalf("wellKnownDelegation must not be seeded: matrix-stack's schema "+
				"has no host under its ingress and sets additionalProperties:false, "+
				"so helm install fails validation. Got key %q", k)
		}
	}
}

func TestGreenfieldDerivesFromServerName(t *testing.T) {
	got := greenfieldHostnames("bxnny.de")

	cases := map[string]string{
		"serverName":                               "bxnny.de",
		"synapse.ingress.host":                     "matrix.bxnny.de",
		"matrixAuthenticationService.ingress.host": "mas.bxnny.de",
		"elementWeb.ingress.host":                  "element.bxnny.de",
		"elementAdmin.ingress.host":                "admin.bxnny.de",
		"matrixRTC.ingress.host":                   "mrtc.bxnny.de",
	}
	for k, want := range cases {
		if got[k] != want {
			t.Errorf("%s = %v, want %v", k, got[k], want)
		}
	}
}

// A repo written by an older build still carries the schema-invalid key, and the
// wizard now keeps existing config instead of overwriting it — so without an
// explicit removal the bad value would survive every retry, for exactly the
// operators who already hit the bug.
func TestGreenfieldRemovesSchemaInvalidKeys(t *testing.T) {
	removals := greenfieldRemovals()

	var found bool
	for _, k := range removals {
		if k == "wellKnownDelegation.ingress.host" {
			found = true
		}
	}
	if !found {
		t.Fatalf("wellKnownDelegation.ingress.host must be removed on deploy, "+
			"otherwise a repo seeded by a broken build can never deploy. Got %v", removals)
	}
}

// Anything removed must not also be written — that would be a loop that quietly
// does nothing.
func TestGreenfieldRemovalsAndHostnamesDoNotOverlap(t *testing.T) {
	set := greenfieldHostnames("example.com")
	for _, k := range greenfieldRemovals() {
		if _, clash := set[k]; clash {
			t.Errorf("%q is both written and removed", k)
		}
	}
}
