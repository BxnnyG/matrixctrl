package k8s

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestRequiredPermissionsLive asks the live cluster whether the identity this
// process runs as holds everything in RequiredPermissions.
//
// This is the check that makes etappe 37 safe to ship. Scoping the ClusterRole from
// `*` to an enumerated list introduced the possibility of a permission that is only
// missed halfway through a Helm upgrade, which leaves the release in the `failed`
// state this install has already had to be recovered from once. Running this first
// converts that into a list of lines to add, before anything is applied.
//
// Skipped unless RUN_LIVE=1. Not a CI test: it is a question about a cluster, and
// CI has none.
func TestRequiredPermissionsLive(t *testing.T) {
	if os.Getenv("RUN_LIVE") == "" {
		t.Skip("set RUN_LIVE=1 to run against a live cluster")
	}

	ns := essNamespace()

	c, err := New()
	if err != nil {
		t.Fatalf("k8s client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	sa := asServiceAccount(ctx, t, c)

	checks, err := sa.Check(ctx, ns, RequiredPermissions)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	t.Logf("checked %d required permissions in namespace %q", len(checks), ns)

	if missing := Missing(checks); len(missing) > 0 {
		t.Errorf("%d required permission(s) denied:\n%s", len(missing), Describe(checks))
	}

	// Optional ones are reported, never failed: a denial costs the named feature
	// and nothing else, and the code already treats absence as "no data".
	opt, err := sa.Check(ctx, ns, OptionalPermissions)
	if err != nil {
		t.Logf("optional check: %v", err)
		return
	}
	for _, o := range opt {
		state := "granted"
		if !o.Allowed {
			state = "not granted"
		}
		t.Logf("optional: %-46s %s — %s", o.Permission.String(), state, o.Why)
	}
}

// TestForbiddenPowersLive asserts that the powers etappe 37 removed are still gone.
//
// The required-permission test above only proves the role is wide enough. This is
// the other half: proof that it is not wide in the ways that made P0-4 a P0. Both
// pass trivially against a cluster-admin binding *except* this one, which is why it
// is the check that actually detects a regression — including one introduced by a
// second ClusterRoleBinding that has nothing to do with this chart.
func TestForbiddenPowersLive(t *testing.T) {
	if os.Getenv("RUN_LIVE") == "" {
		t.Skip("set RUN_LIVE=1 to run against a live cluster")
	}

	c, err := New()
	if err != nil {
		t.Fatalf("k8s client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sa := asServiceAccount(ctx, t, c)

	checks, err := sa.Check(ctx, essNamespace(), ForbiddenAlways)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	for _, ch := range checks {
		if ch.Allowed {
			t.Errorf("still permitted: %s — %s", ch.Permission, ch.Why)
		}
	}
}

// TestNamespaceConfinementLive is etappe 40's proof.
//
// The required-permission test shows the role is wide enough and the forbidden test
// shows it is not wide in the dangerous *kinds*. Neither can see the third axis:
// until E40 the role was a ClusterRole bound cluster-wide, so every rule written for
// the managed namespace applied in all of them, and `list secrets -n kube-system`
// answered yes.
//
// Its predecessor, TestKnownOverGrantsLive, asserted the opposite — that those
// grants were still present — specifically so that closing the gap would break a
// test rather than pass silently. It did, and this replaced it.
func TestNamespaceConfinementLive(t *testing.T) {
	if os.Getenv("RUN_LIVE") == "" {
		t.Skip("set RUN_LIVE=1 to run against a live cluster")
	}

	const unrelated = "kube-system"
	if unrelated == essNamespace() {
		t.Fatalf("the confinement check needs a namespace MatrixCtrl does not manage")
	}

	c, err := New()
	if err != nil {
		t.Fatalf("k8s client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Denied outside the managed namespace…
	sa := asServiceAccount(ctx, t, c)

	outside, err := sa.Check(ctx, unrelated, ConfinedToNamespace)
	if err != nil {
		t.Fatalf("check outside: %v", err)
	}
	for _, ch := range outside {
		if ch.Allowed {
			t.Errorf("still permitted in %s: %s — %s", unrelated, ch.Permission, ch.Why)
		}
	}

	// …and still granted inside it. Without this half the test would pass just as
	// well against a role that grants nothing at all, which would be a panel that
	// cannot do its job rather than a secure one.
	inside, err := sa.Check(ctx, essNamespace(), ConfinedToNamespace)
	if err != nil {
		t.Fatalf("check inside: %v", err)
	}
	for _, ch := range inside {
		if !ch.Allowed {
			t.Errorf("confinement went too far: %s is denied in the managed namespace %s",
				ch.Permission, essNamespace())
		}
	}

	if len(KnownOverGrants) != 0 {
		t.Errorf("KnownOverGrants should be empty after E40, has %d", len(KnownOverGrants))
	}
}

// asServiceAccount returns a client that speaks as the identity the deployed process
// runs as, whoever happens to be running the test.
//
// The comment on TestForbiddenPowersLive already said these checks "pass trivially
// against a cluster-admin binding". Nothing acted on it. So etappe 102 verified the
// media export by streaming a tar out of the Synapse pod from a maintainer's shell,
// reported it as proof, and shipped a feature the service account was forbidden to
// perform — `pods/exec` was in ForbiddenAlways at the time (§4.104).
//
// A permission check is a question about a subject. Asked about the wrong subject it
// is not a weaker check, it is a different question with a misleading answer. The
// subject is therefore established first, and impersonated when it is wrong.
func asServiceAccount(ctx context.Context, t *testing.T, c *Client) *Client {
	t.Helper()
	want := ServiceAccountUser(ownNamespace())

	who, err := c.WhoAmI(ctx)
	if err != nil {
		t.Fatalf("cannot establish which identity this test speaks as: %v\n"+
			"A permission check that does not know its own subject proves nothing.", err)
	}
	if who == want {
		t.Logf("speaking as %s", who)
		return c
	}

	t.Logf("running as %q, which is not %q — impersonating the service account", who, want)
	imp, err := c.As(want)
	if err != nil {
		t.Fatalf("running as %q and cannot build an impersonating client for %q: %v", who, want, err)
	}
	// Prove the impersonation is accepted before trusting a single answer from it.
	// A rejected impersonation would otherwise surface as a pile of denials that look
	// like a missing role.
	if _, err := imp.Check(ctx, essNamespace(), []Permission{
		{Group: "", Resource: "pods", Verb: "list", Namespaced: true, Why: "impersonation smoke test"},
	}); err != nil {
		t.Fatalf("impersonating %q was rejected: %v\n"+
			"Run this inside the pod, or with a kubeconfig allowed to impersonate.", want, err)
	}
	return imp
}

// ownNamespace is where MatrixCtrl itself runs — the namespace of the service
// account, not of the managed release.
func ownNamespace() string {
	if ns := os.Getenv("MATRIXCTRL_NAMESPACE"); ns != "" {
		return ns
	}
	return "matrixctrl"
}

func essNamespace() string {
	if ns := os.Getenv("MATRIXCTRL_ESS_NAMESPACE"); ns != "" {
		return ns
	}
	return "ess"
}

// TestNoWildcardsInRequired guards the list against quietly regrowing the thing
// etappe 37 removed. A `*` here would make every check pass against a cluster-admin
// binding and prove nothing.
func TestNoWildcardsInRequired(t *testing.T) {
	all := append([]Permission{}, RequiredPermissions...)
	all = append(all, OptionalPermissions...)
	all = append(all, ForbiddenAlways...)
	all = append(all, ConfinedToNamespace...)
	for _, p := range all {
		if p.Group == "*" || p.Resource == "*" || p.Verb == "*" {
			t.Errorf("wildcard in permission list: %s", p)
		}
		if p.Resource == "" || p.Verb == "" {
			t.Errorf("incomplete permission: %+v", p)
		}
		if p.Why == "" {
			t.Errorf("permission without a reason: %s", p)
		}
	}
}
