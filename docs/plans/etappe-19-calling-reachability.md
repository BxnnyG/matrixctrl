# Etappe 19 — Say what cannot be verified

**Date:** 2026-08-01 · **System:** S14 · **Addresses:** P2-9

## The problem, from the operator's point of view

Element Call was broken on the production instance for an unknown period. During
that time MatrixCtrl showed: all RTC pods healthy, all SFU patches applied,
dashboard green. Every signal the product produced was true, and every one of
them was beside the point — an external prober found the SFU node ports closed
from the internet.

**MatrixCtrl verifies the half it controls and stays silent about the half that
decides whether a call connects.** Silence next to four green ticks does not read
as "unknown", it reads as "fine". That is worse than showing nothing, because it
spends the operator's trust on a claim the product never made but clearly implied.

The operator asked "why is calling broken" and the answer was not visible anywhere
in a tool whose entire purpose is answering that class of question.

## What is and is not knowable from inside the cluster

This is the whole design, so it goes first.

| Knowable from inside | **Not** knowable from inside |
|---|---|
| Which ports *must* be reachable, and on which protocol | Whether they *are* reachable from the internet |
| Whether the SFU patches are applied | Whether the router forwards them |
| What hostname is announced to clients | Whether the ISP uses CGNAT |
| Whether that hostname resolves, and to what | |

An inbound connectivity test requires a vantage point outside the network. There
isn't one, and inventing a green tick for it would repeat exactly the failure this
etappe exists to fix.

So the page states the right-hand column **as unknown, explicitly**, rather than
omitting it. "MatrixCtrl cannot check this from here, and here is precisely what
you must check yourself" is a useful answer. An empty space is not.

## Approach

Read the facts from live cluster state, never from documentation:

- The node ports come from the actual `NodePort` Services, with protocol and
  current `externalTrafficPolicy`. A README listing ports goes stale; a page
  reading the Service cannot.
- The announced hostname comes from the config store, and is resolved so a
  mismatch between what clients are told and what DNS says becomes visible.
- Patch state comes from the existing hook engine — no second source of truth.

### A finding this etappe surfaces rather than fixes

`ess-matrix-rtc-sfu-turn-tls` has `externalTrafficPolicy: Cluster` while the other
three SFU node-port Services have `Local`. The built-in hook's own description says
it "sets externalTrafficPolicy=Local on the **three** SFU NodePort services", so
the fourth was never in scope.

Whether TURN-over-TLS needs source-IP preservation the way the others do is a
question about Element's SFU, not about MatrixCtrl, and I have not verified it.
So the page **shows the difference** and says the hook covers three of four —
which lets the operator decide — instead of quietly patching a fourth service on
a guess. Changing cluster state on an unverified hunch is how the manual patches
this project exists to protect got fragile in the first place.

## Mandatory edge-case walk ([PROZESS §1](../PROZESS.md))

- **(4) The cluster is slow or gone** — every lookup is a read with a timeout, and
  a failed one renders as "unknown", never as a problem or an all-clear.
- **(5) No outbound internet** — DNS resolution of the announced host will fail on
  an air-gapped cluster. That is reported as unresolvable, not as misconfigured.
- **(1) ESS elsewhere / (3) not just Deployments** — the SFU is a Deployment but
  the Services are looked up by label and namespace rather than by hardcoded name,
  so an adopted release in another namespace still works.
- **(2), (6), (7), (8)** untouched: no Helm operation, no config write, no auth
  change.

## Definition of done

- A `Calls / RTC` page that answers "can calling work?" with the ports to forward,
  read live
- Everything unverifiable from inside labelled as such, in the UI, not only in docs
- The `turn-tls` policy difference visible
- Logic under test without a cluster
- Four regression checks (S11) green

## Outcome

_(filled in when the etappe closes)_
