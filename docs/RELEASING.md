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

The workflow **fails the release** if the tag disagrees with `Chart.yaml`. A
mismatch means one of them is lying, and finding out at publish time is better
than after someone installs it.

**The docs pin nothing.** The README's install command has no `--version`, so
Helm resolves the newest published chart and the quickstart cannot go stale.
This is deliberate: a pinned version in prose has to be remembered on every
release, and "remember to update it" is the exact failure that left GHCR five
versions behind. The problem is removed rather than guarded against.

Reproducibility does not suffer, because each released chart pins its **own**
image tag — "newest chart" still resolves to one exact, immutable pair. Readers
who want a specific release add `--version <x.y.z>` themselves.

A backstop remains: if a concrete version ever reappears in `README.md` or
`docs/`, the release fails unless it matches the tag. Finding none is the
expected case and passes.

## Cutting a release

1. Make sure `master` is green — the release workflow does not re-run the tests.

2. Bump both fields in `deploy/helm/matrixctrl/Chart.yaml`:

   ```yaml
   version: 0.1.15
   appVersion: "0.1.15"
   ```

3. Nothing to change in the README — it pins no version. (If you added one
   deliberately, the release will check it against the tag.)

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
V=0.1.15   # the version you just tagged

helm show chart oci://ghcr.io/bxnnyg/charts/matrixctrl --version "$V"

# The chart must reference the matching image, not "latest".
helm show values oci://ghcr.io/bxnnyg/charts/matrixctrl --version "$V" | grep -A2 '^image:'

# And what a new user actually gets — no --version, newest chart:
helm show chart oci://ghcr.io/bxnnyg/charts/matrixctrl | grep -E '^version|^appVersion'
```

The released chart pins `image.tag` to its own version, so
`helm install --version X` is reproducible. The committed default stays `latest`
for development; only the packaged copy is pinned.

## One-time setup: let Actions write to the packages

**This bit the first four release attempts and is invisible from the code.**

`ghcr.io/bxnnyg/matrixctrl` and `ghcr.io/bxnnyg/charts/matrixctrl` were created by
hand with a personal access token, back when releasing was a manual checklist. A
GHCR package created that way does **not** grant the repository's `GITHUB_TOKEN`
write access, no matter what `permissions: packages: write` says in the workflow.
The build succeeds and the push is refused.

Fix it once, per package:

> **github.com/users/BxnnyG/packages** → select the package → **Package settings**
> → *Manage Actions access* → **Add repository** → `matrixctrl`, role **Write**.

Do it for both `matrixctrl` and `charts/matrixctrl`. Packages that Actions creates
itself inherit this automatically; only ones predating the workflow need it.

Afterwards, re-run the failed job from the Actions UI — no new tag needed.

## Gotchas

- **New GHCR packages default to private.** `matrixctrl` and `charts/matrixctrl`
  are already public. If a publish ever creates a new package name, flip its
  visibility in the GitHub Packages UI or nobody can pull it without auth — and
  the failure looks like "not found", not "forbidden".
- **Build and push are separate steps on purpose.** Job logs need a token, so when
  a release fails the API can say *which step* but not *what happened*. Splitting
  them makes the step name the diagnosis: "Build image" is the Dockerfile, "Push
  image" is permissions. Please keep them separate.
- **The workflow does not run the test suite.** It assumes the tagged commit
  already passed CI on `master`. Tag a commit that has a green run.
- **Re-tagging a published version does not republish it cleanly.** Bump to a new
  patch version instead; registries and caches do not appreciate mutable tags.
- **`latest` is moved by every release.** The chart's committed default points at
  it, which is fine for development but is exactly why released charts are pinned.
