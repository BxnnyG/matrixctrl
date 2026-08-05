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
| S6 Setup & onboarding | ⏳ ⅞ (E15) | Greenfield deploy proven on an empty cluster after fixing 4 defects; only connect-OIDC untested (needs public DNS) |
| S7 UI shell & design system | ✅ (E11) | Tokens + `mc.tsx`; all functional screens migrated |
| S8 Packaging & release | ✅ (E16, E18) | A tag publishes image, chart **and** the GitHub Release, whose notes the workflow cuts from `CHANGELOG.md` itself (§4.17, P2-18). `0.1.16` released, deployed and verified; repo topics, description and tabs configured, homepage deliberately empty |
| S9 Verification & CI | ✅ (E13, E14, E18, E20) | CI on push/PR, 26 frontend tests, 13 backend tests (E14), headless-browser route check, gofmt gate (E18); the route check also produces the README screenshots (§4.18) |
| S10 Audit trail | ✅ (E17) | Middleware over the whole authenticated group, keyset-paginated read endpoint, UI at `/audit`. **This row previously claimed "table + middleware write" — the middleware never existed; 0 rows after two months.** Open: retention (P2-19) |
| S11 Regression safety net | ♾ Rule | Four invariants, checked before every ship — never "finished". #4 ("the SFU patches survive a Helm upgrade") is no longer only a checklist line: E21 checks it continuously and shows it on the dashboard, after it was broken by an upgrade run outside MatrixCtrl and nobody noticed for a day |
| S12 Centralisation | ♾ Rule | "More than one place?" → shared package. Re-decided per change |
| S13 User & room management | ⏳ not started | Phase 2 — parked behind S6 ([VISION.md](VISION.md)) |
| S14 Day-2 operations (RTC/TLS/backup) | ⏳ ⅓ (E19) | RTC: the ports to forward read live from the Services, and inbound reachability stated as **unknown** rather than omitted (P2-9). TLS/DNS and backup not started |
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
`web/scripts/verify-ui.mjs` drives headless chromium over all nine functional
routes after a deploy, failing on console errors or an unmounted root.
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
