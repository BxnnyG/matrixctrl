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

## Outcome (2026-08-04)

Shipped in `0.1.25`. Verified against the live ConfigMap before the release: four
config files, `turn_uris` absent, finding renders as *"Klassische 1:1-Anrufe haben
kein Relay"* at warn level. S11 all four green **after** the deploy (revision 27).

### Corrected while building it

The claim that "the ESS chart offers no TURN" — carried in P1-12 since 2026-08-02
and repeated to the operator earlier today — is too coarse. The chart **does** ship
one: `matrixRTC.sfu.exposedServices.turn`, enabled by default on node port 30004.
It is LiveKit's own, authenticates with LiveKit tokens, and therefore serves Element
Call and nothing else; Synapse needs the REST scheme (`turn_shared_secret`, HMAC)
and cannot use it. So the gap is real, but its shape is "the relay that exists
serves the other path", not "there is no relay anywhere" — and a finding that said
the latter would look wrong to anyone who read the values. The finding now names
both.

Also found: `turnTLS` is `enabled: false` and its `domain` still points at the RTC
host that was replaced. That is the TCP/TLS fallback for clients that cannot get UDP
out — the most interesting switch on the page for the operator's actual symptom.
Enabling it needs a certificate and a forwarded port, and it is **not** claimed here
as a fix: three such claims were made during P1-13 and all three were wrong.

### Found while cleaning up alongside it

`IngressRoute/matrix-rtc-tls` (71 days old, built by hand, no chart) and its
middleware were deleted on the operator's instruction. It routed the *old* RTC host
into the SFU including a path-less catch-all. Namespace `ess` now holds no
hand-built Traefik objects. Nothing in the product would have found it — which is
the open half of P1-11, unchanged.
