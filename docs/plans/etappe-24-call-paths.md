# Etappe 24 — Which call path does this deployment actually support?

**Date:** 2026-08-04 · **System:** S14 · **Addresses:** the product half of P1-12

## The problem

`/rtc` reports on the SFU: ports, patches, announced address, media evidence. On
2026-08-02 every one of those was green **and calling still failed**, because the
clients were not using the SFU at all — they were making legacy Matrix 1:1 calls,
which are plain peer-to-peer WebRTC and need a TURN relay from Synapse's own
config. Synapse has `turn_uris` unset, and the ESS chart offers no option for it.

So the page was a full page of green about a component those calls never touch.
An operator reading it would conclude their setup is fine and go look at their
router, which is exactly what happened, for two days.

## What this etappe adds

A statement of **which call paths this deployment supports**, because "calling"
is two independent mechanisms with two independent failure modes:

| Path | Needs | Fails when |
|---|---|---|
| Element Call / MatrixRTC | the SFU, its node ports | ports unreachable, stale announced address |
| Legacy 1:1 | `turn_uris` in Synapse | no relay and both peers behind NAT |

Everything the page said until now was about row one only.

## Approach

Read the live Synapse config — the rendered ConfigMap the pod actually mounts,
not the chart values. This follows the P1-11 lesson directly: intent and live
state diverge, and the live state is the one answering calls. `GetObjectJSON`
already accepts `configmap`, so this adds no new cluster surface.

The config is a directory of numbered YAML files merged in lexical order, so the
check parses each and takes the **last** `turn_uris`. Parsing rather than string
matching, so a commented-out `# turn_uris:` does not read as configured, and
`turn_uris: []` reads as what it is: present and empty, i.e. no relay.

Three outcomes, and the third is the point:

- `turn_uris` non-empty → **ok**, both paths have a relay.
- `turn_uris` absent or empty → **warn**, legacy 1:1 has no relay.
- config unreadable → **unknown**, never ok.

## The judgement call: is "no TURN" a warning?

It fires on every ESS install, since the chart cannot configure TURN at all. A
warning nobody can clear is noise, and E23 argued the opposite case — a quiet SFU
must report unknown, not warn.

The difference is that a quiet SFU is **an absence of evidence**; missing TURN is
**a measured property of the deployment** that is permanently true until someone
changes it. It is also actionable, just not from inside this panel: run coturn,
forward its ports, put `turn_uris` in `synapse.additional`. So it warns, and the
action says all three steps rather than "consult the documentation".

## Mandatory edge-case walk ([PROZESS §1](../PROZESS.md))

1. **No ESS / ESS elsewhere.** ConfigMap name derives from the release; absent →
   unknown.
2. **Helm release in a bad state.** Untouched — reads a ConfigMap, not a release.
3. **Not just Deployments.** Synapse is a StatefulSet; this reads its config
   rather than the workload, so the distinction does not arise.
4. **Cluster slow or gone.** Read fails → unknown, never ok.
5. **No outbound internet.** In-cluster read only.
6. **Both auth modes.** No new route; extends `/api/v1/rtc/status`.
7. **Config edge shapes.** A file that is not YAML, a `turn_uris` that is a
   string rather than a list, an empty list — each covered by a test.
8. **Helm succeeded, hooks failed.** Unrelated.

## Definition of done

- Production reports the legacy path as having no relay
- A configured `turn_uris` reports ok, and an empty list does not
- Parsing tested with no cluster, including malformed and unexpected shapes
- The page states both paths, not just the SFU
- S11 green **after** the deploy
