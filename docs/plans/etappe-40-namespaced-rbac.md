# Etappe 40 — Confining the role to the namespace it manages (P0-4a)

**Status:** planned · 2026-08-15

## What E37 left

E37 scoped the ClusterRole by resource type and verb: no more `apiGroups: ["*"]`,
no `escalate`, no `impersonate`, no CRDs, no `pods/exec`. What it did **not** do is
scope by namespace, and it said so rather than pretending otherwise:

```
kubectl auth can-i list secrets -n kube-system  →  yes
```

A ClusterRole bound by a ClusterRoleBinding applies its namespaced rules in every
namespace. Helm's release storage needs `secrets` in the managed namespace, so the
panel holds read **and write** on every Secret in the cluster. That is most of what
made P0-4 a P0.

## The blocker I recorded, and why it was wrong

E37's plan stated:

> a RoleBinding cannot be created in a namespace that is not there … so the chart
> has to create the namespace — which conflicts with Helm ownership when adopting
> an ESS whose namespace already exists. That trade is the whole etappe.

There is no trade. Helm's `lookup` answers exactly this question, and it answers it
against the live cluster at install time. Verified rather than assumed:

| | `essNamespaceSeen` | `missingNamespaceSeen` |
|---|---|---|
| `helm template` (client-side) | `no` | `no` |
| `helm upgrade --dry-run=server` | **`yes`** | `no` |

So the chart can render the Namespace **only when it does not already exist**:

- **Greenfield** — absent, so the chart creates it, and the RoleBinding has
  somewhere to live.
- **Adopting an existing ESS** — present, so it is never rendered, and there is no
  ownership conflict to have.

Two things make that safe, and both are load-bearing rather than decorative:

1. `helm.sh/resource-policy: keep` on the Namespace. Without it, `helm uninstall
   matrixctrl` would **delete the ESS namespace and everything in it**.
2. The same annotation covers the second-order case: after a greenfield install the
   namespace exists, so the *next* upgrade's `lookup` renders nothing, and Helm
   deletes resources that were in the old manifest and not the new one. `keep`
   stops that too.

I recorded that blocker as fact in E37 without testing it. It cost this etappe a
day of not existing.

## What has to stay cluster-scoped, measured from the call sites

| resource | verbs | why |
|---|---|---|
| `nodes` | get, list | dashboard capacity, pod→node mapping |
| `metrics.k8s.io` / `nodes`, `pods` | get, list | live CPU/memory |
| `namespaces` | get, list, create | greenfield install |
| discovery non-resource URLs | get | Helm's RESTMapper |

Everything else moves into a `Role` in the managed namespace: pods, pods/log,
events, secrets, configmaps, services, serviceaccounts, PVCs, deployments,
statefulsets, daemonsets, replicasets, ingresses, jobs, roles, rolebindings.

## Two grants this etappe removes rather than relocates

Both were found by reading the call sites, and both are features paying rent in
permissions.

**`ListPVCs(ctx, "")` lists PVCs in every namespace** (`k8s/pods.go:302`, called
from `SysInfo`). It becomes a per-namespace read of the namespaces MatrixCtrl
actually manages. The storage panel then shows the storage belonging to the thing
this panel administers, which is what a reader of that page assumes it already
means.

**`SysInfo` counts pods in `kube-system`** (`handlers/status.go:239`). Keeping it
would require the chart to write a RoleBinding into the cluster's most sensitive
namespace, permanently, so one number can appear on a diagnostics page. Dropped.
An ESS admin panel does not need to know how many pods `kube-system` runs, and
"we install RBAC into kube-system" is a much larger ask of an operator than the
number is worth.

`matrixctrl`'s own namespace keeps its pod count, granted by a second RoleBinding
in the release namespace — that one the chart already owns.

## How it is verified, before it is applied

The same method as E37, which caught a missing rule that neither the chart nor our
own code would have revealed:

1. Render the new role and bindings under probe names, bind them to a throwaway
   ServiceAccount, and ask the **API server** — `SubjectAccessReview`, not
   `kubectl auth can-i`, whose subresource parsing disagrees with the authorizer.
2. `RequiredPermissions` must still be 90/90 in `ess`.
3. `ForbiddenAlways` must still be 7/7 denied.
4. **`KnownOverGrants` must flip to denied.** `TestKnownOverGrantsLive` is written
   to *fail* when that happens — that failure is this etappe's proof, and turning
   the assertion around is part of the work rather than a nuisance.
5. A real ESS `helm upgrade` under the new binding. Only the operator can trigger
   that from the panel, so it is stated as the outstanding half rather than
   claimed.

## Definition of done

- Cluster-wide grants reduced to the four rows above; everything else namespaced
- `helm.sh/resource-policy: keep` on the Namespace, and a test or rendered-output
  check that it is there — its absence is the difference between an uninstall and
  a disaster
- The permission matrix updated: over-grants become forbidden, and
  `KnownOverGrants` empties out
- Signing in still works, S11 green after the deploy
- P0-4a struck through with the `can-i` output reversed
