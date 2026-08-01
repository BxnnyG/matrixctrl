# Etappe 17 — An audit trail that exists

**Date:** 2026-08-01 · **System:** S10 · **Addresses:** P2-1

## The entry was wrong

[BACKLOG.md](../BACKLOG.md) P2-1 and [ROADMAP.md](../ROADMAP.md) both said the same
thing: *"the table and the middleware writes exist; nothing reads them back.
Cheap."* Checked before planning:

```
grep -rni "audit" --include=*.go .     → nothing
SELECT count(*) FROM audit_log;        → 0     (two months in production)
```

**Only the table exists.** No writer, no reader, and `DESIGN.md §1b` has carried
"⏳ ½ · `audit_log` table + middleware write; no UI to read it" for as long as the
status table has existed. The half that was claimed done was never built.

This is the third time this project has found a documented claim that was never
true — after "greenfield deploy works" (E15, broken from day one) and
"Vitest + Testing Library" ([BACKLOG §0](../BACKLOG.md), no frontend tests at all).
The pattern is worth naming: **a status written from intent rather than from
observation decays into a lie, and the lie survives because nobody re-checks
their own notes.** The check above took thirty seconds.

So this etappe is not "cheap UI on existing data". It is the audit trail.

## The problem, from the operator's point of view

MatrixCtrl can upgrade a homeserver, rewrite its configuration, roll it back, run
patches against the cluster and switch its authentication. Afterwards, nothing
anywhere says **who did what, when**. The config repo's git history covers config
edits only, and it attributes every commit to `MatrixCtrl` rather than to a person.

For a single operator that is survivable. For the club or school in
[VISION.md](../VISION.md)'s success criteria it is disqualifying, and it is the
one thing an admin tool cannot credibly leave out.

## Approach

### Writes: middleware, not call sites

Every mutating request is logged by one piece of middleware in the authenticated
group, rather than by a line in each handler.

The centralisation argument (S12) is the weaker one here. The stronger one is
that per-handler logging is **a rule enforced by memory**, and this project has
now been bitten by that four times in two days — the release that nobody
remembered to publish (§4.17), the gofmt drift, the hostname in a plan document
(P0-1c), and this entry itself. A new handler must not be able to be forgotten.

Recorded per request: user, HTTP method, chi's **route pattern** (`/api/v1/helm/
releases/{name}/upgrade`, not the concrete URL — patterns aggregate, URLs do not),
the concrete resource, the status code, and the duration.

### Deliberately **not** recorded: request bodies

The obvious "log the payload" would write MAS client secrets into the audit table
the first time someone connects OIDC, and config YAML — which can contain
anything — on every save. An audit trail that becomes a second copy of your
secrets is a liability, not a control.

The status code plus the route pattern answers "who changed what and did it
work". "What exactly changed" is already answered better by the config repo's git
history, which stores diffs and can roll back.

### Reads

`GET /api/v1/audit` with keyset pagination over `(ts, id)` and optional filters.
Offset pagination on a table that grows at the head re-reads rows on every page.

### Retention

Nothing yet, deliberately — but the table grows without bound and that is a real
operational trap on a 32 GB single-node cluster. Recorded as a follow-up rather
than guessed at: the right retention for an audit trail is a policy decision, not
a default someone invents while building the writer.

## Mandatory edge-case walk ([PROZESS §1](../PROZESS.md))

- **(4) The cluster is slow or gone** — an audit write must never fail or delay
  the request it describes. It runs after the response is written, and its error
  is logged, not returned.
- **(6) Both auth modes** — bootstrap and OIDC both produce a user ID, and both
  must be attributed. Requests with no user (login itself) still matter: a failed
  login is exactly what an audit trail is for.
- **(2) Helm release in a bad state / (8) hooks failed** — these produce non-2xx
  responses, which must be recorded as attempts rather than dropped. An audit
  trail that only lists successes is worse than none.
- Cases 1, 3, 5 and 7 concern cluster shape and config parsing, untouched here.

## Definition of done

- Every mutating request through the authenticated group lands in `audit_log`,
  proven by a test that drives the middleware rather than by inspection
- No request body, header or secret is ever written
- A read endpoint with keyset pagination and filters
- A UI page that reads it back
- `DESIGN.md §1b` S10 corrected to what is actually true, and the false claim in
  P2-1 replaced by what was found
- Four regression checks (S11) green

## Outcome

_(filled in when the etappe closes)_
