# Etappe 35 — The session token is in the log again

**Date:** 2026-08-06 · **System:** S3 · **Found by:** reading the operator's own
deploy log while watching an ESS upgrade

## The finding

```
"GET .../helm/releases/ess/upgrade/4756b6bd-…/logs?token=eyJhbGciOiJIUzI1NiIs… HTTP/1.1"
```

A valid session JWT, in plaintext, in the container log. Anyone who can run
`kubectl logs -n matrixctrl` — or read whatever collects those logs — can take over
the session until it expires. One line per WebSocket connection, so one per upgrade
run and one per config deploy.

## Why it survived E29, which was about exactly this

E29 removed the JWT from the OIDC callback URL. Its own rationale, still in
`internal/auth/authcode.go`, describes the mechanism:

> *"The OIDC callback redirected to `/auth/callback?token=<jwt>` and chi's request
> logger writes the full URL, so the token was written to the application log by the
> very request that delivered it."*

And `internal/api/middleware/auth.go` narrowed `?token=` to WebSocket upgrades only,
with a comment that names the same logger:

> *"…any link, log line or Referer carrying one was a usable session — and chi's
> request logger writes the full URL (P0-5)."*

So the exposure was understood, written down twice, and left in place for the one
route that still needed it. Narrowing *where* a credential may appear in a URL is not
the same as stopping it from being logged. E29 fixed the browser-visible half and left
the server-side half.

## Two halves, and it needs both

**1. The logger must not write credentials.** Even a perfect WebSocket scheme does not
help if some future route puts a secret in a query string. The logger is the choke
point, so it is where the guarantee belongs.

**2. The WebSocket must not carry the session token at all.** Redaction only fixes
*our* log. The URL still travels through the ingress, the tunnel and any proxy in
between, each with its own access log. A credential that must not be logged should not
be in a URL in the first place.

## Approach

- A request logger that redacts a fixed set of sensitive query keys and leaves the
  rest, so `?container=postgres` still helps debugging while `?token=…` never appears.
  Values are replaced, not dropped, so the log still shows the parameter was present.
- A **single-use, short-lived WebSocket ticket**, requested from an authenticated
  endpoint and spent by the handshake. A ticket in a log is worth nothing after the
  connection it opened.

**Why not reuse `AuthCodes` from E29**, despite it being the same shape (random,
single-use, `DELETE … RETURNING`): its codes are redeemable at `/auth/exchange` for a
full session. Sharing the store would mean a leaked WebSocket ticket could be traded
for one — turning a read-only log stream into a complete session, which is the
opposite of the point. Separate store, no shared redemption path. The duplication is
the security property.

In-memory rather than Postgres: a WebSocket connects to the process that issued its
ticket, the lifetime is one handshake, and a pod restart voiding outstanding tickets is
correct rather than a limitation.

## Mandatory edge-case walk ([PROZESS §1](../PROZESS.md))

1. **No ESS / ESS elsewhere.** Unrelated.
2. **Helm release in a bad state.** The stream is how that state is watched, so it
   must keep working while an upgrade is failing.
3. **Not just Deployments.** N/A.
4. **Cluster slow or gone.** Ticket issuing does not touch the cluster.
5. **No outbound internet.** N/A.
6. **Both auth modes.** Bootstrap and OIDC both produce a session; the ticket is
   issued from the session, so both work unchanged.
7. **Config edge shapes.** N/A.
8. **Helm succeeded, hooks failed.** Unrelated.

Additional cases this must survive: a replayed ticket, an expired ticket, a ticket for
one user used by another, a reconnect after the stream drops (E14's reconnect logic
must ask for a new ticket, not reuse the old one), and an unbounded number of issued
tickets.

## Definition of done

- No session JWT appears in the log for any request
- A ticket works exactly once; the second attempt is refused
- An expired ticket is refused
- The upgrade log stream still works end to end, including reconnect
- Non-sensitive query parameters are still logged
- S11 green **after** the deploy
