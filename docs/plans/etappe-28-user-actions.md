# Etappe 28 — User write actions

**Date:** 2026-08-04 · **System:** S13 · **Continues:** E27

## Scope

Lock, unlock, deactivate, reactivate, grant/revoke admin, set password.

## What the API descriptions actually say, and why each is a trap

Read from the running MAS's own spec. Every one of these contradicts what the verb
suggests, and a panel that does not say so is a panel that misleads at the exact
moment someone is responding to an incident:

| Action | The trap |
|---|---|
| **lock** | *Does **not** invalidate existing sessions.* Locking a compromised account does **not** kick the attacker out — their session keeps working the moment it is unlocked, and arguably before. |
| **unlock** | Does **not** reactivate a deactivated user. |
| **reactivate** | Does **not** unlock a locked user. |
| **deactivate** | Invalidates sessions, makes the user leave all rooms, and by default asks the homeserver to **GDPR-erase** them. |
| **set-admin** | Existing sessions **keep** admin access. Revoking admin does not end a session that already has it. |

So the confirmation dialog carries the consequence, not a generic "are you sure?".
"Are you sure" asks the operator to confirm they pressed the button they pressed;
it does not tell them what it does.

## The erase decision

MAS's `deactivate` defaults to `skip_erase: false`, i.e. **it erases**. A one-click
irreversible erasure is the wrong default for an admin panel, and it sits oddly
beside a `reactivate` endpoint that cannot bring the data back.

MatrixCtrl always sends `skip_erase: true` and says so. Erasure is a GDPR workflow
with its own consequences and its own audit requirements; it gets its own etappe or
it does not ship. Deviating from MAS's default is a deliberate choice and is stated
in the UI rather than left for someone to discover.

## Self-lockout

MatrixCtrl only admits MAS admins. So deactivating yourself, locking yourself or
revoking your own admin locks you out of the panel that would let you undo it.
Those three are refused for the acting user.

**The complication:** the session stores whatever `ExchangeCode` returned, which is
`matrix_user_id` from userinfo *if present* and the `sub` (a ULID) otherwise. The two
are different identifiers, so a naive comparison against the target's ULID would
silently fail to protect in one of the two cases — and a safety rail that works only
sometimes is worse than none, because it is trusted. The acting user is therefore
resolved through MAS by whichever form is present, and if resolution fails the action
is **refused**, not allowed: "I could not tell whether this is you" is not permission.

## Audit

The audit middleware records no request body, by design (§ audit.go) — which means a
password can never reach the audit table. It also means the path is the only place a
meaning can live, so the endpoints are **verb-in-path**: `/grant-admin` and
`/revoke-admin` rather than one `/set-admin` taking a boolean. The audit trail then
reads as what happened without logging anything that must not be logged.

## Mandatory edge-case walk ([PROZESS §1](../PROZESS.md))

1. **No ESS / ESS elsewhere.** No MAS → the actions are not offered at all, matching
   E27's list behaviour.
2. **Helm release in a bad state.** Unrelated.
3. **Not just Deployments.** N/A.
4. **Cluster slow or gone.** MAS unreachable → the action reports failure; it never
   reports success it did not observe.
5. **No outbound internet.** In-cluster.
6. **Both auth modes.** Bootstrap mode has no MAS client and no way to identify the
   acting user, so writes are refused there rather than performed unprotected.
7. **Config edge shapes.** A user MAS has since deleted (404), a password MAS rejects
   for complexity (400), password auth disabled (403) — each mapped to a message that
   says which one it was.
8. **Helm succeeded, hooks failed.** Unrelated.

## Definition of done

- Each action works against live MAS and the list reflects it
- Every dialog states the actual consequence, taken from the API's own description
- Self-lockout is refused, and refused when identity cannot be established
- A password never appears in the audit table or in a log line
- 400/403/404 from MAS are distinguishable in the UI
- S11 green **after** the deploy

## Outcome (2026-08-04)

Shipped in `0.1.29`. S11 all four green after the deploy (revision 31). All seven
write routes answer `401` without a token, and `GET` on a write route answers `405` —
none of them can be reached by a page load or a followed link.

Verified against the **live** MAS without touching a real account:

```
lock on an unknown id  -> 404, "MAS kennt dieses Konto nicht (mehr)."
identity resolution    -> both identifier forms land on the same account
```

The first proves the whole write path — token, POST, status mapping — reaches MAS.
The second is the one that matters: the self-lockout rail compares the acting
identity with the target, and the session may hold either an MXID or a ULID. Both
forms resolving to the same account is what makes the rail hold in either
deployment. Changing somebody's real account to learn this was not worth it; the
state changes themselves are pinned by tests against a stub.

### The test that exists to stop a future edit

`TestDeactivateNeverErases` pins `skip_erase: true`. If that ever flips, calls that
look identical start destroying data — and nothing else in the suite would notice.

### Centralised on the way

The confirmation modal existed inline in `config/history.tsx`. A second caller
appeared, so it moved into `mc.tsx` (rule 3) and gained Escape-to-close: a modal
dismissible only by finding the right pixel is one people click through to get rid
of, which defeats a confirmation.

### Deliberately not here

GDPR erasure — P2-25. A separate, explicitly-worded action, not a checkbox on
deactivate.
