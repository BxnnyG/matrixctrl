# Etappe 16 — Release coherence

**Date:** 2026-08-01 · **System:** S8 · **Closes:** P1-3

## The actual state (checked against GHCR, not assumed)

| Artefact | Repo says | GHCR has | Running here |
|---|---|---|---|
| Image | `image.tag: latest` | `0.1.9`, `latest` | `0.1.14` |
| Chart | `version: 0.1.0`, `appVersion: 0.1.0` | `0.1.0` | — |
| Git tags | — | — | **none at all** |

So a stranger who follows the README installs chart `0.1.0`, which pulls image
`latest`, which is the 0.1.9-era build. Five image versions behind, and nothing in
the repo says so. The install instructions are not wrong in form — they are wrong
in outcome, which is worse, because they look authoritative.

The `0.1.10`–`0.1.14` images only ever existed as local `k3s ctr images import`.
That is the root cause: releasing was a sequence of commands in CONTRIBUTING that
a human had to remember and run, so it was skipped every time the pressure was on.

## Approach

**Make the tag the release.** Publishing must be a consequence of tagging, not a
checklist someone follows.

1. **One version, one place.** `Chart.yaml` `version` == `appVersion` == image tag.
   Chart and app move together; for a single-artefact project, two independent
   version lines only create the drift this etappe is fixing.
2. **`.github/workflows/release.yml`, triggered on `v*` tags.** Builds and pushes
   the image, packages and pushes the chart, both to GHCR with the version from the
   tag. Uses the built-in `GITHUB_TOKEN` with `packages: write` — no secret to
   provision, nothing to rotate.
3. **The workflow derives the version from the tag** and fails if `Chart.yaml`
   disagrees. A mismatch should stop the release, not publish a lie.
4. **`docs/RELEASING.md`** replaces the copy-paste block in CONTRIBUTING: what a
   release is, how to cut one, how to verify it afterwards.
5. **Set the version to `0.1.15`** and tag `v0.1.15`. Not `0.1.14` — that tag would
   claim to be the locally-built image, and a CI-built artefact is not the same
   bytes. A fresh number is honest.
6. **Then close the loop**: deploy the *published* `0.1.15` from GHCR to this
   cluster with `pullPolicy: Always`. Until the published artefact has actually
   been installed from the registry, "release coherence" is still a claim.

## Definition of done (§4.12)

- `go test ./...`, `tsc`, frontend build green
- Tag `v0.1.15` pushed; the release workflow publishes image **and** chart
- `helm show chart oci://ghcr.io/bxnnyg/charts/matrixctrl --version 0.1.15` works
  from outside
- The running cluster is on the image **pulled from GHCR**, not a local import
- README and CONTRIBUTING reference `0.1.15`
- S11 regression checks

## Risks

- **GHCR packages default to private.** The existing two are public, but a newly
  created package is not, so the first automated publish may need the visibility
  flipped once in the UI. Written into RELEASING.md rather than discovered later.
- **Pulling from GHCR is a new dependency for this instance**, which has run on
  local imports. If the pull fails the deployment stalls — mitigated by verifying
  the pull before switching `pullPolicy`, and by `keep`-policy PVCs meaning a
  rollback loses nothing.

## Outcome (2026-08-01, `v0.1.15`)

**Published and verified by pulling it**, not by reading a green tick:

```
helm show chart oci://ghcr.io/bxnnyg/charts/matrixctrl   → version 0.1.15
helm show values …                                        → image.tag "0.1.15"
docker pull ghcr.io/bxnnyg/matrixctrl:0.1.15              → "MatrixCtrl 0.1.15 starting"
```

The cluster runs the registry image: the local copy was deleted from containerd
first, so the pull had to be genuine — 6.1 s, 25.7 MB, visible in the pod events.
S11 all green, 9/9 routes verified in headless chromium.

**Four attempts failed first, and none of the causes were the one in the plan.**

1. **25 min, then dead.** The Dockerfile had no `--platform=$BUILDPLATFORM`, so
   buildx ran *every* builder stage under QEMU per architecture — including `npm
   ci` and the whole Vite build, whose output is architecture-independent. Fixed
   by building the builder stages natively and cross-compiling Go.
2. **3.8 min, dead.** Faster, so the emulation fix worked, but something in the
   arm64 path still broke. Dropped arm64 rather than keep guessing under a tag
   (P2-7 tracks restoring it via native runners).
3. **89 s, dead — with amd64 alone.** Faster than the same build takes locally,
   so it was never reaching the end of the build.
4. **Split "Build & push" into two steps** and the failure named itself:
   **Push image**. The GHCR packages had been created by hand with a PAT and
   never granted the repository's Actions token write access;
   `permissions: packages: write` does not cover that.

**The bug and its own history were the same thing.** GHCR sat at `0.1.9` for two
months because releasing had always been a human with a PAT. There had never been
an automation allowed to write, so the first one built ran straight into it.

**Lesson worth keeping:** three cycles were burned guessing because job logs need
a token the agent does not have, and the API could say *which step* but not *what
happened*. Splitting build from push turned an opaque failure into a labelled one
and answered in a single run what three guesses could not. The split stays.

**Two more things fell out of the ship:**
- The instance values file pinned `image.tag: 0.1.12` while `0.1.14` ran, surviving
  only because every deploy passed `--set image.tag`. Released charts pin their own
  image, so the pin is gone — the same drift this etappe fixed, one level down.
- `verify-ui.mjs` reported `/hooks` as empty. It was not: the check sampled
  `innerText` once at `networkidle`, which on a code-split route can land before
  React mounts. Now it polls with a deadline. A flaky check is worse than no check,
  and this one had been passing by luck.
