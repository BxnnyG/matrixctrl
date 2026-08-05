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
