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
running image is `0.1.14`, so a stranger following the README installs something
two months stale.

And the product claim remains unproven. 3 stars, 0 issues, no external users. The
"works for anyone" promise of Phase 1.5 rests entirely on a greenfield path that
**has never been executed once**, and our own instance structurally cannot test
it — ESS already exists, so the guards short-circuit.

**The biggest gaps, by impact:**

1. ~~Two months of work exists in exactly one place.~~ *Closed 2026-07-31.*
2. ~~Nothing enforces the definition of done.~~ *Closed 2026-07-31.*
3. ~~The main product claim is untested.~~ *Tested 2026-08-01 — and it was
   **broken**. Greenfield deploy failed on its very first real run and had never
   worked since the feature shipped. Four defects, now fixed and proven end to end
   (E15). Only connect-OIDC remains untested; it needs public DNS.*
4. **The public install path is wrong.** README → chart 0.1.0 → two months behind.
5. ~~The git history leaks the internal cluster hostname.~~ *Closed 2026-08-01
   (§4.14) — with one residual: GitHub still serves the old objects by SHA
   (P0-1b).*

**In short:** the safety net exists now. The next thing worth doing is proving the
greenfield path on a throwaway cluster — everything the project promises to
strangers depends on a code path nobody has ever run.

## 1. P0 — urgent

- ~~**P0-1 · Git history exposes the internal cluster hostname.**~~ **Done
  2026-08-01** (§4.14). The scope was larger than this entry claimed: besides the
  author of 39 commits, 30 commits carried the hostname **and the node's private
  IP** in `CLAUDE.md`. All 51 commits were rewritten with `git filter-repo` and
  force-pushed; the HEAD tree hash is unchanged, so no file content moved.
- **P0-1b · GitHub still serves the pre-rewrite objects by SHA.** The force-push
  removed the old commits from the branch, but `…/commits/<old-sha>` and the
  contents API still return them, so the hostname and IP remain fetchable by anyone
  who knows a hash. This is normal GitHub behaviour — unreachable objects survive
  until their garbage collection runs.
  *Fix:* ask GitHub Support to purge the unreachable objects and cached views for
  this repo. There were no forks and no PRs, so nothing else pins them; support is
  the only lever. Alternative (heavier, operator's call): delete and re-create the
  repo, which loses the stars and the URL's history.

  *What was exposed, objectively:*
  - **Exposure window:** 2026-05-27 → 2026-08-01, roughly **nine weeks**, in a
    public repository. Cloning and indexing of public repos is automated and
    continuous, so "nobody was looking" is an assumption, not a finding.
  - **What the values reveal.** The hostname is a structured name: it encodes
    environment, role, service and host index. That is topology — it implies the
    existence of sibling hosts under the same convention and the naming scheme used
    across the estate. The address discloses the internal subnet in use. Together
    they are reconnaissance material: someone who later obtains *any* foothold
    (VPN credential, phished session, a device on the LAN) starts with a map
    instead of having to scan for one, and targeted phishing gets more credible
    with real internal names in it.
  - **What was not exposed.** No credential, key, token or password was ever in the
    history — verified by scanning every blob in all 51 commits for private-key
    headers, provider token formats and password-shaped assignments. The address is
    RFC1918 and not routable from the internet, so it is not directly reachable.
  - **Persistence.** Removal from GitHub does not recall copies. Anything cloned,
    mirrored or indexed during the nine weeks is outside anyone's control, and no
    action taken here can retract it.
  - **What actually invalidates the leak** — as opposed to limiting further spread —
    is renaming the host and changing the address. Purging GitHub only stops new
    disclosure.

  *What the rename would actually cost here (checked 2026-08-01, not assumed):*
  All five PersistentVolumes on this cluster are `local-path` and pin themselves to
  the node **by name** via `nodeAffinity` — `ess/ess-postgres-data` (10 Gi),
  `ess/ess-synapse-media` (10 Gi), `matrixctrl/matrixctrl-config`,
  `matrixctrl/matrixctrl-postgres` and `kube-system/traefik`. Rename the node and
  every one of them becomes unschedulable: the PV still demands a node that no
  longer exists, and nothing starts.
  **All five also carry `persistentVolumeReclaimPolicy: Delete`.** A PV's
  `nodeAffinity` cannot be edited after binding, so the obvious repair — delete the
  PV objects and recreate them pointing at the new name — would have
  local-path-provisioner delete the backing directories, taking the homeserver's
  database and media with it.
  So this is a storage migration, not a hostname change. A safe sequence would be:
  back up first · patch all five PVs to `reclaimPolicy: Retain` and confirm ·
  rename · recreate the PV objects with the new affinity against the same
  `/var/lib/rancher/k3s/storage/...` paths · rebind the PVCs · verify · only then
  restore the reclaim policy. It needs a maintenance window and a tested restore.
  *Cheaper options worth weighing:* change only the address (a network change, no PV
  impact) if the subnet is the part that matters; or fold the rename into the next
  planned rebuild rather than doing it as an emergency. Against a disclosure that is
  an internal name plus an RFC1918 address, the risk of the migration may well
  exceed the risk of the leak — that trade is the operator's to make, but it should
  be made with these facts rather than without them.
- ~~**P0-1c · The etappe-18 plan re-published the hostname, in prose.**~~
  **Rewritten out 2026-08-01** (§4.19). The chapter explaining why the node name
  must never reach a public repository spelled that name out, and was pushed.
  Caught about 40 minutes later by a `git grep` over tracked files.

  **The scan it triggered found more than the trigger.** A sweep of every blob in
  the whole history — not just the current tree — turned up three classes of
  string, only one of which anyone was looking for:
  - the node name (1 path),
  - **the admin panel's own public URL** (5 paths, including
    `deploy/helm/matrixctrl/values.yaml`, i.e. the packaged chart's defaults),
  - **the five ESS hostnames derived from the server name** (13 paths, including
    `internal/auth/oidc.go`, a database migration, and the committed frontend
    build artefacts under `cmd/matrixctrl/dist`).

  The server name itself is public by definition — every federating Matrix server
  knows it — and stays. The admin panel's URL is not, and was never repository
  metadata. All of it is now replaced across all 83 commits and force-pushed;
  `v0.1.15` was retagged and the release pipeline re-published from the cleaned
  tree, so **the image in GHCR no longer carries those hostnames in its frontend
  bundle either** — which it did before.

  *What remains — measured, not assumed.* GitHub keeps `refs/pull/*` **permanently**.
  Dependabot, enabled two hours earlier by the same etappe, had opened **six** PRs
  from the old master; all six auto-closed with the force-push and their branches
  are gone, but the six refs remain fetchable. All 943 blobs behind them were
  fetched into a throwaway clone and scanned:

  | Present | Absent |
  |---|---|
  | node name (1 file) | any private IP |
  | admin panel URL (5 files) | any private key or provider token |
  | the five ESS hostnames + bare domain (9 files) | **any live secret** |

  The one secret-shaped value in the committed `values.bxnny.yaml` is literally
  `REDACTED-rotated` — etappe 10's public-repo hardening sanitised **and** rotated
  it before the repo was ever public. So the residue is hostnames, nothing more.

  *Calibration, so this is not over- or under-stated.* The Matrix server name is
  public by definition, and `matrix.` / `mas.` / `element.` / `admin.` / `mrtc.`
  follow ESS's documented convention — anyone holding the server name derives them
  in seconds. The node name discloses a naming scheme. The **admin panel's URL is
  the only genuinely non-derivable item**, and it is an HTTPS endpoint behind
  admin-only OIDC login.

  *Therefore:* deleting and re-creating the repository — the only remaining lever
  once Support is declined — would cost the stars, the PR history and the release
  pages in exchange for removing three hostnames and one subdomain, none of them a
  credential. Not worth it. **If the admin URL specifically matters, rename that
  subdomain instead:** unlike the node rename (P0-1b, a storage migration), it is a
  DNS record plus an ingress host plus the OIDC redirect URI, with no PV involved.

  *The control that was missing, now built:* `scripts/check-sensitive.sh`, run as a
  `pre-commit` hook (`git config core.hooksPath .githooks`) and as a CI step. The
  needles come from a gitignored `.sensitive-patterns` or a repository secret,
  because a list of strings-that-must-not-be-committed cannot live in the
  repository it guards. It reports **file names only, never the matched value** —
  printing it would put the string into a CI log, which is the same mistake one
  layer down. With no pattern source it skips, so outside PRs are not failed by a
  check they cannot satisfy.
- ~~**P0-2 · Etappes 11 and 12 are uncommitted.**~~ **Done 2026-07-31** —
  committed in nine reviewable slices (`9b226c5`…`c8fbd4d`). Tagging is still
  open and folded into P1-3.

## 2. P1 — must-have

- ~~**P1-1 · CI (S9).**~~ **Done 2026-07-31** — `.github/workflows/ci.yml` runs
  `go vet`, `go test ./...`, typecheck, unit tests and the build on push and PR.
- **P1-2 · Greenfield end-to-end test (S6) — deploy proven, connect-OIDC still open.**
  **Done 2026-08-01 for everything except the last step (E15).** Run on a throwaway
  k3d cluster, it found that **greenfield deploy had never worked at all**: four
  defects, each hidden behind the previous one, are written up in
  [the plan](plans/etappe-15-greenfield-first-half.md). After fixing them,
  MatrixCtrl built a complete working ESS from an empty cluster — all eight
  components ready, Synapse answering `/_matrix/client/versions`.
  *Still open:* connect-OIDC, which fetches MAS's `/.well-known/openid-configuration`
  over its **public** URL and so needs real DNS and a certificate the Go client
  accepts. That is option (a) — a throwaway VM with a public subdomain — and is the
  last unproven step of the product claim.
- ~~**P1-3 · Release coherence (S8).**~~ **Done 2026-08-01 (E16).** `v0.1.15` is
  published by CI and was verified by pulling it, not by reading a green tick:
  `helm show chart` without `--version` resolves to 0.1.15, the released chart pins
  `image.tag: "0.1.15"`, and the cluster now runs an image **pulled from GHCR** —
  the local copy was deleted first so the pull had to be real (6.1 s, 25.7 MB, in
  the pod events).
  *Four attempts failed before that, and the cause was not in the code:* the GHCR
  packages had been created by hand with a PAT and never granted the repository's
  Actions token write access. Which is also why GHCR sat at `0.1.9` for two months
  — publishing had always been a human with a PAT, so there was never an automation
  permitted to write. Documented as one-time setup in [RELEASING.md](RELEASING.md).
  *Also fixed on the way:* the instance values file pinned `image.tag: 0.1.12`
  while 0.1.14 ran, surviving only because every deploy passed `--set image.tag`.
  Released charts pin their own image, so that pin is gone.
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
- ~~**P1-7 · The upgrade log stream dies mid-upgrade and reports it as failure (S2).**~~
  **Done 2026-08-01 (E14).** All four defects fixed: long Helm operations emit
  elapsed-time progress every 30 s (at all four blocking call sites, not just the
  upgrade one), the socket carries a 20 s application-level heartbeat, the client
  asks the existing status endpoint what happened before reconnecting with backoff,
  and a clean close is no longer reported as an error. Two further defects were
  found while fixing it: dropped subscribers were never removed from `subs` (a leak
  that reconnects would have made routine), and the terminal status was read outside
  the mutex. Original report below.

  <details><summary>Original finding</summary>

  Reported by the operator 2026-08-01 during the real 26.7.2 upgrade: the log
  stopped after `Loaded 18 config slices from config store.` and printed
  `[Verbindung getrennt]`. **The upgrade itself succeeded** (revision 22,
  `deployed`) — only the UI lost it. Four defects stacked:
  1. `helm.Upgrade()` blocks between `helm.go:160` and `helm.go:174` and emits
     nothing, so the socket is idle for minutes. Traefik's default `idleTimeout`
     is 180 s and closes it.
  2. No keepalive on either side. `golang.org/x/net/websocket` has no ping/pong
     at all — a heartbeat has to be an application-level message, or the handler
     must move to a library that supports control frames.
  3. `web/src/lib/ws.ts` never reconnects, and the upgrade status is never
     re-polled over HTTP, so the outcome is unrecoverable once the socket drops.
  4. `ws.onclose` prints `[Verbindung getrennt]` unconditionally — a clean close
     after `done` looks identical to a crash.
  *Why it matters:* upgrading ESS safely is the product. An operator who sees this
  cannot tell a working upgrade from a broken one, and the honest reaction is to
  start intervening in a cluster that was fine.
  </details>
- ~~**P1-8 · The dashboard is slow because every poll re-reads the whole Helm release (S4).**~~
  **Done 2026-08-01 (E14).** `/status` went from ~1.9–3.2 s to **~0.14–0.25 s**,
  measured through the public ingress. Three causes, not one: the Helm read is now
  cached (§4.15), the reads run concurrently, and — found only after the first two
  fixes — client-go's default QPS 5 / Burst 10 was throttling the process against
  itself for a steady ~1.1 s per request (§4.16). Original report below.

  <details><summary>Original finding</summary>

  Reported by the operator 2026-08-01. `/status` runs six calls **serially**, and
  `GetRelease` uses `action.NewGet`, which fetches and decompresses the entire
  release — manifest, hooks, every chart file — out of a 416 KB secret, purely to
  keep the seven scalars in `ReleaseInfo`. Measured on the live cluster:

  | Call | Latency |
  |---|---|
  | list deployments / statefulsets / nodes / pods, metrics-server | 535–965 ms each |
  | **helm release read** | **~4 000 ms** |

  `helm get metadata` and `helm list` were measured too and cost the same ~4 s —
  they all decompress the same secret, so there is no cheaper SDK call. The fix is
  to cache `ReleaseInfo` and invalidate it on upgrade/rollback, and to run the
  remaining calls concurrently. The dashboard polls this every 15 s.
  *Corroborating hint:* `Get` already carries an 8 s `context.WithTimeout` — the
  slowness was known when it was written, and worked around rather than fixed.
  </details>

## 3. P2 — worth doing

- **P2-1 · There is no audit trail at all (S10).** This entry used to read "the
  table and the middleware writes exist; nothing reads them back. Cheap." That was
  **wrong**, and checking it took thirty seconds: `grep -rni audit --include=*.go`
  returns nothing, and `SELECT count(*) FROM audit_log` returns 0 after two months
  in production. Only the table was ever created — no writer, no reader.
  *Why it matters:* MatrixCtrl can upgrade a homeserver, rewrite its config, roll
  it back and switch its authentication, and afterwards nothing says who did what.
  The config repo's git history covers config edits only, and attributes every
  commit to `MatrixCtrl` rather than a person.
  *Third documented claim found to have never been true*, after "greenfield deploy
  works" (E15) and "Vitest + Testing Library" (§0 above). A status written from
  intent rather than observation decays into a lie, and the lie survives because
  nobody re-checks their own notes. Planned as
  [etappe 17](plans/etappe-17-audit-trail.md).
- **P2-2 · Build artefacts are committed.** `cmd/matrixctrl/dist` is 32 tracked
  files, so every UI change produces a diff full of hashed bundles and hides the
  real change during review. Generate it at build time and gitignore it.
  *Sharper after E14:* the tracked copy was found **stale** — the frontend fix had
  been built, deployed and verified while the embedded copy still held the previous
  bundle. The container image builds its own frontend, so the running pod was
  correct and nothing looked wrong; only a plain `go build ./cmd/matrixctrl` would
  have embedded the old UI. A tracked artefact that can silently disagree with its
  source is worse than the noisy diffs this entry was originally about.
- **P2-7 · Publish an arm64 image again (S8).** Releases are `linux/amd64` only.
  Two attempts at multi-arch failed in the image step: the first spent 25 minutes
  emulating the frontend build (fixed in the Dockerfile — builder stages now run
  natively and Go cross-compiles), the second failed in under four minutes, so
  something else in the arm64 path breaks. The runtime stage's `apk add` runs under
  QEMU and is the prime suspect; a local reproduction died in exactly that spot.
  *Fix:* stop emulating altogether — build each architecture on its own runner
  (`ubuntu-24.04-arm` is free for public repos) and merge the manifests. Job logs
  need a token to read, which is why this was dropped rather than diagnosed further
  under a tag.
  *Why it matters:* k3s on ARM boards is a realistic home-server case, and the
  README does not currently say the image is amd64-only.

- **P1-10 · Element Call is unreachable: the RTC host has no path from outside (S14).**
  Found 2026-08-02, after P1-9 was fixed and calling was *still* bad. The decisive
  measurement was LiveKit's own metrics: `livekit_room_total 0`,
  `livekit_participant_total 0` — **no client has ever joined this SFU.** The calls
  the operator was making were legacy Matrix P2P, which is exactly why they worked
  between some endpoints and not others, and why mobile was worse than desktop:
  P2P cannot traverse the carrier's CGNAT.
  The chain: `.well-known` correctly advertises `rtc_foci` → the client tries the
  token endpoint on the RTC host → **connection never establishes** → Element falls
  back to P2P. Inside the cluster the service is healthy (`405` on GET, `400` on an
  empty POST). The RTC host resolves to the operator's WAN address, while the
  Matrix and Element hosts resolve to a *different* public address and work fine —
  so RTC is the only name pointed at the home connection, and **TCP 443 was not
  among the forwarded ports** (30001, 30004, 31443, UDP 30000-40000).
  *Not verifiable from here, deliberately stated as such:* whether 443 reaches the
  node from the internet needs an outside vantage point — E19's permanent unknown.
  The zero-request log of the auth service is strong evidence, not proof.
  **What this says about the product:** every check MatrixCtrl performs was green
  while the feature was completely dead, because none of them asked *"has anyone
  ever actually used it?"*. LiveKit publishes that number on its metrics port and
  nothing read it. A room counter of zero on a server that has been up for days is
  worth more than any amount of health-check green.
- **P1-9 · The SFU announces a stale public IP after every forced reconnect (S14).**
  *Correction 2026-08-02: real, reproduced and fixed — but it was **not** why calling
  was bad. See P1-10. The stale address would have broken media once clients could
  reach the SFU at all; they never could. Two independent faults, and fixing the
  visible one first proved nothing.*
  Found 2026-08-02 on the production instance while the operator asked, again, why
  calling is unreliable. LiveKit discovers its external address by STUN **once, at
  startup**, and caches it. The ISP re-assigns the WAN address roughly every 24 h;
  DynDNS updates the record, so clients resolve the *correct* address — and the SFU
  keeps offering ICE candidates for the *old* one. Media goes nowhere. Restarting
  the SFU fixed it immediately, which is the proof.
  This was the real answer to a complaint that had been raised repeatedly and
  written off as "calls are just bad".
  **It is knowable from inside the cluster, and E19 put it in neither column of its
  own knowable/not-knowable table.** E19 was right that inbound reachability needs
  an outside vantage point. It never asked whether the address being announced is
  still the address clients are told to use — and both values are already in the
  product: the announced IP, and the DNS record `/rtc` already resolves.
  *Fix:* compare them, warn loudly when they diverge, offer a restart. **No external
  IP service is needed** — DNS is the reference the clients themselves use, so an
  ipify-style lookup would add a dependency and a second source of truth for
  nothing. Auto-restart only behind an explicit opt-in: it is a cluster mutation
  driven by a poll, and that is the operator's decision, not a default.
  *Open question, deliberately not guessed:* where to read the announced IP from.
  Parsing `"nodeIP"` out of the pod log works but is fragile; whether LiveKit exposes
  it on its HTTP port has not been checked.
- **P2-23 · The SFU deployment cannot be restarted at all (S14).** `kubectl rollout
  restart deploy/ess-matrix-rtc-sfu` hangs forever and reports nothing wrong. The
  deployment is `hostNetwork: true`, `replicas: 1`, `strategy: RollingUpdate` with
  **`maxUnavailable=0`**: the old pod must stay Running until the new one is Ready,
  and the new one can never bind the host ports while the old one holds them.
  Observed live — the replacement pod sat `Pending` for 23 minutes with
  `FailedScheduling: didn't have free ports`, while the old pod kept serving the
  stale IP. Only `delete pod` clears it.
  This matters beyond the annoyance: **any automation built for P1-9 that uses
  `rollout restart` will deadlock silently**, and the operator will believe the
  restart happened. `strategy: Recreate` is the honest description of a
  single-replica hostNetwork workload; it fits the existing built-in patch hook
  (`internal/hooks/builtin/ess_rtc_patches.go`) which already survives Helm
  upgrades.
- **P2-24 · The host UDP buffer is 24× smaller than the SFU asks for (S14).** LiveKit
  logs `UDP receive buffer is too small for a production set-up  current: 425984
  suggested: 5000000` on every start; the host runs the kernel default
  `net.core.rmem_max = 212992`. Honest scope: `RcvbufErrors` is currently **0**, so
  nothing is being dropped *yet* — because P1-9 meant almost nobody was connecting.
  It is a latent fault that surfaces under real call load, not the present cause.
  Worth surfacing on `/rtc` as a pre-flight check, since it is read-only and
  knowable from inside.
- **P2-8 · The dashboard sums restarts across containers, which misleads (S4).**
  The operator read "postgres: 30 restarts" and reasonably concluded something was
  badly wrong. It is three containers in one pod — 15 + 8 + 7 — and they are three
  unrelated stories: `postgres-exporter` genuinely crash-loops, while `postgres`
  and `postgres-ess-updater` last died in the *same second* as `element-admin`
  (2026-07-12T14:04:12Z, reason `Unknown`, exit 255), which is a node or kubelet
  restart, not a crash.
  A single summed number turns "one node blip two weeks ago" and "a sidecar dying
  every few days" into the same alarming figure. Show per-container counts in the
  roll-up, and group restarts that share a termination timestamp — simultaneous
  deaths are one incident.
  *Raised by the operator 2026-08-01, twice, because the first answer did not stick.*
- ~~**P2-9 · Nothing tells the operator that calling cannot work (S14).**~~ **Done
  2026-08-01 (E19).** The fix is not a reachability test — that needs a vantage
  point outside the network, and inventing one would repeat the failure. The page
  states inbound reachability as **unknown**, permanently, next to the exact ports
  and protocols read live from the Services. A test asserts that unknown finding
  appears in every code path including the healthy one, because the original bug
  was silence reading as reassurance. Original report:
  Element Call was broken on the production instance and MatrixCtrl showed nothing:
  all RTC pods healthy, all four SFU patches correctly applied, dashboard green.
  The patches are necessary but not sufficient — media needs *direct* inbound
  UDP/TCP on the SFU node ports, and an external prober showed 30001 and 31443
  closed from the internet. MatrixCtrl verifies the half it controls and stays
  silent about the half that actually decides whether calls connect.
  *Fix:* a reachability check for the announced `rtc_foci` URL and the SFU node
  ports, surfaced next to the RTC patch state. This is the concrete shape of the
  "public-IP drift / TLS-DNS" item already sketched for Phase 3.
  *Found 2026-08-01 while answering "why is calling broken" — the answer was not
  visible anywhere in the product.*

- **P2-19 · The audit log grows without bound (S10).** E17 added the writes; nothing
  ever removes them. On a 32 GB single-node cluster sharing its disk with Synapse's
  database and media, an unbounded append-only table is a real operational trap.
  *Deliberately not guessed at while building the writer:* the right retention for
  an audit trail is a policy decision (how long must "who did what" be answerable?),
  and a default invented by whoever wrote the INSERT is the wrong way to make it.
  Needs a decision first, then a scheduled delete plus a documented number.
- **P2-20 · The verification chain passed pages that had not loaded (S9).** Found
  2026-08-01 while shipping E19: `verify-ui.mjs` waited only for React to mount,
  which a sidebar and a skeleton placeholder satisfy instantly. `/status` costs
  ~4.7 s on a cold release cache, and the dashboard screenshot from that window
  was four grey boxes — reported as **PASS**. Now fixed by counting the page's own
  in-flight API requests, but the lesson is the entry: for weeks the chain was
  proving less than it claimed, and only *looking at the picture it produced*
  revealed it. **The screenshots are not a by-product of the check; on this
  occasion they were the check.**
- ~~**P2-21 · A cold `/status` still costs ~4.7 s (S4).**~~ **Done 2026-08-02 (E20).**
  Neither of the fixes proposed below was built. Both accept the 4.3 s Helm read as
  a fact and work around it; measuring first showed it was not one. `action.NewGet`
  asks the storage layer for `Last()`, which fetches and **decodes every revision**
  (11 secrets, 2.93 MB) to return the newest — while the revision, status and
  modification time are right there in the secret's *labels*, readable in ~15 ms
  without transferring any payload. Cold **4.32 s → 505 ms**, and the 60 s staleness
  window is gone rather than merely shortened (§4.20). Original entry:
  E14 took the warm path from ~3.2 s to ~0.18 s and that holds — but the first
  request after the 60 s release cache expires still pays the full Helm read, and
  the dashboard shows a skeleton for all of it. So the operator's original complaint
  ("the dashboard loads slowly") is *half* fixed: it is fast when you are already
  using it, and slow exactly when you arrive. Worth a background refresh that keeps
  the cache warm, or a stale-while-revalidate read so the page renders old numbers
  immediately.
- **P2-22 · `/helm/history` still decodes every revision (S2).** `ListHistory` uses
  `action.NewHistory`, which has exactly the bottleneck E20 removed from
  `GetRelease` — on the production release that is 11 decodes for one page load.
  The label trick does not transfer unchanged, because the history table shows each
  revision's **chart version**, and that is the one field not in the labels. So it
  needs its own answer: memoise per revision (revisions are immutable once
  superseded, so a decoded one never needs re-reading), or accept the cost for a
  page nobody polls. Deliberately left out of E20 rather than guessed at inside it.
  *Found 2026-08-02 while measuring E20; not yet measured on its own.*
- **P2-3 · Persist dashboard metrics.** The CPU/RAM sparklines live in memory and
  reset on reload, so "is this getting worse?" cannot be answered.
- **P2-4 · Release notes per ESS version.** `ess_versions.changelog` and
  `breaking_changes` exist in the schema and are never populated — the upgrade
  wizard asks the operator to jump versions with no information.
- **P2-5 · Decide the System page (§4.13).** Open question: the enriched dashboard
  now covers most of it. Keep, merge, or delete.
- ~~**P2-16 · An upgrade that finished still reads `running-hooks` (S2).**~~
  **Done 2026-08-01.** The cause was not the SQL — the terminal status is written
  by the goroutine driving the upgrade, so if that process dies in between (a pod
  restart, an OOM kill) the row keeps its in-flight status forever and nothing ever
  revisits it. Startup now reconciles them to `interrupted`, which is the honest
  label: the Helm revision may well have gone through, but whether the hooks ran
  cannot be recovered afterwards. The frontend was missing `running-hooks` *and*
  `interrupted` from its status map, so both fell through to the calm blue
  "pending" styling — an unknown status now renders as a warning instead of
  borrowing reassurance. Original report: The
  upgrade history shows `26.5.1 → 26.7.2` from 2026-07-31 21:59 as
  `running-hooks`, a day later, while the release itself is revision #22
  `deployed` and every hook reports OK. The `upgrade_history` row is never moved
  to its terminal state, so the one screen that answers "did that upgrade
  actually finish?" says no when the answer is yes.
  *Found 2026-08-01 while reviewing screenshots for the README — which is also
  why it is worth taking screenshots.*
- ~~**P2-17 · `/helm/history` has no page title (S7).**~~ **Done 2026-08-01** —
  the route was missing from the shell's title map and fell back to the app name.
  Original report: Every other screen shows a
  title and subtitle in the top bar; this one shows the app name. Cosmetic, but it
  is the kind of gap that only becomes visible when someone looks at the product
  as a whole instead of at the feature they are building.
- ~~**P2-18 · The release workflow should write the GitHub Release itself.**~~
  **Done 2026-08-01** — the tag run now cuts the `## [x.y.z]` section out of
  `CHANGELOG.md` and posts it with the built-in `GITHUB_TOKEN`. The changelog check
  runs early, next to the other guards, so a missing entry stops the release before
  anything reaches GHCR; the page is published last, so it only appears once the
  artefacts it describes exist. No personal token is involved in releasing.
- ~~**P2-13 · The GitHub repo surface was unconfigured.**~~ **Done 2026-08-01** —
  ten topics and a description (operator); homepage deliberately **empty**, because
  the only candidate was a live admin panel's URL (P0-1c); Wiki and Projects off,
  Discussions on; and the missing Releases for `v0.1.15` and `v0.1.16` created from
  the changelog. Three empty tabs read as an abandoned project — Discussions is the
  one worth keeping, since a wiki becomes a second documentation that rots next to
  `docs/`.
- **P2-14 · The documentation has no user-facing layer (S12).** 1242 lines in
  `docs/` and every one of them is written for a maintainer or an agent: DESIGN,
  PROZESS, ROADMAP, BACKLOG. A *user* gets exactly one file, the README. Nothing
  explains how to actually use the config editor, what a hook is and why you want
  one, or what to do when an upgrade fails. The product has features nobody is
  taught. *Counterpoint worth keeping in mind:* the internal docs are unusually
  well maintained — 81 internal links, none broken, and zero TODO markers in the
  code. The discipline exists; it is aimed at one audience.
- ~~**P2-10 · The product was invisible.**~~ **Done 2026-08-01 (E18)** — the README
  contained exactly one image, a licence badge, for a tool whose entire value is a
  user interface. Etappe 11 built a design system no reader had ever seen. Six
  screenshots now sit above the fold, generated by `verify-ui.mjs` with a new
  `--redact` flag so producing safe ones is a command rather than a habit.
- ~~**P2-11 · The README disclosed neither the German UI nor the project's age.**~~
  **Done 2026-08-01 (E18)** — a stranger installed this and got a German interface
  with no warning, from a two-month-old tool with one maintainer, at a moment when
  the headline feature had never worked. A status block now says all of it before
  the install command.
- ~~**P2-12 · No CHANGELOG and no dependency automation.**~~ **Done 2026-08-01 (E18)**
  — `CHANGELOG.md` reconstructed from the etappe log, and a grouped monthly
  `dependabot.yml`. Monthly and grouped on purpose: a PR queue nobody reads hides
  the security advisories instead of surfacing them.
- ~~**P2-15 · `helm.go` carried five unrelated responsibilities.**~~ **Done
  2026-08-01 (E18)** — 834 lines covering release state, upgrades, setup, streaming
  and hostname derivation. E15's fourth defect (two fixes that cancelled each other
  out) lived there, and finding it meant reading past four unrelated subjects.
  Split into four files, proven content-neutral. Ten files across the tree had also
  drifted out of `gofmt`; CI now fails on it.
- ~~**P2-6 · README prerequisites assume a cluster already exists.**~~ **Done
  2026-08-01** — a collapsed `<details>` block gets a bare Debian/Ubuntu server to
  k3s + Helm in three commands, so the happy path stays short for readers who
  already have a cluster. It also names the unstated dependency the install command
  carried all along: `ingress.certIssuer=letsencrypt-prod` assumes cert-manager and
  a matching `ClusterIssuer` exist, which nothing said before.
  *Raised by the operator 2026-08-01.*

## 4. P3 — someday / nice-to-have

- **P3-1 · Read-only role.** Today there is exactly one role: full admin.
- **P3-2 · English UI (S17).** The UI ships German only; the repo and docs are
  English. Phase 6, but it is the single biggest barrier to outside contributors.
- **P3-3 · Bulk config edit across sections.** Changing the server name touches
  several files by hand today.
- **P3-4 · Validate config against the running Synapse,** not only the JSON
  Schema — schema-valid values can still be rejected at runtime.
