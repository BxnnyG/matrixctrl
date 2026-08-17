# Etappe 52 — Connect once, and land where you started

Two defects reported by the operator on 2026-08-17, both in the Matrix-admin
authorization that rooms and moderation share.

## Defect 1 — connecting from Moderation drops you on Rooms

`finishSynapseAdmin` ends:

```go
h.matrixTokens.Put(userID, access, refresh, expiresIn)
http.Redirect(w, r, "/rooms", http.StatusFound)
```

`/rooms` is hardcoded, on the success path *and* in `fail()`. E36 built the flow when
rooms was its only caller; E46 reused it for the report queue and nothing carried the
origin. Connecting from Moderation therefore works and then abandons you on a
different screen.

**Fix:** the return path travels with the OAuth state, in `oidc_states` (migration
015). Not in a query parameter: the state is a CSRF token read from the database
precisely because a value the browser carries is not one to branch on, and a redirect
target is exactly the kind of value that becomes an open redirect when it comes from
the client. Server-side allowlist, defaulting to `/rooms`.

## Defect 2 — reconnecting after every restart

Reported as "bei rooms musste ich jedes Mal neu connecten". It is not a bug: E36 chose
it deliberately, and `internal/auth/matrixtoken.go` states the reason —

> That refresh token is never written to disk. Persisting it would leave a
> Synapse-admin-capable credential at rest in Postgres, per operator, to save a
> sign-in.

MAS access tokens live 300 s, so the refresh token is what keeps rooms working, and it
dies with the process. Measured: the audit log holds **four** connects ever, one of
them immediately after this session's 0.1.49 deploy. The operator experienced it as
constant because seven MatrixCtrl versions shipped today; in ordinary use it is once
per panel upgrade. Still not "set it up once and it runs".

**Decision (operator, 2026-08-17):** keep the token in memory — E36's security
posture stands, nothing new at rest — and make the reconnect *silent*. When the token
is missing, the panel starts the authorization itself and returns to the screen the
operator was on. With a live MAS session that is a redirect and back, no clicks.

**The guard matters more than the feature.** An automatic redirect on a failing
condition is a redirect loop waiting to happen, so:

- the failure path already returns with `?error=`, and its presence suppresses the
  automatic attempt — a failed silent reconnect falls back to the manual panel, which
  is the existing UI and needs no new states
- a second attempt inside 30 s is refused even without `?error=`, covering the case
  where the flow succeeds and the token is rejected again immediately
- `403` (the account is not a Synapse admin) never triggers it: reconnecting cannot
  grant a permission that Matrix has not given, and retrying would spin forever
  against a condition no click can fix

## Scope

**Ships:** the return path through the state, and the silent reconnect with its
guards.

**Does not ship: persisting the refresh token.** Explicitly chosen against by the
operator today. The alternative was AES-GCM at rest with the key in the k8s Secret;
recorded in BACKLOG so the option is not re-litigated from scratch.

**Does not ship: requesting the Synapse-admin scope during normal login.** It would
remove the separate flow altogether, and E36 kept it separate on purpose so a wrong
guess about MAS cannot break signing in — the one thing whose failure needs
`kubectl` to recover. Not worth reopening for a redirect saved.

## Definition of done

- Connecting from Moderation returns to Moderation; from Rooms to Rooms
- A return path that is not on the allowlist falls back to `/rooms`
- After a pod restart, opening Rooms or Moderation reconnects with no click
- A failed reconnect shows the manual panel and does not retry
- `403` shows the "not an admin" message and never redirects
- `make check` green
