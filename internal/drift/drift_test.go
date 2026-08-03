package drift

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeFetcher struct {
	objs map[string]string // "resource/ns/name" -> JSON
	err  error
}

func (f fakeFetcher) GetObjectJSON(_ context.Context, resource, ns, name string) ([]byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	key := resource + "/" + ns + "/" + name
	body, ok := f.objs[key]
	if !ok {
		return nil, errors.New("not found: " + key)
	}
	return []byte(body), nil
}

// The two real built-in hook actions, verbatim.
const hostNetworkPatch = `[{"op":"add","path":"/spec/template/spec/hostNetwork","value":true},` +
	`{"op":"add","path":"/spec/template/spec/dnsPolicy","value":"ClusterFirstWithHostNet"}]`

const policyPatch = `{"spec":{"externalTrafficPolicy":"Local"}}`

func sfuAction() Action {
	return Action{
		Hook: "ESS RTC: SFU Host Network", Description: "Set hostNetwork=true",
		Resource: "deployment", Namespace: "ess", Name: "ess-matrix-rtc-sfu",
		PatchType: "json", Patch: hostNetworkPatch,
	}
}

func svcAction(name string) Action {
	return Action{
		Hook: "ESS RTC: Service ExternalTrafficPolicy", Description: "externalTrafficPolicy=Local",
		Resource: "service", Namespace: "ess", Name: name,
		PatchType: "merge", Patch: policyPatch,
	}
}

func one(t *testing.T, a Action, f Fetcher) Finding {
	t.Helper()
	got := Check(context.Background(), []Action{a}, f)
	if len(got) != 1 {
		t.Fatalf("expected exactly one finding, got %d", len(got))
	}
	return got[0]
}

func TestPatchAlreadyAppliedIsSatisfied(t *testing.T) {
	f := fakeFetcher{objs: map[string]string{
		"deployment/ess/ess-matrix-rtc-sfu": `{"spec":{"template":{"spec":{"hostNetwork":true,"dnsPolicy":"ClusterFirstWithHostNet"}}}}`,
	}}
	got := one(t, sfuAction(), f)
	if got.Status != Satisfied {
		t.Fatalf("expected satisfied, got %s (%s)", got.Status, got.Detail)
	}
}

// The 2026-08-02 outage, reproduced. A Helm upgrade run outside MatrixCtrl
// re-rendered the deployment without hostNetwork; the hooks never fired; the SFU
// stopped binding its host ports; every page stayed green. This is the test that
// would have caught it.
func TestHostNetworkLostByHelmUpgradeIsDrifted(t *testing.T) {
	f := fakeFetcher{objs: map[string]string{
		"deployment/ess/ess-matrix-rtc-sfu": `{"spec":{"template":{"spec":{"containers":[{"name":"sfu"}]}}}}`,
	}}
	got := one(t, sfuAction(), f)

	if got.Status != Drifted {
		t.Fatalf("expected drifted, got %s (%s)", got.Status, got.Detail)
	}
	if !containsPath(got.Paths, "spec.template.spec.hostNetwork") {
		t.Fatalf("the finding must name the field, got paths %v", got.Paths)
	}
}

// A value that is present but wrong must drift, not just a missing one. After the
// same upgrade the Services were not missing externalTrafficPolicy — they had
// fallen back to Cluster, which reads like a configured value.
func TestWrongValueIsDriftedNotSatisfied(t *testing.T) {
	f := fakeFetcher{objs: map[string]string{
		"service/ess/ess-matrix-rtc-sfu-tcp": `{"spec":{"externalTrafficPolicy":"Cluster","type":"NodePort"}}`,
	}}
	got := one(t, svcAction("ess-matrix-rtc-sfu-tcp"), f)

	if got.Status != Drifted {
		t.Fatalf("Cluster must drift against a Local patch, got %s", got.Status)
	}
	if !containsPath(got.Paths, "spec.externalTrafficPolicy") {
		t.Fatalf("expected the policy field in paths, got %v", got.Paths)
	}
}

func TestMergePatchAlreadySatisfied(t *testing.T) {
	f := fakeFetcher{objs: map[string]string{
		"service/ess/ess-matrix-rtc-sfu-turn": `{"spec":{"externalTrafficPolicy":"Local","type":"NodePort"}}`,
	}}
	if got := one(t, svcAction("ess-matrix-rtc-sfu-turn"), f); got.Status != Satisfied {
		t.Fatalf("expected satisfied, got %s (%s)", got.Status, got.Detail)
	}
}

// The whole point of the package. A cluster read that fails must never render as
// "fine" — that is the failure mode being fixed, and re-introducing it one layer up
// would be worse than having no check.
func TestFetchErrorIsUnknownNeverSatisfied(t *testing.T) {
	got := one(t, sfuAction(), fakeFetcher{err: errors.New("connection refused")})
	if got.Status != Unknown {
		t.Fatalf("a failed read must be unknown, got %s", got.Status)
	}
	if !strings.Contains(got.Detail, "connection refused") {
		t.Fatalf("the reason must survive into the finding, got %q", got.Detail)
	}
}

// Greenfield: no SFU deployment exists. That is not drift, and calling it drift
// would train the operator to ignore the report.
func TestMissingResourceIsUnknownNotDrifted(t *testing.T) {
	if got := one(t, sfuAction(), fakeFetcher{objs: map[string]string{}}); got.Status != Unknown {
		t.Fatalf("an absent resource must be unknown, got %s", got.Status)
	}
}

func TestNoClusterIsUnknown(t *testing.T) {
	if got := one(t, sfuAction(), nil); got.Status != Unknown {
		t.Fatalf("no fetcher must be unknown, got %s", got.Status)
	}
}

// Strategic merge needs the typed schema to handle lists. Approximating it would
// report drift on fields that are fine, so it is refused out loud.
func TestStrategicMergeIsRefusedRatherThanGuessed(t *testing.T) {
	a := sfuAction()
	a.PatchType = "strategic"
	f := fakeFetcher{objs: map[string]string{"deployment/ess/ess-matrix-rtc-sfu": `{"spec":{}}`}}
	got := one(t, a, f)
	if got.Status != Unknown {
		t.Fatalf("strategic must be unknown, got %s", got.Status)
	}
	if !strings.Contains(got.Detail, "schema") {
		t.Fatalf("the detail should say why, got %q", got.Detail)
	}
}

func TestUndecodablePatchIsUnknown(t *testing.T) {
	a := sfuAction()
	a.Patch = `[{"op":"nonsense"}]`
	f := fakeFetcher{objs: map[string]string{"deployment/ess/ess-matrix-rtc-sfu": `{"spec":{}}`}}
	if got := one(t, a, f); got.Status != Unknown {
		t.Fatalf("a broken patch must be unknown, got %s (%s)", got.Status, got.Detail)
	}
}

// Nothing here knows about the SFU. A hook someone writes next year for an
// unrelated resource has to work without touching this package.
func TestWorksForAHookThisPackageHasNeverSeen(t *testing.T) {
	a := Action{
		Hook: "Someone's future hook", Description: "set replicas",
		Resource: "statefulset", Namespace: "other", Name: "whatever",
		PatchType: "merge", Patch: `{"spec":{"replicas":3}}`,
	}
	f := fakeFetcher{objs: map[string]string{
		"statefulset/other/whatever": `{"spec":{"replicas":1}}`,
	}}
	got := one(t, a, f)
	if got.Status != Drifted || !containsPath(got.Paths, "spec.replicas") {
		t.Fatalf("expected drift on spec.replicas, got %s %v", got.Status, got.Paths)
	}
}

// An empty namespace must resolve the same way the apply path resolves it, or the
// check would read a different object than the patch writes.
func TestEmptyNamespaceDefaultsToEssLikeTheApplyPath(t *testing.T) {
	a := svcAction("ess-matrix-rtc-sfu-turn")
	a.Namespace = ""
	f := fakeFetcher{objs: map[string]string{
		"service/ess/ess-matrix-rtc-sfu-turn": `{"spec":{"externalTrafficPolicy":"Local"}}`,
	}}
	got := one(t, a, f)
	if got.Status != Satisfied {
		t.Fatalf("empty namespace should have resolved to ess, got %s (%s)", got.Status, got.Detail)
	}
	if got.Namespace != "ess" {
		t.Fatalf("the finding should report the resolved namespace, got %q", got.Namespace)
	}
}

func TestSummaryCountsEveryStatus(t *testing.T) {
	f := fakeFetcher{objs: map[string]string{
		"deployment/ess/ess-matrix-rtc-sfu":  `{"spec":{"template":{"spec":{"hostNetwork":true,"dnsPolicy":"ClusterFirstWithHostNet"}}}}`,
		"service/ess/ess-matrix-rtc-sfu-tcp": `{"spec":{"externalTrafficPolicy":"Cluster"}}`,
	}}
	got := Check(context.Background(), []Action{
		sfuAction(),                             // satisfied
		svcAction("ess-matrix-rtc-sfu-tcp"),     // drifted
		svcAction("ess-matrix-rtc-sfu-missing"), // unknown
	}, f)

	s := Summary(got)
	if s[Satisfied] != 1 || s[Drifted] != 1 || s[Unknown] != 1 {
		t.Fatalf("expected 1/1/1, got %v", s)
	}
}

func containsPath(paths []string, want string) bool {
	for _, p := range paths {
		if p == want {
			return true
		}
	}
	return false
}
