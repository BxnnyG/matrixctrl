# Etappe 13 — CI & verification chain (plan)

Follows etappe 12 (observability & correctness fixes, deployed as image 0.1.12 on
2026-07-31). Covers system **S9 · Verification & CI** and unblocks the definition
of done agreed in **§4.12**. No GitHub issue exists — the tracker is empty.

This is the first etappe planned under [PROZESS.md](../PROZESS.md).

## 1. The problem from the operator's point of view

The maintainer just committed to a definition of done: tests green, typecheck
green, build green, then deployed to k3s and verified — all of it autonomous.
Right now nothing enforces any of that. It holds only as long as the agent
remembers, in a session that may be weeks apart from the last one.

Two concrete consequences already exist:

- Two months of work (etappes 11 and 12) sit uncommitted in a single working
  tree. Nothing complained.
- `CLAUDE.md` advertised "Vitest + Testing Library" while zero frontend tests
  existed. Nothing complained about that either.

What the operator wants to be true: *push code, and something independent tells
me whether it is still sound* — plus, when the agent claims a UI change works, a
screenshot proving it rather than an HTTP 200.

## 2. What already exists (inventory, not guesswork)

Verified in the tree on 2026-07-31:

- **Go tests:** `internal/git/diff_test.go`, `internal/config/yamledit_test.go`,
  `internal/config/migrate_test.go`, `internal/helm/versions_test.go`.
  All pass via `go test ./...`.
- **`internal/helm/discover_live_test.go`** — hits a real cluster. Must be
  excluded from CI (build tag or `-short`), or CI will fail on a runner.
- **Typecheck:** `web/node_modules/.bin/tsc --noEmit`, currently clean.
- **Build:** `npm run build` (Vite/rolldown) and `make build` (embeds
  `cmd/matrixctrl/dist`, then `go build`).
- **Makefile targets:** `all build web-build copy-dist web-dev dev test lint clean docker`.
  `make test` already wraps `go test ./...`; `make lint` expects `golangci-lint`,
  which is **not installed** on the build host.
- **`.github/`** holds `PULL_REQUEST_TEMPLATE.md` and three issue templates.
  **There is no `workflows/` directory** — this etappe creates the first one.
- **No frontend test runner.** No Vitest, no Playwright, no browser in
  `web/node_modules/.bin`.
- **Go 1.26**, Node 20. Go is at `/usr/local/go/bin/go`, not on the default PATH.

## 3. What we build

Three things, smallest useful version of each.

**(a) GitHub Actions workflow** — `.github/workflows/ci.yml`, on push and PR:
one job for Go (`go vet`, `go test ./... -short`), one for the frontend
(`npm ci`, `tsc --noEmit`, `npm run build`). Cached module/npm downloads.

*Alternative considered:* a `make ci` target run by the agent instead of a
workflow. Rejected — that is exactly the "enforced by memory" state we are
leaving. CI must run somewhere the agent does not control.

**(b) First frontend tests** — Vitest over the pure logic that has already
broken in production:
- `cmpVersion` in `routes/helm/index.tsx` (a string sort ranked 26.5.1 above
  26.10.0 — the bug that made the version list useless)
- `parseDiff` in `components/config/DiffView.tsx` (rendered nothing when the
  backend emitted no `@@` headers)
- `explainExit` / `podNameToComponent` mapping in `ComponentDrawer.tsx`

These are chosen because each corresponds to a real defect, not because they are
easy to test.

**(c) Browser verification** — Playwright with chromium, plus a
`scripts/verify-ui.mjs` that logs in against a running instance, visits each
functional route, fails on console errors or a non-200, and writes screenshots to
`/tmp/matrixctrl-verify/`. This is what makes §4.12's screenshot step executable.

*Scope guard:* this is a smoke check, not a visual-regression suite. No golden
images — those would fail on every intentional design change and get disabled
within two etappes.

## 4. Data model / interfaces

No database migration. No API change. No config change.

New files only:
```
.github/workflows/ci.yml
web/vitest.config.ts
web/src/**/*.test.ts          (3 test files)
scripts/verify-ui.mjs
```
Changed: `web/package.json` (devDependencies + `test` script), `Makefile`
(`test` target also runs the frontend tests), `docs/DESIGN.md` §1b (S9 status).

`discover_live_test.go` gets a `//go:build live` tag so CI can skip it while the
agent can still run it against the real cluster with `-tags live`.

## 5. Surface / output

CI status appears on commits and PRs — the only user-visible change. Locally:

```
make test        # Go + frontend unit tests
npm run test     # frontend only
node scripts/verify-ui.mjs --base https://matrixctrl.bxnny.de
                 # → screenshots + pass/fail per route
```

Failure output must name the route and the console error, not just "exit 1" —
the agent reads this output, and a useless message costs a whole round trip.

## 6. Test strategy

- **Unit:** the three frontend cases above prove the regression fixes stay fixed.
  Go tests already cover diff hunks, version comparison, YAML comment preservation
  and section migration.
- **Integration:** CI itself is the integration test — the workflow must go green
  on a clean checkout, which also proves `npm ci` and the build work without the
  local cache.
- **Manual, honestly named:** whether the *rendered* UI actually looks right
  stays human judgement. `verify-ui.mjs` proves the pages load, render without
  console errors, and produce a screenshot — it does not prove the design is good.
  The greenfield path (S6) remains untested by this etappe; it needs a throwaway
  cluster and is its own etappe (14).

## 7. Risks & way back

| Risk | How it shows | Way back |
|---|---|---|
| `npm ci` fails in CI because `package-lock.json` drifted from the tree | Frontend job red on the first run | Regenerate the lockfile and commit it; the job is new, so nothing regresses |
| Playwright's chromium download bloats CI and the build host | Slow jobs, disk pressure | Keep the browser out of the CI job — `verify-ui.mjs` runs on the build host only, against a deployed instance |
| `discover_live_test.go` runs on a runner with no cluster | Go job red | The `live` build tag; verify by running the job before relying on it |
| CI turns red for unrelated reasons and gets ignored | Red badge nobody looks at | Keep the workflow to the three checks above. Anything flaky gets removed rather than retried |

Rollback for the whole etappe is deleting `.github/workflows/ci.yml` — nothing in
the product depends on any of this.

## 8. Definition of done for this etappe

Per §4.12, and note the ordering — P0-2 first, because CI on uncommitted work
proves nothing:

1. Etappes 11 + 12 committed and pushed (P0-2) so CI has something to run against.
2. `go test ./...`, `tsc --noEmit`, `npm run build` green locally.
3. The workflow runs green on GitHub for a real push.
4. `verify-ui.mjs` produces screenshots of all functional routes with no console
   errors, against the deployed instance.
5. The four S11 regression checks pass.
6. `DESIGN.md` §1b (S9 → ✅), `ROADMAP.md` (etappe 13 → ✅ with date) and
   `BACKLOG.md` (P1-1, P1-4, P1-5 closed) updated.
