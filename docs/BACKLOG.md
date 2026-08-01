# Backlog & reflection (2026-07-31)

Sorted by **impact**, not chronologically — that is the [ROADMAP](ROADMAP.md).
This file says WHAT is worth doing and WHY.

## 0. Honest assessment — where does this project actually stand?

**Strong.** The differentiator works, and it works in production. Config editing
with preserved comments, git history, Helm upgrades whose SFU patches survive —
that is the part nobody else builds, and it is done rather than promised. The
architectural decisions (never shell out, go-git, one file per ESS section) were
made early and have been followed consistently; there is no drift between what
`CLAUDE.md` claims about the architecture and what the code does. For a two-month-old
single-maintainer project, the documentation is unusually honest — `SETUP.md`
writes down the bootstrap paradox instead of pretending it away.

**The uncomfortable truth (as written this morning).** All 39 commits fell into
four days, 2026-05-27 to 2026-05-30. Since then two substantial feature batches
had shipped — the design system and the observability work — and **neither was
committed**. 107 changed files existed in one working tree on one host, plus a
container image. If that host had died, two months of work was gone and the
public repo still showed May. Not a process gap, a data-loss risk.

Underneath it: nothing automated enforced anything. No CI, so the definition of
done was upheld by memory alone. Zero frontend tests, while `CLAUDE.md` claimed
"Vitest + Testing Library" — documentation asserting a safety net that does not
exist is worse than admitting there is none.

> **Update 2026-07-31 (later the same day).** Etappe 13 addressed the first two:
> the work is committed in nine reviewable slices, CI runs on every push, 22
> frontend tests cover the functions that had already broken in production, and a
> headless-browser check walks all nine routes after a deploy. What follows is
> unchanged.

The published artefact is still not the running one: the chart says `0.1.0`, the
running image is `0.1.14`, so a stranger following the README installs something
two months stale.

And the product claim remains unproven. 3 stars, 0 issues, no external users. The
"works for anyone" promise of Phase 1.5 rests entirely on a greenfield path that
**has never been executed once**, and our own instance structurally cannot test
it — ESS already exists, so the guards short-circuit.

**The biggest gaps, by impact:**

1. ~~Two months of work exists in exactly one place.~~ *Closed 2026-07-31.*
2. ~~Nothing enforces the definition of done.~~ *Closed 2026-07-31.*
3. **The main product claim is untested.** Greenfield has never run end to end.
   This is now the top gap.
4. **The public install path is wrong.** README → chart 0.1.0 → two months behind.
5. ~~The git history leaks the internal cluster hostname.~~ *Closed 2026-08-01
   (§4.14) — with one residual: GitHub still serves the old objects by SHA
   (P0-1b).*

**In short:** the safety net exists now. The next thing worth doing is proving the
greenfield path on a throwaway cluster — everything the project promises to
strangers depends on a code path nobody has ever run.

## 1. P0 — urgent

- ~~**P0-1 · Git history exposes the internal cluster hostname.**~~ **Done
  2026-08-01** (§4.14). The scope was larger than this entry claimed: besides the
  author of 39 commits, 30 commits carried the hostname **and the node's private
  IP** in `CLAUDE.md`. All 51 commits were rewritten with `git filter-repo` and
  force-pushed; the HEAD tree hash is unchanged, so no file content moved.
- **P0-1b · GitHub still serves the pre-rewrite objects by SHA.** The force-push
  removed the old commits from the branch, but `…/commits/<old-sha>` and the
  contents API still return them, so the hostname and IP remain fetchable by anyone
  who knows a hash. This is normal GitHub behaviour — unreachable objects survive
  until their garbage collection runs.
  *Fix:* ask GitHub Support to purge the unreachable objects and cached views for
  this repo. There were no forks and no PRs, so nothing else pins them; support is
  the only lever. Alternative (heavier, operator's call): delete and re-create the
  repo, which loses the stars and the URL's history.

  *What was exposed, objectively:*
  - **Exposure window:** 2026-05-27 → 2026-08-01, roughly **nine weeks**, in a
    public repository. Cloning and indexing of public repos is automated and
    continuous, so "nobody was looking" is an assumption, not a finding.
  - **What the values reveal.** The hostname is a structured name: it encodes
    environment, role, service and host index. That is topology — it implies the
    existence of sibling hosts under the same convention and the naming scheme used
    across the estate. The address discloses the internal subnet in use. Together
    they are reconnaissance material: someone who later obtains *any* foothold
    (VPN credential, phished session, a device on the LAN) starts with a map
    instead of having to scan for one, and targeted phishing gets more credible
    with real internal names in it.
  - **What was not exposed.** No credential, key, token or password was ever in the
    history — verified by scanning every blob in all 51 commits for private-key
    headers, provider token formats and password-shaped assignments. The address is
    RFC1918 and not routable from the internet, so it is not directly reachable.
  - **Persistence.** Removal from GitHub does not recall copies. Anything cloned,
    mirrored or indexed during the nine weeks is outside anyone's control, and no
    action taken here can retract it.
  - **What actually invalidates the leak** — as opposed to limiting further spread —
    is renaming the host and changing the address. Purging GitHub only stops new
    disclosure. This is the operator's call and depends on what that rename costs.
- ~~**P0-2 · Etappes 11 and 12 are uncommitted.**~~ **Done 2026-07-31** —
  committed in nine reviewable slices (`9b226c5`…`c8fbd4d`). Tagging is still
  open and folded into P1-3.

## 2. P1 — must-have

- ~~**P1-1 · CI (S9).**~~ **Done 2026-07-31** — `.github/workflows/ci.yml` runs
  `go vet`, `go test ./...`, typecheck, unit tests and the build on push and PR.
- **P1-2 · Greenfield end-to-end test (S6).** Deploy-ess → connect-oidc on a
  throwaway cluster. This is the product claim; it has never run. Needs a scratch
  k3s (kind/k3d would do) because our instance cannot reach the code path.
- **P1-3 · Release coherence (S8).** `Chart.yaml`, `appVersion`, README and
  CONTRIBUTING all say `0.1.0`; the running image is `0.1.14`. Add a release
  checklist that moves all version strings together, tag the repo (there are no
  tags at all), and publish a chart that matches the image.
- ~~**P1-4 · Frontend tests (S9).**~~ **Done 2026-07-31** — 22 Vitest tests over
  the three functions that had each broken in production (version comparison,
  diff parsing, restart-cause mapping). Component tests deliberately still absent.
- ~~**P1-5 · Headless browser for verification.**~~ **Done 2026-07-31** —
  `web/scripts/verify-ui.mjs` drives chromium over all nine functional routes and
  writes screenshots. First run: 9/9 clean.
- **P1-6 · `hooks-failed` is silent (S3).** If a post-upgrade hook fails, the only
  signal is a badge in a UI nobody is looking at. The whole point is that calling
  breaks otherwise. Needs at least a log line at error level, ideally a Matrix
  message to the admin.
- ~~**P1-7 · The upgrade log stream dies mid-upgrade and reports it as failure (S2).**~~
  **Done 2026-08-01 (E14).** All four defects fixed: long Helm operations emit
  elapsed-time progress every 30 s (at all four blocking call sites, not just the
  upgrade one), the socket carries a 20 s application-level heartbeat, the client
  asks the existing status endpoint what happened before reconnecting with backoff,
  and a clean close is no longer reported as an error. Two further defects were
  found while fixing it: dropped subscribers were never removed from `subs` (a leak
  that reconnects would have made routine), and the terminal status was read outside
  the mutex. Original report below.

  <details><summary>Original finding</summary>

  Reported by the operator 2026-08-01 during the real 26.7.2 upgrade: the log
  stopped after `Loaded 18 config slices from config store.` and printed
  `[Verbindung getrennt]`. **The upgrade itself succeeded** (revision 22,
  `deployed`) — only the UI lost it. Four defects stacked:
  1. `helm.Upgrade()` blocks between `helm.go:160` and `helm.go:174` and emits
     nothing, so the socket is idle for minutes. Traefik's default `idleTimeout`
     is 180 s and closes it.
  2. No keepalive on either side. `golang.org/x/net/websocket` has no ping/pong
     at all — a heartbeat has to be an application-level message, or the handler
     must move to a library that supports control frames.
  3. `web/src/lib/ws.ts` never reconnects, and the upgrade status is never
     re-polled over HTTP, so the outcome is unrecoverable once the socket drops.
  4. `ws.onclose` prints `[Verbindung getrennt]` unconditionally — a clean close
     after `done` looks identical to a crash.
  *Why it matters:* upgrading ESS safely is the product. An operator who sees this
  cannot tell a working upgrade from a broken one, and the honest reaction is to
  start intervening in a cluster that was fine.
  </details>
- ~~**P1-8 · The dashboard is slow because every poll re-reads the whole Helm release (S4).**~~
  **Done 2026-08-01 (E14).** `/status` went from ~1.9–3.2 s to **~0.14–0.25 s**,
  measured through the public ingress. Three causes, not one: the Helm read is now
  cached (§4.15), the reads run concurrently, and — found only after the first two
  fixes — client-go's default QPS 5 / Burst 10 was throttling the process against
  itself for a steady ~1.1 s per request (§4.16). Original report below.

  <details><summary>Original finding</summary>

  Reported by the operator 2026-08-01. `/status` runs six calls **serially**, and
  `GetRelease` uses `action.NewGet`, which fetches and decompresses the entire
  release — manifest, hooks, every chart file — out of a 416 KB secret, purely to
  keep the seven scalars in `ReleaseInfo`. Measured on the live cluster:

  | Call | Latency |
  |---|---|
  | list deployments / statefulsets / nodes / pods, metrics-server | 535–965 ms each |
  | **helm release read** | **~4 000 ms** |

  `helm get metadata` and `helm list` were measured too and cost the same ~4 s —
  they all decompress the same secret, so there is no cheaper SDK call. The fix is
  to cache `ReleaseInfo` and invalidate it on upgrade/rollback, and to run the
  remaining calls concurrently. The dashboard polls this every 15 s.
  *Corroborating hint:* `Get` already carries an 8 s `context.WithTimeout` — the
  slowness was known when it was written, and worked around rather than fixed.
  </details>

## 3. P2 — worth doing

- **P2-1 · Audit log UI (S10).** The table and the middleware writes exist; nothing
  reads them back. Cheap, and it turns dead weight into a feature.
- **P2-2 · Build artefacts are committed.** `cmd/matrixctrl/dist` is 32 tracked
  files, so every UI change produces a diff full of hashed bundles and hides the
  real change during review. Generate it at build time and gitignore it.
  *Sharper after E14:* the tracked copy was found **stale** — the frontend fix had
  been built, deployed and verified while the embedded copy still held the previous
  bundle. The container image builds its own frontend, so the running pod was
  correct and nothing looked wrong; only a plain `go build ./cmd/matrixctrl` would
  have embedded the old UI. A tracked artefact that can silently disagree with its
  source is worse than the noisy diffs this entry was originally about.
- **P2-3 · Persist dashboard metrics.** The CPU/RAM sparklines live in memory and
  reset on reload, so "is this getting worse?" cannot be answered.
- **P2-4 · Release notes per ESS version.** `ess_versions.changelog` and
  `breaking_changes` exist in the schema and are never populated — the upgrade
  wizard asks the operator to jump versions with no information.
- **P2-5 · Decide the System page (§4.13).** Open question: the enriched dashboard
  now covers most of it. Keep, merge, or delete.
- ~~**P2-6 · README prerequisites assume a cluster already exists.**~~ **Done
  2026-08-01** — a collapsed `<details>` block gets a bare Debian/Ubuntu server to
  k3s + Helm in three commands, so the happy path stays short for readers who
  already have a cluster. It also names the unstated dependency the install command
  carried all along: `ingress.certIssuer=letsencrypt-prod` assumes cert-manager and
  a matching `ClusterIssuer` exist, which nothing said before.
  *Raised by the operator 2026-08-01.*

## 4. P3 — someday / nice-to-have

- **P3-1 · Read-only role.** Today there is exactly one role: full admin.
- **P3-2 · English UI (S17).** The UI ships German only; the repo and docs are
  English. Phase 6, but it is the single biggest barrier to outside contributors.
- **P3-3 · Bulk config edit across sections.** Changing the server name touches
  several files by hand today.
- **P3-4 · Validate config against the running Synapse,** not only the JSON
  Schema — schema-valid values can still be rejected at runtime.
