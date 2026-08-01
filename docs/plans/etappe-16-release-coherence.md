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
