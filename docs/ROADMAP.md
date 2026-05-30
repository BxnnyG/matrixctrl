# MatrixCtrl Roadmap

The single source of truth for where MatrixCtrl is and where it's going.
For the onboarding/setup design see [SETUP.md](SETUP.md); for developer context see
[../CLAUDE.md](../CLAUDE.md).

## Vision

An open-source Day-2 admin layer for self-hosted Matrix / Element Server Suite (ESS)
Community — the config, upgrade, and operations UI that ESS Pro keeps proprietary,
as free software for homelabs, small orgs, schools, and non-profits.
*What UniFi is for networks, MatrixCtrl wants to be for Matrix.*

## Status at a glance

| Phase | Scope | State |
|-------|-------|-------|
| **0 — Discovery & PoC** | Architecture, Helm SDK + hooks PoC, OIDC PoC, schema extraction | ✅ done |
| **1 — MVP (the differentiators)** | Config management, Helm upgrades + hooks, dashboard, OIDC, own chart | ✅ done |
| **1.5 — Setup & Onboarding** | Greenfield deploy, adopt existing, auto-register OIDC, runtime auth switch | ✅ mostly done* |
| **2 — Element-Admin parity** | Users, rooms, reports/moderation | ⬜ next |
| **3 — Day-2 operations** | RTC monitoring, TLS/DNS, backup/restore, full dashboard | ⬜ |
| **4 — Federation & bridges** | Federation management, mautrix bridges | ⬜ |
| **5 — Compliance & observability** | Cross-component audit, worker insights | ⬜ |
| **6 — Multi-instance & polish** | Multiple instances, i18n (incl. English UI) | ⬜ |

\* Phase 1.5 building blocks are live; the full greenfield install happy-path still
needs an end-to-end test on a fresh cluster.

---

## Phase 0 — Discovery & PoC ✅

- Analysed the ESS Helm chart structure per version; extracted the values JSON Schema.
- Proved out the Helm SDK (upgrade + post-hooks) and OIDC-via-MAS in Go.
- Decided the architecture: Go + Helm SDK + client-go + go-git, React frontend,
  single container, own Helm chart. **Deliverable:** ADR + PoC.

## Phase 1 — MVP, the differentiators ✅

The part nobody else builds.

- **Dashboard (minimal)** — component health (Synapse, MAS, RTC, Element, Postgres),
  pod status, restarts, node CPU/memory, evicted-pod cleanup, live pod logs.
- **Configuration management** — one versioned YAML file per ESS section, edited as a
  schema-driven **Standard** form *or* raw **YAML** (Monaco), comment-preserving so the
  in-file documentation survives edits. Git-backed: diff, history, rollback.
  JSON-Schema validation before apply.
- **Helm / update management** — version picker from the OCI registry, live upgrade
  logs, and **post-upgrade hooks** that re-apply the SFU patches (hostNetwork,
  `externalTrafficPolicy`) automatically — the core "patches survive upgrades" promise.
  Config → Deploy applies config changes to the cluster in one click.
- **Auth** — admin-only OIDC via MAS, verified through the MAS Admin API, with a local
  bootstrap fallback. DB password + JWT key are self-generated.
- **Deployment** — single container + Postgres sidecar, shipped as its own Helm chart.

**Deliverable reached:** configs are no longer edited by hand in `vim`; Helm upgrades
no longer break WebRTC calling.

## Phase 1.5 — Setup & Onboarding ✅ (mostly)

Turns "works for me" into "works for anyone". Design in [SETUP.md](SETUP.md).

- ✅ **Greenfield deploy** — `/setup` wizard: pick a version + server name, seed config
  from the chart's commented defaults, `helm install`.
- ✅ **Adopt existing ESS** — auto-discovered across namespaces; seed config from
  `helm get values` of the running release.
- ✅ **Auto-register OIDC client** — written into the MAS config MatrixCtrl manages,
  then helm-upgrade (no manual policy patching, because `admin_clients` is static MAS
  policy the Admin API can't change).
- ✅ **Runtime bootstrap→OIDC switch** — DB-backed OIDC config + hot-reload; no restart.
- ⬜ End-to-end greenfield live test on a fresh cluster.

## Phase 2 — Element-Admin parity ⬜ (next)

Standard admin features so MatrixCtrl can stand alone and replace element-admin.

- **User management** (Synapse Admin API + MAS Admin API) — list/search/filter, create,
  deactivate/reactivate, reset password, set admin, external IdP links, devices/sessions.
- **Room management** — list/search, members, state, delete/quarantine, block.
- **Reports & moderation** — event report queue, media quarantine, basic actions.
- **Deliverable:** existing ESS admins can drop element-admin and switch to MatrixCtrl.

## Phase 3 — Day-2 operations ⬜

- **Element Call / RTC monitoring** — active calls, SFU/TURN health, the RTC patch state.
- **TLS / DNS** — certificate overview + expiry, well-known/federation checks,
  DynDNS / public-IP drift detection.
- **Backup / restore** — scheduled backups of Postgres + media + config, one-click restore.
- **Dashboard (full)** — activity feed (joins, federation errors, admin actions),
  resource trends, cert-expiry countdown.

## Phase 4 — Federation & bridges ⬜

- **Federation** — allowlist/denylist management, per-server health checks.
- **Bridges** — mautrix-* and Hookshot via a plugin architecture (each bridge's admin
  API wrapped behind a common UI), rather than hardcoding each one.

## Phase 5 — Compliance & observability ⬜

- **Audit / activity log** — a unified, queryable log across Synapse + MAS + bridges +
  admin actions, cross-referenced with the user module.
- **Worker / scaling insights** — per-worker load, scaling recommendations.
- **Deliverable:** Pro-grade audit/compliance parity.

## Phase 6 — Multi-instance & polish ⬜

- Manage multiple ESS instances from one MatrixCtrl.
- **i18n**, including an English UI (the current UI ships in German).
- Community-feedback-driven polish.

---

## Non-goals (for now)

- Reimplementing Synapse/MAS — MatrixCtrl wraps their APIs, it doesn't replace them.
- Bare-metal / non-Kubernetes installs.
- A hosted SaaS — MatrixCtrl is self-hosted first.

## Success criteria

- **Phase 1:** you administer your production ESS entirely through MatrixCtrl;
  Helm upgrades never break the SFU/hostNetwork config again.
- **Phase 2:** MatrixCtrl fully replaces element-admin for user/room management.
- **Phase 5:** an organisation with real compliance needs (a club, a school) runs it
  in production.
