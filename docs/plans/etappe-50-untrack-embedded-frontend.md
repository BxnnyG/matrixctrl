# Etappe 50 — A committed build artefact that disagrees with its source

P2-2, opened as "these diffs are noisy". It is not a tidiness item. Measured today,
the tracked copy of the embedded frontend is **sixteen days and roughly fifteen
etappes out of date**, and nothing anywhere reports that.

## What is actually true right now

```
$ git log -1 --date=short -- cmd/matrixctrl/dist
452173b 2026-08-01 chore: rebuild the embedded frontend assets

$ git ls-files cmd/matrixctrl/dist | grep -i reports
(nothing)
```

Forty tracked files, last rebuilt 2026-08-01. They contain **no reports screen at
all** — E46, E47 and E48 are simply absent, as are E41's room detail and everything
else since. A plain `go build ./cmd/matrixctrl` today produces a binary that serves
the UI of August 1st, and it exits 0 and looks entirely normal.

Releases are unaffected: the Dockerfile builds the frontend itself, and `make build`
runs `web-build copy-dist` first. The hazard is the *obvious* command — the one a
contributor, an IDE, or `go install` would run — being silently wrong.

## The same shape as the last two etappes

CI already guards this, and the guard is the tell:

```yaml
run: test -d cmd/matrixctrl/dist || { echo "…missing — run 'make copy-dist'"; exit 1; }
```

It asks **does the directory exist**. The question is **is the embedded UI the current
one**. It has been passing, truthfully, for sixteen days, over a directory whose
contents predate a third of the project's screens. §4.40, §4.45, §4.48 — and now this.

## The fix, and why not the obvious one

The obvious fix is "remember to rebuild it", which is what the last sixteen days
already disproved.

The next-obvious is to delete the directory and gitignore it. That does not compile:
`//go:embed all:dist` on a missing or empty directory is a build error —

```
pattern all:dist: cannot embed directory dist: contains no embeddable files
```

— which would break `go test ./...` and CI for anyone who has not built the frontend.
Verified rather than assumed, in a scratch module.

So: **track exactly one file, `dist/.gitkeep`, and ignore the rest.** The package
always compiles, no built asset is ever committed again, and a bare `go build` now
produces a binary with *no* frontend rather than a *stale* one. That is the whole
point — the failure becomes loud and immediate instead of quiet and plausible.

To make it loud, the binary must say so rather than 404:

- at startup, one warning line naming the cause and the fix
- `ServeFrontend` answers a readable page explaining the binary was built without a
  frontend, instead of a bare 404 that reads like a routing bug

## Scope

**Ships:** the 40 tracked artefacts removed, `.gitkeep` + gitignore, the placeholder
detection with its startup warning and explanatory page, CI's guard changed to ask the
right question, and `make build` left exactly as it is because it was already correct.

**Does not ship: embedding a build stamp to compare against.** Tempting — record the
frontend's build time and compare it to the Go build's — but two timestamps that can
disagree is a second artefact with the same disease. Removing the artefact is better
than instrumenting it.

## Definition of done

- `git ls-files cmd/matrixctrl/dist` lists exactly one file, `.gitkeep`
- `go build ./...` and `go test ./...` still work on a clean checkout
- A binary built without the frontend says so at startup and in the browser
- `make build` still produces a working binary with the real UI
- CI asks whether `index.html` is there, not whether a directory is
- `make check` green
