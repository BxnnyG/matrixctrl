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
| **2 — Element-Admin parity** | Users, rooms, reports/moderation | ✅ features done** |
| **3 — Day-2 operations** | RTC monitoring, TLS/DNS, backup/restore, full dashboard | ⬜ |
| **4 — Federation & bridges** | Federation management, mautrix bridges | ⬜ |
| **5 — Compliance & observability** | Cross-component audit, worker insights | ⬜ |
| **6 — Multi-instance & polish** | Multiple instances, i18n (incl. English UI) | ⬜ |

\* The building blocks are live; the greenfield happy path still has no
end-to-end test on a fresh cluster (S6 — the largest open gap).

\*\* Users (E27/E28), rooms (E36/E41), the event-report queue (E46), media
quarantine (E47) and the user-report queue (E48) all ship. What is deliberately
left out is a set of actions with no inverse — deleting media, deleting a room,
deactivating a reported user — plus protect/unprotect, which is reversible but
belongs with a permissions model that does not exist yet. The phase is
feature-complete in the sense that every *read* and every *reversible* action is
there; it is not closed.

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
| 18 | First impression — screenshots, maturity + German-UI disclosure, CHANGELOG, dependabot, `helm.go` split | ✅ 2026-08-01 · [plan](plans/etappe-18-first-impression.md) |
| 17 | Audit trail — the writes that were documented for two months but never existed | ✅ 2026-08-01 · `v0.1.18` · [plan](plans/etappe-17-audit-trail.md) |
| 19 | Calling — the ports that must be forwarded, and an explicit "this half is not checkable from here" | ✅ 2026-08-01 · `v0.1.19` · [plan](plans/etappe-19-calling-reachability.md) |
| 32 | Release Notes auf der Upgrade-Seite + Version aus der Liste übernommen — die andere Hälfte der Pin-Warnung | ✅ 2026-08-05 · `v0.1.33` · [plan](plans/etappe-32-release-notes.md) |
| 33 | OIDC-Init wiederholen statt einmalig aufgeben — ein Neustart vor MAS sperrte den Operator 11 h aus dem eigenen Panel aus | ✅ 2026-08-06 · `v0.1.34` · [plan](plans/etappe-33-oidc-retry.md) |
| 73 | Der Restore, der nichts wiederhergestellt und Erfolg gemeldet hätte — dazu Skeletons und ein ehrlicher Byte-Zähler | ✅ 2026-09-05 · `v0.1.69` · [plan](plans/etappe-73-silent-restore-and-loading.md) |
| 74 | Der Restore, der an den eigenen Fremdschlüsseln scheiterte — gefunden vom ersten echten Durchlauf | 🚧 · [plan](plans/etappe-74-restore-order.md) |
| 75 | Ein frisches Setup, in das man sich auch anmelden kann — Passwort im Secret statt in einer Logzeile | 🚧 · [plan](plans/etappe-75-first-login.md) |
| 72 | Ein Backup statt drei Entschuldigungen | ✅ 2026-09-05 · `v0.1.68` · [plan](plans/etappe-72-one-backup.md) |
| 71 | Wo die Konfiguration wirklich liegt — die beruhigendste Eigenschaft, die nie jemand ausgesprochen hat | ✅ 2026-09-05 · `v0.1.67` · [plan](plans/etappe-71-where-the-config-lives.md) |
| 70 | Die Volumes — und ein Navigationspunkt, der ins Leere zeigte | ✅ 2026-09-05 · `v0.1.66` · [plan](plans/etappe-70-homeserver-export.md) |
| 69 | Wiederherstellen — und was „derselbe Homeserver" eigentlich heißt | ✅ 2026-09-04 · `v0.1.65` · [plan](plans/etappe-69-restore.md) |
| 68 | Ein Backup, das sagt, was es nicht ist — Config-Repo samt Historie und eigene Datenbank, ausdrücklich ohne Homeserver | ✅ 2026-09-03 · `v0.1.64` · [plan](plans/etappe-68-backup.md) |
| 67 | Die Tabelle, auf die jeder zuerst verwiesen wird — S13 stand vier Monate auf „not started" | ✅ 2026-09-03 · kein Artefakt · [plan](plans/etappe-67-inventory-reconciliation.md) |
| 66 | Den Emulator überflüssig machen statt ihn zum Laufen bringen — arm64 scheiterte an einem einzigen `apk add` | ✅ 2026-09-03 · `v0.1.62` · [plan](plans/etappe-66-arch-independent-runtime.md) |
| 65 | Löschung — und was sie nicht löscht | ✅ 2026-09-02 · `v0.1.61` · [plan](plans/etappe-65-gdpr-erasure.md) |
| 64 | Blättern über eine Reihenfolge, die es nicht gibt — zwei von drei Hälften behoben, die dritte benannt | ✅ 2026-09-02 · `v0.1.60` · [plan](plans/etappe-64-stable-paging.md) |
| 63 | Das Schema versprach eine Funktion, die niemand gebaut hat — acht tote Spalten, eine davon wert, gefüllt zu werden | ✅ 2026-09-02 · `v0.1.59` · [plan](plans/etappe-63-schema-promises.md) |
| 62 | Der dritte Aktionstyp, angeboten und nicht implementiert — und die Suche nach dieser Form | ✅ 2026-08-31 · `v0.1.58` · [plan](plans/etappe-62-http-hook-action.md) |
| 61 | Der Trigger, der angeboten wurde und nie feuerte — ein Rollback warf alle Hand-Patches weg | ✅ 2026-08-31 · `v0.1.57` · [plan](plans/etappe-61-rollback-hooks.md) |
| 60 | Die Funktionen, die niemandem erklärt wurden — ein Leitfaden fürs Benutzen statt fürs Warten | ✅ 2026-08-31 · kein Artefakt · [plan](plans/etappe-60-user-guide.md) |
| 59 | Die Sparkline, die ihre eigene Vergangenheit erfunden hat — und die Spalte, die den Ausfall erklärt hätte | ✅ 2026-08-31 · `v0.1.56` · [plan](plans/etappe-59-node-history.md) |
| 58 | Der Teil der Handy-Ansicht, der eng geblieben war — und die Konvention, die es längst gab | ✅ 2026-08-31 · `v0.1.55` · [plan](plans/etappe-58-mobile-polish.md) |
| 57 | Das Panel auf dem Handy — Schublade statt Rail, und als App auf den Startbildschirm | ✅ 2026-08-31 · `v0.1.54` · [plan](plans/etappe-57-mobile-and-pwa.md) |
| 56 | Speicher auch prüfen — aber nur dort, wo es Arithmetik ist und keine Einschätzung | ✅ 2026-08-31 · `v0.1.54` · [plan](plans/etappe-56-memory-too.md) |
| 55 | Die Zahl abfangen, bevor sie zum Ausfall wird — Chart rendern statt Values lesen | ✅ 2026-08-31 · `v0.1.53` · [plan](plans/etappe-55-capacity-preflight.md) |
| 54 | „down" ist ein Status, keine Diagnose — warum ein Pod nicht eingeplant werden kann, mit der Rechnung dahinter | ✅ 2026-08-30 · `v0.1.52` · [plan](plans/etappe-54-why-it-cannot-be-placed.md) |
| 53 | Das lauteste Element der Seite war doppelt falsch — „postgres in Restart-Schleife", obwohl postgres nie neu gestartet ist | ✅ 2026-08-17 · `v0.1.51` · [plan](plans/etappe-53-restart-banner.md) |
| 52 | Einmal verbinden, und da landen wo man war — der Rücksprung hing am State, der Auto-Reconnect an seiner Schleifenbremse | ✅ 2026-08-17 · `v0.1.50` · [plan](plans/etappe-52-silent-reconnect.md) |
| 51 | Der UDP-Puffer, den die SFU anfordert und nicht bekommt — und der Zähler, der aus dem falschen Namespace gelesen worden wäre | ✅ 2026-08-17 · `v0.1.49` · [plan](plans/etappe-51-udp-buffer-preflight.md) |
| 50 | Ein eingecheckter Build-Artefakt, der seiner Quelle widerspricht — das eingebettete Frontend war 16 Tage alt und kannte die Moderation nicht | ✅ 2026-08-17 · kein Artefakt · [plan](plans/etappe-50-untrack-embedded-frontend.md) |
| 49 | Die Prüfung, die bestand, ohne etwas geprüft zu haben — Exit 0 bei 10 von 11 übersprungenen Routen | ✅ 2026-08-17 · kein Artefakt · [plan](plans/etappe-49-verification-that-verifies.md) |
| 48 | Die zweite Queue — und eine ID, die zwei verschiedene Dinge bedeutet | ✅ 2026-08-17 · `v0.1.48` · [plan](plans/etappe-48-user-reports.md) |
| 47 | Medien-Quarantäne — der Endpunkt antwortet 200 und tut manchmal nichts | ✅ 2026-08-16 · `v0.1.47` · [plan](plans/etappe-47-media-quarantine.md) · Round-Trip live offen (P2-32) |
| 46 | Moderation — die Meldungs-Queue, und was „erledigt" heißt, wenn Synapse nur Löschen kennt | ✅ 2026-08-16 · `v0.1.46` · [plan](plans/etappe-46-event-reports.md) |
| 45 | Die Staleness-Warnung war zwölf Tage lang falsch — `addrs[0]` aus einer rotierenden DNS-Antwort; dazu Retention für beide RTC-Tabellen | ✅ 2026-08-16 · `v0.1.45` · [plan](plans/etappe-45-address-set.md) |
| 44 | Calls: wer gerade telefoniert, und ein Verlauf — die SFU-Zähler sterben mit dem Pod, den der Post-Upgrade-Hook jedes Mal löscht | ✅ 2026-08-16 · `v0.1.44` · [plan](plans/etappe-44-call-history.md) |
| 43 | Das Upgrade-Fenster zeigte eine Uhr statt Fortschritt — dazu Versionen mit Datum und Notes, und der Typecheck, der nie etwas geprüft hat | ✅ 2026-08-16 · `v0.1.43` · [plan](plans/etappe-43-upgrade-progress.md) |
| 42 | Räume verbinden ging und funktionierte dann nicht — der Scope, den E36 bewusst weggelassen hatte; dazu Historie über Neustarts hinweg schnell | ✅ 2026-08-16 · `v0.1.43` · [plan](plans/etappe-42-rooms-connect-and-latency.md) |
| 41 | Räume, zweite Hälfte — Detail, Mitglieder, und die eine Aktion, die man zurücknehmen kann | ✅ 2026-08-16 · `v0.1.43` · [plan](plans/etappe-41-room-detail.md) |
| 40 | Namespace-Eingrenzung (P0-4a) — der Blocker aus E37 war nie geprüft worden; `lookup` beantwortet ihn in zehn Minuten | ✅ 2026-08-16 · `v0.1.41` · [plan](plans/etappe-40-namespaced-rbac.md) |
| 39 | Die Historienseite kostete 4 Sekunden pro Aufruf — und beide naheliegenden Optimierungen waren messbar falsch | ✅ 2026-08-15 · `v0.1.40` · [plan](plans/etappe-39-history-read.md) |
| 38 | Der Backlog beschrieb diesen Code nicht mehr — acht Einträge behaupteten längst behobene Probleme; dazu die Restart-Zahl, die 42 sagt und nicht wessen | ✅ 2026-08-15 · `v0.1.39` · [plan](plans/etappe-38-backlog-reconciliation.md) |
| 37 | RBAC-Scoping (P0-4) — die ClusterRole war `*` auf allem; jetzt aufgezählt, vor dem Anwenden per SubjectAccessReview bewiesen, und die verbleibende Über-Vergabe als fehlschlagender Test statt als Absatz | ✅ 2026-08-15 · `v0.1.38` · [plan](plans/etappe-37-rbac-scoping.md) |
| 36 | Räume (read-only) — Synapse-Admin-API mit der Autorität des Operators, eigener Autorisierungsfluss, Refresh-Token nur im Speicher | ✅ 2026-08-15 · `v0.1.37` · [plan](plans/etappe-36-rooms.md) |
| 35 | Session-JWT aus dem Log — WebSocket-Handshake mit Einmal-Ticket statt Session-Token, Logger redigiert Credentials | ✅ 2026-08-06 · `v0.1.36` · [plan](plans/etappe-35-ws-ticket.md) |
| 34 | Guaranteed QoS — das Panel stand mit 14 MB RSS ganz vorn in der Kill-Reihenfolge eines Node-OOM, den es nicht verursacht hatte | ✅ 2026-08-06 · `v0.1.35` · [plan](plans/etappe-34-oom-victim-order.md) |
| 31 | Rollout-Transparenz — der Fortschritt sagt, welcher Pod hängt und warum; gepinnte Image-Tags gegen die Chart-Defaults gemeldet | ✅ 2026-08-05 · `v0.1.32` · [plan](plans/etappe-31-rollout-visibility.md) |
| 30 | "Continue to &lt;ULID&gt;?" — MAS client registration made reconcilable instead of one-shot, so an existing install can gain fields the generator learned later | ✅ 2026-08-05 · `v0.1.31` · [plan](plans/etappe-30-oidc-client-name.md) |
| 29 | Security review — JWT out of the URL, non-root container, login throttling, weak-key fallback removed (RBAC scoping deliberately separate) | ✅ 2026-08-04 · `v0.1.30` · [plan](plans/etappe-29-security-review.md) |
| 28 | User write actions — every dialog states what the verb actually does, self-lockout refused, deactivation never erases | ✅ 2026-08-04 · `v0.1.29` · [plan](plans/etappe-28-user-actions.md) |
| 27 | Phase 2 beginnt — user list from MAS, read-only: shared admin client, cursor paging, locked ≠ deactivated | ✅ 2026-08-04 · `v0.1.28` · [plan](plans/etappe-27-users.md) |
| 26 | Von außen prüfen — the permanent "cannot be checked from here" replaced by an opt-in outside vantage point, with a control that decides whether "closed" can be believed | ✅ 2026-08-04 · `v0.1.27` · [plan](plans/etappe-26-reachability.md) |
| 25 | Hand-Edits — ask the API server who owns each field instead of diffing manifests; closes the half of P1-11 that E21 could not see | ✅ 2026-08-04 · `v0.1.26` · [plan](plans/etappe-25-manual-edits.md) |
| 24 | Calls — which path does this actually support: the page reported on the SFU while the failing calls were legacy 1:1, which needs a relay ESS has no option for | ✅ 2026-08-04 · `v0.1.25` · [plan](plans/etappe-24-call-paths.md) |
| 23 | Media evidence — "has a call ever worked?", from counters that only move when media flows | ✅ 2026-08-04 · `v0.1.24` · [plan](plans/etappe-23-media-evidence.md) |
| 22 | Stale announcement — the SFU discovers its public address once at startup, and a consumer line is re-addressed daily | ✅ 2026-08-03 · `v0.1.22` · [plan](plans/etappe-22-stale-address.md) |
| 21 | Drift — "enabled" is not "applied": every hook patch checked against the live object, after an upgrade run outside MatrixCtrl broke calling | ✅ 2026-08-03 · [plan](plans/etappe-21-drift.md) |
| 20 | The release read was never expensive — cold `/status` 4.32 s → 505 ms, and the staleness window removed rather than shortened | ✅ 2026-08-02 · `v0.1.20` · [plan](plans/etappe-20-release-read.md) |

> Etappes 11 and 12 were committed on 2026-07-31 as part of etappe 13, in nine
> reviewable slices (`9b226c5`…`c8fbd4d`).

## Up next (order is a proposal, movable)

| # | Undertaking | Why now | Status |
|---|-------------|---------|--------|
| 13 | **CI & verification chain** (S9) | Nothing enforced the definition of done except memory. | ✅ 2026-07-31 |
| 14 | **Upgrade stream & dashboard latency** (S2, S4) | Both reported from the real 26.7.2 upgrade; a working upgrade looked like a failed one. | ✅ 2026-08-01 |
| 15 | **Greenfield end-to-end test** (S6) | "Works for anyone" was the product claim and it was **false** — deploy never worked. Fixed and proven. | ✅ 2026-08-01 (connect-OIDC still open) |
| 16 | **Release coherence** (S8) | Published chart 0.1.0 vs running image 0.1.14 — the README mis-installed the project. | ✅ 2026-08-01 |
| 18 | **First impression** (S8, S9) | Nobody had ever looked at the repo as a stranger: no screenshots, no maturity notice, no disclosure that the UI is German. | ✅ 2026-08-01 |
| 17 | **Audit trail** (S10) | Not "a UI over existing writes" — checking found the writes never existed and production had 0 rows. | ✅ 2026-08-01 |
| 19 | **Calling reachability** (S14) | Calling was broken while every signal was green; the deciding half was never shown. | ✅ 2026-08-01 |
| 20 | Phase 2 — users, rooms, moderation (S13) | Started 2026-08-04 with the user list (E27). Rooms, moderation and user *writes* still open. | 🔄 |

## Phase detail

### Phase 2 — Element-Admin parity ⬜ (next)
- **User management** (Synapse + MAS Admin APIs) — list/search/filter, create,
  deactivate/reactivate, reset password, set admin, external IdP links, devices.
- **Room management** — list/search, members, state, delete/quarantine, block.
- **Reports & moderation** — event report queue ✅ (E46), media quarantine ⬜.
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
| Public URL | whatever `ingress.host` is set to in the gitignored instance values (Traefik ingress, cert-manager). **Deliberately not written down here** — this file is public, and the URL of a live admin panel is not repository metadata |
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
| 2026-08-01 | **Second rewrite** ([DESIGN.md §4.19](DESIGN.md), BACKLOG P0-1c): all 83 commits were rewritten again to remove the admin panel's URL and the derived ESS hostnames. Every hash in this file changed a second time and none of the old ones resolve. The tag `v0.1.15` was moved and its release re-published from the cleaned tree. |
| 2026-08-01 | Commit hashes in the etappe table below were rewritten by the history sanitisation ([DESIGN.md §4.14](DESIGN.md)); the old hashes no longer resolve. |
| 2026-07-31 | Vision, non-goals and architecture moved to [VISION.md](VISION.md); etappe chronicle reconstructed from `git log`; operations notes added. |
