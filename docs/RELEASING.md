# Releasing

> Cutting a release is **pushing a tag**. Everything else is automation.

## Why it works this way

Releasing used to be a block of commands in `CONTRIBUTING.md` that a maintainer
ran by hand. Predictably, it stopped happening: images `0.1.10` through `0.1.14`
were built locally and imported straight into k3s, so GHCR sat at `0.1.9` for two
months while the README told strangers to install `latest`. Nothing was broken in
any single step — the process simply depended on someone remembering it under
pressure.

So the tag is the release. `.github/workflows/release.yml` runs on `v*` and
publishes both artefacts; there is no manual path that can be skipped.

## One version, everywhere

Chart `version`, chart `appVersion` and the image tag are **the same number**.
MatrixCtrl ships one artefact; two independent version lines would only recreate
the drift this replaced.

The workflow **fails the release** if the tag and `Chart.yaml` disagree. A
mismatch means one of them is lying, and finding out at publish time is better
than after someone installs it.

## Cutting a release

1. Make sure `master` is green — the release workflow does not re-run the tests.

2. Bump both fields in `deploy/helm/matrixctrl/Chart.yaml`:

   ```yaml
   version: 0.1.15
   appVersion: "0.1.15"
   ```

3. Update the install commands in `README.md` (`--version 0.1.15`).

4. Commit, then tag and push:

   ```bash
   git commit -am "release: 0.1.15"
   git tag v0.1.15
   git push origin master --tags
   ```

5. Watch the run under **Actions → Release**. It builds the frontend, rebuilds the
   embedded assets, pushes a multi-arch image, and packages a chart pinned to that
   exact image tag.

## Verifying afterwards

Do not trust a green checkmark — pull it the way a stranger would:

```bash
helm show chart oci://ghcr.io/bxnnyg/charts/matrixctrl --version 0.1.15

# The chart must reference the matching image, not "latest".
helm show values oci://ghcr.io/bxnnyg/charts/matrixctrl --version 0.1.15 | grep -A2 '^image:'
```

The released chart pins `image.tag` to its own version, so
`helm install --version X` is reproducible. The committed default stays `latest`
for development; only the packaged copy is pinned.

## Gotchas

- **New GHCR packages default to private.** `matrixctrl` and `charts/matrixctrl`
  are already public. If a publish ever creates a new package name, flip its
  visibility in the GitHub Packages UI or nobody can pull it without auth — and
  the failure looks like "not found", not "forbidden".
- **The workflow does not run the test suite.** It assumes the tagged commit
  already passed CI on `master`. Tag a commit that has a green run.
- **Re-tagging a published version does not republish it cleanly.** Bump to a new
  patch version instead; registries and caches do not appreciate mutable tags.
- **`latest` is moved by every release.** The chart's committed default points at
  it, which is fine for development but is exactly why released charts are pinned.
