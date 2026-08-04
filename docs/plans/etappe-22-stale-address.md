# Etappe 22 — The SFU announces an address it discovered once

**Date:** 2026-08-03 · **System:** S14 · **Addresses:** P1-9

## The problem, from the operator's point of view

Calling works after a restart of the SFU and stops working again within a day, on
its own, with nothing in the product changing. The operator experiences this as
"calls are just unreliable" and stops reporting it.

LiveKit discovers its external address by STUN **once, at startup**, and offers that
address in every ICE candidate for as long as the process lives. A German consumer
line is re-addressed roughly every 24 hours. DynDNS updates the record, so clients
resolve the *correct* address — and the SFU keeps offering the old one. The ICE
statistics say it plainly:

```
state: failed   local: <yesterday's address>:30002 udp type(host/)
requestsSent: 8   responsesReceived: 0   requestsReceived: 0
```

Observed twice on the production instance, 22 hours apart, both times fixed by
replacing the pod. It is not an incident. It is a schedule.

## Where the announced address can be read — and why it is not read

The obvious approach is to ask the SFU what it announces. Checked, rather than
assumed:

| Source | Result |
|---|---|
| LiveKit HTTP port (`/`, `/rtc/validate`, `/twirp`, `/debug/pprof`) | no node address |
| Prometheus metrics (`node_id`, `node_type`, `service`, …) | no address label |
| Pod log (`"nodeIP"`, `NAT1To1Ips`) | present — and a log format is not an API |

So the address is only available by scraping a log line, which breaks silently on a
LiveKit upgrade. **The check is therefore built without it.**

## The design: compare times, not addresses

The announced address equals the public address *at the moment the SFU started*.
Therefore:

> The announcement is stale **iff** the public address changed after the SFU pod
> started.

Both halves are already available:

- **When the pod started** — `.status.startTime`, exact, from the API server.
- **When the address last changed** — MatrixCtrl already resolves the announced RTC
  host (E19). Recording what it resolved to, and when that answer changed, gives the
  timestamp. One row per host.

DNS is the right reference precisely because it is what the clients use. If DNS were
wrong, calling would be broken for a different reason, and that deserves its own
finding rather than being folded into this one.

This needs no log parsing, survives a LiveKit upgrade, and works for any SFU that
discovers its address at startup.

## Approach

- A table `rtc_address_history` (host, address, first_seen, last_seen). On every
  resolve: if the address is unchanged, extend `last_seen`; if it changed, insert a
  row. `first_seen` of the newest row is the moment of change.
- The `/rtc` page gains a finding with three states:
  - **stale** — the address changed after the pod started. Says both timestamps and
    what to do.
  - **ok** — the pod started after the last change.
  - **unknown** — fewer than two observations, so no change has been witnessed yet.
    A fresh install must not claim "ok" from having seen nothing, which is the
    §4.21 mistake in a new place.
- A **restart action** on the same page. The daily fix is replacing the pod, and
  making the operator do it by hand every morning is not a product.

### Why the restart deletes the pod rather than rolling the deployment

`kubectl rollout restart` on this deployment **can never complete**: it is
`hostNetwork: true` with `replicas: 1` and `maxUnavailable: 0`, so the old pod must
stay Ready until the new one is, and the new one cannot bind host ports the old one
holds. Observed live — the replacement sat `Pending` for 23 minutes reporting
`FailedScheduling: didn't have free ports` while the old pod kept serving the stale
address. Deleting the pod is the only form that works (P2-23).

That trap is the reason this etappe includes the action at all: an operator who
automates the obvious way gets a restart that silently does nothing.

## Mandatory edge-case walk ([PROZESS §1](../PROZESS.md))

1. **No ESS / ESS elsewhere.** No announced host configured → `unknown`, no finding
   invented. The deployment is looked up by the configured release, not hardcoded.
2. **Helm release in a bad state.** Untouched — this reads a pod and a DNS answer.
3. **Not just Deployments.** The SFU is a Deployment; the pod is found by label, so
   a renamed deployment still resolves.
4. **The cluster is slow or gone.** Both reads take a timeout; failure is `unknown`.
5. **No outbound internet.** DNS resolution fails → `unknown`, and explicitly *not*
   "the address changed". An air-gapped cluster must not report a false alarm.
6. **Both auth modes.** One new mutating route, behind the existing middleware and
   therefore the audit log (E17).
7. **Config edge shapes.** Untouched.
8. **Helm succeeded, hooks failed.** Unrelated.

## Definition of done

- The stale case is detectable from data the product already has, with no log parsing
- Fewer than two observations reports `unknown`, never `ok`
- A failed DNS lookup never reports a change
- The restart replaces the pod and is verified to actually complete
- Logic under test without a cluster or a database
- Four regression checks (S11) green **after** the deploy, not before

## Outcome (2026-08-03)

Shipped in `0.1.22`.

The design decision that mattered was refusing the obvious source. Scraping
`"nodeIP"` out of the pod log would have worked today and broken silently on the
next LiveKit upgrade — and it would have broken *quietly*, reporting `unknown`
forever while the operator assumed they were being watched. Deriving the answer from
two timestamps the product already holds has no such failure mode.

The second one was `Changes < 2 → unknown`. A single observation looks exactly like
the stale case under a naive comparison: the pod started before `first_seen`. Getting
that wrong would have made every fresh install cry wolf on day one, and a page that
cries wolf on day one is switched off on day two.

### The trap that shipped with it

`kubectl rollout restart` on this deployment can never complete — `hostNetwork`, one
replica, `maxUnavailable: 0`. The replacement sat `Pending` for 23 minutes on the
production cluster reporting `FailedScheduling: didn't have free ports`, while the
old pod carried on serving the stale address. Anyone automating the fix the obvious
way gets a restart that silently does nothing. That is why the action ships in the
same etappe as the detection rather than being left to the operator.

### What it does not answer

Whether the media ports are reachable at all (P1-13). This etappe removes one
recurring cause of calls failing; it does not prove that removing it is sufficient.
Those are different claims and today has been an object lesson in not confusing them.
