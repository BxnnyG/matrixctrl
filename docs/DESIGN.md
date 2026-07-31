# MatrixCtrl — Design: systems, gaps, decisions

> Living document. Process: [PROZESS.md](PROZESS.md) · Goal: [VISION.md](VISION.md)
> · Timeline: [ROADMAP.md](ROADMAP.md)
>
> **The question behind every change: "does more than one place need this?" — if
> yes it belongs in a shared package under `internal/`, never duplicated.**

## 0. Guiding idea

MatrixCtrl is a thin, honest layer over three sources of truth it does not own:
the **git config repo**, the **Helm release**, and the **live cluster**. It never
keeps a second copy of their state. Everything the UI shows is read back from
one of the three.

| Layer | Owns | What MatrixCtrl adds |
|---|---|---|
| Git config repo (`/data/config-repo`) | The desired ESS values, one YAML per section | Comment-preserving edits, schema validation, diff/history/rollback |
| Helm release (`ess`) | What is actually deployed | Version discovery, upgrade with live logs, post-upgrade hooks |
| Kubernetes cluster | Runtime truth (pods, events, metrics) | Health roll-up, restart-cause diagnosis, targeted patches |
| PostgreSQL (sidecar) | MatrixCtrl's *own* state only | Sessions, audit log, hooks, upgrade history, instance settings |

The database never mirrors cluster or config state. If Postgres is wiped,
MatrixCtrl loses its history — not the operator's ESS.

## 1. Current inventory (what already exists centrally)

Read this before building. It is the file that exists so an agent does not have
to guess.

**Backend (Go 1.26, module `github.com/bxnnyg/matrixctrl`)**

| Package | What lives there |
|---|---|
| `cmd/matrixctrl/` | Entry point, dependency wiring, `go:embed all:dist` (reads `cmd/matrixctrl/dist`, **not** `web/dist`) |
| `internal/api/` | chi router, handlers, auth/audit middleware. HTTP only — no business logic |
| `internal/config/` | Section YAML store, merge, JSON-Schema validation, `yamledit.go` (yaml.v3 node surgery for comment-preserving writes), `migrate.go` |
| `internal/git/` | go-git wrapper: commit, log, checkout, working-tree diff, per-commit diff |
| `internal/helm/` | Helm v3 SDK: install, upgrade, rollback, history, OCI version discovery, release discovery across namespaces |
| `internal/hooks/` | Hook engine + runner + `builtin/ess_rtc_patches.go` (the four SFU patches, seeded at startup) |
| `internal/k8s/` | client-go: component health, pods + container detail, events, node metrics, PVCs, dynamic-client patching |
| `internal/auth/` | Bootstrap (bcrypt+JWT) and OIDC-via-MAS, hot-reloadable at runtime |
| `internal/db/` | pgx/v5 pool + sequential migrations |

**Frontend (React 18 + TanStack Router/Query, Vite)**

- `web/src/components/mc.tsx` — the design primitives (Icon set, Button, Card,
  Badge, StatusDot, Meter, Sparkline, Toggle, Tabs, EmptyState…). **New UI uses
  these**, not ad-hoc Tailwind.
- `web/src/index.css` — the token system: three switchable directions
  (Aura/Carbon/Graphite) × accent × density, all CSS custom properties on
  `<html>` data attributes. Dark-only.
- `web/src/lib/` — `api.ts` (typed fetch), `ws.ts` (upgrade log stream),
  `theme.ts` (tweaks store), `monaco.ts` (self-hosted editor), `schema.ts`,
  `sections.ts`.
- `web/src/components/config/YamlEditor.tsx` — the lazy boundary that keeps
  Monaco out of the entry bundle. **All editor usage goes through it.**

**Database tables:** `sessions`, `audit_log`, `config_snapshots`, `hooks`,
`hook_run_log`, `upgrade_history`, `ess_versions`, `instance_settings`.

**Deployment:** single container + Postgres sidecar, own Helm chart in
`deploy/helm/matrixctrl/`. Instance values (`values.*.yaml`) are gitignored and
excluded from the packaged chart.

## 1b. Status overview — **maintain this in EVERY etappe**

Legend: ✅ done · ⏳ open · ♾ standing rule (never "done" by design)

| System | Status | Rest / note |
|---|---|---|
| S1 Config management | ✅ (E4, E5, E8) | Comment-preserving, git-backed, schema-validated |
| S2 Helm release & versions | ✅ (E2, E12) | Version list fixed 2026-07-31 (pagination + semver) |
| S3 Post-upgrade hooks | ✅ (E2, E12) | Engine + built-ins + editor; enable/disable per deployment |
| S4 Cluster observability | ✅ (E3, E12) | Health, events, pod drill-down with restart cause |
| S5 Auth (bootstrap + OIDC) | ✅ (E6) | Admin-only via MAS Admin API, runtime switch |
| S6 Setup & onboarding | ⏳ ¾ | Deploy/adopt/connect built; **greenfield never e2e-tested on a fresh cluster** |
| S7 UI shell & design system | ✅ (E11) | Tokens + `mc.tsx`; all functional screens migrated |
| S8 Packaging & release | ⏳ ½ | Published chart is 0.1.0 while the running image is 0.1.12 — see §2 |
| S9 Verification & CI | ⏳ ¼ | Go unit tests exist; **no CI, no frontend tests** |
| S10 Audit trail | ⏳ ½ | `audit_log` table + middleware write; no UI to read it |
| S11 Regression safety net | ♾ Rule | Four invariants, checked before every ship — never "finished" |
| S12 Centralisation | ♾ Rule | "More than one place?" → shared package. Re-decided per change |
| S13 User & room management | ⏳ not started | Phase 2 — parked behind S6 ([VISION.md](VISION.md)) |
| S14 Day-2 operations (RTC/TLS/backup) | ⏳ not started | Phase 3 |
| S15 Federation & bridges | ⏳ not started | Phase 4 |
| S16 Compliance & scale insights | ⏳ not started | Phase 5 |
| S17 Multi-instance & i18n | ⏳ not started | Phase 6 — UI currently ships German only |

## 2. Systems & gaps

### S1 · Config management
**Purpose:** change ESS settings without hand-editing YAML, and be able to undo it.
**Today:** one YAML file per ESS section in a git repo on a PVC. Edited as a
schema-driven **Standard** form, as raw **YAML** (Monaco), or reviewed as a
**diff**. Form writes go through yaml.v3 node surgery so the chart's `##` comments
— which are the field help text — survive.
**Open:** none blocking.
✅ **Done (E4/E5/E8, 2026-05-27…2026-05-30):** slice editor, history, rollback,
per-section migration. Did *not* solve: bulk edit across sections, or validating
a value against the *running* Synapse rather than the schema.

### S2 · Helm release & version management
**Purpose:** see what is deployed, what is available, and move between them safely.
**Today:** release status/history, OCI version discovery from GHCR, upgrade with
live WebSocket logs, rollback.
✅ **Done (E12, 2026-07-31):** version listing was unusable — GHCR paginates and
ESS publishes a `<version>-sha<40hex>` tag per commit, so only ancient `0.2.x`
dev builds were ever shown, sorted as strings (`26.5.1` ranked above `26.10.0`).
Now paginated, build-tags filtered, numerically sorted. Did *not* solve: release
notes / breaking-change warnings per version (`ess_versions.changelog` is unused).

### S3 · Post-upgrade hook engine
**Purpose:** the core promise — manual patches survive `helm upgrade`.
**Today:** priority-ordered hooks with `kubectl_patch` / `wait_rollout` /
`http_request` actions, run via the dynamic client. Built-ins for the four SFU
patches are seeded at startup. Full CRUD editor; enable/disable decides what runs
on the next deployment (works for built-ins too, whose actions stay locked).
**Open:** hook failure surfaces as `hooks-failed` but there is no alert outside
the UI. A failed hook is silent until someone looks.

### S4 · Cluster observability
**Purpose:** answer "something is wrong — what and why?" without `kubectl`.
**Today:** component health for Deployments *and* StatefulSets, node CPU/memory,
PVCs, evicted-pod cleanup, namespace event feed, and a per-component drill-down
showing each container's restart cause (`lastState.terminated`: reason, exit code,
timestamp) plus its events and logs.
✅ **Done (E12, 2026-07-31):** before this, StatefulSets were missing entirely —
Synapse and Postgres, the two most important components, were invisible on the
dashboard. Did *not* solve: historical metrics (the sparkline is in-memory and
resets on reload), and no alerting.

### S5 · Authentication & authorisation
**Purpose:** only Matrix admins get in, without a second user database.
**Today:** OIDC via MAS with admin verified through the MAS Admin API; local
bootstrap (bcrypt+JWT) for greenfield; runtime switch between them without a
restart. JWT key auto-generated and persisted.
**Open:** single admin role — no read-only or per-area permissions.

### S6 · Setup & onboarding ⏳
**Purpose:** turn "works on our cluster" into "works on anyone's".
**Today:** ESS auto-discovery across namespaces, adopt-existing (seeds config from
`helm get values`), greenfield deploy wizard, and one-click Connect-Matrix-Login
that registers MatrixCtrl's own MAS client and helm-upgrades ESS.
**Open — the largest gap in the project:** the greenfield happy path
(deploy-ess → connect-oidc) has **never been run end-to-end on a fresh cluster.**
Our own instance can't exercise it: ESS already exists and OIDC is configured, so
the guards short-circuit. Discovery and adopt *are* live-validated. Design and the
bootstrap-paradox reasoning: [SETUP.md](SETUP.md).

### S7 · UI shell & design system
**Purpose:** one coherent visual language instead of per-page Tailwind.
**Today:** dark-only token system, three switchable directions plus accent and
density, Geist typography, primitives in `mc.tsx`, live Tweaks panel.
**Open:** UI strings are German only (see S17).

### S8 · Packaging & release ⏳
**Purpose:** a stranger can install the thing the README describes.
**Today:** Dockerfile (multi-stage), own Helm chart, GHCR image + OCI chart,
`make docker`.
**Open:** the published chart says `0.1.0` (`Chart.yaml`, README, CONTRIBUTING)
while the running image is `0.1.12`. Anyone following the README installs
something two months behind. There is no release checklist tying the three
version strings together, and no tags in git.

### S9 · Verification & CI ⏳
**Purpose:** prove an etappe is done without a human remembering to check.
**Today:** Go unit tests for `config`, `git`, `helm`; TypeScript typecheck; the
build itself. Verification is currently performed by the agent, by hand, per
session.
**Open:** no CI runs any of it. No frontend tests exist, although
`CLAUDE.md` claimed "Vitest + Testing Library" until 2026-07-31. No headless
browser is installed, so the operator-requested screenshot/UI check cannot run yet.
This is the first planned etappe — see [plans/etappe-13-ci-and-verification.md](plans/etappe-13-ci-and-verification.md).

### S10 · Audit trail ⏳
**Purpose:** who changed what, when.
**Today:** `audit_log` table plus middleware that records state-changing calls.
**Open:** nothing reads it back. There is no UI, so in practice the data exists
and is never seen.

### S11 · Regression safety net ♾
**Purpose:** the four things whose breakage the operator would notice immediately.
Confirmed 2026-07-31 (§4.12). These are checked before every ship, forever:

1. The managed **ESS instance stays reachable** — MatrixCtrl must never take it down.
2. **Saving config destroys no comments or values.**
3. **Admin login (OIDC via MAS) works** — if it breaks, recovery needs `kubectl`.
4. **SFU patches survive a Helm upgrade** — the project's reason to exist.

### S12 · Centralisation ♾
**Purpose:** stop the third parallel implementation before it is written.
Every change asks: does more than one place need this? If yes it goes into a
shared package. Re-decided per change, never "done".

### S13–S17 · Not started ⏳
Phase 2–6 scope. One line each so the IDs are reserved and citable:
**S13** users/rooms/moderation · **S14** RTC + TLS/DNS + backup/restore ·
**S15** federation + bridges · **S16** cross-component audit + worker insights ·
**S17** multi-instance + i18n (English UI). Detail in [ROADMAP.md](ROADMAP.md).

## 2b. Idea box (unprioritised, unjudged)

Things that came up and have not been evaluated. No filter — this exists so ideas
stop getting lost in chat. On evaluation they move to §2 or to [BACKLOG.md](BACKLOG.md).

- Alert when a hook run ends in `hooks-failed` (mail/Matrix message).
- Show ESS release notes / breaking changes next to the version picker.
- Read-only role for a second operator.
- Persist dashboard metrics so the sparkline survives a reload.
- Merge the System page into the dashboard — raised 2026-07-31, undecided (§4.13).

## 3. Prioritisation

By impact, not by effort:

1. **S9 (CI + verification)** — without it, the definition of done the operator
   just committed to is enforced by memory alone. Everything below is riskier
   until this exists.
2. **S6 (greenfield e2e)** — the "works for anyone" claim is the product claim,
   and it is currently unproven.
3. **S8 (release coherence)** — the README currently mis-installs the project.
4. **S10 (audit UI)** — cheap, and it makes an existing table useful.
5. **S13+ (Phase 2)** — deliberately after the above ([VISION.md](VISION.md)).

## 4. Decisions

> Numbered and dated so plans and commits can cite them. Never renumbered.
> Author "operator" = the maintainer; "agent" = decided by the AI agent and left
> standing.

### §4.1 — Never shell out to kubectl or helm (2026-05-27, operator)
**Question:** call the CLIs or use the SDKs?
**Decision:** `helm.sh/helm/v3` and `client-go` only.
**Rationale:** structured errors, no PATH dependency, no binaries in the runtime image.
**Consequences:** binds MatrixCtrl to Kubernetes (see [VISION.md](VISION.md) non-goals). Affects S2, S3, S4.

### §4.2 — Config versioning via go-git, not the git binary (2026-05-27, operator)
**Decision:** pure-Go go-git.
**Rationale:** no CGO, no git binary in the image.
**Consequences:** we implement diff ourselves — which is exactly where §4.11 later bit us. Affects S1.

### §4.3 — One YAML file per ESS section (2026-05-30, operator; commit `b77c9e7`)
**Question:** keep the ESS monolith (`values.yaml` + overrides) or split it?
**Decision:** one file per top-level section, listed in `config-slices.json`; merged into Helm values. Legacy monolith migrated by `Store.MigrateToSections`.
**Rationale:** top-level keys are disjoint, so the merge is order-independent, and the UI gets a natural navigation unit.
**Consequences:** a migrator that must stay idempotent and abort if the merged result would change. Affects S1.

### §4.4 — Comment-preserving config writes (2026-05-30, operator)
**Decision:** form edits use yaml.v3 node surgery (`yamledit.go`), never marshal-and-rewrite.
**Rationale:** the chart's `##` comments *are* the field help text; rewriting would delete the documentation the UI depends on.
**Consequences:** invariant #2 of S11. Affects S1.

### §4.5 — Admin-only OIDC via the MAS Admin API (2026-05-29, operator; commit `ad6da98`)
**Decision:** OIDC `sub` (a ULID) is checked against `/api/admin/v1/users/{sub}` using a client-credentials token.
**Rationale:** no second user database; Matrix admin status is the single source of truth.
**Consequences:** bootstrap auth must remain for greenfield — the bootstrap paradox in [SETUP.md](SETUP.md). Affects S5, S6.

### §4.6 — Hook failure never triggers a Helm rollback (2026-05-27, operator)
**Decision:** if Helm succeeds and hooks fail → status `hooks-failed`, alert, allow re-trigger.
**Rationale:** the deployment is good; rolling back a good release over a patch failure is worse than the patch failure.
**Consequences:** operators must notice `hooks-failed` — currently only visible in the UI (gap in S3).

### §4.7 — AGPL-3.0 (2026-05-30, operator; commit `cdd444b`)
**Decision:** AGPL-3.0, deliberately.
**Rationale:** the counterweight to ESS Pro — a modified network service must offer its source.
**Consequences:** rules out a proprietary hosted fork. Affects [VISION.md](VISION.md).

### §4.8 — Public repository, sanitised (2026-05-30, operator; commits `803dacc`, `5dd6551`)
**Decision:** repo is public; instance values are gitignored and excluded from chart and image; no personal names anywhere.
**Consequences:** every doc and default must assume a stranger reads it. Cluster hostnames/IPs are masked in docs — but see [BACKLOG.md](BACKLOG.md) P0-1, the git history still carries one.

### §4.9 — Dark-only design system with three switchable directions (2026-06-04, operator)
**Decision:** token-driven, dark-only; Aura/Carbon/Graphite × accent × density via `data-*` attributes; primitives in `mc.tsx`.
**Rationale:** one visual language across all screens; the light theme had no users and doubled the surface.
**Consequences:** `useTheme()` is a compatibility shim; Monaco is always `vs-dark`. Affects S7.

### §4.10 — Monaco is self-hosted and lazily loaded (2026-07-31, agent)
**Question:** `@monaco-editor/react` fetches the editor from `cdn.jsdelivr.net` at runtime.
**Decision:** bundle a minimal Monaco locally (`lib/monaco.ts`) and load it behind a `React.lazy` boundary (`components/config/YamlEditor.tsx`).
**Rationale:** a self-hosted admin tool routinely runs without outbound internet — the CDN fetch failed and every editor rendered blank. A top-level import alone is not enough: TanStack's splitter leaves route-file top-level imports in the statically linked reference file, which made every page (including login) pull 3.9 MB.
**Consequences:** all editor usage must go through `YamlEditor`. Affects S1, S7.

### §4.11 — Working-tree diff produces real unified hunks (2026-07-31, agent)
**Question:** `internal/git.Diff()` compared line *i* of old against line *i* of new and emitted no `@@` headers.
**Decision:** proper Myers diff via `sergi/go-diff` with three lines of context.
**Rationale:** any hunk-parsing consumer rendered an empty diff, and a single inserted line marked every following line as changed.
**Consequences:** `sergi/go-diff` promoted to a direct dependency. Regression tests in `internal/git/diff_test.go`. Affects S1.

### §4.12 — Etappe definition, definition of done, and the regression set (2026-07-31, operator)
**Decision:** an **etappe** is one release-worthy feature package that can be deployed. It is **done** when `go test ./...`, the TypeScript typecheck and the build are green **and** the agent has autonomously built the image, imported it into k3s, deployed it, and verified the running result — no manual step by the operator. Before every ship, the four S11 invariants are checked.
**Rationale:** phases are too coarse to finish; sessions are too fine to ship.
**Consequences:** verification must be automatable — which is why S9 is the top priority. The requested screenshot/browser check is not yet possible (no headless browser installed); tracked in [BACKLOG.md](BACKLOG.md).

### §4.13 — System page: kept for now (2026-07-31, agent, **open**)
**Question:** the enriched dashboard now covers most of what `/system` showed — remove the page?
**Decision:** kept, pending the operator's call. Only node conditions, the metric sparklines, the PVC list and per-namespace pod counts remain unique to it.
**Status:** **open (raised 2026-07-31)** — reversible either way; removal is a route deletion.
