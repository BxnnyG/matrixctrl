package k8s

import (
	"context"
	"fmt"
	"strings"

	authv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// The permissions MatrixCtrl needs, as data.
//
// Etappe 37 replaced a ClusterRole of `apiGroups: ["*"] resources: ["*"] verbs: ["*"]`
// with an enumerated one. Enumerating creates a new failure mode the wildcard did not
// have: a permission that was never granted, discovered *during* an upgrade, which
// leaves the release half-applied — the state this install has already had to be
// recovered from once (revision 26, 2026-08-06).
//
// So the list lives here rather than only in the chart, and `Check` asks the API
// server — via SelfSubjectAccessReview, which every authenticated account may do
// without any grant of its own — whether the running ServiceAccount actually holds
// it. A YAML diff against the chart would only prove the chart says what it says;
// this proves what the cluster will do. It also catches the case the chart cannot
// see at all: a role edited by hand, or a binding that was never applied.

// Permission is one thing MatrixCtrl must be allowed to do.
type Permission struct {
	// Group is the API group; "" is core.
	Group string
	// Resource is the plural resource name. Subresource is set separately because
	// SubjectAccessReview takes them as distinct fields, not as "pods/log".
	Resource    string
	Subresource string
	Verb        string
	// Namespaced marks the permission as needed in the managed namespace. When
	// false it is checked cluster-wide, which is a strictly stronger question.
	Namespaced bool
	// Why names the call site or the chart kind that requires it. It is not
	// decoration: when a check fails, this is the sentence that tells the operator
	// which feature they are about to lose.
	Why string
}

func (p Permission) String() string {
	res := p.Resource
	if p.Subresource != "" {
		res += "/" + p.Subresource
	}
	if p.Group != "" {
		res = p.Group + "/" + res
	}
	scope := "cluster"
	if p.Namespaced {
		scope = "namespaced"
	}
	return fmt.Sprintf("%s %s (%s)", p.Verb, res, scope)
}

// helmVerbs is what Helm needs on every kind the chart renders. Helm decides at
// apply time which one each object gets — three-way merge patches an existing
// object, creates a new one, deletes on rollback — so the set is not divisible
// per kind without guessing.
var helmVerbs = []string{"get", "list", "watch", "create", "update", "patch", "delete"}

// chartKinds is what matrix-stack can render, including the Helm hooks that never
// appear in `helm get manifest`. Reading only the live release would have produced a
// permission set that passes today and fails on the next upgrade.
var chartKinds = []struct {
	group     string
	resources []string
	why       string
}{
	{"", []string{"secrets"}, "Helm release storage, and the chart's own Secrets"},
	{"", []string{"configmaps", "services", "serviceaccounts", "persistentvolumeclaims"}, "rendered by matrix-stack"},
	{"apps", []string{"deployments", "statefulsets"}, "rendered by matrix-stack"},
	{"networking.k8s.io", []string{"ingresses"}, "rendered by matrix-stack"},
	{"batch", []string{"jobs"}, "Helm hooks: init-secrets, deployment-markers, synapse-check-config"},
	{"rbac.authorization.k8s.io", []string{"roles", "rolebindings"}, "matrix-stack's three namespaced Roles"},
}

// RequiredPermissions is the full set, built once at init so the chart and the check
// cannot drift into two different opinions.
var RequiredPermissions = buildRequired()

func buildRequired() []Permission {
	out := []Permission{
		// --- MatrixCtrl's own reads and writes ---------------------------------
		{Group: "", Resource: "pods", Verb: "list", Namespaced: true, Why: "pod list and drill-down"},
		{Group: "", Resource: "pods", Verb: "get", Namespaced: true, Why: "pod drill-down"},
		{Group: "", Resource: "pods", Verb: "watch", Namespaced: true, Why: "Helm --wait readiness"},
		{Group: "", Resource: "pods", Verb: "delete", Namespaced: true, Why: "restart a pod; clean up Evicted pods"},
		{Group: "", Resource: "pods", Subresource: "log", Verb: "get", Namespaced: true, Why: "pod logs and rollout failure causes"},
		{Group: "", Resource: "events", Verb: "list", Namespaced: true, Why: "why a pod is unhealthy, not just that it is"},

		// Cluster-scoped, genuinely.
		{Group: "", Resource: "nodes", Verb: "list", Why: "dashboard capacity; mapping pods to nodes"},
		{Group: "", Resource: "namespaces", Verb: "get", Why: "greenfield install"},
		{Group: "", Resource: "namespaces", Verb: "create", Why: "greenfield install creates the ESS namespace"},

		// --- Hook patches (internal/k8s/patch.go knownGVRs) ---------------------
		{Group: "apps", Resource: "daemonsets", Verb: "patch", Namespaced: true, Why: "hook patch target"},
		{Group: "networking.k8s.io", Resource: "ingresses", Verb: "patch", Namespaced: true, Why: "hook patch target"},

		// --- Helm's readiness check, which lives in Helm's internals -------------
		//
		// Wait is on for install, upgrade and rollback. Checking a Deployment ready
		// means calling GetNewReplicaSet, which lists ReplicaSets in the namespace.
		// The chart renders no ReplicaSet, and MatrixCtrl never touches one, so
		// neither source this list was otherwise built from mentions them — the
		// first draft of the scoped role omitted this and would have made every ESS
		// upgrade apply and then fail while waiting.
		{Group: "apps", Resource: "replicasets", Verb: "list", Namespaced: true, Why: "Helm --wait readiness for Deployments"},
		{Group: "apps", Resource: "replicasets", Verb: "get", Namespaced: true, Why: "Helm --wait readiness for Deployments"},
	}

	for _, k := range chartKinds {
		for _, res := range k.resources {
			for _, verb := range helmVerbs {
				out = append(out, Permission{
					Group: k.group, Resource: res, Verb: verb,
					Namespaced: true, Why: k.why,
				})
			}
		}
	}
	return out
}

// OptionalPermissions are not required to run. A missing one costs exactly the
// feature named in Why, and the product degrades rather than failing — metrics
// absent means "no data" on the dashboard, not a broken page.
var OptionalPermissions = []Permission{
	{Group: "metrics.k8s.io", Resource: "nodes", Verb: "list", Why: "live node CPU/memory (needs metrics-server)"},
	{Group: "", Resource: "secrets", Verb: "list", Why: "cluster-wide ESS discovery in the setup wizard"},
}

// KnownOverGrants is empty since etappe 40, and the empty list is the point.
//
// E37 scoped the role by resource type and verb but left it bound cluster-wide, so
// its namespaced rules still applied everywhere: `kubectl auth can-i list secrets
// -n kube-system` answered **yes**. That gap was recorded here as three assertions
// rather than as a paragraph, with a test written to *fail* when they disappeared —
// so that closing it would announce itself instead of leaving three files
// describing a problem that no longer existed.
//
// E40 moved every namespaced rule into a Role in the managed namespace, the
// assertions flipped, and the test failed exactly as designed. The entries moved to
// ForbiddenAlways below. This slice stays, empty, because the next person to widen
// the role should have somewhere obvious to write down what they widened it by.
var KnownOverGrants = []Permission{}

// ForbiddenAlways are the powers etappe 37 removed. Unlike the over-grants, these
// must stay denied — if any becomes allowed, the wildcard has grown back, whether
// through an edited role or a second binding nobody remembered.
var ForbiddenAlways = []Permission{
	{Group: "rbac.authorization.k8s.io", Resource: "clusterroles", Verb: "create", Why: "would allow granting itself anything"},
	{Group: "rbac.authorization.k8s.io", Resource: "roles", Verb: "escalate", Namespaced: true, Why: "escalation"},
	{Group: "apiextensions.k8s.io", Resource: "customresourcedefinitions", Verb: "list", Why: "no CRD is part of managing ESS"},
	{Group: "", Resource: "serviceaccounts", Subresource: "token", Verb: "create", Namespaced: true, Why: "would mint tokens for more privileged accounts"},
	{Group: "", Resource: "pods", Subresource: "exec", Verb: "create", Namespaced: true, Why: "shell into any managed pod"},
	{Group: "", Resource: "namespaces", Verb: "delete", Why: "nothing in the product asks for it"},
	{Group: "", Resource: "users", Verb: "impersonate", Why: "impersonation"},
}

// ConfinedToNamespace must be denied *outside* the managed namespace, and is
// required inside it. The distinction is the whole of etappe 40: these are not
// powers MatrixCtrl should never have, they are powers it should only have where
// it works.
//
// Checked against a namespace the panel has no business in — see
// TestNamespaceConfinementLive. Every one of them answered `allowed` until the
// rules moved out of a ClusterRoleBinding and into a Role. Secrets are why P0-4a
// outranked everything else: Helm's release storage needs them in the managed
// namespace, and a cluster-wide binding turned that into every secret in the
// cluster, readable and writable.
var ConfinedToNamespace = []Permission{
	{Group: "", Resource: "secrets", Verb: "list", Namespaced: true, Why: "secrets outside the managed namespace"},
	{Group: "", Resource: "secrets", Verb: "get", Namespaced: true, Why: "secrets outside the managed namespace"},
	{Group: "", Resource: "secrets", Verb: "delete", Namespaced: true, Why: "secrets outside the managed namespace"},
	{Group: "apps", Resource: "deployments", Verb: "delete", Namespaced: true, Why: "workloads outside the managed namespace"},
	{Group: "apps", Resource: "deployments", Verb: "patch", Namespaced: true, Why: "workloads outside the managed namespace"},
	{Group: "", Resource: "configmaps", Verb: "update", Namespaced: true, Why: "config outside the managed namespace"},
	{Group: "rbac.authorization.k8s.io", Resource: "rolebindings", Verb: "create", Namespaced: true, Why: "granting itself rights in another namespace"},
	{Group: "", Resource: "pods", Verb: "delete", Namespaced: true, Why: "deleting pods outside the managed namespace"},
}

// PermissionCheck is one permission and the API server's answer about it.
type PermissionCheck struct {
	Permission
	Allowed bool
	// Reason is the authorizer's own explanation, kept verbatim. It usually names
	// the rule that matched, which is the fastest route from "denied" to the line
	// of YAML that has to change.
	Reason string
}

// Check asks the API server which of the given permissions the current identity
// actually holds, in namespace ns.
//
// SelfSubjectAccessReview rather than SubjectAccessReview on purpose: it asks about
// *this* process's identity, so it answers the question that matters — "will my next
// call succeed" — and it needs no permission to perform, being granted to
// system:authenticated by the built-in system:basic-user role. A SubjectAccessReview
// would require impersonation-adjacent rights this role deliberately does not have.
func (c *Client) Check(ctx context.Context, ns string, perms []Permission) ([]PermissionCheck, error) {
	if c == nil || c.Static == nil {
		return nil, fmt.Errorf("kubernetes client unavailable")
	}

	out := make([]PermissionCheck, 0, len(perms))
	for _, p := range perms {
		attrs := &authv1.ResourceAttributes{
			Group:       p.Group,
			Resource:    p.Resource,
			Subresource: p.Subresource,
			Verb:        p.Verb,
		}
		if p.Namespaced {
			attrs.Namespace = ns
		}

		review := &authv1.SelfSubjectAccessReview{
			Spec: authv1.SelfSubjectAccessReviewSpec{ResourceAttributes: attrs},
		}
		res, err := c.Static.AuthorizationV1().SelfSubjectAccessReviews().
			Create(ctx, review, metav1.CreateOptions{})
		if err != nil {
			// The review itself failing is not the same as a denial, and reporting it
			// as one would send the operator to edit a role that is fine.
			return out, fmt.Errorf("access review for %s: %w", p, err)
		}
		out = append(out, PermissionCheck{
			Permission: p,
			Allowed:    res.Status.Allowed,
			Reason:     res.Status.Reason,
		})
	}
	return out, nil
}

// Missing returns only the denied checks.
func Missing(checks []PermissionCheck) []PermissionCheck {
	var out []PermissionCheck
	for _, c := range checks {
		if !c.Allowed {
			out = append(out, c)
		}
	}
	return out
}

// Describe renders denied checks as one line each, grouped by the reason they were
// wanted, so an operator reads "what breaks" before "which verb is missing".
func Describe(checks []PermissionCheck) string {
	missing := Missing(checks)
	if len(missing) == 0 {
		return ""
	}
	byWhy := map[string][]string{}
	order := []string{}
	for _, m := range missing {
		if _, seen := byWhy[m.Why]; !seen {
			order = append(order, m.Why)
		}
		byWhy[m.Why] = append(byWhy[m.Why], m.Permission.String())
	}
	var b strings.Builder
	for _, why := range order {
		fmt.Fprintf(&b, "%s:\n", why)
		for _, p := range byWhy[why] {
			fmt.Fprintf(&b, "  - %s\n", p)
		}
	}
	return b.String()
}
