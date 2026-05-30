# MatrixCtrl Roadmap

The single source of truth for where MatrixCtrl is and where it's going.
For the onboarding/setup design see [SETUP.md](SETUP.md); for developer context see
[../CLAUDE.md](../CLAUDE.md).

## Vision

An open-source Day-2 admin layer for self-hosted Matrix / Element Server Suite (ESS)
Community — the config, upgrade, and operations UI that ESS Pro keeps proprietary.
*What UniFi is for networks, MatrixCtrl wants to be for Matrix.*

## Status

| Phase | Scope | State |
|-------|-------|-------|
| **0 — Discovery & PoC** | Architecture, Helm SDK + hooks PoC, OIDC PoC, schema extraction | ✅ done |
| **1 — MVP (the differentiators)** | Config management, Helm upgrades + post-upgrade hooks, dashboard, admin-only OIDC, own Helm chart | ✅ done |
| **1.5 — Setup & Onboarding** | Greenfield deploy, adopt existing ESS, auto-register OIDC client, runtime bootstrap→OIDC | ✅ mostly done* |
| **2 — Element-Admin parity** | User management (Synapse + MAS API), room management, reports/moderation | ⬜ next |
| **3 — Day-2 operations** | RTC/call monitoring, TLS/DNS + DynDNS drift, backup/restore, full dashboard | ⬜ |
| **4 — Federation & bridges** | Federation allowlist + health, mautrix bridges (plugin architecture) | ⬜ |
| **5 — Compliance & observability** | Cross-component audit log, worker/scaling insights | ⬜ |
| **6 — Multi-instance & polish** | Multiple instances, i18n | ⬜ |

\* Phase 1.5 building blocks are live; the full greenfield install happy-path still
needs an end-to-end test on a fresh cluster.

## Phase 1 — delivered

- **Config management** — one versioned YAML file per ESS section, edited as a
  schema-driven **Standard** form *or* raw **YAML**, comment-preserving, git-backed
  (diff, history, rollback).
- **Helm upgrades** — version picker, live logs, post-upgrade hooks that re-apply the
  SFU patches automatically (the core "patches survive upgrades" promise).
- **Config → Deploy** — apply config to the cluster in one click.
- **Auth** — admin-only OIDC via MAS (verified through the MAS Admin API), with a
  local bootstrap fallback. Self-generated DB password + JWT key.
- **Deployment** — single container + Postgres sidecar, shipped as its own Helm chart.

## Phase 1.5 — setup & onboarding

Turns "works for me" into "works for anyone". See [SETUP.md](SETUP.md) for the design.

- ✅ Greenfield deploy (`/setup` wizard → seed config from chart defaults → install)
- ✅ Adopt an existing ESS (auto-discovered across namespaces; seed from `helm get values`)
- ✅ Auto-register the OIDC client (written into the MAS config MatrixCtrl manages, then
  helm-upgrade — no manual policy patching) + runtime bootstrap→OIDC switch
- ⬜ End-to-end greenfield live test on a fresh cluster

## Non-goals (for now)

- Reimplementing Synapse/MAS — MatrixCtrl wraps their APIs, it doesn't replace them.
- Bare-metal / non-Kubernetes installs.
- A hosted SaaS — MatrixCtrl is self-hosted first.

## Success criteria

- **Phase 1:** you administer your production ESS entirely through MatrixCtrl;
  Helm upgrades never break the SFU/hostNetwork config again.
- **Phase 2:** MatrixCtrl fully replaces element-admin for user/room management.
