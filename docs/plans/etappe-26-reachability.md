# Etappe 26 — Ask from outside

**Date:** 2026-08-04 · **System:** S14 · **Addresses:** P1-15

## The problem

E19 established that inbound reachability cannot be tested from inside the network
it terminates in, and wrote it into the product as a permanent `unknown`. That is
true, and the honesty was right. But it quietly implied that *therefore nothing can
be done*, and that was wrong: on 2026-08-04, after three days of measuring from the
inside, one request to a public port checker answered it in seconds.

The answer was that nothing inbound reaches the node at all — which explains every
measurement taken since 2026-08-02 and which the product could have said on day
one.

## What it checks

The address clients are actually told to send media to, tested from outside:

- **TCP** ports from the live NodePort list.
- A **control**: a port known to be open on an unrelated public host.

The control is the part that makes a `closed` trustworthy. A checker that is
blocked, rate-limited or broken reports everything as closed, and an operator who
believes it goes and reconfigures a router that was already correct. If the control
does not come back open, every result is `unknown` — not `closed`.

## The honest limits, stated in the UI and not buried

1. **Free checkers test TCP; the port that matters most is UDP 30002.** TCP 30001
   is still decisive in the negative direction: a router that forwards nothing
   forwards neither. So a closed TCP 30001 is an answer; an open one narrows the
   question rather than closing it.
2. **The address tested is the node's own egress address**, discovered the same way
   LiveKit discovers it. Where the announced RTC hostname resolves to a proxy or a
   tunnel — as on the operator's own cluster — testing the *hostname* would test the
   proxy and mean nothing.
3. A failed check is `unknown`. Never `closed`.

## Consent, because this is not a local read

Every other check in MatrixCtrl is cluster-local. This one sends the deployment's
public address to a third party. That address is not a secret — it is in DNS — but
sending it is the operator's decision, not the product's. So:

- It never runs on page load, on a timer, or as part of `/rtc/status`.
- It is a button, on `POST`, with the third-party hostnames named in the UI *before*
  the click, not in a tooltip afterwards.
- Nothing is stored. No consent flag to forget about, no "we asked once" — a status
  page that silently phones home is one nobody should run.

## Mandatory edge-case walk ([PROZESS §1](../PROZESS.md))

1. **No ESS / ESS elsewhere.** No ports listed → nothing to check, and it says so.
2. **Helm release in a bad state.** Untouched.
3. **Not just Deployments.** Reads the same port list `/rtc/status` already builds.
4. **Cluster slow or gone.** The port list is the only cluster read; failure →
   unknown.
5. **No outbound internet.** This is the one feature that *requires* it — and the
   air-gapped case must produce `unknown` with that reason, not `closed`.
6. **Both auth modes.** Behind the same middleware as the rest of `/rtc`.
7. **Config edge shapes.** Malformed checker responses, partial results, an address
   that fails to resolve — each covered by a test.
8. **Helm succeeded, hooks failed.** Unrelated.

## Definition of done

- On the operator's cluster it reports TCP 30001 closed, with the control open
- A broken or blocked checker reports unknown, never closed
- The UDP limit is stated in the result, not only in the plan
- Nothing runs without a click; nothing is stored
- Parsing and verdict logic tested with no network
- S11 green **after** the deploy

## Outcome (2026-08-04)

Shipped in `0.1.27`. S11 all four green after the deploy (revision 29), and the new
route verified as `401` without a token and `405` on `GET` — it cannot be triggered
by a page load or by following a link.

Run against the operator's own cluster through the shipped code path:

```
control_ok=true  udp_skipped=2
  TCP 30001 -> closed
[warn] Von außen geschlossen: TCP 30001
```

That is the sentence three days of inside-out measurement never produced, and the
product now produces it in about a second.

### Found while shipping it

The `useMutation` hook was placed **after** the component's early return for the
error state, so a failed status query would have changed the hook order between
renders. TypeScript does not catch it and the happy path never shows it.

Number agreement was wrong in the UDP note (*"2 UDP-Ports konnte nicht geprüft
werden"*). Trivial, and worth the test that now pins it: a diagnostic tool that
cannot conjugate reads as one nobody maintains, which is the opposite of what it
needs the reader to feel at the moment it delivers bad news.

### What it still does not say

Whether **UDP** 30002 is open — free checkers speak TCP. The result says so rather
than leaving the reader to generalise. On this cluster it does not matter yet,
because TCP 30001 being closed already means nothing is forwarded at all.
