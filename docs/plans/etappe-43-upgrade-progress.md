# Etappe 43 — The upgrade window shows a clock, not progress

Three findings from the operator, all in the Helm area, reported while running the
real 26.7.2 → 26.8.0 upgrade. Plus two defects found while reproducing them.

## What was reported

> "entweder dauert die anzeige extremst lange mit dem pre release oder sie werden
> nicht zwischen den releases angezeigt"
>
> "qol features like info über release neben den releases oder so"
>
> "die anzeige mit dem upgrade ist eher naja — der cursor verschwindet die ganze
> zeit nach links ausm frame, und man sieht auch nicht genau was passiert, like
> prozent angabe oder ob er gerade beim restart oder beim pullen ist. das könnte
> deutlichst schöner"

## Reproduction

**The version list.** `/api/v1/helm/versions` answers in 0.9–1.3 s (measured from
the container log, six calls), so "extremst lange" is not the endpoint. The second
half of the operator's guess is the right one. A full walk of the GHCR tag list —
3 pages, 2740 raw tags — yields exactly 79 versions: **67 releases and 12
pre-releases**. Every one of the twelve is a `0.x.y-dev` tag:

    0.0.0-dev 0.1.1-dev 0.2.1-dev 0.3.1-dev 0.3.2-dev 0.4.1-dev
    0.4.2-dev 0.5.1-dev 0.6.1-dev 0.6.2-dev 0.7.1-dev 0.7.2-dev

None of them has a GitHub release; the release index lists `0.1.0`, `0.2.0`,
`0.3.0` and so on, plain. They are development builds from the chart's first months
(Dec 2024 – early 2025) under an older naming convention — the same category as the
`<version>-sha<40 hex>` tags `buildTagRe` already drops, just spelled differently.

E42 made the toggle honest: it now names the count and says when they fall outside
the visible window. That was the right fix for the wrong question. A button that
reveals twelve 2024 dev builds of a chart currently on 26.8.0 offers the operator
nothing at all, and E42 spent a paragraph of UI explaining why a useless control
looks useless.

**The version rows carry no information.** `parseReleaseTag` returns
`VersionInfo{Version, Prerelease}` and never sets `PublishedAt`, so the date field
the list already renders (`web/src/routes/helm/index.tsx:150`) is empty on every
row, for every version, always. The list is 25 monospace version numbers and
nothing else. The GitHub releases index answers this in **one** request:
`/repos/element-hq/ess-helm/releases?per_page=100` returns all 67 releases with
`published_at`, `name` and `body`, single page, no pagination.

**The upgrade window.** The log box (`upgrade.tsx:240`) sets `overflowY: auto` and
nothing for X, which computes to `overflow-x: auto`. The pinned-tag warning is a
~200-character line, so the box scrolls horizontally; the auto-scroll on each new
line only sets `top`, so the view stays scrolled right while the cursor `▋` sits at
x=0. That is the operator's "cursor verschwindet nach links ausm frame", exactly.

For content: the stream is a flat list of strings. Between "Loaded 18 config
slices" and the end there is one line every 30 s that says how much time has
passed, plus a probe line when the diagnosis *changes*. On a healthy upgrade the
probe says `6 Pods startet noch` once and then nothing for the remaining two
minutes, because E31's dedupe suppresses an unchanged line. So the operator's
"man sieht nicht was passiert" is precise: on a **healthy** upgrade the panel is
deliberately quietest.

Measured against the live 26.8.0 upgrade: eight workloads roll, each pod pulls an
image and passes a startup probe, and the whole sequence is legible from the API —
`ess-synapse-main-0` alone spent 25 s failing its startup probe with
`connection refused`, which is normal and which the panel never mentioned.

## Two defects found on the way

**`imagepin.Describe` emits broken German.** `internal/imagepin/imagepin.go:136`
interpolates `plural(n, "", "n werden")` into a template that still contains
`wird`, so the plural branch reads *"Diese Komponenten werden wird vom Upgrade
nicht mit aktualisiert."* It is in the operator's paste. Singular is fine, which is
why it survived.

**`tsc --noEmit` type-checks nothing.** `web/tsconfig.json` is `"files": []` plus
two project references, so the command documented in CLAUDE.md and PROZESS.md exits
0 regardless of the code. E41 shipped four real `TS2741` errors past it; they only
appeared eleven minutes into the E42 image build, which is why **0.1.43 was never
built** and why E41 and E42 are still undeployed. The root cause of the errors
themselves: `rooms.tsx` lets TypeScript infer `validateSearch`'s return type, which
gives `{ error: string | undefined }`, and TanStack reads a nullable key as
required — so every `<Link to="/rooms">` demanded a `search` prop.

## What this etappe does

### A. The upgrade window says what is happening

A structured progress channel beside the log, not instead of it. The log is the
audit trail and the reconnect path works; it stays.

- `upgradeStream` gains a **latest-snapshot** progress field, broadcast as
  `{"type":"progress",…}`. It is deliberately *not* appended to `logs`: the buffer
  stays replay-cheap, the log gate is untouched (it only counts `type:"log"`), and
  `ws.ts` already ignores unknown message types, so an old client sees no change.
- Phases (`config → apply → rollout → hooks → done`) render as a stepper, so
  "what is it doing" is answerable at a glance rather than by reading upward.
- `k8s.WorkloadRollout` reports each Deployment/StatefulSet in the ESS namespace as
  desired/updated/ready. That is the honest denominator for a percentage: it is
  what `helm --wait` is itself waiting for, and unlike a pod count it does not
  churn as old pods terminate.
- Per-component state distinguishes **pulling an image** from **starting** from
  **failing**. `ContainerCreating` covers a pull and a mount alike, so the pull is
  read from the kubelet's `Pulling` events — one field-selected List per tick.
- Progress ticks every 3 s instead of 30 s. They no longer go through the log
  buffer, so this costs a WebSocket frame, not a growing array.

The assembly is a pure function in `internal/rollout` (which already owns "what is
this upgrade waiting for"), fed by `internal/k8s`, and tested without a cluster.

### B. The version list carries information

- `VersionInfo.PublishedAt` filled from the GitHub releases index, one cached
  request for all 67. The unauthenticated limit is 60/h and the response header
  showed **52 remaining** during this investigation, so the cache has a TTL and
  serves stale on failure — dates are decoration, and decoration must never be able
  to empty the budget the release-notes fetch depends on.
- A row expands to its release notes in place. The endpoint and its per-version
  cache already exist (E32); the markdown renderer moves out of `upgrade.tsx` into
  `web/src/components/Markdown.tsx` because two screens now need it (§3).

### C. `-dev` tags are build tags

They are dropped in `parseReleaseTag` next to the `sha…` tags, with the reasoning
written down. The toggle and E42's explanatory paragraph go with them — but the
`Prerelease` field stays, and the toggle still appears if a genuine pre-release
(`26.9.0-rc.1`) ever ships. This project has never published one; that is a fact
about today, not a rule.

### D. The two defects

`Describe` builds the whole sentence per branch. `rooms.tsx` annotates its return
type. CLAUDE.md and PROZESS.md say `tsc -b --noEmit`, with the trap written down
next to the `--set image.tag` one, since it is the same shape of failure: a command
that reports success without doing its job.

## Definition of done

- A real upgrade shows a stepper, a ready-count, and per-component state — verified
  against the live cluster, not a mock
- "pulling" is distinguishable from "starting" in that view
- No horizontal scrolling in the log box at any width; the cursor stays visible
- Every version row shows a date; a row expands to its notes
- No `0.x-dev` version anywhere in the list, and no toggle for them
- The plural branch of the pinned-tag warning is grammatical German
- `tsc -b --noEmit` is green **and** `make docker` completes — the first no longer
  implies the second was ever run
