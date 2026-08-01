# Etappe 14 — Reliable upgrade stream & a dashboard that isn't slow

**Date:** 2026-08-01 · **Systems:** S2, S4 · **Closes:** P1-7, P1-8
**Trigger:** both were reported by the operator from the real 26.7.2 upgrade, not
found by inspection.

## Why these two together

They look unrelated and are not. Both are in the path the operator uses every day,
both were diagnosed in the same session, and both come down to the same mistake:
an expensive Helm operation sitting in a request path with nothing done about how
long it takes. Fixing one and shipping without the other means deploying twice.

## Problem 1 — the upgrade log stream (P1-7)

The 26.7.2 upgrade **succeeded** (revision 22, `deployed`), but the UI showed
`[Verbindung getrennt]` and never recovered. Four defects stacked:

| # | Defect | Where |
|---|---|---|
| 1 | `helm.Upgrade()` runs with `Wait=true`, `Timeout=10m` and emits nothing while it waits | `internal/helm/upgrade.go` |
| 2 | No keepalive — the socket is idle for minutes, Traefik's default `idleTimeout` is 180 s | `internal/api/handlers/ws.go` |
| 3 | The client never reconnects and never falls back to polling | `web/src/lib/ws.ts` |
| 4 | `onclose` reports every close as an error, including a clean one after `done` | `web/src/lib/ws.ts` |

**Useful discovery:** `GET /helm/releases/{name}/upgrade/{upgradeId}` already exists
and returns `{status, logs, done}`. The recovery path is a client change, not a new
endpoint.

### Approach

- **Server, application-level heartbeat.** `golang.org/x/net/websocket` has no
  ping/pong, so the heartbeat is a `{"type":"ping"}` message every 20 s while the
  handler waits. Below Traefik's 180 s idle timeout with a wide margin, and it works
  through any proxy because it is ordinary traffic.
- **Server, real progress.** A ticker in the upgrade goroutine emits
  `Waiting for rollout… (elapsed Xm Ys)` every 30 s. It goes through `stream.emit`,
  so it lands in `stream.logs` too and a reconnecting client replays it. This is
  what makes the silent phase legible — the heartbeat only keeps the socket open.
- **Client, recovery.** On an unexpected close, poll the status endpoint; if the
  upgrade is still running, reconnect with backoff (cap the attempts). The handler
  already replays `stream.logs` on connect, so a reconnect loses nothing.
- **Client, honest close.** Only report a problem when the upgrade is not finished.

## Problem 2 — dashboard latency (P1-8)

`/status` makes six calls **serially**, dominated by `GetRelease`, which uses
`action.NewGet` — the entire release (manifest, hooks, every chart file) is fetched
and decompressed out of a 416 KB secret so that seven scalars can be kept. Polled
every 15 s.

Measured on the live cluster (3 runs each):

| Call | Latency |
|---|---|
| k8s lists (deployments, statefulsets, nodes, pods), metrics-server | 535–965 ms |
| `helm get manifest` / `get metadata` / `list` | **~3.9–5.6 s, all three** |

All Helm paths cost the same because they all decompress the same secret. **There is
no cheaper SDK call** — so the fix is caching, not a different API.

### Approach

- **Cache `ReleaseInfo` in `internal/helm`,** not in the handler. Nine call sites
  use `GetRelease`; caching at the client covers all of them (§S12).
- **60 s TTL plus explicit invalidation** on upgrade / rollback / apply. Invalidation
  makes MatrixCtrl's own changes visible immediately; the TTL bounds staleness when
  the release is changed out of band (operator running `helm` directly) to 60 s.
- **Run the remaining `/status` calls concurrently.** They are independent.

## Definition of done (§4.12)

- `go test ./...`, `tsc --noEmit`, frontend build, all green
- New tests: TTL cache hit/expiry/invalidation; elapsed-time formatting
- Image built, imported into k3s, deployed, verified running — no manual step
- The four S11 regression checks
- **Feature proof:** `/status` measurably faster on the second poll, and a real
  upgrade survives >180 s of Helm silence without the socket dropping

## Risks

- **Stale release info.** Bounded by the TTL and cleared by invalidation. The
  dashboard is a status view; 60 s of staleness on a chart version is acceptable,
  and this is written down rather than assumed.
- **A ping the client mishandles** would print junk into the log view. The client
  filters `type: "ping"` explicitly, and unknown types are ignored rather than
  echoed.
- **Verifying defect 1 honestly requires a real upgrade** taking >180 s. ESS is
  already at the newest version, so the full end-to-end proof may have to wait for
  the next real upgrade; the heartbeat itself is testable without one.

## Outcome (2026-08-01, image 0.1.14)

**Latency, measured through the public ingress:**

| | Before | After |
|---|---|---|
| `/status` | 1.9–3.2 s | **0.14–0.25 s** |
| `/status/release` (Helm only) | ~4 s | **~0.16 s** |
| `/status/sysinfo` (k8s only) | ~1.15 s | **~0.15 s** |

**The plan was wrong about one thing, and finding out required measuring rather
than assuming.** Caching Helm and parallelising the reads left `/status` at ~1.9 s.
The shape of what remained gave it away — the first calls fast, then every call
pinned at ~1.1 s with an idle cluster. That is a client-side rate limiter, not a
slow cluster: client-go defaults to QPS 5 / Burst 10, sized for a one-shot CLI.
Parallelising did not cause it, it made it visible. Raised to 50/100 (§4.16), and
only then did the numbers move.

**Two defects were found while fixing P1-7, neither in the original report:**
- Dropped subscribers were never removed from `stream.subs`. Harmless when clients
  never reconnected — and this etappe makes reconnecting normal, so it had to be
  fixed in the same change.
- The terminal status was read outside the mutex after the channel closed.

**One more, found during the ship:** the tracked `cmd/matrixctrl/dist` was stale.
The image builds its own frontend, so the deployed pod was correct and nothing
looked wrong; only a plain `go build ./cmd/matrixctrl` would have embedded the old
UI. Synced here, and it sharpens P2-2.

**Definition of done:** `go vet` + `go test ./...` green (13 new backend tests,
race-clean), `tsc --noEmit` clean, 26 frontend tests green, image built, imported
and deployed to k3s, 9/9 routes verified in headless chromium. All four S11
invariants checked: ESS reachable (matrix/element/mas all 200), 18/18 config slices
still carry their `##` comments, admin login works and unauthenticated requests are
401, SFU patches intact (`hostNetwork=true`, `dnsPolicy=ClusterFirstWithHostNet`).

**Not proven:** defect 1 end-to-end against a real >180 s upgrade. ESS is already at
26.7.2, so there is nothing to upgrade to. The heartbeat, progress emission,
reconnect and replay-dedupe are covered by unit tests, but the first real proof will
be the next genuine ESS upgrade.
