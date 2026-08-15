# Etappe 37 — RBAC scoping (P0-4)

**Status:** planned · 2026-08-15

## The problem

`deploy/helm/matrixctrl/templates/clusterrole.yaml` grants:

```yaml
- apiGroups: ["*"]
  resources: ["*"]
  verbs: ["*"]
- nonResourceURLs: ["*"]
  verbs: ["*"]
```

bound cluster-wide to the `matrixctrl` ServiceAccount. That is `cluster-admin`
spelled differently.

Why it is the top backlog entry: it turns *any* defect in this app — an auth
bypass, an SSRF, a dependency RCE — from "the ESS deployment is compromised"
into "the cluster is compromised". Every other finding is amplified by it.

E36 raised the stakes rather than lowering them: the process now holds a live
Synapse-admin token in memory and makes outbound HTTP calls on the operator's
behalf. The blast radius of a mistake in that code path is currently the whole
cluster.

## What the file's own comment claims, and what is actually true

> Helm SDK needs broad access to install/upgrade/rollback releases which can
> create arbitrary resources. Scoping this tighter would break upgrades of
> releases that contain CRDs, ClusterRoles, etc.

Half true, and the half that is false is doing all the work. Measured against
the real chart rather than assumed:

**What the live ESS release actually contains** (`helm get manifest ess -n ess`):

| apiGroup | kinds |
|---|---|
| (core) | ConfigMap, PersistentVolumeClaim, Secret, Service, ServiceAccount |
| `apps` | Deployment, StatefulSet |
| `networking.k8s.io` | Ingress |

**What the whole chart can create**, including Helm hooks, which do *not* appear
in `get manifest` (grep over `matrix-stack/templates` + subcharts):

| apiGroup | kinds | note |
|---|---|---|
| (core) | Service, ConfigMap, Secret, PersistentVolumeClaim, ServiceAccount | |
| `apps` | Deployment, StatefulSet | |
| `networking.k8s.io` | Ingress | |
| `batch` | Job | hooks: init-secrets, deployment-markers, synapse-check-config |
| `rbac.authorization.k8s.io` | Role, RoleBinding | **namespaced**, 3 each |
| `monitoring.coreos.com` | ServiceMonitor | only with Prometheus Operator |
| `cert-manager.io` | Certificate | only with cert-manager |

So: **no CRDs and no ClusterRoles**. The chart creates namespaced `Role`s, which
is a different and far smaller thing than the comment implies. Thirteen kinds
across seven groups — not "arbitrary".

**Escalation-prevention check.** Kubernetes refuses to let an account create a
Role granting permissions it does not itself hold. The chart's three Roles grant:

- `secrets`: create, get, update
- `configmaps`: create, get, update
- `pods`: list · `statefulsets`: list, get, update

All namespaced to the release namespace, all of which MatrixCtrl needs there
anyway. So the scoped role satisfies escalation prevention without `escalate`
or `bind`. This is the point that would have made a tighter role fail, and it
does not.

## What MatrixCtrl itself needs, derived from the code

Not guessed — enumerated from the client-go call sites.

**Namespaced (the ESS release namespace, and its own):**

| resource | verbs | where |
|---|---|---|
| `pods` | list, get, delete | `k8s/pods.go`, `k8s/node.go:105` (evicted cleanup) |
| `pods/log` | get | `k8s/pods.go:225`, `k8s/rollout.go:131` |
| `deployments`, `statefulsets` | list, get, patch | drill-down, rollout, hook patches |
| `services` | list, get, patch | SFU `externalTrafficPolicy` patches |
| `configmaps`, `daemonsets`, `ingresses` | get, patch | `k8s/patch.go` `knownGVRs` |
| `events` | list | pod drill-down / restart cause |
| `persistentvolumeclaims` | list | storage panel |
| `secrets` | get, list, watch, create, update, patch, delete | **Helm release storage** |

**Cluster-scoped, genuinely:**

| resource | verbs | why |
|---|---|---|
| `nodes` | list | `k8s/node.go:22`, `k8s/pods.go:285` — dashboard capacity |
| `metrics.k8s.io` / `nodes` | get, list | `k8s/node.go:44` via `AbsPath` — this is a *resource* URL, not a non-resource one |

**Helm's own requirements** on every kind the chart renders: get, list, watch,
create, update, patch, delete — plus `pods` list/watch for `--wait` readiness.

**And one that neither source reveals.** `Wait` is on for install, upgrade and
rollback. Helm's readiness check for a Deployment calls `GetNewReplicaSet`, which
**lists ReplicaSets** in the namespace. The chart renders no ReplicaSet and
MatrixCtrl never touches one, so a role derived from "what the chart creates" plus
"what the code calls" — which is how this list was built — omits it. With seven
Deployments in ESS, every upgrade would have applied successfully and then failed
while waiting: the half-applied state this install has already been recovered from
once.

It was found by reading Helm's kube package rather than by the permission matrix,
which is the honest order of events and the reason the matrix now carries it.
Anything reached through a **library's** internals is invisible to both sources this
enumeration otherwise trusts.

## The two decisions this etappe has to make

### 1. `nonResourceURLs` — delete outright

k3s, like every conformant cluster, binds the built-in `system:discovery`
ClusterRole to the group `system:authenticated`, which includes every
ServiceAccount. Verified live: it already grants `get` on `/api`, `/api/*`,
`/apis`, `/apis/*`, `/version`, `/openapi/*`. Helm's RESTMapper needs nothing
more.

So `nonResourceURLs: ["*"] verbs: ["*"]` is pure surplus today. It is also the
rule that grants POST to arbitrary non-resource paths.

**Decision:** drop it, but re-state the read-only discovery paths explicitly in
our own role rather than depending on a cluster default we do not control. The
grant is innocuous and the assumption disappears.

### 2. Cross-namespace discovery — a control that turned out to control nothing

`internal/helm/discover.go` sets `AllNamespaces = true` to find an existing ESS
release to adopt. Helm implements that as **listing Secrets in every namespace**.
There is no narrower form: the release *is* a Secret, and RBAC cannot distinguish
a metadata-only list from a full one — the `Accept` header is not part of the
authorization decision, so requesting only ObjectMeta buys nothing.

**First decision, which was wrong.** Gate it behind `rbac.discovery.allNamespaces`
(default `false`), so the cluster-wide secret read is opt-in.

**What measuring it showed.** Rendered under a probe name, bound to a probe
ServiceAccount, with the flag off:

```
kubectl auth can-i list secrets -n kube-system  →  yes
```

A ClusterRole bound by a ClusterRoleBinding applies its *namespaced* rules in
every namespace. The base `secrets` rule — needed for Helm's release storage in
the managed namespace — therefore already granted exactly what the flag claimed
to withhold. The flag withheld nothing.

**Decision:** remove it. A security control that does not control anything is
worse than none, because it is believed. The limitation is stated in
`clusterrole.yaml` instead, and asserted in `KnownOverGrants` so it is a failing
test the day it stops being true.

The namespace-aware fallback in `Discover` stays: it is correct, it costs one
extra list, and it becomes load-bearing the moment the namespaced Role lands.

## Shape of the fix

A scoped **ClusterRole**, still bound cluster-wide — *not* a namespaced Role.

The reason is greenfield: MatrixCtrl creates the ESS namespace at deploy time
(`internal/helm/install.go:83`, `install.CreateNamespace = true`). A RoleBinding
cannot be pre-created in a namespace that does not exist yet, and MatrixCtrl
cannot create one for itself without `bind`/`escalate` — which would re-open
what this etappe closes.

So the containment this etappe delivers is **by resource type and verb**, not by
namespace. That is a smaller win than "namespaced", and the plan says so rather
than overselling it. What it removes is nonetheless the entire reason P0-4 is a
P0: arbitrary CRDs, arbitrary custom resources belonging to other operators,
`escalate`, `impersonate`, `bind`, `deletecollection` on anything, and every
non-resource URL.

Namespace containment is a follow-up (`P?-new`), and it needs the matrixctrl
chart to create the ESS namespace itself so a RoleBinding has somewhere to live.
Recorded, not attempted here.

`namespaces` get + create stays, cluster-scoped, for the same greenfield reason.

## Measured result, before applying anything

The role was rendered under a probe name, bound to a throwaway ServiceAccount,
and every entry asked of the API server as a `SubjectAccessReview` — the
authoritative form, since `kubectl auth can-i pods/log` parses subresources
differently and answered `yes` to a `serviceaccounts/token` question the API
server answers `false` to.

| set | result |
|---|---|
| 88 required permissions | **0 denied** |
| 7 powers that must stay denied | **7 denied** — `create clusterroles`, `escalate roles`, `list CRDs`, `create serviceaccounts/token`, `create pods/exec`, `delete namespaces`, `impersonate users` |
| 2 optional | both granted (metrics-server present) |
| 3 known over-grants, asked in `kube-system` | all 3 still granted — the documented limit |

## How it is verified

The backlog entry says this "needs a real upgrade run against a live release to
prove nothing broke". That is right but not sufficient — one upgrade exercises
one path. Two layers instead:

1. **A permission matrix checked with `SubjectAccessReview`.** The list above,
   encoded as a table in `internal/k8s/permissions.go`, and a test that asks the
   *API server* — not a YAML diff — whether the `matrixctrl` ServiceAccount may
   do each of them. Non-destructive, exhaustive, and it re-runs forever. This is
   the artefact that outlives the etappe.
2. **A real `helm upgrade` of the live ESS release** through the panel, after
   the scoped role is applied, plus S11.

Failure mode to watch for: an RBAC gap that only appears **mid-upgrade** leaves
the release in the `failed` state that revision 26 was already found in once. So
the matrix check runs *before* the upgrade, not after.

## Definition of done

- `clusterrole.yaml` enumerates groups, resources and verbs; no `*` in any of
  the three, `nonResourceURLs` limited to read-only discovery paths
- The permission matrix exists in code and its test passes against the live
  cluster
- A real ESS `helm upgrade` completes with the scoped role in force
- Signing in still works, and S11 is green **after** the deploy
- P0-4 in BACKLOG.md moves to done with the measurement that justified it
- DESIGN.md records why the scope is by-verb rather than by-namespace

## Out of scope, deliberately

- Namespaced Role/RoleBinding containment (needs chart-managed namespace)
- A preflight permission check surfaced in the UI before an upgrade — valuable,
  and the matrix from step 1 is exactly its input, but it is a feature and this
  is a hardening etappe
