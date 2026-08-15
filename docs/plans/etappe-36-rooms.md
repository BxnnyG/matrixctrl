# Etappe 36 — Rooms, read-only

**Date:** 2026-08-06 · **System:** S13 · **Continues:** Phase 2, after users (E27/E28)

## Scope

List, search and inspect rooms. **No writes.** Same reasoning as E27: deleting a
room, purging history and blocking a room are each destructive in a different way and
need confirmation UX plus audit entries. Bundling them into the etappe that introduces
the whole subsystem would ship the dangerous half untested.

## The question that had to be answered first

Rooms live in **Synapse**, not MAS, and MatrixCtrl has only ever talked to MAS. So
before any of this is designable: what authenticates against Synapse's admin API on a
deployment where MAS owns authentication?

Two candidates, and they differ in how much standing power the tool gains:

**A — a service token.** MAS 1.21 exposes `POST /api/admin/v1/personal-sessions`,
which mints a token acting *on behalf of a user*. Its own schema notes: *"If not set,
the token won't expire."* That would give MatrixCtrl a standing ability to act as
somebody — read their rooms, their messages — with no human present.

**B — the operator's own authority, chosen.** MatrixCtrl already receives an OAuth
access token at login (`internal/auth/oidc.go` uses it for userinfo and then discards
it). Asking for one more scope turns that into exactly the authority of the person who
is signed in, and nothing more.

B is the smaller footprint and the honest authority model for interactive admin work:
the tool can do what the human in front of it can do, while they are there. A stays
available for a later etappe that genuinely needs unattended access, and would then be
a deliberate decision with `expires_in` set.

## What the live system says (inventory, not assumption)

- Synapse's admin API is routed and active: `GET /_synapse/admin/v1/rooms` answers
  **401**, not 404.
- Synapse 1.157 delegates authentication to MAS through the
  `matrix_authentication_service` config block (the successor to the `msc3861`
  experimental block).
- Real sessions in the MAS database carry
  `openid`, `urn:matrix:org.matrix.msc2967.client:api:*` and a
  `…client:device:<id>` scope. So MAS grants Matrix scopes even though its discovery
  document advertises only `openid` and `email` — the discovery list is incomplete,
  and trusting it would have ended this etappe before it started.
- **No session has ever carried `urn:synapse:admin:*`**, and no row in Synapse's
  `users` table has `admin = 1`. Synapse admin authority does not currently exist on
  this deployment in any form.
- Two MAS accounts have `can_request_admin = true`, including the operator's.

That last pair is what makes B work: **MAS enforces the check, not MatrixCtrl.** A user
without `can_request_admin` cannot obtain `urn:synapse:admin:*` at all, so the
privilege boundary is upstream of anything this code could get wrong.

## Approach

- Ask for `urn:synapse:admin:*` — **not** the `…msc2967.client:api:*` scope. That one
  grants the full client-server API and creates a device on the account, which would
  mean the panel could read the operator's messages. The admin API is what rooms need;
  nothing more should be asked for, and a consent screen that asks for less is also
  easier to say yes to.

- **The scope is requested in its own authorization, not added to the login.**

  This changed while building. Putting the scope on the login path would make *every*
  sign-in ask for `urn:synapse:admin:*` — a scope MAS has never granted on this
  deployment. If MAS rejects an authorization that requests a scope the account may
  not have, rather than simply omitting it, the login breaks. For every operator. That
  is [S11](../DESIGN.md#s11--regression-safety-net-) check 3, the one regression this
  project treats as non-negotiable, and it cannot be tested from here without a
  browser.

  So the working login path is left exactly as it is, and the rooms page starts a
  second, explicit authorization the first time it is used. The blast radius of a
  wrong guess about MAS's behaviour is then "rooms do not work" instead of "nobody can
  sign in".

  It also makes the extra authority a thing the operator agrees to on purpose, rather
  than something that quietly grew onto the login they already had.
- A new `internal/synapse` admin client, sharing nothing with `internal/mas` beyond
  patterns — they are different APIs with different auth and different pagination.
- `GET /_synapse/admin/v1/rooms` is offset-paginated (`from`/`limit`) with
  `order_by`/`dir` and a `search_term`, unlike MAS's cursor paging. The UI must not
  pretend they behave the same.

## The token lifetime, measured

Straight from the MAS database, every access token ever issued here:

```
ttl_seconds | count
        300 |    32
```

**Five minutes.** So the operator's access token is useless for a page they might open
half an hour after signing in. Making rooms work past that means keeping the **refresh
token** — a credential that can mint Synapse-admin access tokens for as long as the MAS
session lives.

**Decision: the refresh token is held in memory, never written to disk.**

- Persisting it would put a Synapse-admin-capable credential at rest in Postgres,
  per operator, for the life of the session. That is a larger, quieter power than
  anything MatrixCtrl stores today, and it would be stored to save a login.
- In memory, a restart costs the operator one sign-in and costs an attacker with disk
  or database access nothing to steal, because there is nothing there.
- E33 made a restart survivable for login itself; an extra sign-in before using rooms
  is a modest, visible cost with an obvious cause.

Consequences to build for: the rooms page must handle "no Matrix token in this
process" as a normal state with a clear prompt, not an error — it happens after every
restart and after every session that outlived a redeploy. And the refresh token must
never reach a log line (E35's redaction covers query strings; this one lives in a
response body and a struct, so it needs its own care).

## Mandatory edge-case walk ([PROZESS §1](../PROZESS.md))

1. **No ESS / ESS elsewhere.** Rooms need a reachable Synapse; absent means an empty
   state, not an error page.
2. **Helm release in a bad state.** Unrelated to reading rooms.
3. **Not just Deployments.** N/A.
4. **Cluster slow or gone.** Synapse is reached over HTTP, not through the cluster
   API; its own timeout applies.
5. **No outbound internet.** All in-cluster.
6. **Both auth modes.** **Bootstrap mode has no Matrix token at all.** The rooms page
   must say so plainly rather than appear broken — this is the case most likely to be
   forgotten, because the developer is always logged in via OIDC.
7. **Config edge shapes.** A user with `can_request_admin = false` gets no scope; the
   page must explain that, not show an empty list as though the server had no rooms.
8. **Helm succeeded, hooks failed.** Unrelated.

## Definition of done

- The room list loads, searches and pages against the live Synapse
- A non-admin operator sees why they cannot, not an empty table
- Bootstrap mode says the feature needs Matrix login, and does not look broken
- The token is not requested with more scope than the admin API needs
- **Signing in still works exactly as before** — the login path is untouched, and this
  is checked before anything else
- S11 green **after** the deploy
