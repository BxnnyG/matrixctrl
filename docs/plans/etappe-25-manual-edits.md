# Etappe 25 — Which fields did a human change by hand?

**Date:** 2026-08-04 · **System:** S2 · **Closes:** the open half of P1-11

## The problem

E21 checks every patch a hook *declares*. It cannot see an edit no hook knows
about — and that is the case P1-11 was actually opened for: the RTC Ingress
carried `ingressClassName: disabled` and `kubernetes.io/ingress.class: ignore`,
neither rendered by the chart, both applied by hand 69 days earlier. Helm's
three-way merge preserves fields it has never owned, so the exception outlived
every upgrade and nothing ever mentioned it. Changing the RTC hostname then
produced a correct Ingress that Traefik was still instructed to ignore.

Today the same class of leftover was found again — a hand-built `IngressRoute`
routing the old RTC host into the SFU — and again by hand, not by the product.
Twice found manually is the argument for building it.

## Why the plan in the backlog was wrong

P1-11 proposed rendering the release's manifests and diffing them against live
objects. That is the obvious approach and it is bad: a live object carries
hundreds of fields the manifest never mentions — defaulted values, `clusterIP`,
`resourceVersion`, status. A naive diff reports all of them, so it needs a
curated list of fields to watch, which only ever finds what someone already
thought of. Same shape of mistake as §4.18 and §4.21 both warned about.

## The mechanism that already exists

The API server tracks field ownership itself. Every object carries
`metadata.managedFields`: one entry per manager, each listing exactly which
fields that manager set. On this cluster's SFU deployment:

```
helm             spec (the chart)
matrixctrl       spec.strategy, initContainer images
kubectl-rollout  spec.template.metadata.annotations…restartedAt
kubectl-patch    spec.template.spec.hostNetwork, …dnsPolicy
```

`kubectl-patch` owning `hostNetwork` is a person at a terminal — in this case the
agent, on 2026-08-02, restoring by hand what the product should have applied.
That is the finding, and it needs no diffing, no rendering and no curated list:
the cluster is asked who owns the field and it answers.

## Approach

1. Metadata-only list per resource type in the ESS namespace. `managedFields`
   lives in metadata, so `metadata.Interface` returns everything needed and
   nothing else — the pattern E20 introduced.
2. Classify each manager: **human** (`kubectl-*`), **known automation** (helm,
   matrixctrl, k3s, controllers), or **foreign** (anything else).
3. Report human-owned fields; report foreign managers more quietly.
4. Cross-reference with the hook patches E21 already loads. A hand-edit on a
   field a hook maintains is a different statement from one on a field nothing
   maintains, and collapsing them would bury the second in the first.

Levels:

- human-owned, **no hook covers it** → warn. Nothing maintains this; it survives
  upgrades silently and will surprise whoever meets it next.
- human-owned, **a hook covers it** → info. The intent is owned; someone went
  around the product to apply it.
- foreign manager → info. Another tool owns this, which may be entirely correct.

`status` is always excluded — controllers own it by definition.

## The judgement call: which manager means "human"?

Only `kubectl-*` is reported as human, because it is the one manager name that
unambiguously means someone ran a command. Everything else is either known
automation or an unknown tool, and treating unknown tools as human would fire on
every cluster that runs an operator the product has never heard of. Those are
still surfaced, one level quieter, so the report is complete without crying wolf.

## Mandatory edge-case walk ([PROZESS §1](../PROZESS.md))

1. **No ESS / ESS elsewhere.** Lists a namespace; empty is empty, not an error.
2. **Helm release in a bad state.** Untouched — reads objects, not releases.
3. **Not just Deployments.** StatefulSets, Services, Ingresses and ConfigMaps are
   all listed; the SFU case would have been missed by Deployments alone, and the
   Ingress case *only* appears on an Ingress.
4. **Cluster slow or gone.** Per-type failure degrades to unknown for that type
   and still reports the others.
5. **No outbound internet.** Cluster-local.
6. **Both auth modes.** New route behind the same middleware as `/drift`.
7. **Config edge shapes.** `managedFields` absent, empty, or with an unparseable
   `fieldsV1` — each covered by a test.
8. **Helm succeeded, hooks failed.** Orthogonal; E21 answers that.

## Definition of done

- Production reports `kubectl-patch` owning `hostNetwork` on the SFU deployment
- A field owned only by `helm` is never reported
- Field-path extraction tested without a cluster, including list-key entries
- Hook cross-reference tested: same field, covered and uncovered
- S11 green **after** the deploy

## Outcome (2026-08-04)

Shipped in `0.1.26`. S11 all four green after the deploy (revision 28).

### What production says

Verified against the live namespace before shipping — 40 objects, 98 ownership
entries, and with the real hooks cross-referenced:

```
unmaintained=1  by-hand=4  foreign=0
  [nothing maintains it] ingress    ess-matrix-rtc            kubectl-patch  spec.ingressClassName
  [a hook maintains it]  deployment ess-matrix-rtc-sfu        kubectl-patch  spec.template.spec.{hostNetwork,dnsPolicy}
  [a hook maintains it]  service    …-muxed-udp / -tcp / -turn kubectl-patch  spec.externalTrafficPolicy
```

The one loud line is on **the same object P1-11 was opened about**. The four quiet
ones are the agent's own manual restorations of 2026-08-02, which is exactly what
"someone went around the product" is supposed to look like.

### Calibrated against reality, not against the plan

The first live run produced eight findings, three of which were
`kubectl rollout restart`'s `restartedAt` stamp and one ESS's own `matrix-tools`.
Both are now excluded: a restart stamp records that something happened rather than
changing configuration, and `matrix-tools` is part of the managed stack. Half a
report being noise is how a report gets ignored, and the two findings that mattered
were sitting underneath it.

### The link the unit tests cannot reach

`internal/k8s/ownership_live_test.go` (`RUN_LIVE=1`) asserts that a metadata-only
list really returns `managedFields`. If the API server stripped them the report
would be empty and would look exactly like a clean cluster — the one failure mode
of this feature that is invisible by construction.
