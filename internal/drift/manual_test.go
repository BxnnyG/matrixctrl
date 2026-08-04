package drift

import (
	"encoding/json"
	"strings"
	"testing"
)

// Verbatim from the production SFU deployment on 2026-08-04, which is where this
// etappe's evidence came from. `kubectl-patch` owning hostNetwork is the agent
// restoring by hand on 2026-08-02 what the product should have applied.
const sfuFields = `{
  "f:spec": {
    "f:template": {
      "f:spec": {
        "f:dnsPolicy": {},
        "f:hostNetwork": {}
      }
    }
  }
}`

const helmFields = `{
  "f:spec": {
    "f:replicas": {},
    "f:selector": {},
    "f:template": {
      "f:spec": {
        "f:containers": {
          "k:{\"name\":\"sfu\"}": {".": {}, "f:image": {}, "f:name": {}}
        }
      }
    }
  }
}`

func sfuObject() ObjectFields {
	return ObjectFields{
		Resource: "deployment", Namespace: "ess", Name: "ess-matrix-rtc-sfu",
		Entries: []ManagedFieldsEntry{
			{Manager: "helm", Operation: "Update", FieldsV1: json.RawMessage(helmFields)},
			{Manager: "kubectl-patch", Operation: "Update", Time: "2026-08-03T16:09:14Z", FieldsV1: json.RawMessage(sfuFields)},
		},
	}
}

func TestFindsTheHandEditAndIgnoresHelm(t *testing.T) {
	edits := FindManualEdits([]ObjectFields{sfuObject()}, nil)

	if len(edits) != 1 {
		t.Fatalf("expected exactly the hand edit, got %d: %+v", len(edits), edits)
	}
	e := edits[0]
	if e.Manager != "kubectl-patch" || e.Kind != Human {
		t.Fatalf("got manager=%s kind=%s", e.Manager, e.Kind)
	}
	if strings.Join(e.Paths, ",") != "spec.template.spec.dnsPolicy,spec.template.spec.hostNetwork" {
		t.Fatalf("paths: %v", e.Paths)
	}
	if e.Covered {
		t.Error("no hooks were passed, so nothing can be covered")
	}
}

// The whole value of the check is that the chart's own ownership is silent. If Helm
// showed up, every object would be a finding and the report would be worthless.
func TestHelmAndMatrixctrlAreNeverReported(t *testing.T) {
	obj := ObjectFields{Resource: "service", Namespace: "ess", Name: "x", Entries: []ManagedFieldsEntry{
		{Manager: "helm", FieldsV1: json.RawMessage(helmFields)},
		{Manager: "matrixctrl", FieldsV1: json.RawMessage(sfuFields)},
		{Manager: "k3s", FieldsV1: json.RawMessage(sfuFields)},
		{Manager: "cert-manager", FieldsV1: json.RawMessage(sfuFields)},
		{Manager: "some-controller", FieldsV1: json.RawMessage(sfuFields)},
	}}
	if edits := FindManualEdits([]ObjectFields{obj}, nil); len(edits) != 0 {
		t.Fatalf("automation must be silent, got %+v", edits)
	}
}

// A hand-edit on a field a hook maintains is a different statement from one on a
// field nothing maintains. Collapsing them buries the second in the first.
func TestHookCoverageSplitsTheTwoCases(t *testing.T) {
	key := HookKey("deployment", "ess", "ess-matrix-rtc-sfu")
	hooks := map[string][]string{key: {"spec.template.spec.hostNetwork"}}

	edits := FindManualEdits([]ObjectFields{sfuObject()}, hooks)
	if len(edits) != 1 || !edits[0].Covered {
		t.Fatalf("a hook naming the exact field must mark it covered: %+v", edits)
	}

	// The same edit with the hook pointing elsewhere is the loud case.
	elsewhere := map[string][]string{key: {"spec.replicas"}}
	edits = FindManualEdits([]ObjectFields{sfuObject()}, elsewhere)
	if edits[0].Covered {
		t.Fatal("an unrelated hook must not mark this covered")
	}
}

// A hook that patches a parent still maintains the child, and vice versa. Requiring
// an exact string match would report a maintained field as unmaintained.
func TestCoverageMatchesEitherDirection(t *testing.T) {
	key := HookKey("deployment", "ess", "ess-matrix-rtc-sfu")
	for _, hookPath := range []string{"spec.template.spec", "spec.template.spec.hostNetwork"} {
		edits := FindManualEdits([]ObjectFields{sfuObject()}, map[string][]string{key: {hookPath}})
		if !edits[0].Covered {
			t.Errorf("hook path %q should cover the edit", hookPath)
		}
	}
}

// The unmaintained hand-edit is the only one of the three that nothing will ever put
// back, so it must be first in the list regardless of input order.
func TestUnmaintainedEditsSortFirst(t *testing.T) {
	key := HookKey("deployment", "ess", "ess-matrix-rtc-sfu")
	objs := []ObjectFields{
		{Resource: "ingress", Namespace: "ess", Name: "z", Entries: []ManagedFieldsEntry{
			{Manager: "some-tool", FieldsV1: json.RawMessage(sfuFields)},
		}},
		sfuObject(), // covered below
		{Resource: "ingress", Namespace: "ess", Name: "a", Entries: []ManagedFieldsEntry{
			{Manager: "kubectl-edit", FieldsV1: json.RawMessage(sfuFields)},
		}},
	}
	edits := FindManualEdits(objs, map[string][]string{key: {"spec.template.spec.hostNetwork"}})
	if len(edits) != 3 {
		t.Fatalf("expected 3, got %d", len(edits))
	}
	if edits[0].Manager != "kubectl-edit" || edits[0].Covered {
		t.Fatalf("the unmaintained hand edit must lead: %+v", edits[0])
	}
	if edits[2].Kind != Foreign {
		t.Fatalf("foreign managers go last: %+v", edits[2])
	}
}

func TestClassifyManager(t *testing.T) {
	cases := map[string]ManagerKind{
		"kubectl-patch": Human, "kubectl-edit": Human, "kubectl-client-side-apply": Human,
		"kubectl": Human, "kubectl-rollout": Human,
		"helm": Automation, "matrixctrl": Automation, "k3s": Automation,
		"kube-controller-manager": Automation, "traefik": Automation,
		"argocd-controller": Automation, "flux-operator": Automation,
		"": Foreign, "lens": Foreign, "some-random-tool": Foreign,
		// Not Helm. Folding it in by prefix would hide exactly what this looks for.
		"helm-my-own-tool": Foreign,
	}
	for manager, want := range cases {
		if got := ClassifyManager(manager); got != want {
			t.Errorf("%q: got %s, want %s", manager, got, want)
		}
	}
}

// Controllers own status by definition; reporting it would make every object a
// finding forever.
func TestStatusAndBookkeepingAreIgnored(t *testing.T) {
	fields := `{"f:status":{"f:loadBalancer":{}},"f:metadata":{"f:resourceVersion":{},"f:annotations":{"f:kubectl.kubernetes.io/last-applied-configuration":{}}}}`
	obj := ObjectFields{Resource: "service", Namespace: "ess", Name: "x", Entries: []ManagedFieldsEntry{
		{Manager: "kubectl-apply", FieldsV1: json.RawMessage(fields)},
	}}
	if edits := FindManualEdits([]ObjectFields{obj}, nil); len(edits) != 0 {
		t.Fatalf("only bookkeeping was touched, got %+v", edits)
	}
}

// A status subresource write is a controller doing its job, whatever name it used.
func TestSubresourceWritesAreIgnored(t *testing.T) {
	obj := ObjectFields{Resource: "deployment", Namespace: "ess", Name: "x", Entries: []ManagedFieldsEntry{
		{Manager: "kubectl-patch", Subresource: "status", FieldsV1: json.RawMessage(sfuFields)},
	}}
	if edits := FindManualEdits([]ObjectFields{obj}, nil); len(edits) != 0 {
		t.Fatalf("subresource writes must be ignored, got %+v", edits)
	}
}

func TestFieldPathsDecodesListKeysReadably(t *testing.T) {
	got := FieldPaths(json.RawMessage(helmFields))
	want := "spec.replicas,spec.selector,spec.template.spec.containers.{name=sfu}.image,spec.template.spec.containers.{name=sfu}.name"
	if strings.Join(got, ",") != want {
		t.Fatalf("got %v", got)
	}
}

func TestFieldPathsSurvivesJunk(t *testing.T) {
	for _, raw := range []string{"", "null", "{}", "not json", `{"f:a":"scalar"}`, `[1,2]`} {
		if paths := FieldPaths(json.RawMessage(raw)); len(paths) > 1 {
			t.Errorf("%q produced %v", raw, paths)
		}
	}
}

func TestPatchPathsForBothPatchTypes(t *testing.T) {
	merge := PatchPaths("merge", `{"spec":{"template":{"spec":{"hostNetwork":true}}}}`)
	if strings.Join(merge, ",") != "spec.template.spec.hostNetwork" {
		t.Fatalf("merge: %v", merge)
	}

	jsonp := PatchPaths("json", `[{"op":"replace","path":"/spec/externalTrafficPolicy","value":"Local"}]`)
	if strings.Join(jsonp, ",") != "spec.externalTrafficPolicy" {
		t.Fatalf("json: %v", jsonp)
	}

	// Consistent with drift.apply — a wrong path list would mark a real finding as
	// covered, which is the one direction that loses information.
	if p := PatchPaths("strategic", `{"spec":{}}`); p != nil {
		t.Fatalf("strategic must yield nothing, got %v", p)
	}

	for _, bad := range []string{"", "{", "null", "[]"} {
		if p := PatchPaths("merge", bad); len(p) != 0 {
			t.Errorf("%q produced %v", bad, p)
		}
	}
}

func TestSummariseManual(t *testing.T) {
	edits := []ManualEdit{
		{Kind: Human, Covered: false}, {Kind: Human, Covered: true},
		{Kind: Foreign}, {Kind: Human, Covered: false},
	}
	un, hand, foreign := SummariseManual(edits)
	if un != 2 || hand != 1 || foreign != 1 {
		t.Fatalf("got %d/%d/%d", un, hand, foreign)
	}
}

func TestNoObjectsIsNoFindings(t *testing.T) {
	if edits := FindManualEdits(nil, nil); len(edits) != 0 {
		t.Fatalf("got %+v", edits)
	}
	if edits := FindManualEdits([]ObjectFields{{Resource: "deployment", Name: "x"}}, nil); len(edits) != 0 {
		t.Fatalf("an object with no managedFields is not a finding, got %+v", edits)
	}
}

// `kubectl rollout restart` stamps restartedAt. It records that something happened
// rather than changing configuration, and on the production cluster it was three of
// eight findings — enough noise to teach an operator to skim past the two that
// mattered.
func TestRolloutRestartStampIsNotAFinding(t *testing.T) {
	fields := `{"f:spec":{"f:template":{"f:metadata":{"f:annotations":{"f:kubectl.kubernetes.io/restartedAt":{}}}}}}`
	obj := ObjectFields{Resource: "deployment", Namespace: "ess", Name: "ess-haproxy", Entries: []ManagedFieldsEntry{
		{Manager: "kubectl-rollout", FieldsV1: json.RawMessage(fields)},
	}}
	if edits := FindManualEdits([]ObjectFields{obj}, nil); len(edits) != 0 {
		t.Fatalf("a restart stamp is not a configuration exception, got %+v", edits)
	}
}

// The exact live shape of the case P1-11 was opened for: a field on the RTC Ingress
// owned by a person, maintained by nothing. This is the one finding that must stay
// loud, because nothing in the system will ever put it back or take it away.
func TestTheIngressCaseIsReportedLoudly(t *testing.T) {
	obj := ObjectFields{Resource: "ingress", Namespace: "ess", Name: "ess-matrix-rtc", Entries: []ManagedFieldsEntry{
		{Manager: "helm", FieldsV1: json.RawMessage(`{"f:spec":{"f:rules":{}}}`)},
		{Manager: "kubectl-patch", FieldsV1: json.RawMessage(`{"f:spec":{"f:ingressClassName":{}}}`)},
	}}
	// Hooks that maintain the SFU say nothing about this Ingress.
	hooks := map[string][]string{
		HookKey("deployment", "ess", "ess-matrix-rtc-sfu"): {"spec.template.spec.hostNetwork"},
	}
	edits := FindManualEdits([]ObjectFields{obj}, hooks)
	if len(edits) != 1 {
		t.Fatalf("expected the ingress finding, got %+v", edits)
	}
	if edits[0].Covered || edits[0].Kind != Human {
		t.Fatalf("must be an uncovered human edit: %+v", edits[0])
	}
	if un, _, _ := SummariseManual(edits); un != 1 {
		t.Fatalf("it must count as unmaintained, got %d", un)
	}
}
