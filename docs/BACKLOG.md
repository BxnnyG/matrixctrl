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
running image is `0.1.12`, so a stranger following the README installs something
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
5. **The git history leaks the internal cluster hostname** in every historical
   commit, in a public repo, after the docs were deliberately sanitised. New
   commits no longer do.

**In short:** the safety net exists now. The next thing worth doing is proving the
greenfield path on a throwaway cluster — everything the project promises to
strangers depends on a code path nobody has ever run.

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
- ~~**P0-2 · Etappes 11 and 12 are uncommitted.**~~ **Done 2026-07-31** —
  committed in nine reviewable slices (`1df5690`…`75dc5e4`). Tagging is still
  open and folded into P1-3.

## 2. P1 — must-have

- ~~**P1-1 · CI (S9).**~~ **Done 2026-07-31** — `.github/workflows/ci.yml` runs
  `go vet`, `go test ./...`, typecheck, unit tests and the build on push and PR.
- **P1-2 · Greenfield end-to-end test (S6).** Deploy-ess → connect-oidc on a
  throwaway cluster. This is the product claim; it has never run. Needs a scratch
  k3s (kind/k3d would do) because our instance cannot reach the code path.
- **P1-3 · Release coherence (S8).** `Chart.yaml`, `appVersion`, README and
  CONTRIBUTING all say `0.1.0`; the running image is `0.1.12`. Add a release
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
