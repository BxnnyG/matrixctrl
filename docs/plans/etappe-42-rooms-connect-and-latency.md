# Etappe 42 — The rooms connect that connects and then does not work

**Status:** planned · 2026-08-16 · from the operator's report

Four things the operator reported. Three are fixed here; one is scoped and
deferred because it is a feature, not a defect.

## 1. Connecting to Matrix succeeds and the page still says "connect"

**Reported:** "bei räume verbinden mit matrix klicke ich aber danach steht da
wieder verbinden".

**Reproduced from the logs, and it is not what the report sounds like.** The
authorization *worked*:

```
POST /api/v1/rooms/connect      → 200
GET  /api/v1/auth/oidc/callback → 302   (to /rooms, i.e. success)
GET  /api/v1/rooms/state        → 200 19B   ← {"connected":true}
GET  /api/v1/rooms?from=0…      → 409
```

So the token was granted and stored. It is then rejected by Synapse, the handler
maps that to 409 ("reconnect"), and the page renders the connect panel again —
which looks identical to never having connected.

Synapse says why in one line:

```
SynapseError: 401 - Token doesn't grant access to the Matrix C-S API
```

**This is my E36 decision, and it was wrong.** `SynapseAdminAuthURL` requests
`openid urn:synapse:admin:*` and deliberately omits
`urn:matrix:org.matrix.msc2967.client:api:*`, with this reasoning in the code:

> Deliberately *not* `urn:matrix:org.matrix.msc2967.client:api:*`: that grants the
> full client-server API and creates a device on the account, which would let this
> panel read the operator's messages.

Synapse's MSC3861 path needs the C-S scope to resolve the token **to a user**. The
admin scope says what that user may do; without the other one there is no user for
it to apply to. Admin-only is not a token Synapse can act on.

It failed at exactly the point I told the operator I could not verify:

> "erst dann zeigt sich, ob MAS den Scope so vergibt, wie ich es aus der Datenbank
> abgeleitet habe. Das ist der eine Punkt, den ich von hier aus nicht beweisen
> kann."

**Fix:** request `openid urn:matrix:org.matrix.msc2967.client:api:* urn:synapse:admin:*`.
The **device scope stays out** — `urn:matrix:org.matrix.msc2967.client:device:<id>`
is separate, so no device is created on the operator's account.

**And the UI text has to change, because it currently makes a false security
claim.** The connect panel says today:

> Der Zugriff gilt nur für die Admin-Schnittstelle, nicht für deine Nachrichten

With the C-S scope that is no longer true: the token *could* read the operator's
messages; MatrixCtrl simply does not. Saying "cannot" when the truth is "does not"
is the same class of error as the values flag in E37 that claimed to withhold access
it never withheld. The panel will say what is actually granted, and what this
process does with it.

**A second, smaller defect the same log shows:** the page cannot distinguish "never
connected" from "connected but the token does not work". Both render the same
panel, which is why the report reads as "the button does nothing". After this fix
the second state should not occur — but "should not occur" is not a reason to render
it as something else, so the 409 case says the access was refused rather than
silently offering the same button.

## 2. The upgrade history takes "half a minute"

**Measured, from the request log:**

```
GET /api/v1/helm/releases/ess/history → 200 in 7.743 s
```

E39 took `ListHistory` from 3.2–4.6 s to 25 ms and I described that as "cold 4.7 s
once per process, then 25 ms". Both halves are true and the framing was wrong: the
cache is in memory, so **every deploy resets it**, and this project deploys several
times a day. The operator meets the cold path far more often than "once".

**Fix:** persist the per-revision facts. They are already established as immutable —
that is the whole basis of E39's cache — so the natural place for them is a table,
not a map that dies with the process. Cold cost then happens once *ever* per
revision rather than once per restart.

The existing in-memory map stays in front of it: Postgres is fast, but not 25 ms
fast for fourteen rows plus a metadata list, and the memory layer is already
written and tested.

## 3. "Show pre-releases" appears to do nothing

**It works, and it is useless, which is worse.** Asked directly:

```
total=79  prerelease=12
prereleases at indices 56, 58, 60, 62, 64, 66, 68, 70, 72, 74, 76, 78
```

Every prerelease is a `0.x-dev` tag from the chart's earliest days, and the list is
newest-first. Toggling adds twelve rows past position 56 — nothing changes anywhere
the operator is looking, so a control that functions correctly reads as broken and
trains them to distrust the next one.

**Fix:** the toggle reports its own effect — "12 Pre-Releases ausgeblendet" — and
says nothing at all when there are none to hide. A control that states what it is
doing cannot look broken while working.

## 4. Deferred: calls should show audit, connections, statistics

**Reported:** "calls soll auch wenn möglich audit zeigen verbindungen zeigen maby
auch statistik (statistik maby ausweiten auf das ganze) / logs länge".

This is four features, not a defect: per-call audit, live connection list, RTC
statistics, and a broader statistics story across the panel. LiveKit exposes
session and participant data that MatrixCtrl does not read at all today, so it needs
its own inventory pass first — which of those numbers actually exist, and which
would be invented.

Recorded in BACKLOG rather than started here, because bolting a quarter of it onto
this etappe would produce the kind of half-answer §4.24 was written about.

## Definition of done

- Connecting to Matrix leads to a room list, verified against the live homeserver
- The connect panel describes the access that is actually granted
- History endpoint measured before and after, cold **and** after a restart
- The pre-release toggle states how many versions it is hiding
- Signing in still works, S11 green after the deploy
