# Changelog

All notable changes to MatrixCtrl are documented here.
Format: [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) ·
Versioning: [SemVer](https://semver.org/) — while at `0.x`, minor releases may break things.

The chart and the image share one version number: a released chart pins the
matching image, so a version identifies one exact pair
([DESIGN.md §4.17](docs/DESIGN.md)).

> **Before 0.1.15 there were no published releases.** Those versions were images
> built and imported into a single cluster by hand. The entries below are
> reconstructed from the etappe log in [ROADMAP.md](docs/ROADMAP.md), which is
> dated and accurate, but they were never artefacts anyone else could install.

## [Unreleased]

## [0.1.16] — 2026-08-01

### Fixed

- **Greenfield deploy never worked.** The wizard seeded
  `wellKnownDelegation.ingress.host`, which matrix-stack's schema rejects
  (`additionalProperties: false`), so `helm install` failed validation on *every*
  fresh deploy — the headline feature, broken since it shipped. Found by the first
  end-to-end run on an empty cluster.
- **A failed deploy could never be retried.** The config-seed guard refused to run
  once the config repo had sections, which the failed attempt had just created.
  Existing config is now kept and the deploy continues, rather than deleting work
  that may have been prepared on purpose.
- **Repairing older installs.** Removing the bad key stopped it being written, but
  did nothing for repos that already had it — precisely everyone who had tried.
  It is now stripped on every deploy.
- **First start on a fresh volume crashed once.** The database wait was 60 s, which
  is shorter than Postgres' `initdb` on a new PVC, so a brand-new install greeted
  its operator with `restarts=1`. Raised to 5 minutes.
- **The MAS consent screen showed a ULID** instead of a name. MatrixCtrl now
  registers its OIDC client with `client_name: "MatrixCtrl"`.

### Changed

- `internal/api/handlers/helm.go` (834 lines, five responsibilities) split into
  four files. Pure code motion, no behaviour change.
- CI fails on unformatted Go.

### Security

- **The history was rewritten a second time** and this release is built from the
  cleaned tree. Besides the cluster's node name, a sweep of every blob in the
  history found the admin panel's own URL — including in the packaged chart's
  default values — and the five ESS hostnames derived from the server name, in
  code, a database migration and the committed frontend bundle. The `0.1.15`
  artefacts published before this carried some of those strings inside the image;
  `v0.1.15` was retagged and re-published from the cleaned tree.
  The Matrix server name itself is public by definition and was not touched.
- A `pre-commit` hook and a CI step now refuse content containing a known-sensitive
  string, with the pattern list held outside the repository
  ([DESIGN §4.19](docs/DESIGN.md)).

## [0.1.15] — 2026-08-01

### Added

- **Release pipeline.** A `v*` tag now publishes the image *and* the chart to
  GHCR, with guards that refuse to publish if the tag and `Chart.yaml` disagree
  ([RELEASING.md](docs/RELEASING.md)).
- Released charts pin their matching image, so `helm install` without `--version`
  resolves to one exact, reproducible pair.

### Fixed

- The published chart was `0.1.0` while the running image was `0.1.14` — the
  README's install command shipped a version nobody was running.

## [0.1.14] — 2026-08-01

### Fixed

- **A working upgrade looked like a failed one.** Helm goes silent for minutes
  during an upgrade; the WebSocket had no keepalive, so Traefik's 180 s idle
  timeout cut the connection and the UI printed `[connection lost]` mid-upgrade.
  Added an application-level heartbeat, progress emission every 30 s, reconnect
  with backoff, and recovery of the final state after a drop.
- **The dashboard took seconds to load.** `/status` went from 1.9–3.2 s to
  0.14–0.25 s. Three causes: every Helm read hit the cluster (now cached for 60 s),
  the handler ran its cluster queries serially (now concurrent), and client-go's
  default rate limit of 5 QPS is sized for a CLI, not a server.

## [0.1.12] — 2026-07-31

### Added

- Pod drill-down showing the actual restart cause, a cluster event feed, and a
  hook editor.
- CI, 26 frontend tests, and a headless-browser check that walks every route.

### Fixed

- The ESS version list showed only ancient `0.2.x` dev builds, sorted as strings
  (`26.5.1` ranked above `26.10.0`). GHCR pagination and per-commit build tags are
  now handled, and versions sort numerically.

## [0.1.10] — 2026-06-04

### Added

- Design system: dark-only tokens, three switchable directions, density and accent
  settings, and shared primitives. Every functional screen restyled.

## 0.1.0 – 0.1.9 — 2026-05-27 … 2026-05-30

The initial build, developed against a single cluster and never published:
project skeleton, post-upgrade hooks, dashboard, git-backed per-section config
with comment-preserving edits, history and rollback, admin-only OIDC login via
MAS, the greenfield/adopt setup wizard, and the public-repo hardening that made
the project AGPL and removed every instance detail.

[Unreleased]: https://github.com/bxnnyg/matrixctrl/compare/v0.1.16...HEAD
[0.1.16]: https://github.com/bxnnyg/matrixctrl/releases/tag/v0.1.16
[0.1.15]: https://github.com/bxnnyg/matrixctrl/releases/tag/v0.1.15
[0.1.14]: https://github.com/bxnnyg/matrixctrl/blob/master/docs/plans/etappe-14-upgrade-stream-and-dashboard-latency.md
[0.1.12]: https://github.com/bxnnyg/matrixctrl/blob/master/docs/ROADMAP.md
[0.1.10]: https://github.com/bxnnyg/matrixctrl/blob/master/docs/ROADMAP.md
