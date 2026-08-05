# Etappe 29 — Answering the security review, except the RBAC

**Date:** 2026-08-04 · **Systems:** S1, S8 · **Closes:** P0-5, P1-16, P1-17, P2-26,
P2-27, P2-28 · **Does not touch:** P0-4

## Why not P0-4 in the same etappe

Scoping the ClusterRole is the review's top finding and the one most likely to break
the product: if the enumeration misses a resource type the ESS chart creates, the
next upgrade fails, and it fails at the moment someone is trying to fix something
else. It needs a real upgrade against a live release to prove it. That is its own
etappe with its own verification, not the sixth item on a hardening list.

Everything here is bounded, and one of them is leaking right now.

## P0-5 — the session JWT is in a URL, and in the log

`/auth/callback?token=<jwt>` plus chi's request logger, which writes the full URL.
Verified: 400 of the last 400 log lines carry a complete URL. The token is written to
the application log **by the very request that delivers it**.

**Fix: a one-time code, in the fragment.**

```
redirect → /auth/callback#code=<one-time>
SPA      → POST /api/v1/auth/exchange {code}  →  {token}
```

Two independent properties, and both are needed:

- **The fragment is never sent to the server.** Not in the access log, not in
  `Referer`. A query parameter would still be logged even if it only carried a code.
- **The code is single-use and short-lived.** So the copy left in browser history is
  spent by the time anyone reads it.

Consumed with `DELETE … RETURNING`, the same atomic pattern the OIDC state already
uses — a code that can be redeemed twice is a code that can be replayed.

**And** `extractToken` stops accepting `?token=` everywhere. A browser cannot set
headers on a WebSocket, which is the one real reason the fallback exists; every other
route has no excuse and inherits the exposure.

## P1-16 — worse than the review said

The review flagged the `randomKey()` fallback in `NewBootstrap`, which is ephemeral.
But `randomKey()` is also on the **normal** path, in `getOrCreateJWTSecret`: its
result is base64-encoded and `INSERT`ed as the instance's permanent JWT secret. A
failed `crypto/rand` at first boot therefore persists
`matrixctrl-fallback-<unix-nanos>` as the signing key — derivable from the pod start
time, which Kubernetes publishes, and surviving every restart after.

**Fix: fail hard.** A process that cannot obtain a secure signing key must not start.
There is no degraded mode worth having here: the alternative to not starting is
running with forgeable sessions on an admin panel.

## P1-17 — no rate limit on login

Per-IP and per-user, with backoff. bcrypt makes each attempt slow, which is a cost,
not a limit.

The failed attempts are counted **in Postgres, not in memory**: the pod restarts, and
an in-memory counter would make restart-and-retry the attack. It also has to survive
the case where the DB is unreachable — and there the honest choice is to refuse the
login, not to wave it through, because the alternative is that killing the database
disables the rate limit.

## P2-26, P2-27, P2-28

- **CORS**: the frontend is served by this same binary on the same origin, so nothing
  needs the wildcard. Removed rather than narrowed — a header nobody needs is not
  worth configuring.
- **securityContext**: the ESS chart sets `runAsNonRoot`, `readOnlyRootFilesystem`
  and drops all capabilities on its own workloads. The panel that manages it runs as
  root. Non-root `USER` in the image plus the matching chart block.
- **RevokeSession**: same keyfunc check as `ValidateToken`. Not exploitable today;
  the point is that the two disagreeing is what makes a library upgrade dangerous.

## Mandatory edge-case walk ([PROZESS §1](../PROZESS.md))

1. **No ESS / ESS elsewhere.** Auth-only changes; unaffected.
2. **Helm release in a bad state.** Unaffected.
3. **Not just Deployments.** N/A.
4. **Cluster slow or gone.** The exchange endpoint needs Postgres, which login
   already needs.
5. **No outbound internet.** Nothing added leaves the cluster.
6. **Both auth modes.** Bootstrap login returns its token in a POST body already and
   is untouched by the code exchange; only the OIDC callback changes. Rate limiting
   applies to bootstrap login, the only password endpoint.
7. **Config edge shapes.** Expired code, replayed code, absent code, tampered code —
   each covered by a test.
8. **Helm succeeded, hooks failed.** Unrelated.

## Definition of done

- Logging in produces **no** log line containing a JWT
- A code cannot be redeemed twice, and expires
- `?token=` is rejected on a normal route and still works for the WebSocket
- A failed `crypto/rand` prevents startup instead of persisting a guessable key
- Repeated failed logins are refused, and the counter survives a pod restart
- The container runs as non-root
- S11 green **after** the deploy

## Outcome (2026-08-04)

Shipped in `0.1.30`. S11 all four green after the deploy (revision 32).

Verified on the running system:

```
container            uid=65532 gid=65532
root filesystem      read-only; /tmp writable
config repo          owned 65532, writable          ← would have broken without the initContainer
CORS headers         none
?token= on /status   401
POST /exchange       bogus code → 401
code redemption      1st → 200 with token · 2nd → 401 (single-use holds)
```

The redemption test used a throwaway code and its session was deleted afterwards; no
account was touched.

### What the tests found that the review did not

The login backoff shifted `1 << (failures-5)` unbounded. Past roughly 62 failures the
multiplication overflows and the delay wraps to **zero** — the attacker who has failed
the most would wait the least. Reachable: `Check` keeps returning a backoff after a
lockout window expires without resetting the counter, so `failures` grows without
bound. The test was written for the *shape* of the curve rather than for a specific
value, which is why it caught it.

### What looking before deploying found

The non-root switch would have broken config saving on this very installation: the
repo on the PVC is `-rw-r--r-- root root` from earlier versions, readable by UID
65532 and not rewritable. That is S11 check #2, and it would have failed *after* the
deploy rather than before it.

### The gap I cannot close from here

The full browser round trip — redirect with the fragment, the SPA reading
`location.hash`, the exchange call — is not exercised by anything run here. Both
backend halves are proven (a code redeems exactly once; a bogus one is refused) and
the frontend reads a hash and POSTs it, but "it compiles and the API works" is not
"someone logged in". **That needs one real login to confirm.**

### Not in this etappe

P0-4, the ClusterRole. Its own etappe with a real upgrade run behind it.
