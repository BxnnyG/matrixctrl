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
`MatrixCtrl`, PVC UUIDs are meaningless, and `example.com` is the operator's public
domain and deliberately fine.

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

## Handover — the settings this etappe cannot apply

No `gh`, no API token, SSH remote. All of the following are owner-only clicks on
`github.com/bxnnyg/matrixctrl`. Tracked as **P2-13** so they are not silently lost.

1. **Topics** — repo page → ⚙ next to *About*:
   `matrix` · `matrix-synapse` · `element` · `matrix-homeserver` · `kubernetes` ·
   `helm` · `self-hosted` · `golang` · `react` · `agpl`
   *Highest-value item on this list. Six words decide whether the project is
   findable at all.*
2. **Homepage** — same dialog: `https://matrixctrl.example.com` if it should be
   public, otherwise leave empty rather than pointing at something private.
3. **Description** — same dialog, one line:
   *Day-2 admin UI for self-hosted Matrix / Element Server Suite — config,
   Helm upgrades that keep your patches, and admin-only Matrix login.*
4. **Release for `v0.1.15`** — Releases → *Draft a new release* → choose the
   existing tag `v0.1.15` → body from [CHANGELOG.md](../../CHANGELOG.md). The tag
   exists; the page every visitor checks is empty.
5. **Tabs** — Settings → Features: **Wiki off**, **Projects off**,
   **Discussions on**. Three empty tabs read as an abandoned project. Discussions
   is the one worth keeping: a wiki would become a second documentation that rots
   next to `docs/`.

## Outcome (2026-08-01)

Done, except the five owner-only settings above.

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
- **Forward-fixing is not removal.** The tip no longer contains the string, but the
  blob stays reachable through the commit history until the history is rewritten —
  which is exactly the P0-1b situation, now with a second source. Whether to rewrite
  is the operator's call; a rewrite makes the object unreachable but does not delete
  it from GitHub, so the prepared Support purge request has to cover it either way.

### Not done, and why

The five GitHub settings in the handover above. No `gh`, no token, SSH remote —
so they are written down as an ordered list and tracked as P2-13 rather than
claimed.

### Regression checks (S11)

This etappe ships no image, so nothing was deployed. Checked anyway, after the
work: ESS 200 on `/_matrix/client/versions`, MatrixCtrl 200, `hostNetwork=true`
on the SFU deployment, `externalTrafficPolicy=Local` on the three SFU node-port
services. Config-write and login paths were not touched by any change here.
