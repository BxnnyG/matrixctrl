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

The only instance with real data is production, and its screenshots show the node
name `infra-core-prod-matrix-01` on the Dashboard and System pages. That is the
exact string [§4.14](../DESIGN.md) rewrote 39 commits to remove, so it cannot go
into a public repository through the back door of a PNG.

All nine existing screenshots were reviewed individually. Result: that hostname is
the **only** sensitive string. The config repo's commit author renders as
`MatrixCtrl`, PVC UUIDs are meaningless, and `bxnny.de` is the operator's public
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

## Outcome

_(filled in when the etappe closes)_
