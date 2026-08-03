# Etappe 21 — Ask whether the patch is still applied

**Date:** 2026-08-03 · **System:** S2, S11 · **Addresses:** P1-11 (first half), and the
failure this agent caused on 2026-08-02

## The problem, from the operator's point of view

Calling broke. Every page in MatrixCtrl was green: pods healthy, release deployed,
SFU running, hooks listed and enabled. The SFU had silently stopped binding its host
ports because `hostNetwork: true` was no longer set on the deployment, and
`externalTrafficPolicy` had fallen back to `Cluster` on three services.

The operator spent an evening on their router.

**The cause was a Helm upgrade run outside MatrixCtrl** — by this agent, to change a
hostname. Helm re-rendered the deployment without the two patches, the post-upgrade
hooks never ran because the upgrade never went through MatrixCtrl, and nothing
noticed. `docs/DESIGN.md` S11 lists "the SFU patches survive a Helm upgrade" as one
of four regression checks that must pass before every ship. It is a sentence in a
document. Sentences do not run.

## What already exists, and why that is the whole design

The hooks are not prose. They are rows with `resource`, `name`, `namespace`,
`patch_type` and `patch` — a machine-readable statement of what must be true after
every upgrade:

```json
{"type":"kubectl_patch","resource":"deployment","name":"ess-matrix-rtc-sfu",
 "namespace":"ess","patch_type":"json",
 "patch":"[{\"op\":\"add\",\"path\":\"/spec/template/spec/hostNetwork\",\"value\":true}]"}
```

So the check needs no new specification and no curated list of fields to watch:

> Fetch the live object. Apply the hook's patch to it **in memory**. If the result
> differs from what is already there, the patch is not currently applied.

A patch that changes nothing is a patch that has already taken effect. That is the
entire test, it is exact, and it stays correct automatically when someone adds a
hook — including hooks this etappe will never see.

## Approach

- `internal/drift` takes the enabled `kubectl_patch` actions and answers, per action,
  `satisfied | drifted | unknown`.
- The comparison runs on the live object as fetched, using the same JSON-patch and
  merge-patch semantics the apply path uses. Reusing the semantics matters more than
  reusing the code: a checker that decides differently from the applier would report
  drift nobody can fix, or miss drift that is real.
- `unknown` is a first-class result. A missing object, an unparseable patch or an
  API error must never render as "satisfied" — that is the failure mode this whole
  etappe exists to remove, one layer up.
- Read-only. The check reports; it does not re-apply. Re-applying automatically
  would hide the fact that something reset it, and *that* is the information the
  operator needs.

### What this deliberately does not do

Detect manual edits **no hook knows about**. The RTC Ingress carried
`ingressClassName: disabled` and `kubernetes.io/ingress.class: ignore`, applied by
hand 69 days ago and preserved by Helm's three-way merge because they were never in
any rendered manifest. Finding those needs a different mechanism — diffing live
objects against the release manifest — and a curated field list, because a live
object carries hundreds of defaulted fields and a naive diff is pure noise.

That is the second half of P1-11 and it stays open. Shipping the half that is exact
beats shipping both halves where one of them cries wolf, because a drift report that
is usually wrong gets ignored within two weeks and then the exact half is gone too.

## Mandatory edge-case walk ([PROZESS §1](../PROZESS.md))

1. **No ESS / ESS elsewhere.** Hooks name their own namespace; a hook pointing at a
   resource that does not exist reports `unknown` with the reason, not `drifted` —
   greenfield has no SFU deployment and that is not a defect.
2. **Helm release in a bad state.** Irrelevant to the check: it reads live objects,
   not the release. A `failed` release with correctly patched objects is honestly
   reported as satisfied.
3. **Not just Deployments.** The hook schema already carries `resource`, so
   Services, StatefulSets and Ingresses work through the same path. No type is
   special-cased.
4. **The cluster is slow or gone.** Every fetch takes a context with a timeout; a
   failure is `unknown`, never `satisfied`.
5. **No outbound internet.** Untouched — API-server reads only.
6. **Both auth modes.** One new read-only route behind the existing middleware.
7. **Config edge shapes.** Untouched.
8. **Helm succeeded, hooks failed.** This is exactly the state the check exists to
   make visible, rather than a case it has to survive.

## Definition of done

- Every enabled `kubectl_patch` action reports satisfied / drifted / unknown
- The 2026-08-02 breakage is reproducible as a test: patch removed → `drifted`
- A hook nobody has written yet is handled, because nothing is hardcoded
- An API error can never produce `satisfied`
- Logic under test without a cluster
- Verified against the live cluster, where the answer is currently known

## Outcome (2026-08-03)

Shipped in `0.1.21`. Verified against the live cluster from both directions:

| Case | Result |
|---|---|
| The four real hook patches, currently applied | `satisfied`, **no false positives** |
| `turn-tls`, which runs `Cluster` by design, against a `Local` patch | `drifted`, `paths: [spec.externalTrafficPolicy]` |

The negative case matters more than the positive one. A checker that can only ever
say "fine" is indistinguishable from no checker, and this one had to be shown saying
"no" against a real object — which `turn-tls` allows without touching the cluster,
because E19 already established that its policy difference is deliberate.

The no-false-positives result is the other half. A live Deployment carries hundreds
of defaulted and controller-set fields; a naive diff would have flagged every one of
them and the report would have been switched off within a fortnight.

### What made this cheap

Nothing here had to be specified, because the hooks already were a specification.
That is worth noticing as a pattern: the product had been carrying a machine-readable
statement of "what must be true after an upgrade" since E2, and used it only to
*write*. Reading it was a day's work and it closes a hole that cost the operator an
evening.

### Found on the way

`startProgress`'s `stop()` only signalled the emitter. A tick already in flight could
land after it returned, so a caller writing a final line could have the heartbeat
appear underneath it. It surfaced as a test failing about one run in twenty under
parallel load — the shape of thing that reaches production as "the log order is
sometimes weird" and is never diagnosed. `stop()` now waits.

### Still open

The other half of P1-11: manual edits **no hook knows about**, such as the
`ingressClassName: disabled` that Helm preserved for 69 days. That needs
manifest-versus-live diffing and a curated field list. Shipping the exact half first
is deliberate.
