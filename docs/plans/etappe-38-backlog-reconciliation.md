# Etappe 38 — The backlog no longer describes this code

**Status:** planned · 2026-08-15

## Why this is worth an etappe

CLAUDE.md rule 2 is "inventory instead of guessing", and it names
[BACKLOG.md](../BACKLOG.md) as one of the two places the inventory lives. That
file is currently wrong about its own subject.

E37 found P0-5 — "the session JWT travels in a URL" — still listed as open. It
had been closed in two halves by E29 and E35. Checking the neighbouring entries
found seven more in the same state. The whole **2026-08-04 security review batch**
was fixed and never struck through:

| entry | claim in the backlog | actual code |
|---|---|---|
| P0-5 | JWT in the URL, `?token=` on every route, chi logger writes it | `#code=` fragment (never sent to a server), `extractToken` returns `""` for queries everywhere, `authmw.Logger` redacts — live check `?token=abc` → 401 |
| P1-16 | failed `crypto/rand` persists a guessable JWT key | `randomKey()` calls `log.Fatalf`; the remaining fallback is a *different* one (DB unreachable → ephemeral but still random) and is documented as such |
| P1-17 | no throttle, backoff or lockout anywhere | `internal/auth/throttle.go`, lockout after 15 tries for 15 minutes |
| P2-26 | CORS wildcard on every route at `router.go:158` | no CORS middleware at all; `router.go:42` explains why one is not needed |
| P2-27 | no `USER` in the Dockerfile, no securityContext | `USER 65532:65532`; `runAsNonRoot: true, runAsUser: 65532` on the app container |
| P2-28 | `RevokeSession` keyfunc returns the key unconditionally | checks `*jwt.SigningMethodHMAC`, with a comment naming P2-28 |
| P1-6 | a failed hook is silent | `hooks-failed` rendered on the upgrade page, the history page and the index |
| P1-11 | hand-edits invisible | `/api/v1/drift` → `ListOwnership` → the dashboard's `manual_edits` |

**Why this is harmful rather than untidy.** The repo is public. A reader
evaluating whether to run this in front of their Matrix server currently finds a
document, maintained by the author, stating that the admin panel runs as root
with wildcard CORS, no login rate limiting, a guessable signing-key fallback and
session tokens in URLs. None of it is true. It is a false claim in the direction
that matters least for marketing and most for trust: it makes the project look
*less* safe than it is, which is the kind of error nobody thinks to check.

It also hides the entries that **are** open, by burying them among eight that are
not.

## What this etappe does

1. **Verify every open entry against the code** — all 33, not only the eight
   above. Each is either struck through with the proof and the etappe that closed
   it, or left open unchanged. No entry is closed on the strength of an etappe
   having *claimed* to close it; the check is against the source.
2. **Fix the one that this session proved is actively misleading: P2-8.**

## P2-8 is not a bookkeeping entry

> The dashboard sums restarts across containers, which misleads.

Confirmed during E37's verification, on this cluster, by me:
`ess-postgres-0` reports **42 restarts**. The obvious reading is that the database
has restarted 42 times. It has not — `postgres` has restarted **zero** times. The
42 belong to `postgres-exporter`, a monitoring sidecar, and only reading
`/proc`-level per-container state disclosed it.

I spent a chunk of E37 chasing a database that was never restarting. MatrixCtrl's
pod list makes exactly the same summation (`internal/k8s/pods.go:104` and `:196`),
so the panel would have misled its operator the same way — while the per-container
numbers it needs are already in the struct it is folding away.

*Fix:* keep the total, and name the container that carries it when one dominates.
"42 (postgres-exporter)" answers the question the number raises; "42" invites the
wrong answer.

## Deliberately not in scope

- P2-2 (40 tracked build artefacts — the entry says 32, so it has grown), P2-19
  (audit log retention), P2-22 (`ListHistory` decodes every revision). All
  confirmed still open. Each is a real change with its own risk, not a
  reconciliation.
- P0-4a. It is the top entry and it is one etappe away, not this one.

## Definition of done

- Every open backlog entry checked against the source; each stale one struck
  through with the code that closes it and the etappe that shipped it
- The restart display names its container; the existing per-container data is
  used rather than newly fetched
- Signing in still works, S11 green after the deploy
- DESIGN.md records why a stale inventory is a defect and not untidiness
