# Etappe 20 — The read was never expensive

**Date:** 2026-08-02 · **System:** S4 · **Addresses:** P2-21

## The problem, from the operator's point of view

"The dashboard loads slowly." Etappe 14 answered that with a 60-second cache and
took the warm path from ~3.2 s to ~0.18 s. That holds. But the operator does not
experience the warm path — they experience *arriving*. The first request after the
cache expires still pays the full Helm read, and for those seconds the dashboard is
four grey boxes.

So the complaint was half-fixed in the least useful half: fast once you are already
looking at it, slow exactly when you show up.

## The filed fix was wrong, and measuring said so

P2-21 proposed keeping the cache warm or serving stale-while-revalidate. Both
accept the 4.3 s as a fact of life and work around it. Before building a workaround,
I measured what the 4.3 s actually *is* — and it is not what
[`cache.go`](../../internal/helm/cache.go) claims.

That comment says: *"There is no cheaper API to switch to, so the result is cached
instead."* It was written after measuring `action.NewGet`, `NewGetMetadata` and
`NewList` at ~4 s each and concluding the cost was inherent. All three numbers were
right. The conclusion did not follow.

Measured against the live cluster (`TestReleaseReadCostLive`, `RUN_LIVE=1`):

| What | Cost |
|---|---|
| `action.NewGet` (what we do now) | **~4.3 s**, every call — not a one-off init |
| A single `GET` of the newest release secret | ~120 ms |
| Metadata-only list of all release secrets | **~15 ms** |
| Decoding one release (416 KB) | ~300–600 ms |
| `storage.Deployed()` | ~480 ms |

The release history is 11 secrets holding **2.93 MB**. `action.NewGet` asks the
storage layer for `Last()`, which fetches *and decodes every one of them* in order
to sort by revision and return the newest. We want seven scalars from one revision
and we were paying to decompress ten revisions we discard.

**The expensive thing was never the read. It was asking Helm the wrong question.**

## Approach

Three of the seven scalars are already in the secret's **labels** — no decode needed:

```
{"modifiedAt":"1769459689","name":"ess","owner":"helm","status":"deployed","version":"22"}
```

`version` is the revision, `status` is the release status. A metadata-only list
(`k8s.io/client-go/metadata`, `PartialObjectMetadataList`) returns those for every
revision in ~15 ms without transferring a single byte of release payload.

The chart version is *not* in the labels, so it still needs one decode. But the
chart version cannot change without the secret changing. So:

1. **Probe** — metadata-only list, ~15 ms. Take the highest `version` label. This
   yields the revision, the status, and the secret's identity.
2. **If the identity matches what we already decoded** → return the memoised value.
3. **If it differs** → `storage.Storage.Get(name, revision)`, one secret GET plus
   Helm's own decoder, ~500 ms. Memoise under the new identity.

Identity is `(revision, status, modifiedAt)`. Any change to the secret at all — a
new revision, or `pending-upgrade` flipping to `deployed` on the same revision —
changes it and forces a fresh decode.

### Why this is better than the cache it replaces, not just faster

The 60-second TTL bounded staleness by *time*: for up to a minute the dashboard
could show a release that no longer existed, and nothing in the response said so.
Identity-keyed memoisation has **no staleness window at all**. The value returned
is either confirmed current against the cluster or freshly read. That is a stronger
correctness property than today's, delivered by the change that also makes it fast.

It also removes the need for the thing P2-21 asked for. There is nothing to keep
warm, and no stale value to revalidate, because the probe is cheap enough to run on
every request.

### Decoding stays Helm's job

The release secret's format (base64 → gzip → JSON) is Helm's, and
`storage/driver.decodeRelease` is unexported. We do **not** reimplement it: step 3
goes through `c.cfg.Releases.Get(name, revision)`, Helm's public storage API. We
choose *which* revision to ask for; Helm still does the fetching and decoding. A
format change in a future Helm release cannot silently break us.

### What is deliberately not in scope

`ListHistory` (the `/helm/history` page) has the same shape of problem —
`action.NewHistory` decodes all revisions too — and it genuinely needs the chart
version of every revision, so the same trick does not apply unchanged. Measuring
and fixing that is its own etappe; guessing at it inside this one is how scope
creep gets justified.

## Mandatory edge-case walk ([PROZESS §1](../PROZESS.md))

1. **No ESS / ESS elsewhere.** The probe is a namespaced list by label selector
   `owner=helm,name=<release>`. Greenfield returns zero items → the same
   "release not found" error the current path returns, so `main.go`'s discovery
   fallback still triggers. Namespace and release name stay parameters.
2. **Helm release in a bad state.** `failed` and `pending-upgrade` are values of
   the `status` label, so the probe reports them without a decode. Crucially the
   probe takes the **highest revision**, matching `Last()` — not `Deployed()`,
   which would silently show the last *successful* release and hide a failed
   upgrade. That distinction is the whole reason `Deployed()` was rejected despite
   being a one-line change.
3. **Not just Deployments.** Untouched — this reads release secrets, not workloads.
4. **The cluster is slow or gone.** The probe takes a context with a timeout. If it
   fails, fall back to the existing `action.NewGet` path: correct, slow, and rare.
   A probe failure must never produce a wrong answer, only a slower one.
5. **No outbound internet.** Untouched — all reads are against the API server.
6. **Both auth modes.** No new route, no auth change.
7. **Config edge shapes.** Untouched.
8. **Helm succeeded, hooks failed.** `hooks-failed` is carried through as a status
   label value like any other; no special-casing added or removed.

## Definition of done

- A cold `GetRelease` well under one second, a repeat read in tens of milliseconds
- No time-based staleness window anywhere in the release read
- A failed probe degrades to the old path, never to a wrong answer
- The live measurement kept as a runnable test, so the numbers in this document can
  be re-checked rather than believed
- Unit tests that do not need a cluster
- Four regression checks (S11) green

## Outcome (2026-08-02)

Shipped in `0.1.20`. Measured on the live cluster, same release, same day:

| | Before | After |
|---|---|---|
| Cold read | 4.32 s | **505 ms** |
| Warm read | 2.5 µs | 14 ms |
| Staleness window | up to 60 s | **none** |

The warm path is four orders of magnitude slower and that is the right trade. The
old 2.5 µs was a map lookup that answered from memory and could not tell you whether
it was still true; the 14 ms is a question to the cluster that can. Against a
`/status` response dominated by Kubernetes reads it is invisible, and it buys the
removal of the one thing §4.15 wrote down as a known defect.

### What the failure actually was

Not a missed optimisation. §4.15 measured three Helm functions, found all three cost
~4 s, and concluded the cost was inherent to reading a release. Three independent
confirmations — except they were not independent. `NewGet`, `NewGetMetadata` and
`NewList` all route through `storage.Last()`, which decodes every revision to sort
them. It was one bottleneck measured three times, and the repetition made it look
like evidence.

**Measuring the alternatives you thought of is not the same as measuring the
problem.** The question that broke it open was not "which Helm call is cheapest"
but "what does the 4.3 s consist of" — and the answer was 2.93 MB of releases we
throw away.

### The cheap wrong version

`storage.Deployed()` is one line and ~480 ms, nearly as fast, and it hides failed
upgrades: it returns the last *successful* revision, so an operator whose upgrade
just failed would see the previous release reported as fine. It was rejected for
that reason and `TestProbeReportsAFailedNewestRevisionRatherThanTheLastGoodOne`
exists so nobody re-discovers it as a saving.

### Verification, and what it cost to get it

Deployed as Helm revision 22 with the running image read back, not inferred.
S11: 9 ESS pods healthy, Matrix `200`, MatrixCtrl `200` and `401` without a token,
SFU 3/3 on `Local` (`turn-tls` still `Cluster`, as E19 documented), and **18 config
sections / 2926 comment lines — unchanged across the upgrade**. In the pod logs
`metadata client unavailable` appears zero times, so the fast path is what
production actually runs rather than the fallback.

The eleven-route screenshot check took four attempts, and none of the failures were
in this etappe's code:

1. The minted token was rejected — `MATRIXCTRL_JWT_SECRET` comes from a Secret and
   beats the key persisted in Postgres. Every route silently rendered the login
   page. Noticed only because all five screenshots were byte-identical in size.
2. `/auth/login` makes no API calls, so the settle loop introduced in E19 could
   never exit early and burned its full 25 s — on every run, and on all eleven
   routes at once when the token was bad.
3. **`/rtc` leaked a public IP into a screenshot and was reported PASS.** See
   §4.18's amendment; this is the finding of the etappe that was not planned.
4. The fix for (3) then flagged its own RFC 5737 placeholder as a leak.

All four are fixed and committed. The through-line is the same one E19 ended on:
each was found by *looking at the artefact*, never by a green check.

### Left undone, deliberately

`/helm/history` still decodes every revision — the same bottleneck, but it needs
each revision's chart version, which is the one field the labels do not carry. Filed
as **P2-22** with the shape of the answer sketched, unmeasured, rather than folded
into this etappe on a guess.
