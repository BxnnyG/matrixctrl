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

	checks, err := c.Check(ctx, ns, RequiredPermissions)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	t.Logf("checked %d required permissions in namespace %q", len(checks), ns)

	if missing := Missing(checks); len(missing) > 0 {
		t.Errorf("%d required permission(s) denied:\n%s", len(missing), Describe(checks))
	}

	// Optional ones are reported, never failed: a denial costs the named feature
	// and nothing else, and the code already treats absence as "no data".
	opt, err := c.Check(ctx, ns, OptionalPermissions)
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

	checks, err := c.Check(ctx, essNamespace(), ForbiddenAlways)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	for _, ch := range checks {
		if ch.Allowed {
			t.Errorf("still permitted: %s — %s", ch.Permission, ch.Why)
		}
	}
}

// TestKnownOverGrantsLive records what the role permits beyond its purpose, aimed at
// a namespace MatrixCtrl has no business in.
//
// It does not fail on them: they are the documented limit of etappe 37, which scoped
// the role by resource type and verb but left it bound cluster-wide. It fails if one
// *disappears* without the list being updated — that means the namespaced Role
// landed, and the honest paragraphs in clusterrole.yaml and BACKLOG.md are now
// describing a problem that no longer exists.
func TestKnownOverGrantsLive(t *testing.T) {
	if os.Getenv("RUN_LIVE") == "" {
		t.Skip("set RUN_LIVE=1 to run against a live cluster")
	}

	const unrelated = "kube-system"

	c, err := New()
	if err != nil {
		t.Fatalf("k8s client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	checks, err := c.Check(ctx, unrelated, KnownOverGrants)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	for _, ch := range checks {
		if ch.Allowed {
			t.Logf("over-granted in %s (known): %s — %s", unrelated, ch.Permission, ch.Why)
			continue
		}
		t.Errorf("no longer over-granted in %s: %s — namespace containment has landed; "+
			"update KnownOverGrants, clusterrole.yaml and BACKLOG.md", unrelated, ch.Permission)
	}
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
	for _, p := range append(append([]Permission{}, RequiredPermissions...), OptionalPermissions...) {
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
