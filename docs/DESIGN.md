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
| `internal/api/` | chi router, handlers, auth + audit middleware. HTTP only — no business logic. *(This line claimed the audit half already existed for two months before E17 actually built it — see S10.)* |
| `internal/audit/` | Writes and reads `audit_log`. Never stores request bodies (E17) |
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
| S2 Helm release & versions | ✅ (E2, E12, E14) | Version list fixed 2026-07-31; stream survives Helm's silent phase and recovers from a drop (E14); an upgrade whose process dies is reconciled to `interrupted` at startup instead of reading as running forever (P2-16) |
| S3 Post-upgrade hooks | ✅ (E2, E12, E21) | Engine + built-ins + editor; enable/disable per deployment. Since E21 the product also reports whether each hook's patch is **still in effect** — `enabled` and `applied` are different questions (§4.21) |
| S4 Cluster observability | ✅ (E3, E12, E14, E20) | Health, events, pod drill-down with restart cause; `/status` ~3.2 s → ~0.18 s warm (E14), cold 4.7 s → ~0.5 s with no staleness window (E20) |
| S5 Auth (bootstrap + OIDC) | ✅ (E6) | Admin-only via MAS Admin API, runtime switch |
| S6 Setup & onboarding | ⏳ ⅞ (E15) | Greenfield deploy proven on an empty cluster after fixing 4 defects; only connect-OIDC untested (needs public DNS). **Re-verified 2026-09-03:** all 17 migrations apply from zero, and a fresh install converges on the *same* 104-column schema as the grown one — no drift after eight migrations added since E15 |
| S7 UI shell & design system | ✅ (E11) | Tokens + `mc.tsx`; all functional screens migrated |
| S8 Packaging & release | ✅ (E16, E18) | A tag publishes image, chart **and** the GitHub Release, whose notes the workflow cuts from `CHANGELOG.md` itself (§4.17, P2-18). `0.1.16` released, deployed and verified; repo topics, description and tabs configured, homepage deliberately empty |
| S9 Verification & CI | ✅ (E13, E14, E18, E20, E49, E53) | CI on push/PR; **33 frontend tests, 350 Go test functions across 20 packages** (counted 2026-09-03, the previous "26 and 13" was long stale); headless-browser route check that now *fails* on a skipped route rather than passing (E49); gofmt gate in `make check` as well as CI (E53) |
| S10 Audit trail | ✅ (E17) | Middleware over the whole authenticated group, keyset-paginated read endpoint, UI at `/audit`. **This row previously claimed "table + middleware write" — the middleware never existed; 0 rows after two months.** Open: retention (P2-19) |
| S11 Regression safety net | ♾ Rule | Four invariants, checked before every ship — never "finished". #4 ("the SFU patches survive a Helm upgrade") is no longer only a checklist line: E21 checks it continuously and shows it on the dashboard, after it was broken by an upgrade run outside MatrixCtrl and nobody noticed for a day |
| S12 Centralisation | ♾ Rule | "More than one place?" → shared package. Re-decided per change |
| S13 User & room management | ✅ (E27, E28, E36, E41, E46, E47, E48, E65) | **Was "not started" in this table until 2026-09-03, four months after it shipped.** Users with lock/deactivate/admin/password (E27/E28) and GDPR erasure (E65); rooms with detail, members and block (E36/E41); both report queues with dispositions (E46/E48); media quarantine (E47). Deliberately absent: deleting rooms or media, bulk actions |
| S14 Day-2 operations (RTC/TLS/backup) | ⏳ ½ (E19, E44, E45, E51) | **RTC is substantially built** — ports read from the Services, reachability stated as unknown rather than guessed (E19), call history that survives the SFU restart (E44), the address-set fix (E45), the UDP buffer pre-flight (E51). **TLS is only an editable config slice**, and **backup/restore does not exist at all** — checked 2026-09-03, the only matches were a one-off config-migration backup directory |
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
✅ **Done (E14, 2026-08-01):** the live log stream used to be unreliable exactly
when it mattered — a *successful* upgrade ended in `[Verbindung getrennt]`. Now
long Helm operations report elapsed time every 30 s, the socket carries a 20 s
heartbeat, and a dropped client asks `GET …/upgrade/{id}` what actually happened
before reconnecting with backoff. Did *not* solve: release notes per version
(`ess_versions.changelog` is still unused).

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
✅ **Done (E14, 2026-08-01):** `/status` took ~1.9–3.2 s per poll. Three causes, all
fixed: the Helm release read is cached (§4.15), the reads run concurrently, and
client-go's default QPS 5 / Burst 10 throttle was raised (§4.16) — that last one was
invisible until the reads ran in parallel and was responsible for a steady ~1.1 s of
pure client-side queueing. Measured after: **~0.14–0.25 s**.

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
while the running image is `0.1.14`. Anyone following the README installs
something two months behind. There is no release checklist tying the three
version strings together, and no tags in git.

### S9 · Verification & CI ⏳
**Purpose:** prove an etappe is done without a human remembering to check.
**Today:** GitHub Actions runs `go vet`, `go test ./...`, the TypeScript
typecheck, 26 Vitest unit tests and the frontend build on every push and PR.
`web/scripts/verify-ui.mjs` drives headless chromium over the functional routes
after a deploy — **fourteen** since E49 — failing on console errors, an unmounted
root, or a route it never rendered. Run it with `make verify-ui BASE=…`; without
`MATRIXCTRL_TOKEN` it checks only `/auth/login` and exits non-zero, because until
2026-08-17 that same run exited 0 having skipped ten of eleven routes (§4.48).
✅ **Done (E13, 2026-07-31):** the definition of done is now enforced outside the
agent's memory. Did *not* solve: component/snapshot tests (deliberately omitted —
they break on every intentional design change), and the greenfield path (S6),
which needs a throwaway cluster.
Plan: [plans/etappe-13-ci-and-verification.md](plans/etappe-13-ci-and-verification.md).

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

### S13 · User, room and moderation management ✅
**Purpose:** do the day-to-day admin work without `kubectl` and without Element Admin.
**Today:** users (lock/unlock, deactivate/reactivate, admin rights, password, GDPR
erasure), rooms (list, detail, members, block), both of Synapse's report queues with
dispositions kept in MatrixCtrl, and media quarantine with read-back.
**Deliberately absent:** deleting rooms and media, bulk actions, and protect/unprotect —
every one of them either has no inverse or belongs with a permissions model that does
not exist yet. See §4.39.

*This section read "not started" until 2026-09-03, roughly four months after the first
half of it shipped.* It is the same failure the backlog keeps producing (§4.38, P2-4,
P2-19): a document describing a state the code left behind. It matters more here than
elsewhere, because CLAUDE.md rule 2 sends every new reader to this table first.

### S14–S17 · Partly or not started ⏳
**S14** Day-2 ops — RTC substantially built (E19, E44, E45, E51); TLS/DNS is an editable
config slice and nothing more; backup/restore does not exist ·
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

### §4.3 — One YAML file per ESS section (2026-05-30, operator; commit `6cc23d1`)
**Question:** keep the ESS monolith (`values.yaml` + overrides) or split it?
**Decision:** one file per top-level section, listed in `config-slices.json`; merged into Helm values. Legacy monolith migrated by `Store.MigrateToSections`.
**Rationale:** top-level keys are disjoint, so the merge is order-independent, and the UI gets a natural navigation unit.
**Consequences:** a migrator that must stay idempotent and abort if the merged result would change. Affects S1.

### §4.4 — Comment-preserving config writes (2026-05-30, operator)
**Decision:** form edits use yaml.v3 node surgery (`yamledit.go`), never marshal-and-rewrite.
**Rationale:** the chart's `##` comments *are* the field help text; rewriting would delete the documentation the UI depends on.
**Consequences:** invariant #2 of S11. Affects S1.

### §4.5 — Admin-only OIDC via the MAS Admin API (2026-05-29, operator; commit `dc7b0c7`)
**Decision:** OIDC `sub` (a ULID) is checked against `/api/admin/v1/users/{sub}` using a client-credentials token.
**Rationale:** no second user database; Matrix admin status is the single source of truth.
**Consequences:** bootstrap auth must remain for greenfield — the bootstrap paradox in [SETUP.md](SETUP.md). Affects S5, S6.

### §4.6 — Hook failure never triggers a Helm rollback (2026-05-27, operator)
**Decision:** if Helm succeeds and hooks fail → status `hooks-failed`, alert, allow re-trigger.
**Rationale:** the deployment is good; rolling back a good release over a patch failure is worse than the patch failure.
**Consequences:** operators must notice `hooks-failed` — currently only visible in the UI (gap in S3).

### §4.7 — AGPL-3.0 (2026-05-30, operator; commit `6476a8b`)
**Decision:** AGPL-3.0, deliberately.
**Rationale:** the counterweight to ESS Pro — a modified network service must offer its source.
**Consequences:** rules out a proprietary hosted fork. Affects [VISION.md](VISION.md).

### §4.8 — Public repository, sanitised (2026-05-30, operator; commits `284cb03`, `d8dcabf`)
**Decision:** repo is public; instance values are gitignored and excluded from chart and image; no personal names anywhere.
**Consequences:** every doc and default must assume a stranger reads it. Cluster hostnames/IPs are masked in docs, and since 2026-08-01 in the git history too (§4.14).

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

### §4.14 — The git history was rewritten to remove the cluster hostname (2026-08-01, operator)
**Question:** 39 commits carried `root@<k3s-node>` as their author, and 30 of them
carried `<k3s-node>` plus the node's private IP inside `CLAUDE.md`. The docs had
been sanitised (§4.8); the history had not.
**Decision:** rewrite all 51 commits with `git filter-repo` (`--mailmap` for the
identity, `--replace-text` for the two literals) and force-push.
**Rationale:** sanitising the working tree while the same value sits one `git show`
away is not sanitising. No credential was ever in the history (verified by scanning
every blob), but a structured hostname plus an internal address discloses topology:
the naming convention, the implied sibling hosts, and the subnet in use. It was
public for nine weeks, and §4.8 says a stranger reads this repo.
**Verification:** the rewrite is content-neutral by construction — the HEAD tree
hash is **unchanged** (`490e2573`), all 51 subjects and author dates are identical,
and only the author identity and the two literals differ. Old tip `2ff2370` →
new tip `2ff2370`. CI is green on the rewritten tip.
**Consequences:** every commit hash changed, so existing clones must re-clone —
there were no forks and no open PRs, so no other repository was affected. **The old
objects remain reachable by SHA on GitHub** until GitHub garbage-collects them; only
GitHub Support can force that, and no action retracts copies made during the nine
weeks the values were public. Only renaming the host and changing the address
invalidates the disclosed values themselves — see [BACKLOG.md](BACKLOG.md) P0-1b for
the full assessment. Backup of the pre-rewrite history is a verified `git bundle`
held outside the repo.

### §4.17 — The tag is the release (2026-08-01, agent)
**Question:** publishing was a block of commands in `CONTRIBUTING.md`. Images
`0.1.10`–`0.1.14` were built locally, imported into k3s and never pushed, so GHCR
sat at `0.1.9` for two months while the README told strangers to install `latest`.
**Decision:** pushing a `v*` tag publishes the release. Image and chart go out
together from `.github/workflows/release.yml`, and the run **fails** if the tag and
`Chart.yaml` disagree.
**Rationale:** no individual step of the old process was wrong — it depended on a
human remembering it under pressure, which is the failure mode that guarantees
recurrence. A better checklist would have been the same bug with more words.
**Decision detail:** chart `version` == `appVersion` == image tag; one artefact, one
number. Released charts pin `image.tag` to their own version so
`helm install --version X` is reproducible, while the committed default stays
`latest` for development.
**Consequences:** releasing needs a green `master` first — the release workflow does
not re-run tests. New GHCR packages default to private, which is a first-publish
trap documented in [RELEASING.md](RELEASING.md). Affects S8.

### §4.19 — The needle list cannot live in the haystack (2026-08-01, operator + agent)
**Question:** §4.14 rewrote 39 commits to remove the cluster hostname. Ten weeks
later the *plan document explaining that rule* contained the hostname and was
pushed (P0-1c). Every control in place pointed at screenshots. What actually stops
this recurring?
**Decision:** an automated string check at two points — `pre-commit` via
`core.hooksPath .githooks`, and CI — whose patterns come from a **gitignored file
or a repository secret**, never from a tracked file.
**Rationale:** the obvious implementation is a committed list of forbidden
strings, which publishes exactly what it is meant to hide. The pre-commit hook
matters more than the CI step: CI catches a leak that is already public, the hook
catches it while the fix is still free. P0-1c was live for 40 minutes.
**Decision detail:** the check prints **file names only, never the matched value**
— echoing it would write the string into a CI log. With no pattern source it exits
0 rather than failing, so a fork or an outside PR is not blocked by a check it
cannot satisfy; a check people cannot pass is a check people delete.
**Consequences:** the pattern list is per-clone and can silently drift or go
missing — the skip-when-absent behaviour that makes forks work also means a
mis-set local file fails open. Accepted: this is a backstop for attention, not a
boundary. Also, this whole class of fix cannot recall what was published — the
rewrite made the objects unreachable, but `refs/pull/*` on GitHub is permanent.
Affects S9.

### §4.18 — Screenshots are generated with redaction, not taken by hand (2026-08-01, agent)
**Question:** the README had no screenshots, for a product that is entirely a user
interface. The only instance with real data is production, and its Dashboard and
System pages show the node name — the exact string §4.14 rewrote 39 commits to
remove. How do you publish screenshots of a private cluster?
**Decision:** `verify-ui.mjs` — which already walks every route and screenshots it —
gained `--redact from=to`, which rewrites text nodes immediately before each shot
and reports how many it changed. `docs/img/` is produced by that command.
**Rationale:** the alternatives were worse. Blurring looks doctored and is a manual
step repeated from memory every time; a second demo cluster costs an ESS deploy per
screenshot refresh; renaming the production node was already refused (§4.14's
outcome — all five PVs are node-pinned with `reclaimPolicy: Delete`). Putting the
capability in the tool means the *next* person cannot forget it, which is the only
form of a security rule that survives (S12).
**Decision detail:** the replacement count is printed per route and a run where no
rule matched prints a warning — a redaction that silently matched nothing is more
dangerous than none, because it produces false confidence. The flag is a backstop,
not the control: every image is looked at before it is committed.
**Consequences:** screenshots go stale as the UI changes, and nothing detects that.
Accepted — deliberately no golden-image comparison, for the same reason
`verify-ui.mjs` never had one: it fails on every intentional design change and gets
disabled within two etappes. Affects S9.

**Amended 2026-08-02 (E20).** The claim above that "the *next* person cannot forget
it" was false, and it failed on the very next release. `--redact` removes the
strings someone passes; the 0.1.20 run passed the node name and the admin hostname,
and `/rtc` — a page added *after* this record was written — renders a different
hostname next to a resolved **public IP**. Nothing matched, the replacement counter
read 0, the route was reported PASS, and the screenshot carried the operator's IP
address. `check-sensitive.sh` skips binaries with a comment stating that screenshots
are covered here instead; they were not. Two safeguards, each pointing at the other.

So redaction is no longer the guarantee — a **category scan** is. After every
redaction pass the rendered text is checked against `.sensitive-patterns` (the same
untracked source `check-sensitive.sh` uses) plus a built-in IPv4 rule that needs no
secret and therefore also protects forks. A route that would leak fails and **no
image is written**. `--redact-ips` replaces address literals whose value cannot be
known in advance, because the page shows what the *pod* resolved.

Two things this cost, both kept in the code as comments: the RFC 5737 placeholder
the tool writes is itself a valid IPv4 address, so the scan flagged its own output
and the retry loop replaced a placeholder with a placeholder four times; and the
report header had been printing the production hostname on every run since the day
it was written. The rule that generalises: **a safeguard aimed at an artefact type
protects that artefact type only, and "the tool makes it impossible to forget" is a
claim to re-test, not to assert.**

### §4.15 — Release info is cached, because there is no cheap way to read it (2026-08-01, agent)
> **Superseded by §4.20 (2026-08-02).** The measurements below are correct and the
> conclusion drawn from them is not. Kept unedited, because *how* a well-measured
> decision came out wrong is the useful part.

**Question:** `/status` was dominated by `GetRelease`. Is there a lighter Helm call?
**Decision:** no — cache it. `action.NewGet`, `NewGetMetadata` and `NewList` were all
measured at ~3.9–5.6 s against the live cluster, because every one of them fetches
the release secret (416 KB for ESS) and decompresses the whole release: manifest,
hooks and every chart file. `GetRelease` keeps seven scalars out of that, every 15 s.
**Decision detail:** the cache lives in `internal/helm`, not in the handler — nine
call sites use `GetRelease` (§S12). 60 s TTL, plus explicit `InvalidateRelease` on
upgrade, rollback and install, called via `defer` so a *failed* operation that still
moved the release cannot leave a stale entry.
**Consequences:** a release changed outside MatrixCtrl (operator running `helm`
directly) can read stale for up to 60 s. That is the deliberate trade and it is
written down rather than discovered later. Affects S2, S4.

### §4.16 — client-go's default rate limit is wrong for a server (2026-08-01, agent)
**Question:** after caching Helm and parallelising the reads, `/status` still cost
~1.9 s, and the pattern was suspicious: the first few calls were fast, then every
call settled at a steady ~1.1 s while the cluster itself was idle.
**Decision:** raise the client-go rate limiter to QPS 50 / Burst 100 in
`internal/k8s.New`.
**Rationale:** client-go defaults to QPS 5 / Burst 10, which is sized for a one-shot
CLI. MatrixCtrl polls continuously and — once the status reads ran concurrently —
in bursts, so it spent the burst immediately and then queued on its *own* limiter.
The latency was self-inflicted and invisible from the cluster side.
**Consequences:** more load is possible against the API server; for one admin server
against one cluster this is modest. The lesson generalises: any client-go default
tuned for CLIs should be re-examined before it runs in a server. Affects S4.

### §4.20 — The release read was never expensive; the question was (2026-08-02, agent)
**Question:** a cold `/status` still cost ~4.7 s. P2-21 proposed keeping the cache
warm or serving stale-while-revalidate. Both work *around* the 4.3 s Helm read —
before building either, is the 4.3 s actually necessary?

**Decision:** no. Read the release secret's **labels** instead of the release.
`{"modifiedAt":…,"name":"ess","owner":"helm","status":"deployed","version":"22"}` —
a metadata-only list (`PartialObjectMetadataList`) returns those for every revision
in ~15 ms, transferring no release payload at all. Decode exactly one revision, and
only when the identity `(revision, status, modifiedAt)` differs from what is already
memoised. Decoding stays Helm's job via `storage.Storage.Get`, so the secret format
never becomes our problem.

**Rationale — why §4.15 got it wrong with correct numbers:** `action.NewGet`,
`NewGetMetadata` and `NewList` were each measured at ~4 s and the cost was concluded
to be inherent. All three are slow *for the same reason*: they ask the storage layer
for `Last()`, which fetches and decodes **every** revision to sort them. The
production release is 11 secrets holding 2.93 MB, and `GetRelease` keeps seven
scalars. Three measurements of three functions that share one bottleneck read as
corroboration, and they were really one measurement repeated. **Measuring the
alternatives you thought of is not the same as measuring the problem.**

**Decision detail:** `storage.Deployed()` would have been a one-line change at
~480 ms and was rejected — it returns the last *successful* revision, so a failed
upgrade would leave the dashboard showing the release before it. The probe takes
the highest revision, matching `Last()`. A test pins that specific behaviour.

**Consequences:**
- cold **4.32 s → 505 ms**; warm **2.5 µs → 14 ms**.
- The warm path got slower on purpose. The old 2.5 µs came with up to 60 s of
  undetectable staleness (the trade §4.15 wrote down); the new 14 ms is a probe
  that *confirms* the value is current. There is now no staleness window at all —
  a stronger correctness property than before, of which the speedup is a side
  effect.
- Every failure mode of the fast path (no metadata client, probe error, decode
  error) falls back to `action.NewGet`. The worst case is the old latency, never a
  wrong answer. A live test (`RUN_LIVE=1`) asserts both paths return identical
  `ReleaseInfo`.
- `InvalidateRelease` is no longer required for correctness and is kept as a
  deliberate belt-and-braces reset, which the code says out loud.
- **Not fixed:** `ListHistory` (`/helm/history`) has the same shape of problem and
  genuinely needs every revision's chart version, so the same trick does not apply
  unchanged. Left as P2-22 rather than guessed at here. Affects S2, S4.

### §4.21 — "Enabled" and "applied" are different questions (2026-08-03, agent)
**Question:** on 2026-08-02 a Helm upgrade was run outside MatrixCtrl. The chart
re-rendered the SFU deployment without `hostNetwork: true` and three Services fell
back to `externalTrafficPolicy: Cluster`. The SFU stopped binding its host ports and
calling broke. Every screen stayed green — pods healthy, release deployed, hooks
listed as **enabled**. How does the product notice?

**Decision:** `internal/drift` fetches each live object, applies the hook's patch to
it **in memory**, and asks whether anything would change. A patch that changes
nothing has already taken effect.

**Rationale:** no new specification was needed, because the hooks already are one —
each `kubectl_patch` action carries `resource`, `name`, `namespace` and the patch
body. So the check is exact rather than heuristic, and it keeps working for hooks
nobody has written yet: nothing in the package names the SFU or any field. The
alternative on the table was a curated list of fields to watch, which would only
ever find what someone already thought of — the same shape of mistake as §4.18.

**Decision detail:** `unknown` is a first-class result. A failed read, an absent
resource (greenfield has no SFU) or an undecodable patch never render as
`satisfied`. Strategic merge is **refused** rather than approximated: it needs the
typed schema to handle lists, and guessing would flag fields that are fine — a
report that cries wolf is ignored within two weeks, taking the exact part with it.
The check is read-only: re-applying automatically would hide that something reset
it, and that is the information the operator actually needs.

**Consequences:** S11 check #4 ("the SFU patches survive a Helm upgrade") stops
being a sentence in a document and becomes a number on the dashboard. What it does
*not* cover is manual edits no hook knows about — the `ingressClassName: disabled`
that Helm preserved for 69 days needs manifest-versus-live diffing, which is the
open half of P1-11. Affects S2, S11.

### §4.22 — A status page must say which question it is answering (2026-08-04, operator + agent)
**Question:** on 2026-08-02 the entire MatrixRTC path was repaired and verified end
to end from the internet, and calling still failed. The calls being made were
**legacy Matrix 1:1** — plain peer-to-peer WebRTC that never touches the SFU and
needs a TURN relay from Synapse's own config. `/rtc` was a full page of green about
a component those calls never used. How does a status page avoid being confidently
right about the wrong thing?

**Decision:** the page states **which call paths the deployment supports** before it
reports on any of them, and `internal/rtc/callpath.go` reads `turn_uris` out of the
live Synapse ConfigMap to answer for the second path.

**Rationale:** reading the *live* ConfigMap rather than the chart values follows
§4.21 and P1-11 directly — intent and live state diverge, and the live state is the
one answering calls. The config is a merged directory, so files are sorted and the
last definition wins; parsed as YAML rather than string-matched, so a commented-out
`# turn_uris:` does not read as configured and `turn_uris: []` reads as what it is:
present, empty, and exactly as relayless as absent.

**Decision detail:** this warns where E23 deliberately would not. A quiet SFU is an
*absence of evidence* and reports unknown; a missing relay is a *measured property*
of the deployment that stays true until someone changes it, and it is actionable —
so it warns, and the action names all three steps rather than pointing at a manual.
The finding must also name LiveKit's own TURN (`matrixRTC.sfu.exposedServices.turn`,
on by default) and say that it serves Element Call only, because it authenticates
with LiveKit tokens rather than the REST scheme Synapse uses. Without that sentence
an operator reads "no TURN", finds `turn.enabled: true` in the values, and concludes
the panel is broken.

**Consequences:** the ESS chart has no option for a Synapse-side relay at all, so
this finding is permanently true for every ESS install until one is run alongside.
That makes it the strongest argument yet for MatrixCtrl managing a TURN deployment
itself (P1-14) — the product can now name the gap, and naming a gap it cannot close
is only half a feature. Affects S14.

### §4.23 — Ask the cluster who owns the field, do not diff for it (2026-08-04, agent)
**Question:** §4.21 checks every patch a hook *declares*. It cannot see an edit no
hook knows about — the case P1-11 was actually opened for, where an Ingress carried
`ingressClassName: disabled` applied by hand 69 days earlier and Helm's three-way
merge preserved it through every upgrade. The same class of leftover was found
again on 2026-08-04, and again by hand. How does the product see it?

**Decision:** read `metadata.managedFields`. The API server records one entry per
manager naming exactly which fields that manager set, so the question becomes "who
owns this field" — which the cluster answers — instead of "what differs from the
chart", which needs interpretation.

**Rationale:** the plan recorded in P1-11 was to render the release manifests and
diff them against live objects. That is the obvious approach and it is bad: a live
object carries hundreds of fields no manifest mentions — defaults, `clusterIP`,
`resourceVersion`, status — so a naive diff reports all of them and needs a curated
list of fields to watch. A curated list only ever finds what someone already
thought of, which is the failure §4.18 and §4.21 both already warned about. Field
ownership needs no list, no rendering, and no schema.

**Decision detail:** only `kubectl-*` is reported as human, because it is the one
manager name that unambiguously means somebody ran a command; unknown tools are
surfaced a level quieter as *foreign* rather than accused. Hook coverage splits the
report in two: a hand-edit on a field a hook maintains means someone bypassed the
product, while one on a field nothing maintains means nothing will ever restore it,
and only the second is loud. Calibration against the live cluster removed two
sources of noise that were three of eight findings — `kubectl rollout restart`'s
`restartedAt` stamp, which records an event rather than a configuration exception,
and ESS's own `matrix-tools`.

**Consequences:** on production the report is exactly one loud line —
`ingress/ess-matrix-rtc: spec.ingressClassName`, human-owned, hook-less — which is
the same object P1-11 was opened about. Four further hand-edits are shown quietly
because hooks own them; they are the agent's own manual restorations of 2026-08-02,
which is itself the record of having gone around the product. Affects S2, S11.

### §4.24 — "Cannot be checked from here" was true and still too small (2026-08-04, agent)
**Question:** §4.18 and E19 established that inbound reachability cannot be tested
from inside the network it terminates in, and wrote it into `/rtc` as a permanent
`unknown`. Three days of measuring from the inside followed — ICE statistics,
packet counters, conntrack — and none of it answered the question. Then one request
to a public port checker answered it in seconds: nothing inbound reached the node at
all, which explained every measurement taken since 2026-08-02.

**Decision:** `internal/reach` performs the check from outside, on an explicit
click, and reports open/closed per TCP port.

**Rationale:** the original statement was accurate and its implication was not.
"MatrixCtrl cannot test this" quietly became "this cannot be tested", and the
product stopped looking. The vantage point outside the network was always one HTTP
request away.

**Decision detail:** a **control** decides whether any result can be believed — a
port known to be open on an unrelated public host. A checker that is blocked,
rate-limited or broken reports everything as closed, and an operator who acts on
that reconfigures a router that was already correct, then builds the next three
attempts on top of that mistake. If the control does not come back open, every
result is `unknown` and the action explicitly says to change nothing.
The address tested is the node's **own egress address**, not the announced RTC
hostname: where that resolves to a proxy or a tunnel — as it does on the cluster
this was built for — testing the hostname tests the proxy.
Free checkers speak TCP while the port that matters most is UDP, so the untested
UDP count is carried into the result rather than dropped. A closed TCP port is
still decisive: a router that forwards nothing forwards neither.

**Consequences:** this is the only code in MatrixCtrl that leaves the cluster, so it
is `POST`, never runs on a page load or a timer, names both third-party hosts in the
UI before the click, and stores nothing — no consent flag to forget about. A status
page that silently phones home is one nobody should run. Affects S14.

### §4.25 — One way to talk to MAS, and it reports MAS (2026-08-04, agent)
**Question:** Phase 2 begins with user management, and MAS admin access already
existed — inside `internal/auth/oidc.go`, minting a `client_credentials` token per
call to answer one question at login. Does user management get its own?

**Decision:** no. `internal/mas` holds the client; the login path uses it too. The
token is cached with its `expires_in`, and a `401` drops the cache and retries once.

**Rationale:** CLAUDE.md rule 3, applied where it actually bites. Two clients would
not fail loudly — they would drift the day someone changes the scope, the issuer
trimming or the 404 semantics in one copy, and the symptom would be an admin who
can log in but sees no users, or the reverse. The per-call token mint was also
correct for one check per login and wrong for a paged list, where it doubles the
request count for every page.

**Decision detail:** the API shape was read from the **live** OpenAPI spec at
`/api/spec.json`, not from documentation. That is where the design constraint came
from: MAS pages by **cursor**, not offset, so there is no page number to render and
a UI with page numbers would be a lie told in the client. Cursors are extracted from
the returned links rather than replayed whole — replaying a link would let the
caller ask MAS for whatever that link happened to contain.

**Decision detail:** `locked_at` and `deactivated_at` are separate timestamps and
stay separate all the way to the UI. Locked is reversible and usually temporary;
deactivated is the account being gone. Collapsing both into "disabled" would throw
away exactly what an operator needs in order to decide what to do.

**Consequences:** the page reports **MAS accounts** and says so. Synapse keeps its
own user table and the two can disagree on a migrated stack; reconciling them is
real work and gets its own etappe rather than a footnote. Writes — lock, deactivate,
set-admin, set-password — are deliberately not in this etappe: each is destructive in
a different way and needs confirmation plus audit entries, and shipping the dangerous
half alongside the subsystem that introduces it is how the dangerous half goes
untested. Affects S13.

### §4.26 — The confirmation must carry the consequence (2026-08-04, operator + agent)
**Question:** E27 shipped the user list and deliberately left the writes out. Lock,
unlock, deactivate, reactivate, set-admin and set-password are one POST each — what
is there to decide?

**Decision:** every dialog states what the action actually does, taken from MAS's own
API description, and three actions are refused against the acting user's own account.

**Rationale:** each of these verbs does something narrower than it sounds, and in the
direction that matters:

- **lock** does **not** invalidate existing sessions. Locking a compromised account
  does not eject the attacker — the classic reason for pressing that button.
- **unlock** does not reactivate; **reactivate** does not unlock.
- **set-admin** leaves existing sessions with the access they already have.

A dialog asking "are you sure?" asks the operator to confirm they pressed the button
they pressed. It does not tell them the incident they believe they just handled is
still running. So the consequence *is* the dialog's content, and the generic question
is not asked at all.

**Decision detail — erasure:** MAS's `deactivate` defaults to `skip_erase: false`,
i.e. it asks the homeserver to GDPR-erase the account. MatrixCtrl always sends
`skip_erase: true` and says so in the dialog. A one-click irreversible erasure is the
wrong default for a panel, and it sits oddly beside a `reactivate` that cannot bring
the data back. Erasure gets its own decision or it does not ship (P2-25).

**Decision detail — self-lockout:** MatrixCtrl admits only MAS admins, so locking
yourself, deactivating yourself or revoking your own admin closes the door you would
need in order to reopen it. Those three are refused. The complication is that the
session stores whatever the OIDC exchange returned — `matrix_user_id` when userinfo
provides it, the ULID `sub` otherwise — so a direct comparison would protect in one
deployment and silently not in the next. The acting identity is resolved through MAS
in whichever form it arrives, and **if resolution fails the action is refused**: "I
could not tell whether this is you" is not permission. A rail that gives way when
unsure is worse than none, because it is trusted.

**Decision detail — audit:** the audit middleware records no request body by design,
so a password can never reach the audit table. That also means the path is the only
place a meaning can live, which is why the endpoints are verb-in-path —
`/grant-admin` and `/revoke-admin` rather than one `/set-admin` taking a boolean. An
audit line reading "set-admin" without saying which way cannot answer the question
the audit trail exists for. Affects S10, S13.

### §4.27 — Answering a security review, and what a review is worth (2026-08-04, operator + agent)
**Question:** an external static review arrived with seven findings. What is the
right way to consume one?

**Decision:** every finding was re-verified against the code before being written
into the backlog, and the ones that could be checked against the running system were.
One turned out to be **worse** than reported, and one live measurement decided a
priority. What the review found *clean* was recorded too.

**Rationale:** a review is a set of claims, and claims are cheap. Two of the seven
needed a measurement, not a reading:

- The JWT-in-URL finding is severe only if the URL is retained somewhere. chi's
  request logger is enabled and writes the full URL — **400 of the last 400 log
  lines** carry one. So the token is written to the application log by the very
  request that delivers it, which moved this from "bad practice" to "leaking now".
- The weak-fallback-key finding named `NewBootstrap`, where the fallback is
  ephemeral. `randomKey()` is also called from `getOrCreateJWTSecret`, whose result
  is **persisted** as the instance's signing key. A failed `crypto/rand` at first
  boot therefore writes `matrixctrl-fallback-<unix-nanos>` into the database forever,
  derivable from the pod start time that Kubernetes publishes.

**Decision detail — the token:** a one-time code carried in the URL **fragment**.
Both properties are load-bearing: the fragment is never sent to a server, so there is
nothing to log and no Referer leak; and the code is single-use with a one-minute life,
so the copy in browser history is spent. Redemption is `DELETE … RETURNING`, the same
atomic consume the OIDC state already uses. `?token=` now survives only on a genuine
WebSocket upgrade, tested from the request's own headers rather than a path list — a
path list has to be kept in step with the router, and the day it is not, the fallback
quietly returns for a route that never wanted it.

**Decision detail — the non-root switch nearly broke config saving.** The config repo
on the PVC was written by earlier root-owned versions: `-rw-r--r-- root root`, which
UID 65532 can read and cannot rewrite. Caught by looking before deploying, not after.
`fsGroup` is the usual answer and is unavailable here, because it would also apply to
the Postgres sidecar's volume and Postgres refuses to start when its data directory
is group-accessible. So a one-shot `chown` initContainer, which terminates and
therefore does not deadlock against the sidecar.

**Decision detail — the throttle's own bug.** The backoff shifted `1 << (failures-5)`
without a bound. Past ~62 failures the multiplication overflows and the delay wraps
to **zero**, so the attacker who had failed the most would wait the least — and it is
reachable, because the counter keeps growing after a lockout window expires. Found by
a test written for the shape of the curve, not by review.

**Consequences:** P0-4 — the ClusterRole being cluster-admin in all but name — is
deliberately **not** in this etappe. It is the review's top finding and the one most
able to break the product: an enumeration that misses a resource type the ESS chart
creates fails the next upgrade, at the moment someone is trying to fix something
else. It needs a real upgrade against a live release to prove, which is its own
etappe. Affects S1, S8.

### §4.28 — Registration has to be reconcilable, not one-shot (2026-08-05, operator + agent)
**Question:** the operator logged in and MAS asked *"Continue to
01KSPV9ZMR7NB4B2BBWMPYSD1P?"* — a ULID where the application name belongs, at the
moment someone decides whether to trust this thing with their homeserver. The
generator already emits `client_name`. Why is it missing?

**Decision:** `ConnectOIDC` stops answering `409 Conflict` when a client already
exists and **reconciles** instead: fields the current generator writes and the
stored fragment lacks are added, everything else is left alone. `/setup/status`
reports what is missing, so the page can offer the repair rather than expecting the
operator to know one is needed.

**Rationale:** the missing field was the symptom; the defect is that registration was
one-shot. Any field the generator learns to write later can only ever reach fresh
installs, and every existing one is stranded with no path through the product —
which leaves hand-editing YAML, the activity this product exists to remove. That is a
shape of bug, not a one-off: it will recur for the next field.

**Decision detail:** the reconcile never regenerates the client ID or secret.
Re-running "connect" on a working instance must not invalidate the credential that
instance is authenticating with, which would log the operator out of the panel they
ran the repair from. It also never overwrites a value that is present — an operator
who chose their own display name keeps it. A fragment that cannot be parsed is
**refused rather than replaced**: it may have been hand-edited, and overwriting it
would destroy that edit while reporting success.

**Decision detail:** the check and the fix are the same function, run as a dry run
for the check. Two implementations of "is this complete?" would eventually disagree,
and the disagreement would show up as a button that does nothing or one that never
appears.

**Two things this corrected:**

- A comment in `helm_setup.go` claimed `client_name` "is not in MAS's documented
  field list" and might not render. MAS 1.15's own published config schema lists
  `ClientConfig.client_name` beside `client_id` and `redirect_uris`. The comment
  hedged about something checkable and was wrong; hedging ages into folklore.
- A note in this project's memory said the display name had been set directly in the
  MAS database and that the static-client sync would leave it alone. The live row
  read `client_name = NULL, is_static = t`, and `mas-cli` has a `config sync`
  subcommand that rewrites static clients from the config file. The database edit did
  not survive. Config is the only durable place for it. Affects S6.

### §4.29 — A progress bar that never looked at the cluster (2026-08-05, operator + agent)
**Question:** an upgrade to ESS 26.8.0 printed `Waiting for Helm rollout… (30s
elapsed)` fifteen times and then failed. The operator asked *"kein Balken oder was
passiert da genau?"* — what **is** happening?

**Decision:** the progress ticker gets an optional probe. On each tick it lists the
namespace's pods, separates the ones that are failing from the ones that are merely
starting, and prints the failing container's own error text.

**Rationale:** the honest answer to the question was that `startProgress` is a
**clock**. It could not say what it was waiting for because it never asked. One pod
sat in `Init:CrashLoopBackOff` the entire time with the explanation in its logs:
`password authentication failed for user "…"`. Every byte of that was available to
the process printing the elapsed time.

**Decision detail:** the probe is strictly additive and cannot degrade an upgrade.
It runs on the ticker's goroutine with a five-second timeout, reads at most three
container logs per tick, and every failure path returns no information rather than
an error. A diagnostic that can abort a deploy is a worse defect than the one it
reports. It also stays quiet when nothing is wrong: pods that are merely young are
counted, not narrated, and an unchanged diagnosis is not repeated — otherwise the one
useful line becomes the same wallpaper the elapsed counter already was. An unknown
waiting reason counts as *starting*, so a new Kubernetes state does not turn every
rollout into an alarm.

**The root cause underneath, which is the second half:** the credentials were
correct — verified against the live database from a pod. Chart 26.8.0 renders the MAS
config with `database.password_file`, which needs MAS ≥ 1.22, and the config **pinned**
`matrixAuthenticationService.image.tag: "1.15.0"`. MAS 1.15 received a field it does
not know, ignored it, connected with no password, and Postgres refused it.

The pin was not a one-off. Measured against chart 26.8.0, the config was behind on
four components — MAS 1.15.0 vs 1.22.0, Synapse v1.151 vs v1.158, Element Web
v1.12.14 vs v1.12.25, Element Admin 0.1.11 vs 0.1.12. The config migration froze
every image tag at the moment it ran, so each chart upgrade since had been upgrading
templates while keeping old images: **partially inert, and nobody was told.** 26.8.0
was simply the first version where the mismatch turned fatal instead of stale.

**Decision detail:** `internal/imagepin` reports this before the rollout starts, and
**does not fix it**. Unpinning is an upgrade decision with consequences — here a
seven-minor-version MAS jump carrying database migrations — and CLAUDE.md rule 6 puts
that with the operator. It reports only tags that are *older*: one ahead of the chart
is a deliberate choice, and anything not confidently orderable (a digest, a branch, a
date) is left alone. A wrong "you are behind" costs an upgrade nobody needed, and the
check has to be believed to be worth having. Affects S2, S4.

### §4.30 — The changelog is the other half of the pin warning (2026-08-05, operator + agent)
**Question:** the operator asked for release notes on the update tab, and for the
version to carry from the list into the upgrade dialog. Both read as convenience.
Are they?

**Decision:** build both, and place the notes on the **upgrade page** rather than the
list — beside the button that starts the upgrade.

**Rationale:** the second ask is convenience. The first is not. ESS 26.8.0's notes
say, in their own body, *"Upgrade Element Web to v1.12.25"* and *"Upgrade Synapse to
v1.158.0"* — precisely the two upgrades the operator's pinned image tags were
silently preventing (§4.29). One screen now carries both halves: the notes say what
the version brings, and the pin warning says what a pin will stop it bringing. Either
alone is half a sentence.

**Decision detail — no opt-in, unlike §4.24.** The reachability check needed consent
because it discloses the deployment's public address to a third party. This is a
public GET for a public version's notes and carries no address, hostname or identity;
`ListVersions` already fetches from ghcr.io on the same page, so outbound traffic here
is established rather than new. What it does need is to fail quietly: an air-gapped
install must read "could not be fetched", which is a different conclusion from "this
version has no notes", and the two are kept apart.

**Decision detail — cached per version, and bounded.** Published notes do not change,
and GitHub's unauthenticated limit is 60 requests an hour: a page refetching on every
render would exhaust it and then show nothing at all. Failures are cached too, or a
polling page would hammer a rate-limited API to stay equally empty.

**Decision detail — a markdown subset, not a dependency.** Headings, list items,
links and inline code, in about forty lines. Release notes have a fixed shape, and a
markdown library for four constructs is a lot of bundle for a little text — the same
reasoning that keeps Monaco behind a lazy boundary (§4.10). Links render only for
`http(s)`: a `javascript:` URL in third-party text must not become clickable in an
admin panel, and anything unrecognised renders as plain text rather than as markup.

**Consequences:** the version string reaches the backend as a URL path segment, so it
is validated against a strict pattern and **refused** rather than escaped — refusing
is simpler to be certain of than escaping. Affects S4.

### §4.31 — A dependency that is slow to start must not lock the operator out (2026-08-06, operator + agent)

**Context:** the container was OOMKilled at 21:43 and restarted before MAS was
serving. Discovery answered with a proxy error page instead of JSON — the `'o'` in
`invalid character 'o' in literal null` is the second character of `no healthy
upstream`. `NewOIDCService` failed once, `oidcSvc` stayed `nil` for the life of the
process, and the panel presented a username/password box for eleven hours while MAS
was healthy seconds after the failure. The operator wrote *"idk jetzt ist login mit
username password statt mit mas"* and could only get back in because someone else had
cluster access.

**Decision:** a failed OIDC init retries in the background with capped backoff, and
never gives up on its own. Giving up after N attempts restores the same lockout on a
delay, and an issuer down for an hour differs from one down for a minute mainly in
how likely the operator is to be asleep for it.

**The trap, which is the real content of this section:** `AuthHandler.ReloadOIDC`
already loads config, builds the service and swaps it under a lock, and is documented
"safe to call repeatedly". Retrying by calling it in a loop is the obvious move — and
on this deployment it would have done nothing at all, silently. `ReloadOIDC` reads the
**DB**; startup prefers **env** ("env always wins"), and this instance is
env-configured with no OIDC row in the DB. `LoadOIDCConfig` would return `ok == false`,
`ReloadOIDC` would return `nil` — success — and OIDC would stay off forever while the
log claimed a recovery was under way. A silent no-op is worse than the bug it
replaces, because it removes the symptom that would have prompted a second look. The
retry therefore rebuilds from the **effective startup config**, and `ReloadOIDC` keeps
its DB-first behaviour, because applying newly persisted config is the setup flow's
entire job.

**Also decided:** the setup flow beats an in-flight retry — a person acting
deliberately outranks a background loop. And `/auth/oidc/available` now reports
`retrying` alongside `enabled`, because "this install uses local login" and "Matrix
login exists but its issuer is unreachable" look identical on screen and lead to
opposite actions: wait, versus go find your password. It carries no error detail; the
endpoint is unauthenticated by necessity, so the reason stays in the log.

**Consequences:** a transient IdP failure no longer re-opens the local password login
on a public URL for an indefinite period — the window now closes by itself. The login
page polls while a retry runs and switches over without a reload. Affects S3, S4.

### §4.32 — Being first in the kill order (2026-08-06, agent, from measuring §4.31's cause)

**Context:** §4.31 shipped with a stated next step — *"something spikes; measure before
raising the limit"* — and named the Helm render as the likely culprit. That guess was
wrong, and the measurement is worth more than the guess was.

```
Out of memory: Killed process (matrixctrl) anon-rss:14084kB oom_score_adj:997
oom-kill:constraint=CONSTRAINT_NONE, global_oom
```

**14 MB resident against a 512Mi limit.** `CONSTRAINT_NONE` with `global_oom` is a
node-wide exhaustion, not a container hitting its cgroup ceiling. MatrixCtrl has no
memory problem, and raising its limit would have been the careful repair of something
that was never broken. The process that exhausted the node appears three lines later:
`node`, 18.2 GB, in a user session scope, right after `claude invoked oom-killer` —
the agent's own tooling, outside the product entirely.

**What the log does prove:** the kill order is derived, not arbitrary. kubelet computes
`oom_score_adj = 1000 − 1000 × memoryRequest / nodeCapacity`; 128Mi against 35 GiB gives
≈996, and the kernel logged 997. The small request — not the limit — put the panel near
the top of the list. The cascade killed, in order, `nginx`, `postgres`,
`postgres_exporter`, `matrixctrl` (14 MB), `livekit-server` (20 MB), and only last the
18 GB process that caused it. Every victim was tiny; the cause was reached last.

**Decision:** `requests == limits` for both containers, making the pod **Guaranteed**
(`oom_score_adj -997`). QoS is a *pod* property — setting it on one container of the two
leaves the class Burstable and achieves nothing, which is the way this change is easy to
get half-right.

**Stated plainly, because it is a trade and not a free win:** this creates no memory. It
changes who is killed instead. It is defensible here because the reservation is ~2% of
the node, and because the gap between a 128Mi request and a 512Mi limit was buying
nothing — steady state is 81Mi — while costing the kill order.

**Limits deliberately unchanged.** Lowering the 512Mi ceiling to match real usage was
tempting and rejected: the peak during a Helm render has never been measured, and that
would trade a rare collateral kill for a self-inflicted one.

**Consequences:** the same failure shape as §4.31 — a component that removes itself at
the worst possible moment — closed from the other side. The ESS pods killed in the same
cascade are Burstable for the same reason, but they belong to the ESS chart and stay the
operator's decision. Affects S8.

### §4.33 — Narrowing where a credential may appear is not the same as not logging it (2026-08-06, agent, from the operator's deploy log)

**Context:** while watching an ESS upgrade, the operator's own deploy wrote this:

```
"GET .../upgrade/<id>/logs?token=eyJhbGciOiJIUzI1NiIs… HTTP/1.1"
```

A valid session JWT in plaintext, one line per WebSocket connection. Anyone able to
run `kubectl logs -n matrixctrl` could take the session until it expired.

**What makes it interesting is that it was already understood.** §4.27/E29 removed the
same token from the OIDC callback URL and wrote the reason down in
`internal/auth/authcode.go` — *"chi's request logger writes the full URL, so the token
was written to the application log by the very request that delivered it."* It then
narrowed `?token=` from every route to WebSocket upgrades only, with a comment naming
the same logger. The hole was documented twice and left open once, because that route
genuinely could not set an `Authorization` header.

The lesson is the section title: restricting *where* a credential may travel in a URL
does not stop it being logged. It only reduces how often.

**Decision, in two halves, because either alone is insufficient:**

1. **The logger stops writing credential values.** A replacement for chi's `Logger`
   redacts a fixed key set (`token`, `ticket`, `code`, `client_secret`, `password`, …)
   and keeps everything else, so `?container=postgres` still helps a reader.
   The value is replaced rather than dropped: *"ticket=[redacted]"* and no ticket at
   all are different facts when reading back a failed handshake. It parses the raw
   query by hand rather than with `url.ParseQuery`, which drops pairs it cannot parse
   — a token in a malformed query would otherwise vanish from the sanitiser's view and
   reappear in the log.
2. **The handshake carries a single-use ticket, not the session.** Redaction only
   fixes *our* log; the URL still passes the ingress, the tunnel and any proxy in
   between. A ticket is spent by the connection it opens, so the copies left in those
   logs are inert. `extractToken` now has no query fallback on any route.

**Deliberately not reusing `AuthCodes`**, despite being the same shape (random,
single-use, atomic redemption): those codes are redeemable at `/auth/exchange` for a
full session, so a shared store would let a leaked ticket be traded for one — turning
a read-only log stream into a complete session. The separation is the security
property, and worth the small duplication. In-memory rather than Postgres, because a
WebSocket connects to the process that issued its ticket and a restart voiding
outstanding tickets is correct rather than a limitation.

**Consequences:** the reconnect path (§4.14) must fetch a *new* ticket per attempt —
the previous one was consumed by the connection that just dropped. Two E29 tests
asserted the old contract and were rewritten rather than deleted: one now proves a
query token is refused even on a handshake, the other tests `isWebSocketUpgrade`
directly instead of through the token path it no longer feeds. Affects S3.

### §4.34 — Borrowing the operator's authority instead of holding your own (2026-08-06, operator + agent)

**Context:** Phase 2 continues with rooms, which live in **Synapse** — a system
MatrixCtrl had never spoken to. MAS owns authentication here, so the first question was
not "what does the UI look like" but "what authenticates against Synapse's admin API at
all". Two answers existed:

- **A service token.** MAS 1.21 exposes `POST /api/admin/v1/personal-sessions`, minting
  a token that acts on behalf of a user, with the schema noting *"If not set, the token
  won't expire."* Standing power to act as somebody, with no human present.
- **The operator's own authority**, via one extra scope. Chosen.

**What the live system said, none of which was assumable:** MAS advertises only
`openid` and `email` in its discovery document, but the session table shows real
clients holding `urn:matrix:org.matrix.msc2967.client:api:*` — the discovery list is
incomplete, and believing it would have ended the etappe before it began. No session
has ever carried `urn:synapse:admin:*`, and no row in Synapse's `users` table has
`admin = 1`: Synapse admin authority did not exist here in any form. Two MAS accounts
have `can_request_admin`.

That last fact is the design: **MAS enforces the privilege check, not MatrixCtrl.** An
account without `can_request_admin` cannot obtain the scope, so the boundary sits
upstream of any code here.

**Decided: the scope is requested in its own authorization, not added to login.**
Putting it on the login path would make every sign-in request a scope MAS has never
granted on this deployment; if MAS rejects such an authorization rather than omitting
the scope, nobody can sign in — S11 check 3, and untestable from the server side. The
separate flow means a wrong guess costs the rooms page instead of the panel. Both flows
return to the same callback, because MAS validates `redirect_uris` strictly and the
static client is registered with exactly one; which flow a request belongs to is read
from the state's database row, never from anything the browser carried.

**Decided: the refresh token lives in memory and is never written down.** Measured, not
assumed: every access token MAS has issued here has a TTL of exactly **300 seconds**,
so a page opened ten minutes after signing in needs a refresh token — a credential that
can keep minting Synapse-admin tokens for the life of the MAS session. Persisting it
would leave that at rest in Postgres, per operator, to save a sign-in. In memory, a
restart costs one login and costs an attacker with database access nothing.

**Two defects this found in existing code**, both of which would have shipped silently:

- `api.ts` threw a bare `Error`, so every frontend branch on a status code was dead.
  The rooms page's "this account is not an admin" explanation could never have
  appeared. Fixed with an `ApiError` carrying `status`.
- `api.ts` ends the session on **any** 401. The rooms endpoint answering 401 for an
  expired *Matrix* token would therefore have signed the operator out of MatrixCtrl
  every five minutes. Downstream credential failures now answer **409**; 401 keeps its
  single meaning.

**Consequences:** signing out drops the Matrix session too, or a refresh token would
outlive the login it came from. The refresher is resolved per call rather than captured
at startup, because connect-OIDC and the §4.31 retry both replace the OIDC service at
runtime. Synapse is reached in-cluster, not through the public hostname: a bearer token
for a call between two pods in one namespace should not cross the ingress and the
tunnel, which is three more places to be logged after §4.33 removed one. Affects S3, S13.

### §4.35 — Scoping by verb is not scoping by namespace (2026-08-15, agent, from P0-4)

The ClusterRole was `apiGroups: ["*"] resources: ["*"] verbs: ["*"]` plus
`nonResourceURLs: ["*"]`, bound cluster-wide. Its own comment defended this: a
tighter scope "would break upgrades of releases that contain CRDs, ClusterRoles,
etc."

**Measured, the defence was false.** matrix-stack renders 13 kinds across 7 groups
and creates no CRDs and no ClusterRoles. It does create three namespaced `Role`s,
which is the fact that looked like a blocker and was not: Kubernetes refuses to let
an account create a Role granting permissions it does not itself hold, and those
three grant only `secrets` create/get/update, `configmaps` create/get/update, `pods`
list and `statefulsets` list/get/update — all in the managed namespace, all of which
MatrixCtrl needs there anyway. So escalation prevention is satisfied without
`escalate` or `bind`.

Two things about the enumeration matter more than its content:

- **`helm get manifest` is the wrong source.** It omits Helm hooks, so the live
  release shows 8 kinds and the chart has 13. A role built from the running release
  would pass every check today and fail on the next upgrade, halfway through, in the
  `batch/jobs` rule — leaving the release in the `failed` state this install has
  already had to be recovered from once.
- **`kubectl auth can-i` is the wrong instrument.** Asked
  `create serviceaccounts/token` it answers `yes`; asked the same thing as a
  `SubjectAccessReview` the API server answers `false`. Subresource parsing differs.
  Only one of the two is the authorizer.

The scope is by **resource type and verb**, not by namespace, and the difference is
not cosmetic. A ClusterRole bound by a ClusterRoleBinding applies its namespaced
rules in every namespace, so `secrets` — required for Helm's release storage —
remains readable and writable cluster-wide.

An earlier draft hid this behind a values flag, `rbac.discovery.allNamespaces`,
documented as withholding cluster-wide secret access when off. It withheld nothing:
`kubectl auth can-i list secrets -n kube-system` answered `yes` with the flag off,
because the base rule already granted it. The flag was **removed rather than
documented** — a control that does not control is worse than none, since it is
believed. What replaced it is `k8s.KnownOverGrants`, a list of three permissions the
role grants beyond its purpose, asserted by a test that **fails when they go away**.
A limitation written as prose gets fixed and nobody notices; written as an assertion
it announces its own repair.

**Consequences:** `internal/k8s/permissions.go` holds the required set as data,
derived from call sites and rendered kinds, and `Check` asks the API server via
`SelfSubjectAccessReview` — which needs no permission of its own, being granted to
`system:authenticated` — whether the running identity holds it. That answers "will my
next call succeed", which a diff against the chart cannot: it also catches a
hand-edited role, or a binding that was never applied. `Discover` now falls back to
the configured namespace when a cluster-wide release scan is refused, and reports
which of the two it did, so "no ESS found" cannot be read as "there is no ESS" after
a search that never left one namespace. Affects S6, S9, S13.

**A third source, which neither of the first two contains.** The scoped role was
built from what the chart renders plus what MatrixCtrl calls. That missed
`apps/replicasets`: `Wait` is on for install, upgrade and rollback, and Helm's
readiness check for a Deployment calls `GetNewReplicaSet`, which **lists**
ReplicaSets. The chart renders none and this code touches none, so both sources are
silent — and with seven Deployments in ESS, every upgrade would have applied
successfully and then failed while waiting.

It was found by reading Helm's `pkg/kube` rather than by the matrix, which is the
honest order and the reason the entry now carries the citation. The general form:
**a permission reached through a library's internals is invisible to an enumeration
of your own call sites**, so a dependency that acts on the cluster on your behalf has
to be read, not inferred from what you asked it to do.

### §4.36 — A stale inventory is a defect, not untidiness (2026-08-15, agent, from E37)

CLAUDE.md rule 2 is "inventory instead of guessing", and it names BACKLOG.md as one
of the two files the inventory lives in. While closing P0-4, P0-5 turned out to be
listed as open and to have been fixed months earlier. Checking its neighbours found
**eight** entries in that state — the entire 2026-08-04 security review batch, all
closed by E29 and E35, none struck through.

So a reader of the public repo found a document, maintained by the author, stating
that this admin panel runs as root with wildcard CORS, no login rate limiting, a
guessable signing-key fallback, and session tokens in URLs. Every one of those was
false.

That is a specific kind of wrong, and it is worth naming because the instinct is to
treat it as bookkeeping:

- **It errs toward looking worse than reality**, which is the direction nobody
  audits. A project overstating its safety gets caught; a project understating it is
  simply believed, and the cost lands on the reader who decides not to use it, or on
  the maintainer who re-fixes something already fixed.
- **It hides the entries that are open** by burying them among eight that are not. A
  P0 list where most items are already done trains its own author to skim it.
- **It is self-similar.** P2-1 in this same file already records the lesson —
  "a status written from intent rather than observation decays into a lie, and the
  lie survives because nobody re-checks their own notes" — written about a claim
  that greenfield deploy worked. The file containing that sentence had drifted the
  same way, in the opposite direction. Writing the lesson down does not apply it.

**Consequences:** every entry closed in this pass names the code that closes it and
the etappe that shipped it, not merely "done" — an entry closed by assertion is the
same defect one iteration later. Struck-through entries keep their original text,
because several of them are worth more as a record of how a thing was wrong than as
a record of it being fixed. And a claim in this file that describes code is now
something to re-run, not re-read: `P1-11` was marked done in its body while its
heading still read as open, which is how eight of them survived a reader who was
looking directly at them. Affects S9, S12.

### §4.37 — Two plausible optimisations the measurement rejected (2026-08-15, agent, P2-22)

The history page cost **3.2–4.6 s on every load**, measured on the production
release's 14 revisions. `action.NewHistory` fetches and decodes every revision to
fill a four-column table. It is the page an operator opens *because something went
wrong*, so the latency arrives exactly when patience is shortest.

§4.20 solved the same shape for `/status` by reading the release secret's labels
instead of decoding payloads. Applying it one page over looked like a transcription
job. It was not — **both** obvious moves are wrong, and each would have shipped
looking correct:

**"Decode each revision individually and cache it."** Measured on the same 14:

```
14 × Releases.Get     →  7.328 s
 1 × Releases.History →  5.270 s
```

Per-revision fetching is **40 % slower** than the call it replaces, because each is
its own round trip. The refinement that sounds strictly better makes the cold path
worse. It wins only for the incremental case — one new revision after an upgrade —
so the code chooses by count, with the crossover taken from that measurement rather
than from taste.

**"The `modifiedAt` label can supply the timestamp."** §4.20 already reads it, so
reusing it looks free. It is not per-revision: nine revisions spanning ten weeks
share one value, while their real `LastDeployed` times all differ. §4.20 uses it
correctly — as a cache-invalidation key for the newest revision, never as a
displayed time — and copying it into a "deployed at" column would have put
confidently wrong dates in front of the operator. **A field that is correct in one
role is not thereby correct in another.**

What survives is the part that is arithmetic rather than a trade: a revision's
chart and deployment time are fixed when Helm writes it, and only its *status*
changes afterwards. So the immutable two are cached forever and the mutable one is
read from the labels every time. That is not a staleness window; there is nothing
to go stale.

**Consequences:** cold 4.7 s once per process, then **25 ms**. `ListHistory`'s
`max` parameter now does something — Helm's own `History` action accepts a `Max`
and never reads it, so asking for ten returned fourteen and cost the same as
thirty. Two defects were caught by measuring rather than by review: pruning the
cache against the *truncated* list made a small page destructive (2.958 s where 40
ms was expected), and storing the timestamp as Unix seconds made the fast path
disagree with the fallback by up to a second — found only because the live test
compares the two paths row by row, which is the single thing that makes a fast path
safe to add. Affects S2, S4.

### §4.38 — A blocker I recorded without testing (2026-08-16, agent, closes P0-4a)

E37 scoped the ClusterRole by resource type and verb and stated plainly that it had
not scoped it by namespace: `kubectl auth can-i list secrets -n kube-system`
answered **yes**, because a ClusterRole bound by a ClusterRoleBinding applies its
namespaced rules everywhere. Helm's release storage needs `secrets` in the managed
namespace; the binding turned that into read and write on every secret in the
cluster.

E37's plan explained why that could not be fixed:

> a RoleBinding cannot be created in a namespace that is not there … so the chart
> has to create the namespace — which conflicts with Helm ownership when adopting
> an ESS whose namespace already exists. **That trade is the whole etappe.**

There was no trade. Helm's `lookup` answers exactly that question against the live
cluster at install time:

| | `ess` (exists) | a name that does not |
|---|---|---|
| `helm template` | `no` | `no` |
| `helm upgrade --dry-run=server` | **`yes`** | `no` |

So the chart renders the Namespace only when it is genuinely absent: greenfield gets
one created, an adopted install never sees the object, and there is no ownership
conflict to have. One `lookup` call, tested in ten minutes, against a paragraph of
confident reasoning that cost the fix a day of not existing.

**The lesson is not "use lookup".** It is that the sentence *"that trade is the whole
etappe"* was written in the same pass as measurements that were real, and inherited
their authority without earning it. §4.35 records two assumptions that measurement
overturned and §4.37 records two more; this is the third instance in three etappes,
and in this one the untested claim was the reason for **not** doing the work.

**Consequences:** `helm.sh/resource-policy: keep` on that Namespace is load-bearing
twice, and both failures are severe. Without it, `helm uninstall matrixctrl` deletes
the ESS namespace and everything in it — removing the admin panel would remove the
homeserver. And because a later upgrade's `lookup` finds the namespace and renders
nothing, Helm would delete an object present in the old manifest and absent from the
new one, which is the same disaster on the second upgrade instead of at uninstall.

Two grants were removed rather than relocated, because each was a diagnostics number
paying rent in permissions: `ListPVCs(ctx, "")` listed claims in every namespace, and
`SysInfo` counted pods in `kube-system` — keeping that would have meant the chart
writing a RoleBinding into the cluster's most sensitive namespace so one figure could
appear on a page.

Proven before applying, on a probe identity: **90/90** required granted in the
managed namespace, **7/7** forbidden powers denied, and the eight confined
permissions denied in `kube-system` while still granted in `ess`. That last pair is
the point — checking only the denial would pass equally well against a role that
grants nothing, which is a broken panel rather than a safe one. Affects S9, S13.

### §4.39 — A verb that sounds more final than it is (2026-08-16, agent, etappe 41)

E28 established that every write dialog must state what the verb actually does,
after `deactivate` turned out to GDPR-erase by default — a verb doing **more** than
its name suggested.

Room blocking is the mirror image. Synapse's block flag refuses **new joins** and
nothing else: everyone already in the room stays, every message stays, and the
conversation carries on. An admin who blocks a room to stop something happening and
walks away has not stopped it. The same rule therefore produces the opposite text —
the dialog spends its words on what blocking *does not* do, because that is the part
a reasonable person would otherwise assume.

It ships alone, without deletion, for the reason E36 gave and then partly got wrong:
E36 grouped blocking with deleting as "destructive". Blocking is a flag with an
inverse; deleting evicts every member and purges history and has none. Grouping a
reversible action with an irreversible one delays the safe half for the sake of the
dangerous one.

Two smaller decisions worth keeping:

- **The block state is read back after the write, never assumed.** A 200 on the PUT
  says the request was accepted, not that the flag is now set, and the UI renders
  that field as a fact. §4.20 made the same distinction for release status.
- **Unblocking needs no confirmation; blocking does.** The confirmation exists
  because the action changes what *other people* can do. Restoring the default takes
  nothing away, and a control whose reverse is hard to find is one operators avoid
  using at all.

**Consequences:** room IDs contain `!` and `:` and are path-escaped rather than
concatenated — a test uses real IDs, because a hand-typed example would pass either
way. Synapse's member endpoint has no paging of its own, so the boundaries are ours
and `sliceMembers` is separated out to be tested without a server: every failure
there is an off-by-one at an edge, and edges are what never come up while clicking
through a small room. Affects S13.

### §4.40 — A command that reports success without doing its job (2026-08-16, agent, etappe 43)

`web/tsconfig.json` contains `"files": []` and two project references. Plain
`tsc --noEmit` therefore type-checks *zero files* and exits 0 whatever the code says.
That was the command in CLAUDE.md and in PROZESS.md's ship checklist for the life of
the project, so every "typecheck green" reported before this date was a no-op.

The errors were never invisible — the Dockerfile ends `npm run build` with
`tsc -b --noEmit`, which is the real check. But that runs eleven minutes into an
image build, and its failure is a hundred lines of BuildKit output ending in
`exit code: 2`. E42's build failed exactly this way, the failure went unread, and
**0.1.43 did not exist** while two etappes were recorded as built.

This is the third instance of one shape, and worth naming as a class:

| The report | What it was actually about |
|---|---|
| `rollout status` succeeded (§4.17) | the *old* Deployment, untouched |
| `helm list` said APP VERSION 0.1.33 | the chart's label, not the running image |
| `tsc --noEmit` exited 0 | zero files, not zero errors |

Each answered its own question correctly and none answered the question being asked.
The defence is the same one PROZESS.md already applies to deploys: **read back the
subject, not the verdict.** A check that cannot say *what it checked* is not a check.

**Consequences:** the documented command is `tsc -b --noEmit`, with the trap written
down beside the `--set image.tag` one, since they fail identically. The four errors
it had been hiding were real — TanStack treats a `validateSearch` return type of
`{ error: string | undefined }` as a *required* search param, so every
`<Link to="/rooms">` in the app was a type error. Annotating the return type as
`{ error?: string }` is the fix.

### §4.41 — A progress bar has to have an honest denominator (2026-08-16, operator + agent, etappe 43)

§4.29 replaced a clock that never looked at the cluster with a probe that reports
which pod is stuck and why. It fixed the failing case and left the healthy one
worse than it looked: the probe's diagnosis is deduplicated, so a *smooth* rollout
emits one line and then nothing, and the operator watching a working upgrade sees
the least output of any scenario. Reported during the 26.8.0 upgrade as "man sieht
nicht genau was passiert".

Three choices made the structured version honest rather than merely prettier.

**Workloads, not pods, are the denominator.** During a rollout old pods terminate
while new ones start, so a pod-based ratio falls while everything is going right. A
bar that goes backwards is worse than no bar. The workload set is fixed for the
operation, and is what `helm --wait` is itself waiting on.

**Generation is part of "ready".** A Deployment Helm has just patched still has
every old replica matching the old spec, so the replica counters alone call it
ready. Every workload is in that state in the first moments of an upgrade — the
screen would open at 100 %, fall, and climb back. While `Generation > ObservedGeneration`
the controller has not seen the new spec, which is exactly "not started".

**"Pulling an image" cannot come from the pod.** Its container status reads
`ContainerCreating` while pulling, mounting a volume and attaching a network alike,
and those have very different expected durations — the operator specifically asked
to be able to tell them apart. The kubelet says which it is, as an event, so that is
where it is read from, bounded by a field selector and by the operation's own start
time (events live an hour; without the cut a pod that pulled fifty minutes ago reads
as pulling now).

The progress snapshot is a **latest value, not an event stream**: it is stored on the
stream and pulled by the WebSocket rather than pushed through the log's subscriber
channel. A client that reconnects or drops a frame renders the current truth on the
next tick instead of folding a history. It also keeps the log path exactly as
reliable as it was — the log is the audit trail, and it must not begin dropping lines
because a progress feature shares its buffer.

**Consequences:** phases are announced by the code that performs them, never
inferred; there is no `apply` step in the stepper because helm's Upgrade applies and
waits in one blocking call and the boundary is not observable. `internal/rollout`
keeps its no-client-go rule, so the assembly is a pure function with unit tests, and
a live test proves the two reads it depends on are permitted under E40's namespaced
Role and that Generation is actually populated. Affects S11.

### §4.42 — A counter that resets is not a history (2026-08-16, operator + agent, etappe 44)

The operator asked the calls page for audit, connections and statistics. The
inventory that P2-29 demanded before building any of it produced one fact that
decided the shape of all three.

Every number LiveKit publishes is **process-lifetime**. Read ten hours after the
26.8.0 upgrade, on a server that has been running for months:

    livekit_room_total                   0
    livekit_participant_total            0
    livekit_room_duration_seconds_count  0
    livekit_quality_score_count          0

None of that means "this deployment has never carried a call". It means "this
process has not", and the process is young because MatrixCtrl's own post-upgrade
hook deletes the SFU pod on every ESS upgrade to restore `hostNetwork` — several
times a week here. A statistics panel reading those counters directly would have
said "since the last upgrade" in the voice of "ever", which is §4.24 again with a
different subject: reporting confidently on the part that happens to be readable.

**So the history is recorded, not read.** The precedent was already in the package:
`internal/rtc/watcher.go` samples DNS on a timer because "a history built from page
views has gaps exactly where nobody was looking, which is most of the time." Here
the argument is stronger — unrecorded, this history is not merely gappy, it is
destroyed on a schedule.

Three things make the recording trustworthy:

- **A reset is a first-class event, not an anomaly to smooth over.** When a counter
  comes back lower, the delta is the *new value* — everything the old process
  counted was already recorded by samples taken while it ran — and the restart is
  stored, because it explains a discontinuity in every other series. All three
  counters are checked, not one: the headline counter is zero on both sides of most
  restarts on this instance, so watching only it would miss them.
- **Exact totals from an inexact interval.** Both underlying counters are
  cumulative, so calls and minutes between two samples are right even for a call
  that began and ended between them. Only the *timing* is coarse, and the page says
  which is which instead of implying both are precise.
- **A failed read records nothing.** An unreachable metrics endpoint is not an SFU
  with zero rooms. Writing a zero would fabricate a quiet period, and would then
  read as a counter reset on the next successful sample — inventing a restart that
  never happened.

**What was deliberately not built:** LiveKit's RoomService API would give room names
and participant identities. "Three people are in a call" and "who is in a call with
whom" are different classes of data, and no question this page answers needs the
second. The gauges answer "connections" without reading a secret, minting an admin
token, or storing anyone's identity.

**Also not built:** "Statistik ausweiten auf das ganze". A panel-wide statistics
story is a design question about what an operator wants to see over time, not a
matter of finding more counters. The plan says so rather than half-doing it — the
general shape is better argued from one working example than in advance.
**Consequences:** migration 011 keeps the raw counter values beside the resolved
deltas, so a bug in the delta logic can be corrected against the original
observations rather than having destroyed them. Affects S14.

### §4.43 — Recording one member of a set (2026-08-16, agent, etappe 45)

Found while checking whether E44's new table would grow without bound. It would —
but the older table beside it already had, and for a reason that mattered far more
than the disk.

`rtc_address_history` is meant to hold one row per *change* of the announced RTC
host's address. It held 1778, in twelve days, split **889 / 889** between two
Cloudflare addresses. That symmetry is a coin flip, not a history.

The host is proxied, so DNS returns two A records, and `LookupHost` rotates their
order per query. The writer recorded `addrs[0]`. `NextObservation` — correct for
what it was handed — saw a different answer roughly half the time.

**The cost was not the rows.** `AssessFreshness` compares the newest observation
against the SFU pod's start time, so an observation minutes old and a pod hours old
can only produce one verdict. The calls page showed this continuously from
2026-08-04:

> **Die SFU kündigt eine veraltete Adresse an** … *Action:* SFU-Pod ersetzen.

A false `WARN`, above a button that drops any call in progress. E22 built the check
to catch a real failure — the SFU announcing a stale address after a forced
reconnect (P1-9). For twelve days it reported on DNS round-robin.

Two fixes, and the second is the one worth keeping:

**Record the set, sorted.** A host with several A records has an address *set*; a
change is a change to that set. This stops the noise, and it is small.

**Refuse the verdict when its premise fails.** E22's reasoning is sound and is stated
in the file: the SFU discovers its public address by STUN at startup, so the
announcement is stale exactly when the address changed after that moment. It depends
entirely on the announced host's A record *being the node's public address*. Behind a
CDN it is not — those anycast addresses do not move when the operator's line
reconnects, and they move for reasons unrelated to this deployment. More than one A
record is a reliable signal of that, because a home connection has one WAN address.

So the answer is `Unknown` **with the reason**. That state already existed for
exactly this purpose and the page already rendered it; what was missing was
recognising this as a case of it. An operator behind a CDN now learns that this check
cannot see their setup, instead of being told to restart the SFU daily.

This is §4.40's table with a fourth row. `addrs[0]` answered "did the first element
change?" correctly, and never "did the public address change?" — the report was true
about its own subject and silent about the one being asked. The defence is unchanged:
**read back the subject, not the verdict.**

**Consequences:** the noise rows cannot be repaired into set-shaped ones — each holds
one member of a set whose other members were never written — so migration 012 deletes
them by their flapping signature rather than migrating them. Retention was added to
both RTC tables at the same time; that is *not* P2-19, which concerns the audit log
and is a compliance decision, while these are operational telemetry with no such
duty. Affects S14.

### §4.44 — A queue whose only "done" is deletion (2026-08-16, agent, etappe 46)

Phase 2's last feature. The interesting part was not reading the reports — that is
one admin endpoint — but deciding what "handled" means, because Synapse has no
opinion on it.

Synapse offers exactly one way to clear the report queue:
`DELETE /_synapse/admin/v1/event_reports/<id>`, which destroys the record. There is
no resolved flag, no assignee, no note. A report is present or it is gone.

Building the queue on that primitive would have been wrong twice over:

- **It destroys the evidence.** A report is a user's statement that something was
  wrong. Deleting it after acting on it means the next admin cannot see that it
  existed, what it said, or that anyone looked. And the case that matters most —
  one account reported five times — is precisely the one that disappears, one report
  at a time, exactly as each is dealt with. The pattern is the finding; the
  individual report often is not.
- **It is irreversible**, and §4.39 already settled how this project treats that: an
  action with an inverse ships early, one without gets its own careful treatment.

So disposition lives in MatrixCtrl (migration 013) and Synapse's record is never
touched. Reopening is a row delete and takes nothing away.

Three smaller decisions worth keeping:

- **`open` is not a stored value.** It is the *absence* of a decision, expressed by
  the absence of a row. Storing it too would create two representations of one state
  that could then disagree, and every reader would have to know which to trust.
- **`handled` and `dismissed` are not collapsed.** They both take a report off the
  open queue and they say different things to the next admin — something was done,
  versus the report was judged not to need it. That distinction is the only part of
  a closed report anyone reads later.
- **Two user IDs on one record.** Synapse's report carries `user_id` (who filed it)
  and `sender` (who wrote the reported event). The names give no hint which is which,
  and swapping them accuses the wrong person. They are `Reporter` and `Sender` in Go,
  translated in exactly one place, and neither is ever rendered without its role
  beside it. A test asserts the mapping, because this is the kind of thing that is
  obviously right until it is obviously wrong in production.

**An encrypted event is a finding, not an empty state.** A reported
`m.room.encrypted` event says so explicitly — the server cannot read it, so neither
can this panel. Rendering it as a message with no text would suggest the content was
checked and found unremarkable.

**Consequences:** media quarantine, the other half of the roadmap's "Reports &
moderation", is deliberately not in this etappe — the reported event has to be parsed
for media references and reconciled with `room/<id>/media`, and bolting that on would
give the reversible-pair reasoning a fraction of the attention it needs. The Matrix
connect panel became shared at the same time, because the authorization is per
operator rather than per page: one token serves rooms and moderation alike (§3).
Affects S13.

### §4.45 — A 200 that carries no information at all (2026-08-16, agent, etappe 47)

§4.20 established that a successful write says the request was accepted, not that the
state changed, and E41 acted on it by reading a room's block flag back. Synapse's
media quarantine is the strongest case of that rule this project has met, and it was
found by reading Synapse's source out of the running container rather than its
documentation — which does not mention two of the relevant endpoints.

The handler, in full:

```python
await self.store.quarantine_media_by_id(server_name, media_id, requester.user.to_string())
return HTTPStatus.OK, {}
```

An empty body, every time. Not whether the media exists, not whether it was already
quarantined, not whether anything changed. And in the store:

```python
if quarantined_by is not None:
    hash_sql += " AND safe_from_quarantine = FALSE"
```

Media marked safe is **silently skipped**, and the caller gets `200 {}` — byte for
byte what success looks like. An admin quarantining a protected file would see a
green result and walk away believing they had taken it down.

Note which side the condition is on. The filter applies when `quarantined_by is not
None`, i.e. on quarantine and **not** on release, so the two directions are not
symmetric: protected media cannot be quarantined and can be released. A panel that
assumed a reversible pair behaves reversibly would be wrong in one direction only —
the harder kind of wrong to notice.

So every write is followed by `GET /media/<server>/<id>` and the panel reports
`quarantined_by` as found. `Changed` is computed by comparing the read-back with the
request rather than from `safe_from_quarantine`, so a future reason to silently no-op
is caught by the same code without anyone having to predict it.

**What was left out, and why it is not laziness.** Deleting media has no inverse —
same treatment as room deletion and GDPR erasure. Protect/unprotect *is* a reversible
pair and would have been four lines, but "protected" is a flag one admin sets to stop
*other* admins acting; shipping that toggle beside the quarantine button invites using
it to win an argument, and it belongs with a permissions model this project does not
have.

**Consequences:** three reference sites are read from an event — `content.url`,
`content.info.thumbnail_url`, and `content.file.url` for encrypted rooms — and the
kind travels with each, because quarantining a thumbnail while the full image stays
served is not what the admin meant. Media ids containing a slash are refused rather
than escaped, since they become a URL path segment. Reading the source also turned up
`/_synapse/admin/v1/user_reports`, an entire second report queue that E46 does not
know exists; it is recorded as P2-30 rather than folded in. Affects S13.

### §4.46 — An identifier is only unique inside the thing that issued it (2026-08-17, agent, etappe 48)

E46 stored a report's disposition in `event_report_dispositions` with
`report_id BIGINT PRIMARY KEY`. That was correct for exactly as long as there was one
queue. Synapse has two: `event_reports` and `user_reports`, each with **its own
sequence**. Both therefore contain an id 1, an id 2, an id 3, and they mean unrelated
reports.

Adding the user queue on top of that table would have made event report 5 and user
report 5 the same row. Marking one handled marks the other; reopening one reopens the
other. Nothing raises, no constraint is violated, no log line appears — the queue just
shows a decision nobody made. It is §4.43 again (recording one member of a set and
reading it back as the set), with the twist that the two members come from different
systems and are indistinguishable by inspection: both are small positive integers.

The fix is the boring one — the key is `(kind, report_id)`, migration 014 — but the
point worth keeping is the question that found it, which was not "how do I store user
reports?" but **"is the id I am about to use as a key unique in the scope I am using
it in?"** Foreign ids are unique inside the system that issued them and nowhere else.
This project already had the same shape twice: a media id is unique per server, which
is why `MediaRef` carries both, and a Helm revision is unique per release.

The kind is bound to the `Dispositions` value at construction rather than passed to
each method. Four methods taking a kind parameter is four call sites that can pass the
wrong one, and the whole reason this bug is dangerous is that a wrong kind looks
exactly like a right one. Affects S13.

### §4.47 — The fallback route decides what "this endpoint does not exist" sounds like (2026-08-17, agent, etappe 48)

`r.NotFound(deps.Status.ServeFrontend)` — one line, correct-looking, and it meant every
unmatched **API** path answered `200 text/html` with the app shell. A misspelled
endpoint in the frontend passed `res.ok`, then died inside `JSON.parse` with a message
about unexpected `<`, which sends the reader looking at their parsing code instead of
at their URL.

It is the same defect as §4.45 one layer out: a 200 that means "no such thing". It was
found not by a bug report but while *verifying* something else — the E47 route probe
used "404 means unregistered" as its control, and the control came back 200.

The second-order cost is worse than the confusing error. It made "is this route
registered?" unanswerable: only a route behind auth was distinguishable from the
fallback, because a 401 proved something was there. Any future unguarded route would
have been indistinguishable from a typo, and every verification built on probing would
have quietly measured nothing — the §4.40 family again.

The fix has two halves and only one of them is the interesting one. Returning JSON 404
under `/api/` is trivial. Not breaking the SPA is the part that needs a test: every
frontend route is served by index.html, so a `NotFound` that stops doing that turns
every page reload into a 404. The test asserts both directions, and it was run against
the old code first to confirm it fails with exactly the reported symptom
(`status = 200`, an HTML body) rather than passing for its own reasons. Affects S9.

### §4.48 — The check that passes when it checked nothing (2026-08-17, agent, etappe 49)

`verify-ui.mjs` walks every functional route in a real browser and is the answer to
"an HTTP 200 does not prove the page rendered". Its last line was:

```js
process.exit(failed ? 1 : 0);
```

`skipped` was counted and printed and is not in that expression. Without
`MATRIXCTRL_TOKEN` ten of eleven routes skipped and the process exited **0**. The
output said so plainly; the exit status did not, and the exit status is the half that
a Makefile, a CI job or a shell `&&` reads.

That makes four members of this family now, and they are worth listing together
because the shape is identical every time:

| the check | what it answered | what was being asked |
|---|---|---|
| `kubectl rollout status` | the old ReplicaSet is healthy | is the *new* spec live |
| `tsc --noEmit` (no `-b`) | the zero files I was given are fine | does the app typecheck |
| Synapse quarantine `200 {}` | the request was accepted | did the media get quarantined |
| `verify-ui.mjs` exit 0 | nothing I ran failed | were the pages verified |

Each answers its own question correctly. None answers the one being asked, and each
is *more* dangerous than an error, because a green result ends the investigation.

Two things generalise. First, the defence stays what §4.40 recorded — **read back the
subject, not the verdict** — and here the subject is "how many routes rendered",
which the tool already knew and simply did not act on. Second, **"skipped" is not a
third outcome next to pass and fail.** It is "did not run", and code that treats it as
benign will report success for a run that did nothing. A skip is now a failure unless
`--allow-skip` says the partial run was intended.

Found, again, while doing something else: the route list was being read to see whether
E48's screen was covered. It was not — `/users`, `/rooms`, `/rooms/{id}` and
`/reports` had never been in it, so the report queue was rewritten by three
consecutive etappes without this check once opening it. A tool that is not wired into
`make` or CI drifts out of date silently, because nothing fails when it goes stale.
Affects S9.

### §4.49 — A generated file under version control is two sources of truth (2026-08-17, agent, etappe 50)

`cmd/matrixctrl/dist` — the compiled React frontend embedded into the Go binary — was
40 tracked files. P2-2 had it filed as a tidiness item: noisy diffs at review time.
Measured before fixing it:

```
$ git log -1 --date=short -- cmd/matrixctrl/dist
452173b 2026-08-01 chore: rebuild the embedded frontend assets

$ git ls-files cmd/matrixctrl/dist | grep -i reports
(nothing)
```

Sixteen days and roughly fifteen etappes stale, with **no moderation screen in it at
all**. `go build ./cmd/matrixctrl` produced a binary serving the UI of August 1st,
exited 0, and looked entirely normal. Releases were never affected — the Dockerfile
builds the frontend itself and `make build` runs `web-build copy-dist` — so the only
broken path was the most obvious command, which is the worst place for it.

CI's guard is the tell, and it is the fourth appearance of the same defect in three
days:

```yaml
run: test -d cmd/matrixctrl/dist || …
```

It asks *does the directory exist*. The question is *is the embedded UI current*. It
passed truthfully every day while the answer to the real question was no.

The fix is not "remember to rebuild", which the preceding sixteen days already
disproved. It is to stop having a second copy: track one placeholder, `dist/.gitkeep`,
ignore everything else. `//go:embed all:dist` is a build error on an empty directory —
verified in a scratch module, not assumed — so the placeholder is what keeps
`go test ./...` working on a clean checkout.

That converts a *stale* frontend into *no* frontend, which is only an improvement if
the absence is loud. So the binary says so at startup and serves a page naming the
cause and the command, rather than a 404 that reads like a routing fault.

The general rule: **a generated artefact under version control is a second source of
truth that can silently disagree with the first, and it will.** Instrumenting the copy
— embedding a build stamp to compare — was considered and rejected: two timestamps
that can disagree is another artefact with the same disease. Delete the copy instead.

A tail worth recording, because it is the same mistake one level down: `copy-dist`
begins `rm -rf cmd/matrixctrl/dist`, which deleted the very placeholder the fix
depends on. The first `make build` after the change left `.gitkeep` staged-and-deleted
and would have broken the next clean checkout's compile. Caught by looking at
`git status` after the build rather than at the build's exit code. Affects S8, S9.

### §4.50 — Reading a namespaced counter from the wrong namespace (2026-08-17, agent, etappe 51)

LiveKit warns at every start that its UDP receive buffer is far below what it wants:

```
WARN livekit rtcconfig/rtc_unix.go:31 UDP receive buffer is too small for a
     production set-up  {"current": 425984, "suggested": 5000000}
```

P2-24 asked for that surfaced on `/rtc` as a pre-flight check. The obvious build is to
read `net.core.rmem_max` and `/proc/net/snmp` from MatrixCtrl's own process. Both are
**network-namespaced**, and MatrixCtrl does not run with `hostNetwork` while the SFU
does. Measured before writing any of it:

| | InDatagrams | RcvbufErrors |
|---|---|---|
| node | 48009 | 0 |
| matrixctrl pod | **320** | 0 |

A drop counter read in this process counts *MatrixCtrl's own UDP traffic*. It would
read zero forever regardless of what the SFU experienced, and it would look
authoritative doing it — the panel would answer "no packets dropped" from a source
structurally incapable of ever saying anything else. That is §4.43 and E45's `addrs[0]`
once more: a value taken from the wrong member of a set, correct-looking and unrelated
to the question.

So both readings come from the SFU's own vantage point — the sizes from its startup
log line, the drops from `livekit_node_packet_total{type="dropped"}`, which was in
this package's own test fixture from the beginning and had never been parsed, because
only `type="out"` was.

Two smaller decisions worth keeping:

**Which "current" is current.** `net.core.rmem_max` is 212992 on this node while
LiveKit reports 425984 — exactly double, because `SO_RCVBUF` accounting returns twice
what was set. Both are defensible; the panel reports LiveKit's, because that is the
number in the log the operator is reading. A panel that contradicts its component's
own log costs more time than it saves. (P2-24's "24× too small" used the sysctl; against
LiveKit's figure it is ~12×.)

**A warning has to say whether it is biting.** The buffer is undersized and
`RcvbufErrors` is 0 — a latent fault, not a present one. Saying only the first half
sends someone hunting a problem that is not happening, so the finding states the drop
count, and says something different when it is non-zero. An absent metric is a third
case again: not zero, not fine, just not read.

The fix is a host sysctl, outside a cluster this project has spent two etappes
narrowing the privileges of (E37, E40). Naming what it cannot change is the correct
scope here, unlike P1-14 where the same posture was called half a feature — the
difference is that here the remedy is one documented command and the panel's value is
knowing it is needed *before* the first call drops. Affects S14.

### §4.51 — A redirect target is a security input, and a retry is a loop (2026-08-17, agent, etappe 52)

Two operator-reported defects in the shared Matrix-admin authorization, both about
what happens *around* the flow rather than inside it.

**The callback had one destination.** `finishSynapseAdmin` ended in
`http.Redirect(w, r, "/rooms", …)`, hardcoded on the success path and in its `fail`
closure. Correct in E36, when rooms was the only caller; wrong the moment E46 reused
the flow for the report queue and nothing carried the origin. Connecting from
Moderation worked and then abandoned the operator on a different screen — and the
failure case was worse, showing an error beside a connect button for a feature they
were not using.

The return path now travels in the `oidc_states` row, for the same reason the purpose
already did: the state is consumed from the database precisely because a value the
browser carries is not one to branch authorization on. It is then mapped through an
**allowlist**, not validated by a rule. "Starts with a slash" is the rule people reach
for and it is not sufficient — `//evil.example.com` is protocol-relative and browsers
follow it off-site. Two screens use this flow; enumerating them cannot be got subtly
wrong later, and a predicate can.

**"Connect once and it runs" versus a credential at rest.** The operator asked why
rooms needs reconnecting. It is not a bug: E36 deliberately keeps the refresh token in
memory, because persisting it leaves a Synapse-admin-capable credential in Postgres per
operator. The complaint was still legitimate — measured, the audit log holds four
connects ever, and the reason it *felt* constant is that seven panel versions shipped
that day, each restart discarding the token.

Offered as a decision rather than assumed, per rule 6. The operator chose to keep
nothing at rest and make the reconnect invisible instead: when the token is missing the
panel starts the authorization itself and returns to the screen in hand. Encrypted
persistence remains the recorded alternative, not a rejected idea nobody wrote down.

**The guard is the feature.** An automatic redirect triggered by a failing condition is
a redirect loop unless something stops it, and the obvious guard is the wrong one. The
dangerous case is not a *failed* authorization — that returns with `?error=`, whose
presence suppresses the retry — but a *successful* one whose token is refused again
immediately, which would bounce through MAS forever with no error to show. Hence a
session-scoped timer as well, and one that fails closed when storage is unavailable.

The subtler trap was in clearing it. Clearing on `connected == true` is the natural
reading and re-arms the loop exactly when it matters: a reconnect can succeed, flip
`connected`, and have the very next request answer 409. The guard is cleared only on a
**successful data load**, because that is the only evidence the token actually works —
the same distinction as §4.45, where the state was read back rather than inferred from
an accepted request. And `403` never triggers any of it: reconnecting cannot grant a
permission Matrix has not given, so a retry there is an infinite loop against a
condition no click can fix. Affects S13, S6.

### §4.52 — The loudest element on the screen was wrong twice (2026-08-17, agent, etappe 53)

Found in a dashboard screenshot the operator sent while reporting something else. The
red alert at the top read:

> **postgres** in Restart-Schleife — Ursache ansehen.

while the component row six lines below it read `63× ⚠ postgres-exporter`. Two claims
about one fact, on one screen, and the louder one was wrong in both halves.

**Wrong subject.** Measured: `postgres` 0 restarts, `postgres-ess-updater` 0,
`postgres-exporter` 64. The database under the whole homeserver has never restarted,
and the banner said it was crash-looping. This is P2-8 / §4.43 exactly — and E38
*already fixed it*, in the table row and the drawer badge. `ComponentHealth.RestartsBy`
was populated, correct, and rendered two elements away. The banner never read it. **A
fix applied to some of the places that render a value has a half-life**, and the place
most likely to be missed is the one added at a different time — a banner, an alert, an
export — because it does not look like the thing that was fixed.

**Wrong tense.** The trigger was `restarts > 20`: a lifetime counter, with no time in
it anywhere. It cannot distinguish a container dying every thirty seconds from one that
misbehaved a fortnight ago. Live numbers: 64 restarts across twelve days, stable for
the last seventeen hours, rendered as an emergency. §4.42 said a counter that resets is
not a history; the mirror image is that **a counter that only accumulates is not a
present state**.

Kubernetes answers the present-tense question outright and was never asked:
`state.waiting.reason == "CrashLoopBackOff"` means it is happening now, and
`lastState.terminated.finishedAt` says when the last one was. Both were one field away
the entire time.

The screen now carries two different statements: an active loop stays red and names the
container, while a high count that is not looping is amber and reads as history, with
when it last happened. That split is the point. **Rendering history in red is how red
banners come to be ignored** — and the operator had been looking at this one for weeks.

A tail with the same shape: `make check` did not run `gofmt`, which CI does, so "check
green" never implied "CI green" — and E51 shipped unformatted code that only the
pipeline would have caught. Added to `make check`, and verified by introducing drift
and watching it fail rather than trusting that it would. Affects S4, S9.

### §4.53 — A reservation larger than the machine is an outage waiting for a reboot (2026-08-18, agent + operator, incident)

The managed homeserver was **down for 37 hours** and this is the write-up, because the
failure mode is one this project will meet again.

**What happened.** The node was resized from 32 cores to 6. Nothing broke at that
moment: Kubernetes never re-schedules a *running* pod, so postgres and Synapse carried
on with reservations the machine could no longer honour. The host then rebooted, every
pod was re-scheduled from scratch, and postgres could not be placed —
`FailedScheduling … Insufficient cpu`, repeated 423 times over 35 hours. Synapse waited
on the database, haproxy crash-looped against absent backends (450 restarts), and the
homeserver answered nothing.

**Why the arithmetic was so far off.** Two multipliers that are easy to miss:

1. `postgres.resources` is applied to **postgres *and* postgres-ess-updater** — the
   config comment said so and it was still surprising in practice. 4000m written once
   reserved 8000m.
2. A pod's CPU request is `max(sum(containers), max(initContainers))`. Synapse's
   `render-config` and `db-wait` inherited 4000m each, so Synapse reserved 4000m the
   whole time it was merely *waiting for the database*.

4000 + 4000 + 500 for the postgres pod, 4000 for Synapse: **12.5 cores demanded of a
6-core node**, against a measured whole-node usage of 773m.

**The rule.** A resource request is a claim on a machine, and it is only ever valid
relative to a machine that exists. E34 chose these numbers deliberately and correctly
*for the node it was written on*, recorded its reasoning in the config, and still
produced a latent outage — because the reasoning was never re-evaluated when the
premise changed. **Generous requests are not free caution; they are a scheduling
constraint that fails closed, silently, at the next restart.**

**The product gap, and it is the important part.** MatrixCtrl reported all four
affected components as `down` — correctly, immediately, for 37 hours. What it never
said was **why**. "Unschedulable because requests exceed node capacity" was sitting in
a `FailedScheduling` event the whole time, one API call away, and the panel showed a
red status with no cause. That is the difference between a monitor and an admin tool,
and closing it is worth an etappe: a component that is `down` because it *cannot be
placed* is a different fault from one that is crashing, and the arithmetic that proves
it (requests vs allocatable) is available locally.

Recorded as **P1-16**. Affects S4, S14.

### §4.54 — "down" is a status, not a diagnosis (2026-08-30, agent, etappe 54)

Built directly from §4.53. During that outage the panel reported four components
`down` — correctly, within seconds, for 37 hours — and never said *why*. The scheduler
had been publishing the reason in a `FailedScheduling` event the entire time.

`kubectl get pods` already answers "is it broken". The reason an admin tool exists is
to answer **"it cannot be placed, because it asks for 8500m on a 6000m node"**. The gap
between those two sentences is where 37 hours went.

What makes this more than printing an event message is the arithmetic, and the
arithmetic has two traps that this project met the hard way:

**A pod's request is not the sum of its containers.** It is
`max(sum(containers), max(initContainers))`, because init containers run alone and
first. Synapse's `render-config` and `db-wait` had each inherited 4000m while the
`synapse` container asked for 1000m — so Synapse reserved 4000m the whole time it was
*merely waiting for the database*. A naive sum reports 1000m and makes the diagnosis
look wrong.

**One `resources` block can cover several containers.** `postgres.resources` applied to
postgres *and* postgres-ess-updater, so 4000m written once reserved 8000m. A
per-container number understates the pod by half.

Verified end to end rather than by unit test alone: a deliberately unschedulable probe
pod, whose 40000m request sat in an *init* container behind a 100m app container, was
diagnosed live at 40000m against 6000m allocatable — the max-of-init path exercised on
real cluster data, then the probe deleted.

Two deliberate limits. `ExceedsNode` separates "larger than any node" from "the cluster
is full right now", because only the second one can resolve itself — telling an
operator to wait for a pod that can never fit is worse than saying nothing. And the
panel does not propose a number: what postgres *should* request depends on what else
the operator intends to run, so naming the arithmetic is the job and choosing the value
is theirs.

The preventive half — warning at config-save time, before the value becomes an outage
at the next reboot — is **not** here, and the reason is worth recording: knowing what a
config *would* request means knowing the chart's container topology (which containers
share a block, which init containers inherit it), which is a property of the chart and
not of the YAML being edited. MatrixCtrl has the Helm SDK and can render it; inferring
it from the values file would be exactly the guessing this project refuses. P1-16b.
Affects S4.

### §4.55 — The last place that can catch the number (2026-08-31, agent, etappe 55)

E54 explains an outage after it has happened. This is the ten seconds before it starts.

The values that took the managed homeserver down for 37 hours were written **through
this panel**. MatrixCtrl is the last thing that sees them before they reach the
cluster, and it had nothing to say about `cpu: 4000m` on a node with six cores.

**Why reading the values file would not have worked.** The obvious implementation
checks the numbers in the YAML being edited. It misses both halves of the real fault:
`postgres.resources` is one block covering *two* containers, and Synapse's init
containers *inherit* it while a pod's request is
`max(sum(containers), max(initContainers))`. Neither fact is in the values file. Both
are properties of the chart. So the check renders the chart — `action.NewUpgrade` with
`DryRun`, hooks disabled — and measures the pods that would actually exist.

Proven against the live cluster by rendering the real config twice:

| config | verdict |
|---|---|
| as it stands today | 0 findings |
| as it was on 2026-08-06 | `ess-postgres` **8250m vs 6000m — blocked**, `ess-synapse-main` 4000m — warn |

**8250m out of a file that says 4000m.** That number is the whole justification for
rendering rather than reading.

Two deliberate limits, both of the same kind — knowing what *not* to claim:

**It warns; it does not refuse.** A config that can schedule nothing is exactly the
thing to block, and E49 established that a skipped check should fail rather than pass
quietly. It is left out anyway, because a false positive here blocks *every*
deployment, and this check has never run in anger. Warn first, watch it be right, then
decide (P1-16c). The precedent cuts both ways and the difference is the blast radius of
being wrong.

**CPU only.** The same arithmetic carries memory and the plumbing already does, but
memory overcommit is normal and the kernel reclaims; a warning tuned like the CPU one
would cry wolf, and a check nobody believes is worse than none. CPU is what caused the
outage and CPU is what ships checked.

Cost worth knowing: the preflight pulls the chart, so an apply now pulls it twice —
about twenty seconds added ahead of a multi-minute upgrade. Correct before fast; if it
ever matters, the pulled chart can be handed to the upgrade that follows. Affects S1, S4.

### §4.56 — Two resources, two thresholds (2026-08-31, agent, etappe 56)

E55 shipped the capacity preflight for CPU only and gave a reason: a memory warning
tuned like the CPU one would fire constantly and be ignored. The operator asked for
memory anyway, and was right — that reasoning argued against *one particular
threshold*, not against measuring memory at all.

The check already reported two different things, and the split is what makes memory
safe to add:

| | CPU | memory |
|---|---|---|
| larger than any node | blocked | **blocked** |
| larger than what is free now | warn | *not reported* |

"Larger than any node" is arithmetic: no eviction and no waiting places that pod, and
that is as true of 64 GiB on a 36 GiB machine as it is of CPU. "Larger than what is
free" is a judgement about pressure, and memory behaves differently under it —
a node with 36 GiB and 30 GiB requested is ordinary Kubernetes, not a problem, and
saying so every time would teach the operator to skip the whole panel. **A check nobody
believes is worse than no check**, which is the same reason §4.48 refused to let a
skipped route pass quietly.

The general form is worth keeping: when a limit is declined, record whether the
objection is to *measuring the thing* or to *the threshold proposed for it*. The two
have different fixes, and E55 wrote down the wrong one.

### §4.57 — The panel on a phone (2026-08-31, agent, etappe 57)

Nothing here was subtle; it is recorded for the two decisions that were.

**A hook, not a media query.** This codebase styles inline. Reaching an inline style
from CSS needs `!important` plus a selector matching the style string
(`[style*="padding: 28px"]`), which works and is exactly the sort of clever-fragile
thing that is unreadable six months later. `useIsMobile()` is `matchMedia` with a
listener and reads as what it is. The breakpoint is 860px and deliberately not a
device: a narrow window on a laptop has the same problem and the layout should not care
which it is.

**Icons without a rasteriser.** A manifest needs PNGs; the build host has no
`rsvg-convert`, no ImageMagick, no Pillow, and nothing in `node_modules`. The choices
were to add a dependency for three files that change roughly never, ship SVG-only icons
and accept that iOS falls back to a screenshot of the page, or draw them. `make-icons.py`
draws them — path flattening including a proper elliptical-arc conversion (the mark has
one; approximating it as a straight line would visibly cut a corner), a supersampled
nonzero scanline fill, white on the brand square.

Committed together with the generator. §4.49 says a generated artefact under version
control is a second source of truth that will eventually disagree with its source; the
defence is not to refuse to commit it but to make regenerating it one documented
command, and to pick artefacts that change roughly never.

And it was verified by **looking at it**. A 512px PNG produced by 200 lines of
hand-written rasteriser is exactly the thing that can be subtly wrong — a filled
counter, an inverted winding rule — while every test one would think to write passes.
The same argument as P2-20, where the screenshots were the check. Affects S9.

### §4.58 — The convention was already there (2026-08-31, agent, etappe 58)

E57's plan rejected CSS for the phone layout: "a CSS rule cannot reach an inline style
without `!important` and a selector matching on the style string itself — clever,
fragile, unreadable in six months." So it built `useIsMobile()`.

`index.css` already contained:

```css
@media (max-width: 920px) { .mc-dash-grid { grid-template-columns: 1fr !important; } }
```

A class, a media query and an `!important`, overriding an inline style, written months
earlier for exactly this purpose. E57's objection was to matching on the *style string*
(`[style*="padding: 28px"]`) — which is a genuinely bad idea and a different one. Having
conflated them, it invented a second mechanism next to a working one.

Both now exist and the split that fell out is the right one, so it is written down
rather than left to be rediscovered: **CSS classes for layout, the hook for behaviour.**
A padding does not need to know it is on a phone in order to exist; the navigation
drawer does — it has to not be rendered at all.

The audit that opened this etappe is the other half of the lesson. Grepping for what
breaks at 360px found three suspects and the most obvious one — the dashboard's
`minmax(300px, 1fr)` two-column grid — **was already fixed**. Starting from the
assumption instead would have "fixed" it twice and left the real ones alone.

Verified by measuring the built stylesheet rather than reading the source: a headless
browser at 1280px and 360px, reporting computed padding, resolved grid columns, whether
the chevron is displayed, whether name and status share a line, and whether the document
scrolls horizontally. Desktop unchanged, phone folded to three columns, no overflow at
either. The authenticated screens still cannot be rendered here, so this reduces
guessing without replacing the operator's eyes.

### §4.59 — The sparkline that invented its own past (2026-08-31, agent, etappe 59)

P2-3 filed it as "the CPU/RAM sparklines live in memory and reset on reload". Reading
the code found something worse than a reset:

```js
historyRef.current[n.name] = { cpu: new Array(MAX_HISTORY).fill(cpuP), ... }
```

A freshly loaded page **pre-filled its history with the current value**. The chart drew
a flat line that reads as "stable for the past hour" and was one reading repeated forty
times. Not blank, not obviously missing — confidently wrong, which is the failure this
project keeps meeting (§4.42, §4.48, §4.52). An empty chart would have been better: it
says nothing, and nothing was known.

The fix is E44's shape reused rather than reinvented: a table, a sampler on a timer,
retention with the number in code. What is new is the column, and it comes from the
outage rather than from the entry:

**`allocatable` is recorded alongside `used`.** Usage answers "is this getting worse".
Allocatable answers "did the machine change under us" — the question that cost 37 hours
on 2026-08-16, when the node went from 32 cores to 6 and the only surviving evidence
was a screenshot the operator happened to have taken beforehand (§4.53). A change
between two samples is now a sentence on the page, with both numbers.

Worth separating, because they look alike and are not: usage swings every minute and
means nothing on its own, while capacity changes almost never and means everything when
it does. The detector compares each node's newest sample against its oldest *differing*
one, so the change is still reported hours later instead of only inside the single
interval it happened in — a warning that expires before anyone looks at it is a warning
that was never shown.

And per node, never aggregated. A two-node cluster whose total stays flat while one
node halves is exactly the case an average hides — §4.43's rule, applied before it
could bite rather than after.

### §4.60 — Writing the documentation found the bug (2026-08-31, agent, etappes 60 & 61)

P2-14 said the docs have no user-facing layer: 1242 lines written for maintainers, and
a README that explains how to run the container but never what a hook *is*. Verified
before acting on it — because the entry immediately above it, P2-4, described a feature
that already existed and was nearly built a second time.

Three claims were checked against the README: hooks explained (the word appears six
times, never defined), the config editor (absent), what to do when an upgrade fails
(absent). All three hold, and all three are the reason the project exists rather than
incidental features.

Then, while writing one sentence about hook triggers, the guide's own rule caught
something. The sentence was going to be "currently `post-upgrade`". Checking it:

```
$ grep -rn 'RunTrigger(' internal/ | grep -v _test
  RunTrigger(ctx, hooks.TriggerPostUpgrade, "deploy:"+h.essRelease, userID)
  RunTrigger(ctx, hooks.TriggerPostUpgrade, upgradeUUID.String(), userID)
```

`TriggerPostRollback` is declared, **offered in the hook editor's dropdown**, labelled
in the list view — and fired by nothing. An operator could create a hook, set it to run
after a rollback, save it, watch it appear as enabled, and it would never run.

The dead dropdown entry is the small half. The large half is what it implies: **a Helm
rollback recreates every object from the old revision's manifests**, which drops manual
patches for exactly the same reason an upgrade does. Rolling ESS back therefore removed
the SFU's `hostNetwork` and `externalTrafficPolicy` patches, broke Element Call's media
path, and left a green dashboard — the precise failure this project was built to
prevent, on the one operation an operator reaches for *when something is already wrong*.

The fix is not a second copy of every hook. A rollback now runs `post-rollback` hooks
**and** `post-upgrade` ones, because almost every hook in existence means "re-apply my
patch after the chart overwrote it" and both operations overwrite. The reverse does not
hold — a rollback-specific hook after an ordinary upgrade would be a surprise. The UI
labels changed to match ("Nach Upgrade und Rollback"), because the stored value stays
`post-upgrade` and a name that undersells what it does is how the next reader gets it
wrong.

The lesson is about documentation rather than hooks. **Writing down what the software
does, honestly, is a test of the software** — the guide's rule was "every technical
claim checked against the source while writing", and the first sentence that rule was
applied to turned out to be false. Two prior documents in this repo described features
that did not exist (§4.17's audit trail, documented for two months before it was
built). This is the same class of gap found from the other direction, by prose rather
than by a bug report. Affects S3, S12.

### §4.61 — A list of declared constants reads exactly like a list of features (2026-08-31, agent, etappe 62)

E61 found a trigger that was declared, offered in the UI, and fired by nothing. That is
a *shape*, not an incident, so it was worth sweeping for: constants whose only
occurrences are their own declaration and a dispatch that goes nowhere.

The sweep counted references per constant, outside its declaration:

```
ActionHTTPRequest        1
TriggerManual            1
ActionWaitRollout        2
TriggerPostRollback      3   ← E61, already fixed
ActionKubectlPatch       6
```

`TriggerManual` at 1 is fine — it is the value recorded when an operator runs a hook by
hand. `ActionHTTPRequest` at 1 was the third instance of the family:

```go
func (r *Runner) runHTTP(_ context.Context, _ HookAction) error {
	return fmt.Errorf("http_request actions not yet implemented (Phase 1)")
}
```

Declared, dispatched, offered in the hook editor with a complete form — method, URL,
body — and guaranteed to fail. Saving worked; the failure arrived the first time the
hook ran, which is during an upgrade.

Three in one session, all the same: **something is declared, something renders it, and
nothing performs it.** The reason this class survives review is in the title. Reading
`types.go` shows three action types and two triggers, and that reads as a capability
list; the absence lives in a different file, in a default branch or a stub, where
nobody looking at the feature looks. Both defects were found by counting *consumers*,
never by reading declarations — and that is the technique worth keeping.

**I produced an instance of it myself, in the same session.** E60's guide listed
`http_request` as one of three working action types, written from `types.go` without
checking the runner — one paragraph after that guide's own rule ("every technical claim
checked against the source while writing") had caught the rollback bug. Applying a rule
once does not make it a habit; the guide is corrected in the same commit as the fix.

Implementing it rather than removing it was the right end, because the form had already
promised it and the notification it enables is a real want (the idea box asks for an
alert when a hook run fails). It is deliberately narrow: one request, 2xx or the hook
fails, a bounded timeout because hooks run inside an upgrade phase an operator is
watching, no retries because a silent retry hides a broken endpoint, and **no header
field** — which is the interesting omission, since a header field is where a secret
would end up stored in plain text in the hooks table. Affects S3.

### §4.62 — The schema promised a feature nobody built (2026-09-02, agent, etappe 63)

Third pass of the sweep behind E61 and E62. Those counted consumers of declared
*constants*; this one counted consumers of **columns**. Eight columns and a whole table
with no Go code reading or writing them.

Checked on the running database before touching anything, because "nothing in the
repository writes it" and "it is empty" are different claims and only the second one
licenses a `DROP`: `config_snapshots` held 0 rows, and every column removed was NULL in
every row of its table — 7 upgrades, 175 versions.

They were not all the same thing, and sorting them was the work:

**Superseded.** `config_snapshots` and the `values_snapshot` key into it were a second
home for config history; the real one is the git repository on the config volume, which
gives diffs and rollback for free. An unused snapshot table sitting beside it is exactly
the second source of truth §4.49 warns about, waiting for somebody to wire it up and
disagree with git. Likewise `ess_versions.changelog`, `breaking_changes`, `chart_digest`
and `published_at`: release notes come from the published releases (E32) and dates from
the release index (E43).

**Never built, and one had become worth building.** `upgrade_history.pre_flight` has
existed since migration 003 and never held a row. E55 then added a capacity preflight
whose findings went to the **live log stream and nowhere else** — a WebSocket, gone when
the tab closes. So after this month's outage, "did the panel warn us before we applied
that?" had no answer, while the column designed to hold exactly that answer sat empty
two migrations away.

The distinction that makes it useful is between *absent* and *empty*. A NULL means the
upgrade ran before the check existed, or the check could not run. An empty array means
checked and clean. Collapsing them would let an old upgrade look as though it had
passed a check that did not exist — §4.59's fabricated sparkline, in a different table.
The seven historical rows are deliberately not backfilled for the same reason.

The broader point, after three passes: **a schema is a design document that keeps
running.** Columns outlive the intention that created them, and unlike a stale comment
they look like capability — someone reading `upgrade_history` sees a system that records
its Helm output and its preflight, and neither was true. The fix is the same as for the
constants: count who reads it, not who declared it.

### §4.63 — Paging over an order that does not exist (2026-09-02, agent, etappe 64)

Synapse orders both report queues by `received_ts` alone. Rows sharing a timestamp
therefore have no defined order between two queries, and with offset paging that means a
row can appear on two consecutive pages while another is never returned. The case that
produces equal timestamps is a burst of reports about one incident — the case in which
somebody is paging through the queue looking for something.

Three things could be done and they are not equally honest, which is the substance of
this entry:

1. Sorting each page by `(received_ts, id)` makes what is displayed deterministic.
2. Remembering which page an id was *first* seen on removes duplicates entirely.
3. A row the server never returned cannot be recovered without walking the whole queue,
   which is what `limit` exists to avoid.

Two are fixed and the third is stated, rather than calling the result "paging fixed". A
duplicate is visible and confusing; a skip is invisible and worse, and it is the one
that cannot be closed from this side.

The keying is the part worth remembering. The obvious implementation is a set of ids
already seen, and it is wrong: paging *back* to an earlier page would then blank it,
because every row on it has been seen. Keyed on which page first claimed an id, going
back works and duplicates still vanish.

**And the typecheck was no help at all.** The first version put the hook call after
`if (!connected) return`, which is a conditional hook — React counts hooks per render,
so the count would have changed the moment the connection came up and thrown "Rendered
more hooks than during the previous render". `tsc` was perfectly happy. The rule that
caught it is the same one this file keeps arriving at from different directions: a green
check answers its own question, never the one being asked (§4.40, §4.48, §4.52).

The pure part — the ordering and the first-seen decision — was then moved to `lib/`, so
it is testable without rendering a component. Seven tests, including the one that would
have caught the naive seen-set: going back to page 0 must still show page 0.

### §4.64 — Erasure, and what it does not erase (2026-09-02, agent, etappe 65)

P2-25: MatrixCtrl could not GDPR-erase an account at all. E28 sends `skip_erase: true`
on every deactivation — a deliberate choice, because a one-click irreversible erasure is
the wrong default. But "not the default" and "not available" are different things, and
for a homeserver in the EU the second is a compliance problem: an operator answering a
legal request had to leave the panel.

The feature is four lines. The etappe is what the confirmation says, and that came from
reading Synapse rather than the word "erase".

`deactivate_account.py` with `erase_data` true deletes displayname, avatar and custom
profile fields — with Synapse's own caveat that they *"may persist as historical state
events in rooms"* — and marks the user erased. The flag is then consulted in
`visibility.py`:

```python
if sender_erased and not membership_result.joined:
    event = prune_event(event)
```

**Message content is pruned only for viewers who were not joined at the time.** Everyone
who was in the room still reads it, permanently. Uploaded media is untouched, and the
old display name survives inside historical state events.

So a button labelled "Löschen" that stops at the verb would be a false statement made by
software — the same shape as a 200 that changed nothing (§4.45), a check that checked
nothing (§4.48), and a sparkline that invented its past (§4.59). The difference here is
that the false statement would be made to someone answering a legal request, who may
still need to redact events or delete media and would have been told the job was done.

The confirmation therefore has three paragraphs — removed, remaining, irreversible —
and the dialog renders `pre-line` so they stay three paragraphs. A wall of text is
skimmed, which defeats the reason for writing it.

Two further decisions worth keeping: erasure is its own action rather than a checkbox on
deactivate, because a parameter lets someone perform an irreversible operation while
reading a label that says "deactivate"; and it is offered whether or not the account is
already deactivated, since an erasure request usually arrives after the account is gone.
Bulk erasure is deliberately absent — a loop over a selection is how an irreversible
action reaches the wrong rows. Affects S13.

### §4.65 — Stop needing the emulator (2026-09-03, agent, etappe 66)

P2-7 has said "releases are amd64 only" since August, with two failed multi-arch
attempts behind it and a suspicion recorded: the runtime stage's `apk add` running under
QEMU. The suspicion was right, and reading the Dockerfile showed how narrow the problem
actually was.

Every stage that does work is already native — the frontend and backend stages are
pinned to `$BUILDPLATFORM`, and Go cross-compiles with `GOARCH=${TARGETARCH}`. Exactly
**one instruction in the whole build** needed the target architecture to execute:

```dockerfile
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
```

A package install fetching two files. So the fix was not to make emulation work — the
thing two previous attempts tried — but to stop needing it:

- `tzdata` → `import _ "time/tzdata"`, one line, about 450 KB of binary
- `ca-certificates` → a PEM text bundle, architecture-independent by nature, copied out
  of the builder stage that already has one and already runs natively

The runtime stage now runs no command at all. Assembling an arm64 image is copying
files, which needs no emulator, so the workflow asks for both platforms in one manifest
rather than the per-architecture runners and manifest merge the backlog had planned.

**QEMU is deliberately not set up in that job.** Adding it would work and would also
hide the property this etappe created: if someone later adds a `RUN` to the runtime
stage, the arm64 build should fail loudly rather than quietly become a slow emulated
build that works until it does not.

What is verified and what is not, stated rather than blurred: this host offers
`linux/amd64` only — `docker buildx inspect` lists no other platform, there is no QEMU —
so **the arm64 image could not be built or tested here at all**. What was checked is
that removing the packages did not break the things they provided: the built image
carries the CA bundle (179 359 bytes, identical to the builder's), the binary contains
the zone database, and the deployed pod completed OIDC discovery over HTTPS, which is a
TLS handshake against MAS using exactly those copied certificates. Whether arm64
publishes is the next tagged release's answer, and the README says so instead of
claiming support that has not been demonstrated.

### §4.66 — A backup that says what it is not (2026-09-03, agent, etappe 68)

S14 has listed "backup/restore" as project scope since the beginning. E67 checked and
found **nothing behind the word** — the only matches in the code were a one-off
config-migration directory and a comment. For a tool whose job is running a homeserver,
that was the one remaining gap where a failure costs data rather than time.

What MatrixCtrl can capture correctly, measured rather than assumed: its own database
(14 MB) and the config repository (968 KB, 71 files, **with git history** — every ESS
value and every change ever made to it). What it cannot: Synapse's database and media,
which live on volumes this pod does not mount. Reaching those needs a Job with the
volumes attached, which is a different mechanism rather than a bigger loop.

**So the archive's own manifest names what it does not contain**, and so does the card
that offers it. An operator who believes they hold a backup of their Matrix server and
learns otherwise during a restore is precisely the failure this project keeps writing
up — something that looks complete and is not (§4.45). Putting that sentence only in
documentation would be the §4.60 mistake: prose that stops being read long before it
stops being true.

**No schema in the archive.** It carries data per table; a restore rebuilds the schema by
running the migrations, which already *are* the schema's definition and were tested from
zero as recently as E67. A copy inside every archive would be a second source of truth
that ages differently from the first (§4.49) — and it would restore onto the schema the
backup was taken on rather than the current one, which is the opposite of what anyone
wants.

Two implementation notes worth keeping, both about not weakening something else to make
this convenient:

**The download is `fetch` + blob, not a link.** A navigation cannot carry an
Authorization header, and the two obvious workarounds were both worse: putting the
session token in the URL is exactly the leak E35 removed, and widening the single-use
WebSocket ticket to cover ordinary downloads would loosen a mechanism deliberately built
narrow. The browser buffers the archive, which is a property of authenticated downloads
rather than a design decision — the server still streams.

**Telemetry is labelled, not omitted.** `rtc_samples` and `node_samples` are 31 000 of
the 31 100 rows, and losing them costs history rather than function. They are included
anyway — a backup that quietly drops things is the defect, not the feature — but marked
`regenerable` in the manifest so a restore can make an informed choice.

Restore is deliberately a separate etappe. Taking a backup is harmless; writing one back
destroys what is there, and shipping both together would mean the dangerous half arrives
less carefully tested than the safe one.

### §4.67 — "The same homeserver" is two different sentences (2026-09-04, operator + agent, etappe 69)

E68 shipped a backup whose card read *"not included: the homeserver"*. The operator
pushed back and was right:

> aber wäre es dann nicht schlau beim setup, backup einspielen und er baut es genau so
> auf — also gleiche versionen gleiche config

The archive holds `hostnames.yaml`, `rtc.yaml`, `tls.yaml` and every other slice with git
history. That is not "not the homeserver". It is everything needed to stand the same one
up again — the same server name, the same ingress hosts, the same TLS issuer, the same
RTC settings, and the hooks that keep the SFU patched. What it cannot return is what
users made: accounts, rooms, messages, media.

E68 collapsed those into one pessimistic sentence. **Understating a feature is a smaller
error than overstating one, and it is still an error** — an operator reading "does not
contain the homeserver" would reasonably conclude the archive is not worth taking before
a rebuild, which is exactly when it is worth most. The manifest now carries both lists,
`restores` and `not_included`.

The question also exposed a real gap: "gleiche Versionen" was not possible. The manifest
recorded MatrixCtrl's own version and nothing about ESS, so a restore would have
reproduced the configuration onto whatever chart version happened to be newest — a
different deployment wearing the same hostnames. It now records the release name, chart
version and revision, and the restore screen shows them **before** anything is written.

**Restoring across a schema change** is the case the design exists for. The archive
carries data and no schema (§4.66), so a backup taken at schema N lands on schema N+1:
columns are matched by header name, ones since dropped are skipped, ones since added take
their defaults. An archive from before migration 017 therefore restores onto today's
schema without special handling — which is the whole return on not carrying a schema.

Three guards worth keeping:

- `schema_migrations` is **never** restored. It is the database's bookkeeping about
  itself, and a backup's copy would report migrations that have run as pending, or worse,
  pending ones as done.
- The database goes back in one transaction, so a failure part-way leaves it as it was.
  The config repository is written beside the live one and swapped for the same reason.
- The two halves can still end up from different moments — config restored, database
  not — and the error says so plainly instead of reporting a clean failure. An operator
  who does not know which half moved cannot choose what to do next.

### §4.68 — A navigation entry that pointed at nothing (2026-09-05, operator + agent, etappe 70)

The sidebar has carried a **Backup** item under "Betrieb · Day-2" since the design system
shipped, greyed out because `NavRow` disables anything without a route. E68 and E69 then
built backup and restore onto `/system`.

> ok aber auf der website ist backup noch ausgegraut ...

So the feature existed for two etappes and the label promising it stayed dark. **A
disabled item beside a working feature is worse than no item at all** — an absent entry
says nothing, a greyed one actively reports that the thing is not there. It is the same
family as §4.61's declared-but-unwired constants, seen from the user's side rather than
the code's: something rendered, nothing behind it. The sweep that found those looked at
constants, endpoints and columns. It did not look at navigation.

### §4.69 — The half that makes it the same server (2026-09-05, etappe 70)

The operator's second question answered itself as they asked it:

> aber wiso sollte man volumes sichern, ahh wegen chats und so, ja dann braucht man eig ..

Measured: 4 accounts, 9 rooms, **19 057 events**, 67 media files — 304 MB of database and
40 MB of media. E68's archive rebuilds the deployment; this is what makes the result the
same server rather than a fresh one wearing the same hostnames.

What is reachable decided the scope, not what would be nice. Synapse's database is
network-reachable and its password sits in a secret this pod may read, so it ships. The
media is on a PVC only the Synapse pod mounts, so it does not — and that is in the
export's own manifest, where E68 and E69 put their limits too.

**Consistency is the part worth getting right.** A live database read table by table
yields tables from different moments: a room that exists with no creation event, a
membership pointing at an event that is not there. The whole export therefore runs inside
one `REPEATABLE READ` transaction, which is what `pg_dump` does and for the same reason.
Without it the archive looks complete and is subtly torn — this project's recurring
failure wearing new clothes, and one that would only be discovered while restoring.

**No restore button for it.** Writing a homeserver database back needs Synapse stopped;
doing it while the server runs corrupts what is there. The export is a file to be
restored deliberately with `psql`. §4.39's rule about actions without a safe inverse
applies at its strongest here — the thing being overwritten is every conversation on the
server.

Two smaller decisions: the password is read per request rather than held on the handler,
because a credential kept for the life of a process outlives the reason it was needed;
and a failed connection is logged rather than echoed, because the error carries the DSN
and the DSN carries the password.

### §4.70 — The most reassuring property, never stated (2026-09-05, operator + agent, etappe 71)

The operator, after the backup work landed:

> aso und natürlich auch dann schönner und besser speichert als ich das gemacht habe,
> bei liegts in irgendeinem ordner … am beste wäre das wenn es dann in opt landet wie
> art compose oder halt in volume ist idk

I read it as being about backups. It was about the configuration:

> bruder ich meinte die configs!!!

And the answer is that MatrixCtrl already does what they were asking for. Their values
sit in `/root/ess-config-values`; MatrixCtrl's live in `/data/config-repo` on a dedicated
PVC, as a **git repository** with history, diffs and rollback. The old folder is the
*seed*, read once at first start and never again.

**They had been editing configuration for months without knowing that.** Ten screens
about configuration and not one of them names the volume it is kept on. This is a
documentation failure of a particular kind — not a stale sentence but an absent one, and
absent about the property most worth knowing. The panel's answer to "where does my
configuration live" was silence, so the operator kept the mental model they arrived with.

The fix is one line in the editor's header: the path, that it is versioned, and how many
versions there are, with the seed named in the tooltip so the old folder stops looking
like the source of truth.

**The second half was a real gap.** `MATRIXCTRL_CONFIG_REPO` is read from the environment
by the Go code and the chart hardcoded it, so "einstellbar" was half-true: the volume's
size and storage class were values, its path was not. That is §4.61's shape once more —
something the code supports and nothing exposes.

Fixing it needed care worth recording. The path appears in **four** places in the
template: the env var, the mount, the ownership fix in the init container, and the
second container's mount. Changing only the env var — which is what the first pass did —
would have produced a pod whose configuration path points where no volume is mounted:
strictly worse than the hardcoded value, because at least that one agreed with itself.
A half-applied parameterisation is a new bug wearing the old one's clothes.

Deliberately not done: moving the configuration to `/opt` on the host. It is what "like a
compose setup" means literally and it would be a step backwards — a hostPath ties the pod
to one node and survives no migration, which is exactly what a volume avoids. The request
underneath was *knowing where it is*, and that was the part missing.
