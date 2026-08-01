# MatrixCtrl — Roadmap

> Where the project has been and what comes next.
> Goal: [VISION.md](VISION.md) · Process: [PROZESS.md](PROZESS.md) ·
> Systems: [DESIGN.md](DESIGN.md) · Onboarding design: [SETUP.md](SETUP.md)

An **etappe** is one release-worthy feature package that can be deployed (§4.12).
Phases below are the coarse grid; etappes are the unit of work.

## Phases at a glance

| Phase | Scope | State |
|-------|-------|-------|
| **0 — Discovery & PoC** | Architecture, Helm SDK + hooks PoC, OIDC PoC, schema extraction | ✅ done |
| **1 — MVP (the differentiators)** | Config management, Helm upgrades + hooks, dashboard, OIDC, own chart | ✅ done |
| **1.5 — Setup & Onboarding** | Greenfield deploy, adopt existing, auto-register OIDC, runtime auth switch | ⏳ mostly done* |
| **2 — Element-Admin parity** | Users, rooms, reports/moderation | ⬜ next |
| **3 — Day-2 operations** | RTC monitoring, TLS/DNS, backup/restore, full dashboard | ⬜ |
| **4 — Federation & bridges** | Federation management, mautrix bridges | ⬜ |
| **5 — Compliance & observability** | Cross-component audit, worker insights | ⬜ |
| **6 — Multi-instance & polish** | Multiple instances, i18n (incl. English UI) | ⬜ |

\* The building blocks are live; the greenfield happy path still has no
end-to-end test on a fresh cluster (S6 — the largest open gap).

## Etappes

Etappes 1–10 are **reconstructed from `git log`** (39 commits, 2026-05-27 →
2026-05-30) and grouped after the fact; they were not planned under this process.

| # | Etappe | Status |
|---|--------|--------|
| 1 | Project skeleton — Go module, chi router, React app, Docker/Helm scaffolding | ✅ 2026-05-27 · `8a0addc` |
| 2 | Phase 0 feature-complete — hooks + upgrade pages, SPA routing fix | ✅ 2026-05-27 · `9dacf8c` |
| 3 | Dashboard basics — dark mode, node metrics, evicted pods, ESS version | ✅ 2026-05-27 · `e03f7a0` |
| 4 | Config management — git-backed YAML slice editor (Phase 1 core) | ✅ 2026-05-27 · `6a899ec` |
| 5 | Config history & rollback, Config→Helm wiring, pod actions, system tab | ✅ 2026-05-28 · `17f2e97` |
| 6 | Auth — OIDC via MAS, admin-only via MAS Admin API, production chart | ✅ 2026-05-29 · `fc8c750`, `dc7b0c7` |
| 7 | Self-configuring install — auto-generated secrets, login redesign, in-cluster cutover | ✅ 2026-05-29 · `ef4bdb1`, `baf8d2f` |
| 8 | Per-section config — Standard/YAML modes, comment-preserving migrator, full-bleed settings | ✅ 2026-05-30 · `6cc23d1` |
| 9 | Phase 1.5 setup — greenfield deploy, connect-OIDC, ESS discovery + adopt | ✅ 2026-05-30 · `a46dd74`, `c81ead8`, `eb4e69f` |
| 10 | Public-repo hardening — sanitised chart, AGPL, community health files, module rename | ✅ 2026-05-30 · `284cb03`…`0566513` |
| 11 | Design system — dark-only tokens, 3 directions, `mc.tsx` primitives, all screens restyled | ✅ 2026-06-04 · image 0.1.10 |
| 12 | Observability & correctness — pod drill-down with restart cause, event feed, hook editor, version-list + diff fixes | ✅ 2026-07-31 · image 0.1.12 |
| 13 | CI & verification chain — GitHub Actions, 22 frontend tests, headless-browser route check | ✅ 2026-07-31 · [plan](plans/etappe-13-ci-and-verification.md) |
| 14 | Reliable upgrade stream & dashboard latency — heartbeat, reconnect, release cache, client-go limits | ✅ 2026-08-01 · image 0.1.14 · [plan](plans/etappe-14-upgrade-stream-and-dashboard-latency.md) |
| 16 | Release coherence — tag-triggered publish of image + chart, version guard, [RELEASING.md](RELEASING.md) | ✅ 2026-08-01 · `v0.1.15` · [plan](plans/etappe-16-release-coherence.md) |
| 15 | Greenfield proven — 4 defects found by the first real run on an empty cluster; ESS deployed end to end | ✅ 2026-08-01 · [plan](plans/etappe-15-greenfield-first-half.md) |

> Etappes 11 and 12 were committed on 2026-07-31 as part of etappe 13, in nine
> reviewable slices (`9b226c5`…`c8fbd4d`).

## Up next (order is a proposal, movable)

| # | Undertaking | Why now | Status |
|---|-------------|---------|--------|
| 13 | **CI & verification chain** (S9) | Nothing enforced the definition of done except memory. | ✅ 2026-07-31 |
| 14 | **Upgrade stream & dashboard latency** (S2, S4) | Both reported from the real 26.7.2 upgrade; a working upgrade looked like a failed one. | ✅ 2026-08-01 |
| 15 | **Greenfield end-to-end test** (S6) | "Works for anyone" was the product claim and it was **false** — deploy never worked. Fixed and proven. | ✅ 2026-08-01 (connect-OIDC still open) |
| 16 | **Release coherence** (S8) | Published chart 0.1.0 vs running image 0.1.14 — the README mis-installed the project. | ✅ 2026-08-01 |
| 17 | **Audit log UI** (S10) | The table and the writes already exist; nothing reads them back. Cheap. | ⏳ |
| — | Phase 2 — users, rooms, moderation (S13) | Deliberately parked behind the above ([VISION.md](VISION.md)). | ⬜ |

## Phase detail

### Phase 2 — Element-Admin parity ⬜ (next)
- **User management** (Synapse + MAS Admin APIs) — list/search/filter, create,
  deactivate/reactivate, reset password, set admin, external IdP links, devices.
- **Room management** — list/search, members, state, delete/quarantine, block.
- **Reports & moderation** — event report queue, media quarantine.
- **Deliverable:** existing ESS admins can drop element-admin.

### Phase 3 — Day-2 operations ⬜
- Element Call / RTC monitoring — active calls, SFU/TURN health, RTC patch state.
- TLS / DNS — certificate expiry, well-known/federation checks, public-IP drift.
- Backup / restore — scheduled Postgres + media + config backups, one-click restore.
- Dashboard (full) — activity feed, resource trends, cert-expiry countdown.

### Phase 4 — Federation & bridges ⬜
- Federation allowlist/denylist, per-server health checks.
- Bridges — mautrix-* and Hookshot behind a common UI via a plugin architecture,
  rather than hardcoding each one.

### Phase 5 — Compliance & observability ⬜
- Unified, queryable audit log across Synapse + MAS + bridges + admin actions.
- Per-worker load and scaling recommendations.

### Phase 6 — Multi-instance & polish ⬜
- Manage multiple ESS instances from one MatrixCtrl.
- i18n, including an English UI (the current UI ships German only).

## Success criteria

- **Phase 1:** the operator administers their production ESS entirely through
  MatrixCtrl; Helm upgrades never break the SFU/hostNetwork config again.
- **Phase 2:** MatrixCtrl fully replaces element-admin for user/room management.
- **Phase 5:** an organisation with real compliance needs (a club, a school) runs
  it in production.

## Operations notes

The things you need at 3 a.m. **No passwords here — only where they live.**

| What | Where |
|---|---|
| MatrixCtrl namespace / release | `matrixctrl` / `matrixctrl` (Helm) |
| Managed ESS | namespace `ess`, release `ess`, chart `matrix-stack` |
| Public URL | `https://matrixctrl.example.com` (Traefik ingress, cert-manager) |
| Image | `ghcr.io/bxnnyg/matrixctrl` — published by CI on a `v*` tag and **pulled from GHCR**; local `k3s ctr images import` is only for dev builds |
| Instance Helm values | `deploy/helm/matrixctrl/values.bxnny.yaml` — **gitignored**, excluded from the packaged chart |
| Config repo | `/data/config-repo` on a PVC inside the pod; one YAML per ESS section + `config-slices.json`. Pre-migration monolith in `_backup-pre-sections/` |
| Database | PostgreSQL 16 sidecar in the same pod, own PVC |
| Secrets | Kubernetes secrets in ns `matrixctrl`, auto-generated on first install (`resource-policy: keep`) |
| JWT signing key | This install injects it via `MATRIXCTRL_JWT_SECRET` from `secret/matrixctrl-secret` key `jwt-secret`, so the env var wins and the `instance_settings` fallback stays empty. A fresh install without that env var generates and persists a key in `instance_settings` instead |
| MatrixCtrl's MAS client | defined in the ESS values under `matrixAuthenticationService.additional` + `policy.data.admin_clients` |
| Go toolchain | `/usr/local/go/bin/go` (not on default PATH) |
| Frontend embed | Go embeds `cmd/matrixctrl/dist` — the Makefile copies `web/dist` there before building |

## Changes to this file

| Date | What changed |
|------|--------------|
| 2026-08-01 | Commit hashes in the etappe table below were rewritten by the history sanitisation ([DESIGN.md §4.14](DESIGN.md)); the old hashes no longer resolve. |
| 2026-07-31 | Vision, non-goals and architecture moved to [VISION.md](VISION.md); etappe chronicle reconstructed from `git log`; operations notes added. |
