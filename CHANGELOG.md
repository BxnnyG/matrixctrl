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

## [0.1.67] — 2026-09-05

### Added

- **The configuration screen now says where the configuration lives** — the path, that
  it is a git repository on its own volume, and how many versions it holds. It never
  did, so an operator could edit configuration for months while still assuming it landed
  in the folder they originally seeded it from. The seed directory is named in the
  tooltip so it stops looking like the source of truth.
- `storage.config.path` in the chart values. The Go side has always read this from the
  environment; the chart hardcoded it, so the one thing an operator might want to move
  was the one thing they could not.

### Notes

- The path occurs in four places in the deployment template — the environment variable,
  two mounts and the ownership fix. Parameterising only the first would have pointed the
  configuration at a path where no volume is mounted, which is worse than a hardcoded
  value that at least agrees with itself.
- Not done, deliberately: putting the configuration in `/opt` on the host. A hostPath
  ties the pod to one node and survives no migration — which is what a volume exists to
  avoid.

## [0.1.66] — 2026-09-05

### Fixed

- **The "Backup" navigation entry was greyed out** while backup and restore had shipped
  two versions earlier — onto `/system`, where the sidebar never said to look. They now
  live at `/backup`, which is what the entry always promised. A disabled item beside a
  working feature is worse than no item: it actively reports the thing is absent.

### Added

- **Export of Synapse's own database** — the accounts, rooms and messages. The
  configuration archive rebuilds the deployment; this is what makes a restored server the
  *same* server rather than a fresh one wearing the same hostnames. On this install that
  is 19 057 events.
- The export is one `REPEATABLE READ` snapshot, so every table comes from the same
  moment. Read table by table from a live database, a backup can contain a room whose
  creation event is missing — complete-looking and subtly torn.

### Notes

- **Media is not included.** 40 MB on a volume only the Synapse pod mounts; reaching it
  needs a Job with that PVC attached. Said in the export's manifest, not only here.
- **No restore button for the homeserver database.** It needs Synapse stopped, and doing
  it while the server runs corrupts what is there. The export is a file to restore
  deliberately with `psql`.
- The database password is read per request from the cluster secret, never held on the
  handler and never echoed in an error — a failed connection message carries the DSN.

## [0.1.65] — 2026-09-04

### Added

- **Restore.** Upload an archive and get the deployment back: the full ESS configuration
  with git history — hostnames, server name, TLS issuer, RTC settings — plus the hooks
  that re-apply manual patches, the upgrade history and the report dispositions.
- **A preview before anything is written**, showing when the archive was taken, which
  MatrixCtrl version made it and **which ESS release it came from**, so nobody discovers
  after the fact that they put a 26.8.0 configuration onto a different cluster.
- The backup manifest now records the ESS release (name, chart version, revision). It
  previously recorded only MatrixCtrl's own version, which made "restore the same
  versions" impossible.

### Changed

- The backup now says what it **does** restore, not only what it does not. The archive
  is everything needed to rebuild the same homeserver minus what users created; the
  previous wording collapsed both into one pessimistic sentence.

### Notes

- An archive taken before a migration restores onto today's schema: columns are matched
  by name, ones since dropped are skipped, ones since added take their defaults. That is
  the return on carrying data and no schema.
- `schema_migrations` is never restored — it is the live database's bookkeeping about
  itself.
- The database is restored in one transaction; the config repository is written beside
  the live one and swapped. If the two ever do end up from different moments, the error
  says so rather than reporting a clean failure.

## [0.1.64] — 2026-09-03

### Added

- **Backup.** The System page can produce an archive containing the config repository
  *with its full git history* — every ESS value and every change ever made to it — and
  MatrixCtrl's own database: hooks, upgrade history, report dispositions, recorded node
  capacity. "Backup/restore" had been listed as project scope since the beginning with
  nothing behind it.

### Notes

- **The archive does not contain the homeserver.** Synapse's database and its uploaded
  media live on volumes this pod does not mount. That sentence is in the archive's own
  manifest and on the card that offers it, not only in documentation — an operator who
  believes they hold a backup of their Matrix server and finds out during a restore is
  the failure this feature exists to avoid.
- No schema is carried: a restore rebuilds it from the migrations, so an archive comes
  back onto the *current* schema rather than the one it was taken on.
- Telemetry tables are included but marked `regenerable` in the manifest — they are
  31 000 of the 31 100 rows, and losing them costs history rather than function.
- Restore is deliberately a separate change. Taking a backup is harmless; writing one
  back destroys what is there.

## [0.1.62] — 2026-09-03

### Changed

- **The runtime image runs no command.** `apk add ca-certificates tzdata` was the single
  instruction in the whole build that needed the *target* architecture to execute — every
  other stage is native and Go cross-compiles — and it is why two attempts at an arm64
  image failed under emulation. The timezone database is now embedded in the binary
  (`import _ "time/tzdata"`, ~450 KB) and the CA bundle is copied out of the builder
  stage, since it is architecture-independent text.
- **The release builds `linux/amd64` and `linux/arm64`** in one manifest. No QEMU is set
  up, deliberately: assembling the arm64 image is now copying files, and leaving the
  emulator out means a future `RUN` in the runtime stage fails loudly instead of quietly
  becoming a slow emulated build.
- The README states which architectures a release actually carries. It previously said
  nothing, so an ARM user found out when the pull failed.

### Notes

- This build host offers `linux/amd64` only and has no QEMU, so **the arm64 image could
  not be built or tested here**. What was verified is that removing the packages broke
  neither of the things they provided: the image carries the CA bundle, the binary
  contains the zone database, and the deployed pod completed OIDC discovery over HTTPS —
  a TLS handshake using exactly those copied certificates. Whether arm64 publishes is
  the next tagged release's answer.

## [0.1.61] — 2026-09-02

### Added

- **GDPR erasure for a single account.** MatrixCtrl previously sent `skip_erase: true`
  on every deactivation, so it could not erase at all and an operator answering a legal
  request had to leave the panel. Erasure is now its own action — never a checkbox on
  deactivate, because a parameter lets someone perform an irreversible operation while
  reading a label that says something reversible.

### Notes

- **The confirmation says what erasure does not reach**, which is the substance of the
  change. Read from Synapse's source: message content is pruned only for viewers who
  were *not* in the room at the time, so everyone who was there still reads it. The old
  display name survives in historical room events, and uploaded media is untouched and
  must be quarantined or deleted separately.
- Offered whether or not the account is already deactivated, since an erasure request
  usually arrives after the account is gone.
- No bulk erasure. A loop over a selection is how an irreversible action reaches the
  wrong rows.

## [0.1.60] — 2026-09-02

### Fixed

- **Paging a report queue could show the same report twice.** Synapse orders both queues
  by timestamp alone, so reports filed in the same second have no defined order between
  two queries — and a burst of reports about one incident is exactly what produces them.
  Each page is now ordered by `(timestamp, id)`, which is stable across reloads, and a
  report is shown on the page it first appeared on, which removes duplicates.

### Notes

- A row the server never returned still cannot be recovered from this side without
  walking the whole queue, which is what a page limit exists to avoid. Two of the three
  halves are fixed; the third is documented rather than papered over. At the default
  page size of 50 a typical queue has no page boundary at all.
- The ordering and the first-seen rule live in `lib/paging.ts` with tests, rather than
  inside the component.

## [0.1.59] — 2026-09-02

### Added

- **The capacity preflight's verdict is recorded on the upgrade.** It previously went
  only to the live log stream — a WebSocket, gone once the tab closed — so "did the
  panel warn us before we applied that?" had no answer afterwards, which is exactly when
  the question gets asked. The upgrade history now carries it.
- An upgrade that was never checked is distinguishable from one that was checked and
  found nothing. Absent and empty are different answers; old upgrades are not backfilled
  with a verdict they never received.

### Removed

- `config_snapshots` (whole table, 0 rows) and `upgrade_history.values_snapshot`.
  Config history is git-backed, and a second unused snapshot table beside it is a second
  source of truth waiting to disagree with the first.
- `upgrade_history.helm_output` — `error_message` already carries what a failed upgrade
  said; the rest was a blob nobody would query.
- `ess_versions.changelog`, `breaking_changes`, `chart_digest`, `published_at` —
  superseded by release notes fetched from the published releases and dates from the
  release index.

### Notes

- Every column removed was verified NULL in every row on the live database first, and
  the table verified empty. "Nothing writes it" and "it is empty" are different claims.

## [0.1.58] — 2026-08-31

### Added

- **`http_request` hook actions actually work.** The action type was declared, offered
  in the hook editor with a full form for method, URL and body, and returned
  "not yet implemented" the first time it ran — which is during an upgrade. It now
  performs the request: 2xx is success, anything else fails the hook with the status
  code in the run log, a bounded timeout so a dead endpoint cannot stall an upgrade's
  hook phase, and no retries, because a silent retry hides a broken endpoint.

### Fixed

- `docs/GUIDE.md` described `http_request` as a working action type. It was written
  from the type declarations without checking the runner — corrected here, in the same
  change that makes the claim true.

### Notes

- No header field, deliberately: it is where a secret would end up stored in plain text
  in the hooks table. An endpoint needing auth can carry a token in its URL, which is
  the caller's decision rather than something this feature encourages.
- Found by sweeping for the shape 0.1.57 had just produced — constants that are
  declared and rendered but performed by nothing. Three such defects turned up in one
  session, all found by counting consumers rather than by reading declarations.

## [0.1.57] — 2026-08-31

### Fixed

- **Rolling back a release silently dropped every manual patch.** A Helm rollback
  recreates objects from the old revision's manifests, exactly as an upgrade does — so
  it removed the SFU's `hostNetwork` and `externalTrafficPolicy` patches, broke Element
  Call's media path, and left a healthy-looking dashboard. Nothing ran hooks after a
  rollback at all, although the hook editor offered "Nach Rollback" as a trigger: a hook
  could be created, saved, listed as enabled, and never run.
  A rollback now runs `post-rollback` hooks **and** `post-upgrade` ones, since almost
  every hook means "re-apply my patch after the chart overwrote it" and both operations
  overwrite. The trigger labels say so.
- A rollback whose hooks fail now reports `hooks-failed` rather than plain success —
  the release *is* on the older revision, but the patches that make it work may be gone.

### Added

- **[`docs/GUIDE.md`](docs/GUIDE.md) — a guide for using the product**, not maintaining
  it. What a hook is and why you want one, how the config editor's sections and
  comment-preserving edits work, and what to do when an upgrade ends in `failed` or
  `hooks-failed`. The README explained how to run the container and never what the
  container does.

### Notes

- The rollback bug was found *by writing that guide* — checking a sentence about
  triggers against the code instead of writing what seemed obvious.

## [0.1.56] — 2026-08-31

### Added

- **Node usage and capacity are recorded server-side**, once a minute, surviving
  restarts. The system page's sparklines are drawn from them.
- **A change in node capacity is now visible.** When a node's allocatable CPU or memory
  differs from what it was, the page says so with both numbers. That is the question
  nobody could answer during the outage of 2026-08-16…18: the node went from 32 cores
  to 6 and the only surviving evidence was a screenshot taken beforehand.

### Fixed

- **The sparkline no longer invents its own past.** It kept history in a browser ref
  that died on reload and — worse — pre-filled a fresh page with the current value, so
  the chart drew a flat line that read as an hour of stability and was one reading
  repeated forty times. An empty chart would have been more honest; a recorded one is
  better still.

### Notes

- Ninety days of retention, the same reasoning as the RTC tables: operational telemetry
  with no duty to answer for anything, unlike the audit log whose retention stays an
  open question for the operator (P2-19).
- Per node, never aggregated. A two-node cluster whose total stays flat while one node
  halves is exactly what an average hides.

## [0.1.55] — 2026-08-31

### Changed

- **The phone layout stops at the shell no longer.** Routes no longer set their own
  28px page padding — on a 360px screen that spent 16% of the width on margins. Below
  860px it is 14px, above it unchanged.
- **Component rows fold instead of squeezing.** Five columns across 360px left each
  metric about forty pixels; the name now takes the first line and the three numbers
  share the second. Same information, no horizontal scroll, nothing hidden.

### Notes

- Measured against the built stylesheet in a headless browser at 1280px and 360px:
  padding 28→14, five columns → three, no horizontal overflow at either width, desktop
  untouched.
- The dashboard's two-column grid was *already* responsive and needed nothing — found
  by auditing rather than assuming, which is the only reason it was not "fixed" twice.

## [0.1.54] — 2026-08-31

### Added

- **The panel works on a phone.** Below 860px the navigation rail becomes a drawer
  instead of occupying two thirds of the screen, the topbar drops what a phone cannot
  use (the `⌘K` hint, the chart version) and keeps what it needs, and the layout uses
  `100dvh` so the browser chrome sliding away no longer leaves a dead strip.
- **Installable on a home screen.** Web manifest, PNG icons at 192/512 plus an
  apple-touch-icon, theme colour, and the iOS-only tags — Safari ignores the manifest
  for both the icon and the status bar, and without them puts a screenshot of the page
  on the home screen.
- The tab title is now `MatrixCtrl` rather than `web`, Vite's default. On a home screen
  that string is the app's name.

### Changed

- **The capacity preflight measures memory too**, in the one case where it is
  arithmetic rather than a judgement: a pod asking for more than any node has. Memory
  *pressure* is still not reported — a node with 36 GiB and 30 GiB requested is ordinary
  Kubernetes, and warning about it would teach everyone to ignore the panel.

### Notes

- The icons are generated by `web/scripts/make-icons.py`, committed alongside them.
  There is no SVG rasteriser on the build host, so the script draws the mark directly —
  path flattening with a real elliptical-arc conversion, supersampled scanline fill.
- No offline service worker. "Installable" and "works offline" are different features,
  and an admin panel showing a cached cluster state from twenty minutes ago is worse
  than one that says it cannot reach the server.

## [0.1.53] — 2026-08-31

### Added

- **Applying a config now checks whether it fits the cluster first.** The values that
  took the homeserver down for 37 hours were written through this panel; it is the last
  place they can be measured before they reach the cluster. Every workload the chart
  would create is measured against the largest node, and the result goes into the apply
  stream before the upgrade runs.
- Two levels, because they are different problems: a pod larger than any node can never
  be scheduled, while one larger than what is currently free may be placed later.

### Notes

- The check **renders the chart** (dry run, hooks disabled) rather than reading the
  values file. It has to: `postgres.resources` covers two containers and Synapse's init
  containers inherit it, so `4000m` in the file is 8250m in the cluster. Verified by
  rendering the real config as it was on 2026-08-06 — reported `ess-postgres` at
  **8250m against a 6000m node**, from a file that says 4000m.
- It warns, it does not refuse. A false positive would block every deployment and this
  check has not yet run in anger; the option to refuse is recorded as P1-16c rather
  than taken by default.
- CPU only. Memory overcommit is normal and a warning tuned like this one would cry
  wolf.
- An apply now pulls the chart twice (once to render, once to upgrade) — about twenty
  seconds ahead of a multi-minute operation.

## [0.1.52] — 2026-08-30

### Added

- **A component that is `down` now says why, when the reason is the scheduler.** The
  panel showed four components down for 37 hours during the outage of 2026-08-16…18
  without once mentioning that postgres was asking for more CPU than the node had —
  the scheduler had been saying so in a `FailedScheduling` event the whole time. The
  dashboard now carries the scheduler's own words plus the arithmetic: the pod's
  effective request against the largest node's allocatable.
- Pods that ask for more than any single node can provide are called out separately.
  A full cluster may place them later; these never will, and telling someone to wait
  is worse than saying nothing.

### Notes

- The effective request is `max(sum(containers), max(initContainers))`, not the sum of
  the containers. Synapse's init containers had inherited 4000m each while its own
  container asked for 1000m, so it reserved 4000m while merely waiting for the
  database — a naive sum would have reported 1000m and made the diagnosis look wrong.
- No suggested value. What a component *should* request depends on what else is meant
  to run on the node; naming the arithmetic is the panel's job, choosing the number is
  the operator's.
- Verified against a live unschedulable pod whose 40000m request sat in an init
  container behind a 100m app container, not by unit test alone.

## [0.1.51] — 2026-08-17

### Fixed

- **The dashboard's restart alert named the wrong thing and called an old count a
  loop.** It read "postgres in Restart-Schleife" while the row below it correctly said
  `63× postgres-exporter` — and postgres has restarted **zero** times. The alert now
  names the container that actually restarted, using the attribution that already
  existed and was rendered two elements away.
- **"Restart-Schleife" is now a present-tense claim with present-tense evidence.** The
  trigger was `restarts > 20`, a lifetime counter with no time in it, so a container
  that misbehaved a fortnight ago looked identical to one dying every thirty seconds.
  An active loop (`CrashLoopBackOff`) stays a red alert; a high count that is not
  currently looping is amber and says when the last restart was. The "kritisch" counter
  follows the same split.
- `make check` now runs `gofmt`, which CI has always enforced and `make check` never
  did — so a green local check did not imply a green pipeline. Verified by introducing
  drift and watching it fail.

### Added

- `looping` and `last_restart` on each component in `/api/v1/status`.

## [0.1.50] — 2026-08-17

### Fixed

- **Connecting from Moderation dropped you on Rooms.** The callback redirected to a
  hardcoded `/rooms` — on success *and* on failure — because the flow was built when
  rooms was its only caller. The origin now travels with the OAuth state and is mapped
  through a server-side allowlist, so a redirect target coming from the browser cannot
  become an open redirect.

### Changed

- **The Matrix admin access reconnects by itself.** It is still never written to disk —
  that stays deliberate — but a missing token no longer means a button to press: the
  panel restarts the authorization and returns you to the screen you were on. With a
  live Matrix session that is a redirect and back, no clicks.
- The connect panel's wording follows: the access is still *not stored*, and is now
  *automatically re-established* rather than something you must reconnect by hand.

### Notes

- The automatic attempt is refused when the previous one failed (`?error=` in the URL),
  when one was made in the last 30 seconds, and when session storage is unavailable —
  an automatic redirect on a failing condition is a redirect loop without them. It
  never fires on `403`: reconnecting cannot grant a permission Matrix has not given.
- The guard clears only after data actually loads, not when the connection reports
  itself established. A reconnect can succeed and have the next request refused, and
  clearing it there would re-arm the loop.

## [0.1.49] — 2026-08-17

### Added

- **`/rtc` now reports the SFU's UDP receive buffer.** LiveKit asks for 5 MB and gets
  ~426 KB on a default kernel, warns about it once at startup, and nothing ever
  surfaced that. The finding carries LiveKit's own two numbers, states whether packets
  are actually being dropped yet — currently none, so it is a capacity warning rather
  than an active fault — and names the host sysctl that fixes it, along with the fact
  that MatrixCtrl cannot apply it itself.
- The SFU's dropped-packet counter (`livekit_node_packet_total{type="dropped"}`) is
  now read. It was in this package's test fixture from the start and had never been
  parsed; only `type="out"` was.

### Notes

- Both readings come from the SFU's own network namespace — its startup log and its
  metrics — never from `/proc` in MatrixCtrl's process. MatrixCtrl does not run with
  `hostNetwork` and the SFU does, so a counter read here reports MatrixCtrl's own
  traffic: 320 datagrams against the node's 48009. It would have read zero drops
  forever.
- A missing startup line is reported as *unknown*, not *fine*. LiveKit logs it once,
  so a long or rotated log legitimately lacks it.

### Changed — no image or chart change

- **The compiled frontend is no longer committed.** `cmd/matrixctrl/dist` was 40
  tracked files, last rebuilt 2026-08-01 — sixteen days and roughly fifteen etappes
  stale, with no moderation screen in it at all. A bare `go build ./cmd/matrixctrl`
  therefore produced a binary serving an August 1st UI and said nothing about it.
  Released images were never affected (the Dockerfile builds the frontend itself), and
  neither was `make build`. Now generated and gitignored, with a single tracked
  `dist/.gitkeep` because `//go:embed` will not compile on an empty directory.
- **A binary built without a frontend says so** — one warning line at startup, and a
  page naming the cause and `make build` instead of a 404 that reads like a routing
  bug.
- CI's check changed from "does the dist directory exist" to "is the placeholder
  present, and have built assets been committed again".

### Fixed — tooling only, no image or chart change

- **The post-deploy UI check passed without checking anything.** `verify-ui.mjs`
  exited 0 when routes were *skipped*, so a run without `MATRIXCTRL_TOKEN` skipped ten
  of eleven routes and still reported success. A skipped route is now a failure unless
  `--allow-skip` is passed, and the summary counts what actually rendered.
- **Four screens were missing from that check.** `/users`, `/rooms`, `/rooms/{id}`
  and `/reports` had never been in the route list, so the report queue was rewritten
  by three consecutive etappes without the check once opening it in a browser. The
  room detail screen is opt-in via `--room-id`, since no id is valid on every instance.
- **Nothing ran the check.** Added `make verify-ui BASE=…`; it had existed only as a
  command copied out of a plan file, which is how its route list went stale unnoticed.

## [0.1.48] — 2026-08-17

### Added

- **The second report queue.** Synapse keeps reports about *users* separately from
  reports about *events*, with its own endpoints and its own id sequence. Moderation
  shipped in 0.1.46 knowing only about events, so an admin could empty that screen
  and still have an untouched queue behind it. Both queues now appear as tabs, and
  **both tabs always carry their count** — a tab without a number would reproduce the
  original problem in a smaller space.

### Fixed

- **A disposition could have been written against the wrong queue.** `report_id` was
  the primary key on its own, but Synapse numbers the two queues independently, so
  event report 5 and user report 5 would have been the same row: marking one handled
  would have marked the other, with no error anywhere. Migration 014 renames the table
  to `report_dispositions`, adds `kind`, and makes the key the pair. Existing rows
  are backfilled as `event`, which is what they all were.
- **An unknown `/api/` path answered `200 text/html`.** The single-page-app fallback
  caught unmatched API paths too, so a misspelled endpoint passed `res.ok` and then
  failed inside `JSON.parse`, pointing at parsing rather than at the wrong URL. API
  paths now answer a JSON 404, and a known path with the wrong verb answers 405 —
  a different mistake deserves a different answer. Every frontend route still loads
  from index.html, which is the half of this change worth testing (P2-31).

### Notes

- No detail endpoint for user reports, on purpose: Synapse's `/user_reports/<id>`
  returns exactly the five fields its list already carries, so a row expands in place
  rather than making a second call for an identical answer.
- The reporter/target search fields are searches, not filters — Synapse matches them
  with `LIKE '%…%'`, so one user id also matches every id containing it.

## [0.1.47] — 2026-08-16

### Added

- **Media quarantine, from the report queue.** The files a reported event references
  — the item, its thumbnail, and the encrypted-room variant — each with the
  quarantine state read from Synapse, and a button that blocks or releases it.

- **And it tells you when Synapse accepted the request and did nothing.** This is the
  point of the etappe. `POST /media/quarantine/...` returns `200 {}` in every case:
  not whether the media exists, not whether anything changed. And the store skips
  protected media:

  ```python
  if quarantined_by is not None:
      hash_sql += " AND safe_from_quarantine = FALSE"
  ```

  So quarantining a protected file looks exactly like success. Every write is
  followed by a read of `GET /media/<server>/<id>`, and the panel reports the state
  it found rather than the one it asked for — including "accepted and changed
  nothing", which the API itself cannot express.

  Note the condition: the filter applies on quarantine and not on release, so the two
  directions genuinely behave differently. Releasing protected media works.

### Notes

- Deleting media, and protect/unprotect, are deliberately out. Deletion has no
  inverse; protection is a control aimed at *other admins* and belongs with a
  permissions story that does not exist yet.
- Reading Synapse's source for this turned up a **second report queue** —
  `/_synapse/admin/v1/user_reports`, for reported users rather than events — that
  yesterday's moderation screen knows nothing about. Recorded as P2-30 rather than
  bolted on.

## [0.1.46] — 2026-08-16

### Added

- **The event report queue.** What users have reported, with the reported event's
  content, paging, and links to the room. This is Phase 2's last feature — users,
  rooms and moderation now all exist, which was the whole of *"existing ESS admins
  can drop element-admin"*.

- **A report can be marked handled or dismissed, with a note — and reopened.**

  Synapse has no "resolved" state. Its only way to clear the queue is
  `DELETE /event_reports/<id>`, which destroys the record, so the decision is stored
  in MatrixCtrl instead and Synapse's copy is never touched.

  That is deliberate, not a shortcut. A report is a user's statement that something
  was wrong; deleting it after acting on it means the next admin cannot see that it
  existed or that anyone looked. If one account is reported five times and each
  report is deleted as it is handled, the pattern — the part that actually matters —
  is erased one report at a time. Marking is also reversible, which §4.39 requires of
  anything that ships this early.

  *Handled* and *dismissed* are kept apart because they say different things to the
  next admin: something was done, versus the report was judged not to need it.

- **Encrypted events say so.** A reported `m.room.encrypted` event renders as "the
  server cannot read this, so MatrixCtrl cannot either" rather than as an empty
  message — the one case where absence of content is the finding.

### Changed

- **The Matrix connect panel is shared between rooms and moderation.** The
  authorization is per *operator*, not per page: one token serves both screens, so
  both show the same panel and start the same flow.

## [0.1.45] — 2026-08-16

### Fixed

- **The calls page had been warning "Die SFU kündigt eine veraltete Adresse an" for
  twelve days, falsely.** It is a `WARN` finding whose stated remedy is replacing the
  SFU pod — which drops any call in progress.

  `rtc_address_history` held **1778 rows**, split 889/889 between two Cloudflare
  addresses. The announced RTC host is proxied, so DNS returns two A records and the
  resolver rotates their order per query; the writer recorded `ips[0]`, so every
  other poll looked like the address had changed. The freshness check compares the
  newest observation against the SFU pod's start time, and with a "change" every few
  minutes that comparison could only ever say stale.

  The whole sorted set is recorded now, so a rotation is not a change — while a
  genuine change to the set still is.

- **And the verdict is refused when its premise does not hold.** More than one A
  record means something sits in front of the node — a home connection has one WAN
  address — so the DNS answer is not what the SFU discovered by STUN, and the
  comparison cannot answer the question however carefully it is fed. That is now
  `unknown` with the reason stated, rather than a confident wrong answer. An operator
  behind a CDN learns this check cannot see their setup, instead of being told to
  restart the SFU every day.

### Changed

- **The two RTC telemetry tables are bounded.** Samples are kept 90 days, address
  observations a year; the 1778 noise rows are deleted on upgrade. E44 shipped a
  table growing 1440 rows a day with no bound, on a single-node cluster sharing its
  disk with Synapse's database and media.

  This is not the retention decision P2-19 declines to guess at — that entry is about
  the audit log, which answers "who did what" and whose retention is a compliance
  question. These are operational telemetry with no such duty.

## [0.1.44] — 2026-08-16

### Added

- **The calls page says whether anyone is actually on a call.** Rooms and
  participants, live from the SFU. Both numbers were on the metrics port from the
  beginning and nothing read them — which is the same gap P1-10 was about: every
  check on this page was green while the feature was completely dead, because none
  of them asked whether anyone had ever used it.

- **A call history that survives the SFU.** Calls, talk time and SFU restarts over
  24 hours and per day.

  This had to be recorded rather than read. Every LiveKit counter is
  process-lifetime, and the post-upgrade hook deletes the SFU pod on every ESS
  upgrade to restore `hostNetwork` — so a statistics page built directly on those
  numbers would silently mean "since the last upgrade" while reading as "ever".
  Measured ten hours after the 26.8.0 upgrade, every counter on the server was `0`.

  The totals are exact regardless of the sampling interval: the underlying counters
  are cumulative, so a call that starts and ends entirely between two samples is
  still counted. Only its *timing* is bounded by the interval, and the page says so
  rather than leaving it to be discovered.

  A counter that comes back lower means the SFU restarted. That is recorded as a
  restart — never as a negative delta — and shown, because it explains a
  discontinuity in every other series on the page.

- **No participant identities are read or stored.** LiveKit's RoomService API would
  give room names and participants; it is deliberately not used. "Three people are
  in a call" and "who is in a call with whom" are different classes of data, and
  none of the questions this page answers needs the second.

## [0.1.43] — 2026-08-16

Three etappes in one release: 0.1.42 was written but never built — the image build
failed on type errors that the documented typecheck command could not see (below),
and the failure went unread.

### Added

- **Room detail.** Who is in a room and what its state says: members with paging,
  join rule, history visibility, encryption, creator, room version and event count.
  Reached by clicking a row in the room list.
- **Block and unblock a room** — the one moderation action that can be undone, and
  the only one this release ships. The dialog says what blocking *does not* do:
  it refuses new joins and nothing else, so nobody is removed, no message is
  deleted, and an ongoing conversation carries on. E36 grouped blocking with
  deleting as "destructive"; it is a flag with an inverse, and deleting is not.

  The state shown afterwards is read back from Synapse rather than assumed from the
  write having returned 200. Unblocking needs no confirmation — it restores the
  default and takes nothing away — while blocking does, because it changes what
  other people can do.

  Deleting a room stays out. It evicts every member and purges the history, and it
  gets its own etappe, as user deactivation did.

- **The upgrade screen shows what is happening.** A stepper for the phase
  (Konfiguration → Rollout → Hooks → Fertig), a ready-count over the workloads Helm
  is actually waiting for, and a live row per component saying whether it is
  waiting, pulling an image, starting, ready, or failing — with the reason on the
  failing one. It refreshes every three seconds, against a log line every thirty.

  The denominator is workloads rather than pods on purpose: pods churn as old ones
  terminate, so a pod-based bar can go *backwards* during a healthy upgrade.
  Workloads are also compared by generation, so a Deployment Helm has just patched
  reads as "not started" instead of "ready" — without that the bar opens at 100 %,
  falls, and climbs back.

  "Pulling an image" comes from the kubelet's own events. A pod cannot report it:
  its container status says `ContainerCreating` while pulling, mounting and
  attaching alike, and those have very different expected durations.

- **Every version in the list has a date, and expands to its release notes.** The
  date column had existed since the list was written and had never once been filled
  — `ListVersions` reads the registry's tag list, which is a list of strings. The
  dates come from the GitHub release index in a single cached request.

### Fixed

- **Connecting rooms to Matrix succeeded and then did not work.** The
  authorization completed, the token was stored, the page said connected — and every
  admin call came back 401, so the connect panel reappeared and the whole thing read
  as a button that does nothing. Synapse said why in one line: *Token doesn't grant
  access to the Matrix C-S API*.

  E36 deliberately left `urn:matrix:org.matrix.msc2967.client:api:*` out of the
  requested scope, reasoning that it grants more than rooms need. Under MSC3861 that
  scope is what resolves a token **to a user**; the admin scope says what that user
  may do, and without the other one there is no user for it to apply to. It is
  requested now, and the granted scopes are both checked before the token is kept —
  the earlier check looked only for the admin one, which is why a token that could
  never work was stored as if it had.

  The **device** scope is still excluded, so no device is created on the account.

- **The connect panel no longer makes a false claim about what it can reach.** It
  said the access was "nur für die Admin-Schnittstelle, nicht für deine Nachrichten".
  With the client-API scope that is untrue: the token *could* read the operator's
  messages, and MatrixCtrl does not. "Cannot" and "does not" are different claims.

- **"Show pre-releases" did nothing, and the toggle is gone.** The list renders the
  first 25 rows; every "pre-release" the chart has published is a `0.x.y-dev` tag
  from its first months, sitting at index 56 or beyond. All twelve are development
  builds under the naming scheme that preceded `-sha…`, and none is an upgrade
  target — so they are dropped where the other build tags are, on the server, and
  the control that revealed them is no longer needed. The pre-release *badge* stays
  for a `26.9.0-rc.1` that may one day exist.

- **The upgrade log no longer scrolls sideways.** `overflow-y: auto` computes
  `overflow-x` to `auto` as well, so the 200-character pinned-tag warning made the
  box scroll horizontally — and the auto-scroll only ever set `top`, leaving the view
  parked to the right while the blinking cursor sat off-screen at the left. Lines
  wrap now, and the log is collapsed behind a toggle: the structured panel above it
  is the thing to read.

- **"N Pods startet noch" counted pods that had already finished.** Jobs and old
  ReplicaSets leave `Succeeded` pods behind — six of them on the production namespace
  — and since they are never `Ready`, the rollout probe counted them as still
  starting, permanently. `PodState.Phase` had been carried since E31 and never read;
  this is what it was for.

- **A raw `{"revision":…,"status":…}` blob no longer appears in the upgrade log.**
  Nothing consumed it, and the human sentence saying the same thing was on the very
  next line.

- **The pinned-tag warning is grammatical.** The plural branch read *"Diese
  Komponenten werden wird vom Upgrade nicht mit aktualisiert."* — a fragment
  interpolated into a template that still carried its own verb. It was on screen
  during the 26.8.0 upgrade. The test that covered it asserted a `Contains` of a
  prefix, which the broken string satisfied.

- **`tsc --noEmit` type-checked nothing.** `web/tsconfig.json` is `"files": []` plus
  project references, so the command documented in CLAUDE.md and PROZESS.md exited 0
  regardless of the code. It is `tsc -b --noEmit` now. The four real type errors it
  had been hiding are fixed: `validateSearch` in the rooms route let TypeScript infer
  `{ error: string | undefined }`, and TanStack reads a nullable key as required, so
  every `<Link to="/rooms">` demanded a `search` prop.

### Performance

- **The upgrade history is fast after a restart, not just within one.** E39 cached
  the per-revision facts in memory and described the cold read as "once per process";
  with several deploys a day the operator met that path often, and measured 7.7 s.
  Those facts are immutable, so they are now persisted — the cold read costs once per
  revision rather than once per pod.

## [0.1.41] — 2026-08-16

### Security

- **The panel can no longer read or write secrets outside the namespace it
  manages.** E37 scoped the ClusterRole by resource type and verb but left it bound
  cluster-wide, so every rule written for the managed namespace applied in all of
  them — `kubectl auth can-i list secrets -n kube-system` answered **yes**. Those
  rules now live in a `Role` in the managed namespace, and what stays cluster-scoped
  is only what Kubernetes cannot namespace: nodes, node metrics, namespace
  get/list/create, and read-only discovery.

  E37 recorded this as blocked — a RoleBinding needs an existing namespace, and on a
  greenfield install the ESS namespace does not exist yet. That was written without
  testing it. Helm's `lookup` answers the question against the live cluster, so the
  chart creates the namespace only when it is genuinely absent, and an adopted
  install never renders the object at all.

  Proven before being applied, on a throwaway identity: 90/90 required permissions
  granted in the managed namespace, 7/7 forbidden powers denied, and eight confined
  permissions denied in `kube-system` while still granted in `ess`.

- **Two cluster-wide grants removed rather than relocated.** `SysInfo` listed
  persistent volume claims in every namespace and counted pods in `kube-system`.
  Each was one number on a diagnostics page; the second would have required the
  chart to write a RoleBinding into the cluster's most sensitive namespace,
  permanently. The storage panel now reports the storage of the deployment this
  panel manages.

### Changed

- Uninstalling MatrixCtrl can never remove the ESS namespace: the namespace the
  chart may create carries `helm.sh/resource-policy: keep`. Removing the admin panel
  must not remove the homeserver it administers.

## [0.1.40] — 2026-08-15

### Performance

- **The upgrade-history page went from 3.2–4.6 s to 25 ms.** It decoded every
  revision of the release — 14 on the production instance — to fill a table of four
  columns, on every single visit. Two of those columns are in the release secret's
  labels and free to read; the other two are fixed when Helm writes a revision and
  never change again, so they are decoded once and kept.

  Cold cost after a restart is one full read (~4.7 s), then 25 ms for every load
  after it. The old code path remains as the fallback for every way the fast path
  can fail, so the worst case is the previous latency rather than a wrong answer —
  and a live test compares the two paths row by row.

### Fixed

- **`max` on the history read did nothing.** Helm's `History` action accepts a
  `Max` and never reads it, so asking for 10 revisions returned 14 and cost the same
  as asking for 30. It now bounds both the result and the work done to produce it.

## [0.1.39] — 2026-08-15

### Fixed

- **A restart count no longer implies the wrong container.** The dashboard summed
  restarts across every container in a pod, so `ess-postgres` read **42** — which
  says "the database is crash-looping". It was not: `postgres` had restarted zero
  times and all 42 belonged to `postgres-exporter`, a monitoring sidecar. The total
  stays, since it is what `kubectl` shows, but when one container carries at least
  two thirds of it the row and the drawer badge now name that container.

  Deliberately silent when attribution would be a guess — single-container pods,
  zero restarts, or anything more even than 2:1, because three containers at 14 each
  is genuinely "the pod" and picking one would invent a culprit. Ties resolve by
  name so the answer cannot flicker between two identical reads.

### Documentation

- **Eight backlog entries claimed problems that no longer existed.** The whole
  2026-08-04 security review batch — P0-5, P1-16, P1-17, P2-26, P2-27, P2-28 — plus
  P1-6 and P2-1 had been fixed by E17, E29 and E35 and never struck through, so the
  public repo carried a document, maintained by the author, stating that this admin
  panel runs as root with wildcard CORS, no login rate limiting, a guessable
  signing-key fallback and session tokens in URLs. None of it was true.

  Every entry was re-checked against the source rather than against the etappe that
  claimed to close it, and each now names the code that closes it. `P1-11` had been
  marked done inside its body while its heading still read as open, which is how
  eight of them survived readers looking directly at them.

## [0.1.38] — 2026-08-15

### Security

- **The ClusterRole is no longer `cluster-admin` in all but name.** It granted
  `apiGroups: ["*"] resources: ["*"] verbs: ["*"]` plus `nonResourceURLs: ["*"]`,
  which turned any defect in this app — an auth bypass, an SSRF, a dependency RCE —
  from "the ESS deployment is compromised" into "the cluster is compromised". It is
  now enumerated: 7 API groups, 15 resources, named verbs, and read-only discovery
  paths.

  The comment defending the old rule claimed a tighter scope would break upgrades of
  releases containing CRDs and ClusterRoles. Measured against the chart, matrix-stack
  creates neither; its three `Role`s are namespaced and grant only permissions
  MatrixCtrl already holds, so Kubernetes' escalation prevention is satisfied without
  `escalate` or `bind`.

  Proven **before** being applied: rendered under a probe name, bound to a throwaway
  ServiceAccount, every entry asked of the API server as a `SubjectAccessReview`.
  88/88 required permissions granted; `create clusterroles`, `escalate roles`,
  `list customresourcedefinitions`, `create serviceaccounts/token`,
  `create pods/exec`, `delete namespaces` and `impersonate users` all denied.

- **A values flag that claimed to withhold cluster-wide secret access was removed
  rather than documented.** `rbac.discovery.allNamespaces`, off by default, was
  described as gating the permission Helm needs to scan every namespace for a
  release. It gated nothing: a ClusterRole bound by a ClusterRoleBinding applies its
  namespaced rules everywhere, so the base `secrets` rule — required for Helm's
  release storage — already granted it. A security control that does not control is
  worse than none, because it is believed.

### Added

- `internal/k8s/permissions.go` — the permissions MatrixCtrl needs, as data derived
  from call sites and from the kinds the chart renders, plus `Check`, which asks the
  API server via `SelfSubjectAccessReview` whether the running identity holds them.
  A diff against the chart would only prove the chart says what it says; this also
  catches a hand-edited role or a binding that was never applied.
- `KnownOverGrants` — the three permissions the role still grants beyond its purpose,
  because it is bound cluster-wide. Asserted by a test that **fails when they
  disappear**, so closing the gap announces itself instead of leaving three files
  describing a problem that no longer exists.

### Changed

- ESS discovery falls back to the configured namespace when a cluster-wide release
  scan is refused, and reports which of the two it did. "No ESS found" after a search
  that never left one namespace is a different answer from "there is no ESS".

## [0.1.37] — 2026-08-06

### Added

- **Rooms.** A list of every room on the homeserver, with search and paging, read from
  Synapse's admin API. Read-only: deleting, purging and blocking a room are each
  destructive in a different way and get their own etappe, as user writes did.
- Rooms use **the operator's own Synapse-admin authority**, not a service token. MAS
  grants the scope only to accounts with `can_request_admin`, so the privilege check
  happens in MAS rather than in this code. The panel can do what the person signed into
  it can do, while they are signed in.
- The authority is granted in **its own authorization**, deliberately not folded into
  login: a login that asks for a scope MAS might refuse could lock everyone out, and
  that cannot be tested from the server side.

### Security

- The Matrix refresh token is held **in memory only and never written to disk**. MAS
  access tokens live 300 seconds (measured, not assumed), so keeping a refresh token is
  unavoidable — but persisting it would leave a Synapse-admin-capable credential at
  rest in Postgres to save a sign-in. A restart costs one login instead.
- Signing out drops the Matrix session too, so a refresh token cannot outlive the login
  it came with.
- Synapse is reached in-cluster rather than through the public hostname, so an admin
  bearer token stops crossing the ingress and the tunnel for a call between two pods in
  the same namespace.

### Fixed

- **API errors now carry their status code.** The client threw a bare `Error`, so every
  frontend branch on a status was dead code — including a "this account is not an
  admin" explanation that could never have rendered.
- **A downstream credential expiring no longer signs you out.** The client ends the
  session on any 401, which is right for its own session and wrong for anything behind
  it; an expired Matrix token would have logged the operator out of MatrixCtrl every
  five minutes. Those cases answer 409 now.

## [0.1.36] — 2026-08-06

### Security

- **The session token no longer appears in the log.** Watching an ESS upgrade, the
  operator's own deploy wrote a valid session JWT in plaintext — one line per
  WebSocket connection — because the upgrade-log stream carried `?token=<jwt>` and
  chi's logger writes full URLs. Anyone who could read the log could take the session.
- **WebSocket handshakes now use a single-use ticket**, requested from an
  authenticated endpoint and spent by the connection it opens. Redaction alone would
  only fix *our* log; the URL still passes the ingress, the tunnel and any proxy in
  between. A spent ticket in any of those logs opens nothing.
- **`?token=` is no longer accepted on any route**, including WebSocket upgrades.
  0.1.30 narrowed it to handshakes for the good reason that a browser cannot set a
  header there. Narrowing where a credential may appear in a URL is not the same as
  stopping it from being logged.
- The request logger redacts credential-bearing query values (`token`, `ticket`,
  `code`, `client_secret`, `password`, …) and keeps the rest, so `?container=postgres`
  still helps debugging. It parses the raw query itself rather than using
  `url.ParseQuery`, which drops pairs it cannot parse — a token in a malformed query
  would otherwise have slipped through the sanitiser into the log.

### Fixed

- The stream's reconnect path fetches a fresh ticket per attempt. Reusing the previous
  one would fail every reconnect, since it was consumed by the connection that dropped.

## [0.1.35] — 2026-08-06

### Changed

- **The pod is now Guaranteed QoS**, so the panel is no longer among the first
  processes killed when the node runs out of memory. `requests` now equal `limits` for
  both containers, which moves `oom_score_adj` from 997 to -997.

  This came out of measuring 0.1.34's trigger instead of assuming it. MatrixCtrl was
  killed while holding **14 MB** against a 512Mi limit: the kernel logged
  `constraint=CONSTRAINT_NONE, global_oom`, meaning the whole node was exhausted — by
  an unrelated 18 GB process — not the container. There was no memory problem to fix.
  What the log did show is that kubelet derives the kill order from the memory
  *request*, so a 128Mi request against a 512Mi limit put the admin panel near the
  front of the queue.

  It creates no memory; it changes who is killed instead. The reservation is about 2%
  of the node, and the request/limit gap was buying nothing at 81Mi steady state.

  Note that QoS is a **pod** property: both containers need `requests == limits` or
  the class stays Burstable.

- Memory and CPU **limits are unchanged**. Lowering the 512Mi ceiling to match real
  usage was rejected — the peak during a Helm render has never been measured, and that
  would trade a rare collateral kill for a self-inflicted one.

## [0.1.34] — 2026-08-06

### Fixed

- **A slow-starting MAS no longer locks you out of your own panel.** The container was
  OOMKilled and restarted before MAS was serving; discovery returned a proxy error
  page, the single OIDC init attempt failed, and the panel showed a username/password
  box for eleven hours while MAS was healthy seconds later. OIDC now retries in the
  background with capped backoff and never gives up on its own — giving up after N
  attempts is the same lockout on a delay.
- The retry rebuilds from the **effective startup config**, not from the database.
  Reusing `ReloadOIDC` would have been a silent no-op here: it reads the DB, startup
  prefers env, and this deployment is env-configured. The logs would have claimed a
  recovery was running while nothing changed.
- The connect-OIDC setup flow wins over an in-flight retry — a person acting
  deliberately outranks a background loop.

### Added

- `/api/v1/auth/oidc/available` reports `retrying` alongside `enabled`. "This install
  uses local login" and "Matrix login exists but its issuer is down" look identical on
  screen and lead to opposite actions. The login page shows the distinction and polls
  until it can switch back on its own, with no reload.

### Security

- A transient IdP failure used to re-open the local password login on a public URL
  indefinitely, because bootstrap login is only disabled while OIDC is configured.
  That window now closes by itself. The endpoint reports only that a retry is running,
  never the discovery error — it is unauthenticated by necessity.

## [0.1.33] — 2026-08-05

### Added

- **Release notes for the version you are about to install**, shown on the upgrade
  page beside the button that starts it. Not decoration: 26.8.0's notes say "Upgrade
  Element Web to v1.12.25" and "Upgrade Synapse to v1.158.0" — exactly the upgrades
  the pinned image tags were silently preventing. The screen now says both what the
  version brings and, from the pin warning, what a pin will stop it bringing.
- **"Upgrade auf X" arrives with X selected.** The version travels from the list as a
  search parameter instead of being picked twice.
- Notes are cached per version and the cache is bounded — published notes do not
  change, and GitHub's unauthenticated limit is 60 requests an hour.
- "Could not be fetched" and "no notes published" are different messages, because
  they lead to different conclusions.

### Security

- The version becomes a URL path segment and is validated against a strict pattern,
  refused rather than escaped.
- Rendered markdown links only follow `http(s)`. A `javascript:` URL in third-party
  text must not become clickable in an admin panel.

### Fixed

- **The documented local deploy shipped the wrong image.** The chart's committed
  default is `image.tag: "latest"` — CI rewrites it to the exact version only when it
  packages a *released* chart — so a deploy from the working tree rendered `:latest`
  and ran whatever stale build containerd held. The first attempt at this release
  deployed `0.1.32` while `helm list` said `APP VERSION 0.1.33` and `rollout status`
  said success. [PROZESS §4](docs/PROZESS.md#4-verify--ship) now passes
  `--set image.tag` and ends the read-back with the container's own startup line,
  which reads the artefact rather than a declaration about it.

## [0.1.32] — 2026-08-05

### Added

- **The upgrade log says what the rollout is waiting for.** It used to be a clock:
  `Waiting for Helm rollout… (30s elapsed)`, fifteen times, while one pod sat in
  `Init:CrashLoopBackOff` with the explanation in its own logs. Now each tick names
  the failing pod, its container, the reason, and the container's error text.
- Pods that are merely starting are counted rather than narrated, and an unchanged
  diagnosis is not repeated — the useful line must not become wallpaper.
- **Image tags pinned behind the chart are reported before the rollout starts.** On
  the instance this was built for, four components were behind: MAS 1.15.0 against
  1.22.0, Synapse v1.151 against v1.158, Element Web v1.12.14 against v1.12.25,
  Element Admin 0.1.11 against 0.1.12. Chart upgrades had been updating templates
  while keeping old images, and nothing said so.
- The MAS pin is what made the 26.8.0 upgrade fail: chart 26.8.0 writes
  `database.password_file`, MAS 1.15 does not know the field, so it connected with no
  password at all.

### Deliberately not done

- Pins are **reported, not fixed**. Unpinning is an upgrade decision with
  consequences — a seven-minor-version MAS jump with database migrations — and it
  belongs to the operator.
- Only tags *older* than the chart's are reported. Running ahead is a choice, and
  anything not confidently orderable is left alone: a wrong "you are behind" costs an
  upgrade nobody needed.

## [0.1.31] — 2026-08-05

### Fixed

- **MAS asked "Continue to &lt;ULID&gt;?" instead of "Continue to MatrixCtrl?"** on the
  consent screen. The generator already writes `client_name`; instances registered by
  an earlier version do not have it, and there was no way to add it through the
  product.
- **Registration is now reconcilable rather than one-shot.** Connecting OIDC used to
  answer `409 Conflict` for ever after the first time, so any field the generator
  learned to write later could only reach fresh installs — every existing one was
  stranded with hand-edited YAML as the only route. The setup page now reports what
  the stored client is missing and offers to complete it.
- The reconcile never regenerates the client ID or secret, never overwrites a value
  that is already set, and refuses a fragment it cannot parse rather than replacing
  it.

### Changed

- A code comment claiming `client_name` was undocumented and might not render is
  replaced by the verification: MAS 1.15's published config schema lists
  `ClientConfig.client_name`. Config is also the only durable place for it — a
  database edit does not survive `mas-cli config sync`.

## [0.1.30] — 2026-08-04

Answers six of the seven findings from an external security review. The seventh —
the ClusterRole being cluster-admin in all but name — is deliberately separate: it
is the most likely to break upgrades and needs its own verification.

### Security

- **The session JWT no longer travels in a URL.** The OIDC callback handed it over
  as `/auth/callback?token=<jwt>`, and chi's request logger writes the full URL —
  400 of the last 400 log lines carried one, so the token was written to the
  application log by the very request that delivered it. It is now a **one-time code
  in the URL fragment**: fragments are never sent to a server, so there is nothing to
  log and no Referer leak, and the code is single-use with a one-minute life, so the
  copy left in browser history is spent.
- **`?token=` is accepted only on a genuine WebSocket upgrade**, judged from the
  request's own headers. It used to work on every route, which made any log line or
  link carrying one a usable session.
- **A failed `crypto/rand` now stops the process.** It used to fall back to a
  time-seeded string — and that fallback was reachable from the path that
  *persists* the JWT secret, so a bad first boot would have written
  `matrixctrl-fallback-<unix-nanos>` into the database as the permanent signing key,
  derivable from the pod start time Kubernetes publishes.
- **Login throttling** with per-IP and per-user counters, progressive backoff and a
  lockout. Counted in Postgres, because an in-memory counter would make "restart the
  pod and try again" the attack.
- **The container runs as non-root** (65532) with a read-only root filesystem, no
  capabilities and no privilege escalation. The ESS chart it manages already held
  its own workloads to this standard.
- **The CORS wildcard is gone.** The frontend is served by this same binary on the
  same origin, so nothing needed it.
- `RevokeSession` now checks the signing method, matching `ValidateToken` in the
  same file.

### Fixed

- The login backoff shifted without a bound: past ~62 failures the delay overflowed
  to **zero**, so the most persistent attacker would have waited the least. Reachable,
  because the counter keeps growing after a lockout expires. Found by a test.
- The switch to non-root would have broken config saving on every existing install —
  the config repo was owned by root from earlier versions. A one-shot `chown`
  initContainer fixes ownership; `fsGroup` was unavailable because it would also
  apply to the Postgres sidecar's volume, and Postgres refuses to start when its data
  directory is group-accessible.

## [0.1.29] — 2026-08-04

### Added

- **User write actions**: lock, unlock, deactivate, reactivate, grant/revoke admin,
  set password — each behind a confirmation that states what it actually does.
- **The dialogs carry the consequence, not "are you sure?"**, because every one of
  these verbs is narrower than it sounds: locking does **not** end existing sessions,
  unlock does not reactivate, reactivate does not unlock, and revoking admin leaves
  existing admin sessions intact. An operator locking a compromised account needs to
  know the attacker is still connected.
- Only the actions that fit the account's actual state are offered — no "unlock" on
  an account that is not locked.
- **Self-lockout is refused.** MatrixCtrl admits only MAS admins, so locking or
  deactivating yourself, or revoking your own admin, would close the door you need to
  reopen it. Refused too when the acting identity cannot be resolved: not being able
  to tell is not permission.
- `ConfirmDialog` moved into the shared primitives from the one route that had it
  inline, and now closes on Escape.

### Security

- **Deactivation never erases.** MAS defaults to asking the homeserver to GDPR-erase
  the account; MatrixCtrl always sends `skip_erase: true` and says so in the dialog.
- Passwords cannot reach the audit table — the audit middleware records no request
  bodies. Because of that the endpoints are verb-in-path (`/grant-admin`,
  `/revoke-admin`), so the trail says which way the change went without logging
  anything that must not be logged.

## [0.1.28] — 2026-08-04

### Added

- **Phase 2 starts: a user list.** Accounts from the Matrix Authentication Service,
  searchable, filterable by state, with cursor paging. Until now the answer to "show
  me the users" was "go use element-admin".
- **Locked and deactivated stay distinct** — separate timestamps, separate states,
  separate wording. Locked is reversible and usually temporary; deactivated is the
  account being gone, and an operator deciding what to do needs to know which.
- The page states that it reads **MAS**, which is authoritative for accounts under
  MSC3861, rather than implying it lists every user Synapse has ever seen.
- Bootstrap mode explains that the feature needs MAS credentials instead of showing
  an empty list that reads as "no users".

### Changed

- MAS admin access moved out of `internal/auth` into `internal/mas`, shared with the
  login path. The admin token is now cached with its lifetime instead of minted per
  call — the old behaviour doubled the request count for every page — and a `401`
  drops the cache and retries once, so a rotated secret costs one retry rather than
  every request until the process restarts.

### Not in this release

- Writes: lock, deactivate, set-admin, set-password. Each is destructive in a
  different way and needs confirmation plus audit entries.

## [0.1.27] — 2026-08-04

### Added

- **Calls / RTC can now check the ports from outside**, on an explicit click. E19
  recorded inbound reachability as a permanent unknown — true from inside, and it
  quietly implied nothing could be done. One request to an outside vantage point
  answered in seconds what three days of inside-out measurement could not.
- A **control** decides whether the result is believable: a port known to be open on
  an unrelated host. A blocked or broken checker reports everything as closed, and
  acting on that means reconfiguring a router that was already correct. Without the
  control, every result is `unknown` and the action says to change nothing.
- The result names the distinction that cost three days: a **port forward (DNAT)** is
  not the same as a firewall rule allowing the port.
- Untestable UDP ports are counted and stated rather than dropped — the most
  important port on an RTC deployment is UDP, and free checkers speak TCP.

### Privacy

- This is the only code in MatrixCtrl that leaves the cluster. It is `POST`, never
  runs on a page load or a timer, names both third-party hosts in the UI before the
  click, and stores nothing.

## [0.1.26] — 2026-08-04

### Added

- **Fields changed by hand are now visible.** E21 checks the patches a hook
  declares; it could not see an edit no hook knows about — which is the case P1-11
  was opened for, where an Ingress carried `ingressClassName: disabled` applied by
  hand and Helm's three-way merge preserved it through every upgrade in silence.
- The mechanism is `metadata.managedFields`: the API server records which manager
  set which field, so this is read rather than inferred. No manifest rendering, no
  curated list of fields to watch — a curated list only ever finds what someone
  already thought of.
- Two levels, because they are two statements: a hand-edit **no hook maintains**
  will never be restored by anything and is loud; one a hook maintains means someone
  bypassed the product and is quiet.
- Metadata-only listing, so the scan costs kilobytes rather than most of a megabyte
  per poll. Ownership lives in metadata; the spec is never fetched.

### Fixed

- `kubectl rollout restart`'s `restartedAt` stamp and ESS's own `matrix-tools` are
  not reported. On the production cluster they were three of eight findings — enough
  noise to teach an operator to skim past the two that mattered.

## [0.1.25] — 2026-08-04

### Added

- **Calls / RTC states which call paths the deployment supports**, before it reports
  on any of them. Calling is two independent mechanisms: Element Call routes media
  through the SFU, while a classic 1:1 call is peer-to-peer and never touches it,
  needing a TURN relay from Synapse instead. On 2026-08-02 the entire SFU path was
  repaired and verified from the internet and calling still failed, because the calls
  being made were the other kind — a full page of green about a component they never
  used.
- `turn_uris` is read out of the **live** Synapse ConfigMap rather than the chart
  values, for the reason P1-11 made expensive: intent and live state diverge. The
  config is a merged directory, so files are sorted and the last definition wins, and
  it is parsed as YAML — a commented-out `# turn_uris:` is not a setting, and
  `turn_uris: []` is present, empty and exactly as relayless as absent.

### Fixed

- The finding now names LiveKit's own TURN (`matrixRTC.sfu.exposedServices.turn`, on
  by default) and says it serves Element Call only, because it authenticates with
  LiveKit tokens rather than the REST scheme Synapse uses. Without that, an operator
  reads "no TURN", finds `turn.enabled: true` in the values, and concludes the panel
  is wrong.

## [0.1.24] — 2026-08-04

### Added

- **Calls / RTC now answers "has a call ever actually worked?"** Every check the
  product had reported on a component — pods healthy, ports listed, patches applied,
  signalling reachable — and none of them answered that. The SFU has counted it all
  along: `livekit_quality_score_count` and `livekit_forward_latency_ns_count` only
  move when media is being carried.
- The distinction that makes it usable: **rooms created but zero media samples** is
  a fault and says so, pointing at the media path rather than at signalling.
  **Zero rooms** is not a fault — nobody called — and is reported as unknown rather
  than as an alarm. An alarm that fires on a quiet night is switched off before it
  ever fires on a real one.
- `livekit_node_packet_total{type="out"}` rises on a completely idle SFU (16208 →
  18633 in two minutes with no participants), so it is shown for context and
  explicitly does not count as evidence.
- Every statement is scoped "since the SFU started", with the uptime, because the
  counters reset with the pod and a number without its window gets misread.

## [0.1.23] — 2026-08-03

### Fixed

- **The stale-address check only observed when someone opened the Calls page.**
  Which is exactly when they would notice the problem anyway. It measures *when* the
  public address changed, and that timestamp can only be as good as the observation
  interval — a history built from page views has gaps precisely where nobody was
  looking. A background watcher now resolves the announced host every five minutes,
  bounding the error to five minutes against a change that happens about daily.
- A failed lookup still records nothing, so a cluster that briefly loses DNS does
  not appear to change address every five minutes.

## [0.1.22] — 2026-08-03

### Added

- **Calls / RTC now says when the SFU is announcing an address that no longer
  routes.** LiveKit discovers its public address once, at startup, and offers it in
  every ICE candidate for as long as the process lives. A consumer line is
  re-addressed roughly every 24 hours: DynDNS updates the record so clients resolve
  the correct address, while the SFU keeps naming the old one. Media goes nowhere
  and everything else looks healthy. Seen twice on a production instance, 22 hours
  apart.
- The check never reads the announced address, because LiveKit does not expose it
  anywhere except a log line. It compares two timestamps instead — when the SFU pod
  started, and when the announced host's DNS answer last changed. Same answer, no
  dependency on a log format.
- **A "restart SFU" button**, because the fix is otherwise a daily manual chore. It
  deletes the pod rather than rolling the deployment: with `hostNetwork`, one replica
  and `maxUnavailable: 0`, a rolling update deadlocks — the old pod holds the ports
  the new one needs, and the replacement waits forever while reporting nothing wrong.
- Before any change has been observed, the verdict is **unknown**, not "fine". A
  fresh install has seen nothing, and having seen nothing is not evidence.

## [0.1.21] — 2026-08-03

### Added

- **Drift detection: the dashboard now says when a hook patch is no longer in
  effect.** A Helm upgrade run outside MatrixCtrl re-rendered the RTC SFU without
  `hostNetwork: true` and reset three Services to `externalTrafficPolicy: Cluster`.
  Calling broke. Every panel stayed green, because "the hook is enabled" and "the
  hook's effect is in the cluster" are different statements and only the first one
  was ever checked.
- The check needs no list of fields to watch: each hook already records the
  resource, the name and the patch. The live object is fetched, the patch is applied
  **in memory**, and if nothing would change, the patch is in effect. That is exact,
  and it covers hooks that do not exist yet.
- `unknown` is reported as itself. A cluster read that fails, a resource that is
  absent on a greenfield install, or a patch that cannot be parsed are never shown
  as satisfied — the whole defect being fixed was an unknown reading as fine.

### Fixed

- The upgrade stream's progress heartbeat could emit one more line after being
  stopped, so a final message could end up underneath it. `stop()` now waits for the
  emitter instead of only signalling it. Found as a test that failed about one run
  in twenty under parallel load.

## [0.1.20] — 2026-08-02

### Changed

- **The dashboard is fast when you arrive, not only once you are already there.**
  A cold `/status` cost ~4.7 s and showed a skeleton for all of it, because reading
  the Helm release took ~4.3 s. It no longer does: **cold 4.32 s → 505 ms.**
- The fix was not the one on the backlog. Keeping a cache warm and serving
  stale-while-revalidate both treat the 4.3 s as a fact of life; measuring showed it
  was not one. `action.NewGet` asks Helm's storage layer for the newest revision,
  and to find it that layer fetches and **decodes every revision** — 11 secrets,
  2.93 MB on the production release — to return one. The revision, the status and
  the modification time are in the secret's *labels*, and a metadata-only list reads
  those for all revisions in ~15 ms without transferring any release payload.
- **The 60-second staleness window is gone, not shortened.** A cached value used to
  be trusted because a timer had not expired; it is now trusted because the cluster
  still reports the same release secret. The warm path costs 14 ms instead of 2.5 µs
  as a result — a deliberate trade of an unmeasurable amount of speed for an answer
  that is never quietly out of date.
- Every way the fast path can fail falls back to the previous code, so the worst
  case is the old latency and never a wrong answer. A live test asserts both paths
  return the same thing.

## [0.1.19] — 2026-08-01

### Added

- **A Calls / RTC page that says what it cannot check.** Element Call was broken on
  a production instance while every signal MatrixCtrl produced was green — pods
  healthy, patches applied — because the half that decides whether a call connects
  (are the node ports reachable from the internet?) was never looked at, and its
  absence read as "fine".
  The page now lists the exact ports to forward **with their protocol**, read live
  from the NodePort services rather than from documentation, and states inbound
  reachability as **unknown** rather than omitting it. An inbound test needs a
  vantage point outside the network; inventing a green tick for it would repeat the
  original failure.
- It also surfaces that `turn-tls` runs with `externalTrafficPolicy: Cluster` while
  the other three SFU services use `Local` — the built-in hook covers three of four
  by design. Shown, deliberately not silently patched: whether TURN-over-TLS needs
  source-IP preservation is a question about Element's SFU, and changing cluster
  state on a hunch is how manual patches became fragile in the first place.

## [0.1.18] — 2026-08-01

### Added

- **An audit trail.** Every change made through MatrixCtrl — upgrade, config
  deploy, rollback, hook run, pod restart, OIDC connect — is now recorded with
  user, time, route and result, and readable under **Audit-Log**. Failed attempts
  are recorded too; read-only requests deliberately are not.
  The documentation had claimed since the first release that the writes existed.
  They never did: the table was created and nothing ever wrote to it.
- Request bodies, headers and query strings are **never** stored. Logging the
  payload would put MAS client secrets and config YAML into the audit table; what
  exactly changed is answered by the config repo's git history instead.

## [0.1.17] — 2026-08-01

### Fixed

- **An upgrade whose process died kept saying it was still running.** The terminal
  status is written by the goroutine driving the upgrade, so a pod restart in the
  middle left the row in its in-flight state forever — the production instance had
  one reading `running-hooks` a day after the release was `deployed` and both hooks
  had reported OK. Startup now closes such rows as `interrupted`, which is the
  honest label: the Helm revision may well have gone through, but whether the hooks
  ran cannot be recovered after the fact.
- The upgrade history was missing `running-hooks` and `interrupted` from its status
  styling, so both rendered in the same calm blue as `pending`. An unrecognised
  status is now shown as a warning instead of borrowing reassurance, and the labels
  are German like the rest of the UI.
- `/helm/history` shows a page title, like every other screen.

### Added

- Releasing a tag now publishes the GitHub Release too, with notes cut from this
  file. A missing changelog section fails the release **before** anything reaches
  GHCR.

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

[Unreleased]: https://github.com/bxnnyg/matrixctrl/compare/v0.1.19...HEAD
[0.1.19]: https://github.com/bxnnyg/matrixctrl/releases/tag/v0.1.19
[0.1.18]: https://github.com/bxnnyg/matrixctrl/releases/tag/v0.1.18
[0.1.17]: https://github.com/bxnnyg/matrixctrl/releases/tag/v0.1.17
[0.1.16]: https://github.com/bxnnyg/matrixctrl/releases/tag/v0.1.16
[0.1.15]: https://github.com/bxnnyg/matrixctrl/releases/tag/v0.1.15
[0.1.14]: https://github.com/bxnnyg/matrixctrl/blob/master/docs/plans/etappe-14-upgrade-stream-and-dashboard-latency.md
[0.1.12]: https://github.com/bxnnyg/matrixctrl/blob/master/docs/ROADMAP.md
[0.1.10]: https://github.com/bxnnyg/matrixctrl/blob/master/docs/ROADMAP.md
