# Etappe 18 — First impression

**Date:** 2026-08-01 · **Systems:** S8, S9, S12 · **Addresses:** P2-10 … P2-14

## The problem, from the visitor's point of view

Everything shipped so far was aimed at the operator who already runs this. Nobody
has ever looked at the repository as a **stranger who arrives with no context**,
and that view is currently poor in three specific ways:

1. **The product is invisible.** The README contains exactly one image — a licence
   badge. For a tool whose entire value is a user interface, a visitor has to
   imagine it. Etappe 11 built a full design system that no reader has ever seen.
2. **Two facts arrive too late.** The UI ships **German only** (S17, [ROADMAP](../ROADMAP.md)
   Phase 6) and this is stated **nowhere** in the README. And there is no maturity
   signal at all — no visitor can tell that this is two months old with one
   maintainer, while it asks to manage their homeserver. Etappe 15 found that the
   headline feature had never worked; somebody who installed it before that was
   not treated fairly.
3. **The GitHub surface is unconfigured.** No topics (so the repo is
   unfindable by search), no homepage, `v0.1.15` exists as a **tag with no
   release**, no CHANGELOG, no dependency automation, and three empty tabs
   (Wiki/Projects/Discussions) that read as an abandoned project.

Separately, and unrelated to visitors: `internal/api/handlers/helm.go` has grown
to **834 lines** carrying five unrelated responsibilities. Etappe 15's defect #4 —
two fixes that silently cancelled each other out — lived in that file.

## Scope

| In | Out |
|---|---|
| Screenshots in the README, generated reproducibly | Any UI or behaviour change |
| Maturity + German-UI disclosure | Translating the UI (that is Phase 6 / S17) |
| `CHANGELOG.md`, `.github/dependabot.yml` | Cutting a new version tag (separate decision) |
| Splitting `helm.go` — pure code motion | Changing any handler's logic |
| An exact, ordered list of the GitHub settings **the operator must click** | Doing them (no `gh`, no token on this host — see below) |

## Approach

### 1. Screenshots without leaking the cluster

The only instance with real data is production, and its screenshots render the
cluster's node name on the Dashboard and System pages — the exact string
[§4.14](../DESIGN.md) rewrote 39 commits to remove. It cannot go into a public
repository through the back door of a PNG, and it does not belong in this file
either: an earlier draft of this very plan quoted it in prose, which is recorded
below as the etappe's own mistake.

All nine existing screenshots were reviewed individually. Result: that hostname is
the **only** sensitive string. The config repo's commit author renders as
`MatrixCtrl`, PVC UUIDs are meaningless, and the Matrix server name is public by
definition — every federating server already knows it.

Blurring is ugly and a one-off. Instead `verify-ui.mjs` — which already produces a
screenshot per route — gets a **`--redact from=to`** flag that rewrites text nodes
in the DOM before the shot is taken. That makes safe screenshots a **repeatable
command** rather than a manual cleanup somebody forgets next time (S12: the
capability belongs in the tool, not in a habit).

Screenshots land in `docs/img/`, which `.dockerignore` already excludes, so they
never enter an image layer.

They also solve problem 2 for free: a reader **sees** the German UI instead of
being told about it in a footnote.

### 2. Honesty at the top of the README

A short status block directly under the title: what is proven, what is not, that
the UI is German, that there is one maintainer. The material already exists and is
already this blunt — [BACKLOG.md](../BACKLOG.md) and the etappe 15 plan — it has
simply never been visible from the front page.

### 3. `CHANGELOG.md`

Reconstructed from the etappe log in [ROADMAP.md](../ROADMAP.md), which is
accurate and dated. Keep-a-Changelog format. The greenfield fixes from etappe 15
and the `client_name` fix are already committed but unreleased, so they go under
`Unreleased` — which is exactly the honest state and gives the next tag its text.

### 4. `helm.go` → four files

Pure motion, no edits to any function body:

| File | Contents |
|---|---|
| `helm.go` | handler struct, constructor, `GetRelease`, `GetHistory`, `Rollback`, `ListVersions`, shared helpers |
| `helm_upgrade.go` | `Upgrade`, `ApplyConfig`, `GetUpgradeStatus` |
| `helm_setup.go` | `DeployESS`, `ConnectOIDC`, `buildMASClientConfig`, `greenfieldHostnames`, `greenfieldRemovals` |
| `helm_stream.go` | `upgradeStream` and its methods, `startProgress`, `formatElapsed` |

Same package, so this cannot change behaviour — but it is verified, not asserted
(below).

### 5. What this etappe cannot do itself

There is **no `gh` CLI and no API token on this host**, and the remote is SSH. So
topics, homepage, tab visibility and the GitHub Release for `v0.1.15` are written
up as an exact click-list for the operator and are **not** claimed as done.

## Mandatory edge-case walk ([PROZESS §1](../PROZESS.md))

Seven of the eight cases concern runtime behaviour that this etappe does not
touch. The two that genuinely apply, because the file split moves the code that
implements them:

- **(1) No ESS / ESS elsewhere** — `DeployESS` and `ConnectOIDC` carry the guards
  that short-circuit differently per case. Their tests
  (`greenfield_test.go`) must pass unchanged after the move, or it was not a move.
- **(6) Both auth modes** — `ConnectOIDC` is the switch between them. Same rule.

Cases 2, 3, 4, 5, 7 and 8 are untouched: no handler logic, no k8s call, no config
write and no hook path changes.

## Risk

Low, and concentrated in one place. The file split is the only change to compiled
code; Go's package model means a same-package move either compiles identically or
does not compile at all. The documentation changes cannot affect a running
instance.

The real risk is the screenshots: a leaked internal identifier is not revertable
once pushed to a public repo. Mitigated by generating them through the redaction
flag **and** re-reading every produced image before the commit.

## Definition of done

- Screenshots in the README, generated by a documented command, and **every image
  visually re-checked** for the hostname before committing
- Maturity and German-UI disclosure visible without scrolling
- `CHANGELOG.md` and `.github/dependabot.yml` exist
- `helm.go` split; `go build ./...`, `go vet ./...` and `go test ./...` pass, and
  the route table is unchanged
- Four regression checks (S11) green
- ROADMAP row, DESIGN §1b, a §4 decision entry, BACKLOG updated
- The GitHub click-list handed over, explicitly marked as not-done

## Repository settings — done 2026-08-01

Topics and description by the operator; the rest applied through a short-lived
fine-grained token (Contents + Administration), then revoked:

| Setting | Result |
|---|---|
| Topics | 10, from `matrix` to `self-hosted` — the repo is findable at all now |
| Description | one line, set |
| Homepage | **empty, as a decision** — see below |
| Wiki / Projects | **off** |
| Discussions | **on** |
| Releases | `v0.1.15` and `v0.1.16` created from `CHANGELOG.md` |

Everything below is what the reasoning was.

**Homepage stays empty**, decided rather than skipped. The only candidate is the
operator's own MatrixCtrl instance, and the URL of a live admin panel is not
repository metadata — the same reasoning that removed it from
[ROADMAP.md](../ROADMAP.md) under P0-1c.

**The two release pages were the last hand-made publishing step**, which is
exactly the failure §4.17 removed from the artefacts. They exist now, but the
lasting fix is that `release.yml` cuts the `## [x.y.z]` section out of
`CHANGELOG.md` and posts it with the built-in `GITHUB_TOKEN` — so from the next
tag on, nobody needs a personal token and nobody has to remember (P2-18).

## Outcome (2026-08-01)

Done. The repository settings above are applied; `0.1.16` is released, deployed
and verified.

### The screenshots

Nine routes captured from production, two redactions fired: the node name on
Dashboard and System, and a pod IP that appeared in a liveness-probe event on the
Dashboard. The second one was **not** predicted — it was found by looking at the
rendered image, which is the whole reason the flag is a backstop rather than the
control. Every one of the nine was then read individually before committing.
What remains visible: `example.com` (the operator's public domain, deliberately
fine), PVC UUIDs, and generic Kubernetes object names.

The redacted text renders in the same font as everything else, so the images do
not look doctored — which matters, because a screenshot that looks edited is
worth less than no screenshot.

### The split, proven rather than asserted

All 26 declarations present exactly once across the four files, and every body
byte-identical apart from `gofmt` alignment in two of them. The alignment noise
existed because the original file **was never gofmt-clean** — and neither were
nine others. That is now a CI gate.

### Two defects found by looking at the product

Reviewing the screenshots surfaced two things no test covers, both now in the
backlog:

- **P2-16** — the 26.5.1 → 26.7.2 upgrade still reads `running-hooks` a day after
  it finished. The release is revision #22 `deployed` and both hooks report OK, so
  the screen that answers "did that upgrade finish?" is lying.
- **P2-17** — `/helm/history` shows the app name where every other screen shows a
  page title.

Neither is severe. Both are the kind of thing that stays invisible for months
while you look at the feature you are building rather than at the product, and
both cost one glance at a picture.

### The mistake this etappe made

The plan chapter above originally **spelled the node name out in prose** — while
explaining why that name must never reach a public repository. It was committed
in `cc60076` and pushed before the check that found it (a `git grep` across
tracked files, run after the screenshots were verified).

Two things are worth recording rather than quietly fixing:

- **The control that caught it was the last one, not the first.** Every safeguard
  built here pointed at the images: the redaction flag, the per-route replacement
  count, the individual review of all nine PNGs. The leak went through plain text
  in the document describing the safeguard. A rule enforced on one channel gets
  routed around by the channel nobody instrumented.
- **Forward-fixing is not removal.** Removing the string from the tip leaves the
  blob reachable through the commit history. The operator authorised a full rewrite
  the same day, and the sweep it triggered — every blob in the history, not just the
  current tree — **found more than the trigger**: the admin panel's own URL in five
  paths including the packaged chart's default values, and the five ESS hostnames
  derived from the server name in thirteen more, among them `internal/auth/oidc.go`,
  a database migration and the committed frontend bundle. Looking for one string
  found three classes; only one of them was the one anyone was looking for.

All 83 commits were rewritten, verified (0 matches across 948 reachable blobs, tip
byte-identical, tests green), and force-pushed. `v0.1.15` was retagged and its
release re-published from the cleaned tree — the previously published image
carried those hostnames inside its frontend bundle.

**What the rewrite could not reach.** Dependabot — enabled two hours earlier by
this same etappe — had already opened a pull request from the old master. Its
branches disappeared with the force-push, but GitHub keeps `refs/pull/*`
permanently, so pre-rewrite commits stay fetchable by SHA. Only GitHub Support or
deleting and re-creating the repository removes that, and the operator declined
the support route. Recorded in P0-1b/P0-1c rather than left implied.

The control that was missing is now built and is described in
[§4.19](../DESIGN.md): a `pre-commit` hook and a CI step reading their patterns
from outside the repository, tested against a planted string rather than only
against a clean tree.

### Not done, and why

The five GitHub settings in the handover above. No `gh`, no token, SSH remote —
so they are written down as an ordered list and tracked as P2-13 rather than
claimed.

### Shipped and verified

`0.1.16` was cut, published to GHCR, and rolled out to the production instance
(release revision 18, image `0.1.16`). All four S11 checks after the upgrade:

1. **ESS reachable** — 9 pods Running; Matrix `/_matrix/client/versions`, Element
   Web and MatrixCtrl all 200.
2. **Config intact** — 18 section files, 2926 comment lines still present in the
   config repo.
3. **Admin login** — `oidc/available` reports enabled, `oidc/redirect` 302s to MAS
   `/authorize`, and a protected route without a token still returns 401.
4. **SFU patches survived the upgrade** — `hostNetwork=true`, and
   `externalTrafficPolicy=Local` on the three SFU node-port services.

Then the headless-browser check against the deployed build: all nine routes
rendered cleanly, no console errors, no failed requests.
