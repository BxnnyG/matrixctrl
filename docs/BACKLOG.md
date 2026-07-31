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

**The uncomfortable truth.** All 39 commits fall into four days, 2026-05-27 to
2026-05-30. Since then two substantial feature batches have shipped — the design
system and the observability work — and **neither is committed**. 107 changed
files exist in one working tree on one host, plus a container image. If that host
dies tonight, two months of work is gone and the public repo still shows May.
That is not a process gap, it is a data-loss risk.

Underneath it: nothing automated enforces anything. There is no CI, so the
definition of done the maintainer just committed to is upheld by memory alone.
There are zero frontend tests, while `CLAUDE.md` claimed "Vitest + Testing
Library" until today — documentation asserting a safety net that does not exist
is worse than admitting there is none. And the published artefact is not the
running one: the chart says `0.1.0`, the running image is `0.1.12`, so a stranger
following the README installs something two months stale.

Finally, the product claim is unproven. 3 stars, 0 issues, no external users. The
"works for anyone" promise of Phase 1.5 rests entirely on a greenfield path that
**has never been executed once**, and our own instance structurally cannot test
it — ESS already exists, so the guards short-circuit.

**The biggest gaps, by impact:**

1. **Two months of work exists in exactly one place.** Everything else on this
   list is theoretical until this is fixed.
2. **Nothing enforces the definition of done.** Every regression is caught by a
   human happening to look.
3. **The main product claim is untested.** Greenfield has never run end to end.
4. **The public install path is wrong.** README → chart 0.1.0 → two months behind.
5. **The git history leaks the internal cluster hostname** in every commit, in a
   public repo, after the docs were deliberately sanitised.

**In short:** commit the work and get CI running before building anything new.
Feature 13 is worth less than making features 11 and 12 durable and verifiable.

## 1. P0 — urgent

- **P0-1 · Git history exposes the internal cluster hostname.**
  Commits `e589ec1`…`a4869ec` carry `root@<internal-k3s-host>` as their author in a
  public repo — the same hostname that was deliberately masked as `<k3s-node>` in
  the docs (§4.8). Not a credential, but infrastructure disclosure.
  *Fix:* rewrite the author of the historical commits (`git filter-repo --mailmap`)
  and force-push. Coordinate first — it rewrites every hash and breaks existing
  clones and forks.
  *Already done (2026-07-31):* `user.name`/`user.email` are set repo-locally to the
  GitHub noreply identity, so commits from here on no longer leak it.
- **P0-2 · Etappes 11 and 12 are uncommitted.** 107 changed files against
  `a4869ec`, including the entire design system and the observability work.
  Single point of failure. *Fix:* commit in reviewable slices, push, tag.

## 2. P1 — must-have

- **P1-1 · CI (S9).** GitHub Actions running `go test ./...`, `tsc --noEmit`,
  `npm run build` and `go vet` on every push. Without it, §4.12 is an intention.
  Planned: [etappe 13](plans/etappe-13-ci-and-verification.md).
- **P1-2 · Greenfield end-to-end test (S6).** Deploy-ess → connect-oidc on a
  throwaway cluster. This is the product claim; it has never run. Needs a scratch
  k3s (kind/k3d would do) because our instance cannot reach the code path.
- **P1-3 · Release coherence (S8).** `Chart.yaml`, `appVersion`, README and
  CONTRIBUTING all say `0.1.0`; the running image is `0.1.12`. Add a release
  checklist that moves all version strings together, tag the repo (there are no
  tags at all), and publish a chart that matches the image.
- **P1-4 · Frontend tests (S9).** Zero exist. Start with the pure logic that has
  already broken once: version comparison in `helm/index.tsx`, diff parsing in
  `DiffView.tsx`, restart-cause mapping in `ComponentDrawer.tsx`.
- **P1-5 · Headless browser for verification.** The maintainer asked for
  screenshot/UI checks as part of the definition of done (§4.12); no browser is
  installed, so that step cannot run. Add Playwright (or chromium + a small
  script) so verification can reach past HTTP status codes.
- **P1-6 · `hooks-failed` is silent (S3).** If a post-upgrade hook fails, the only
  signal is a badge in a UI nobody is looking at. The whole point is that calling
  breaks otherwise. Needs at least a log line at error level, ideally a Matrix
  message to the admin.

## 3. P2 — worth doing

- **P2-1 · Audit log UI (S10).** The table and the middleware writes exist; nothing
  reads them back. Cheap, and it turns dead weight into a feature.
- **P2-2 · Build artefacts are committed.** `cmd/matrixctrl/dist` is 38 tracked
  files, so every UI change produces a diff full of hashed bundles and hides the
  real change during review. Generate it at build time and gitignore it.
- **P2-3 · Persist dashboard metrics.** The CPU/RAM sparklines live in memory and
  reset on reload, so "is this getting worse?" cannot be answered.
- **P2-4 · Release notes per ESS version.** `ess_versions.changelog` and
  `breaking_changes` exist in the schema and are never populated — the upgrade
  wizard asks the operator to jump versions with no information.
- **P2-5 · Decide the System page (§4.13).** Open question: the enriched dashboard
  now covers most of it. Keep, merge, or delete.

## 4. P3 — someday / nice-to-have

- **P3-1 · Read-only role.** Today there is exactly one role: full admin.
- **P3-2 · English UI (S17).** The UI ships German only; the repo and docs are
  English. Phase 6, but it is the single biggest barrier to outside contributors.
- **P3-3 · Bulk config edit across sections.** Changing the server name touches
  several files by hand today.
- **P3-4 · Validate config against the running Synapse,** not only the JSON
  Schema — schema-valid values can still be rejected at runtime.
