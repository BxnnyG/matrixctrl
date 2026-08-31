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
> headless-browser check walks the functional routes after a deploy (fourteen
> since E49, and a run that skips them now fails instead of passing). What follows is
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

### Security review, 2026-08-04

An external static review of the Go backend, the Helm chart and the auth flow.
**All seven findings were re-verified against the code before being written down
here**, and one turned out to be worse than reported. What the review found clean is
recorded too, because a review that only lists faults cannot be used to judge
coverage: no `exec.Command` with user input, config slice names validated against a
fixed manifest (no path traversal), bcrypt for passwords, session revocation
implemented, OIDC state consumed atomically via `DELETE … RETURNING` (CSRF-safe), and
`RequireAdmin` defaulting to true.

- ~~**P0-4a · The ClusterRole is bound cluster-wide, so its namespaced rules reach
  every namespace.**~~ **Done 2026-08-16** (E40, `v0.1.41`, DESIGN §4.38). Every
  namespaced rule moved into a `Role` in the managed namespace. The `can-i` output
  that opened this entry is now reversed:

  ```
  kubectl auth can-i list secrets -n kube-system  →  no
  kubectl auth can-i list secrets -n ess          →  yes
  ```

  **The blocker recorded below was never tested, and was wrong.** Helm's `lookup`
  answers "does this namespace already exist" against the live cluster at install
  time, so the chart creates it only when absent — greenfield gets one, an adopted
  install never renders the object, and the ownership conflict this entry called
  "the whole etappe" does not arise. Ten minutes of testing against a paragraph of
  confident reasoning that cost the fix a day.
  Also removed rather than relocated: a cluster-wide PVC list and a `kube-system`
  pod count, each one number on a diagnostics page, the second of which would have
  required writing RBAC into kube-system permanently.
  Original entry: What is left after E37 scoped the role by resource type and
  verb. Helm's release storage needs `secrets` in the managed namespace, and a
  ClusterRoleBinding turns that into `secrets` everywhere. Measured, not inferred:

  ```
  kubectl auth can-i list secrets -n kube-system  →  yes
  ```

  Asserted in `k8s.KnownOverGrants` and checked by `TestKnownOverGrantsLive`, which
  **fails when the over-grant disappears** — so closing this announces itself instead
  of leaving three files describing a problem that no longer exists.
  **Why it is now the top entry:** read/write on every Secret in the cluster is most
  of what made P0-4 a P0. Type-and-verb scoping removed the escalation paths; it did
  not remove this.
  *Fix:* a `Role` + `RoleBinding` in the managed namespace for everything namespaced,
  leaving only `nodes`, `namespaces` and `metrics.k8s.io` cluster-scoped. The blocker
  is greenfield: `install.CreateNamespace = true` means the namespace may not exist
  when the chart is installed, and a RoleBinding cannot be created in a namespace
  that is not there. Granting `bind`/`escalate` so MatrixCtrl could create its own
  would re-open what E37 closed, so the chart has to create the namespace — which
  conflicts with Helm ownership when adopting an ESS whose namespace already exists.
  That trade is the whole etappe.

- ~~**P0-4 · The ClusterRole is cluster-admin in all but name.**~~ **Scoped
  2026-08-15** (E37, `v0.1.38`, DESIGN §4.35). Was `apiGroups: ["*"] resources: ["*"]
  verbs: ["*"]` plus `nonResourceURLs: ["*"]`.
  The comment defending it claimed a tighter scope "would break upgrades of releases
  that contain CRDs, ClusterRoles, etc." Measured against the chart, that was false:
  matrix-stack renders 13 kinds across 7 groups, creates **no** CRDs and **no**
  ClusterRoles, and its three Roles are namespaced and grant only permissions
  MatrixCtrl already holds — so Kubernetes' escalation prevention is satisfied
  without `escalate` or `bind`, which was the load-bearing question.
  Proven before applying, with the role rendered under a probe name and every entry
  asked of the API server as a `SubjectAccessReview`: **88/88 required granted**, and
  denied for `create clusterroles`, `escalate roles`, `list CRDs`,
  `create serviceaccounts/token`, `create pods/exec`, `delete namespaces`,
  `impersonate users`. What remains is P0-4a above.

- ~~**P0-5 · The session JWT travels in a URL.**~~ **Done 2026-08-06** (E29 + E35,
  `v0.1.30` / `v0.1.36`), closed in two halves and verified on 2026-08-15:
  the callback now redirects to `/auth/callback#code=…` — a *fragment*, which browsers
  never send to the server, so it cannot reach a log at all — and `extractToken`
  returns `""` for query parameters on **every** route including the WebSocket
  handshake, which authenticates with a single-use ticket instead. chi's
  `middleware.Logger` was replaced by `authmw.Logger`, which redacts credential-shaped
  query keys. Live check: `GET /api/v1/rooms?token=abc` → 401.
  *Original entry, for the record:* `auth.go:185` redirected to
  `/auth/callback?token=<jwt>`, `extractToken` accepted `?token=` on every route, and
  chi's logger wrote the full URL — 400 of the last 400 log lines carried one, so the
  token was written to the log by the very request that delivered it.
  *Fix (as planned, and as shipped):* a one-time, short-lived exchange code in the
  redirect, swapped for the JWT
  over a POST whose body is never logged; and the `?token=` fallback restricted to the
  WebSocket route, which is the only place a browser cannot set a header.


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

- ~~**P1-16 · A failed `crypto/rand` writes a guessable JWT key to the database
  (S1).**~~ **Done 2026-08-04 (E29), struck through 2026-08-15.** `randomKey()`
  (`internal/auth/bootstrap.go:92`) now calls `log.Fatalf` — there is no degraded
  mode worth having, since the alternative to not starting is an admin panel with
  forgeable sessions. The fallback still visible at `bootstrap.go:47` is a
  *different* one: the database was unreachable, so the key is ephemeral but still
  cryptographically random, and it is logged. Original entry: From the 2026-08-04 review, and **worse than reported**. The review noted
  the fallback in `NewBootstrap`, which is ephemeral. But `randomKey()` is also
  called on the **normal** path, in `getOrCreateJWTSecret`
  (`internal/auth/bootstrap.go:57`): the generated value is base64-encoded and
  `INSERT`ed as the instance's permanent JWT secret. If `crypto/rand.Read` fails at
  first boot, the persisted signing key becomes
  `matrixctrl-fallback-<unix-nanos>` — derivable by anyone who can see roughly when
  the pod started, which Kubernetes events publish. It survives every restart
  thereafter, and nothing logs that it happened on this path.
  *Fix:* fail hard. A process that cannot obtain a secure signing key must not start.

- ~~**P1-17 · No rate limiting on the bootstrap login (S1).**~~ **Done 2026-08-04
  (E29), struck through 2026-08-15.** `internal/auth/throttle.go`: progressive
  backoff, then outright refusal after 15 attempts for 15 minutes, keyed per IP and
  per user and persisted in `login_attempts`. Original entry: From the 2026-08-04
  review; verified — there is no throttle, backoff or lockout anywhere in the router
  or the login handler. bcrypt makes each attempt slow, which is a cost, not a limit.
  *Fix:* per-IP and per-user limiting with progressive backoff.


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
  `web/scripts/verify-ui.mjs` drives chromium over the functional routes and
  writes screenshots. First run: 9/9 clean.
  *Corrected 2026-08-17 (E49):* the list had drifted — `/users`, `/rooms`,
  `/rooms/{id}` and `/reports` were never in it, and a run that skipped routes still
  exited 0. Both fixed; the list is fourteen entries and a skip is a failure unless
  `--allow-skip` is passed.
- ~~**P1-6 · `hooks-failed` is silent (S3).**~~ **Done, struck through 2026-08-15.**
  `helm_upgrade.go:109` sets the status and three screens render it:
  `routes/helm/upgrade.tsx:252` explains it inline after a run, and both
  `history.tsx` and `index.tsx` label it "Hooks fehlgeschlagen" with a warning tone.
  Original entry: If a post-upgrade hook fails, the only
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

- ~~**P2-26 · CORS is a wildcard on every route (S1).**~~ **Done 2026-08-04 (E29),
  struck through 2026-08-15.** The middleware was removed rather than scoped;
  `router.go:42` now carries the reason — the frontend is served by this same binary
  on the same origin, so nothing ever needed it. Original entry: From the 2026-08-04 review;
  verified at `internal/api/router.go:158`. Auth is a bearer token rather than a
  cookie, so this is not directly exploitable — but the frontend is **served by this
  same binary on the same origin**, so nothing needs the wildcard at all.
  *Fix:* scope to the configured ingress host, or drop the middleware.

- ~~**P2-27 · The container runs as root with no securityContext (S8).**~~ **Done
  2026-08-04 (E29), struck through 2026-08-15.** `USER 65532:65532` in the
  Dockerfile; `runAsNonRoot: true`, `runAsUser: 65532` on the app container. The
  `fix-ownership` init container still runs as root deliberately — it exists to
  chown the mounted volume, which is why a pod-wide `runAsUser` would break it, and
  the template says so. Original entry: From the
  2026-08-04 review; verified — no `USER` in the `Dockerfile`, no `securityContext`
  anywhere in the chart templates. Meanwhile the ESS chart it manages sets
  `runAsNonRoot`, `readOnlyRootFilesystem` and drops all capabilities on its own
  workloads. The admin panel holds itself to a lower standard than the thing it
  administers.
  *Fix:* non-root `USER` in the image; `runAsNonRoot`, `allowPrivilegeEscalation:
  false`, `capabilities.drop: [ALL]` and a read-only root filesystem where the
  Postgres sidecar allows it.

- ~~**P2-28 · `RevokeSession` does not check the signing method (S1).**~~ **Done
  2026-08-04 (E29), struck through 2026-08-15.** Its keyfunc now rejects anything
  that is not `*jwt.SigningMethodHMAC`, matching `ValidateToken`, with a comment
  naming this entry. Original entry: From the
  2026-08-04 review; verified at `internal/auth/bootstrap.go:190` — its keyfunc
  returns the key unconditionally, while `ValidateToken` in the same file checks for
  `*jwt.SigningMethodHMAC`. Not exploitable today: the library refuses `none` without
  an explicit opt-in and an HMAC byte key is not a usable RSA key. It is on the list
  because the inconsistency is the kind that becomes real on a library upgrade
  nobody reads the changelog for.


- ~~**P2-1 · There is no audit trail at all (S10).**~~ **Done 2026-08-01 (E17),
  struck through 2026-08-15.** Writer, reader and UI all exist:
  `/api/v1/audit` (`router.go:83`), and production now holds rows rather than zero.
  Original entry, which is kept in full because its *lesson* outlived its subject: This entry used to read "the
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
- **P2-2 · Build artefacts are committed.** ✅ **Done 2026-08-17 (E50,
  [DESIGN.md §4.49](DESIGN.md)).** Untracked and gitignored; only `dist/.gitkeep`
  remains, because `//go:embed all:dist` will not compile on an empty directory. The
  entry undersold it: measured before the fix, the tracked copy was **sixteen days and
  ~15 etappes stale and contained no moderation screen at all**, so a bare `go build`
  produced a binary serving the UI of 2026-08-01 and reported nothing. A binary built
  without a frontend now says so at startup and serves a page naming the fix.
  *Original entry:* `cmd/matrixctrl/dist` is 32 tracked
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

- **P2-33 · Both report queues page on an unstable sort (S13).** Found 2026-08-17
  reading `get_user_reports_paginate`: Synapse orders both queues by `received_ts`
  alone, never by id. Reports sharing a timestamp therefore have no defined order
  between two queries, so paging can show one twice and skip another — most likely
  exactly when it matters, since a burst of reports about one incident is precisely
  the case that produces equal timestamps.
  *Why it was not fixed in E48:* the ordering is Synapse's, and the only fix on this
  side is to accumulate pages and sort client-side, which changes what a "page" is and
  what `next_token` means. That is a real design decision about the queue, not a
  patch, and E48 was already carrying a migration and a routing change.
  *Cheap partial:* order by `(received_ts, id)` locally within a page, which fixes
  display order but not the boundary between pages.

- **P2-31 · An unknown `/api/` path answers 200 with the SPA, not 404 (S9).** ✅ **Fixed 2026-08-17 (E48, [DESIGN.md §4.47](DESIGN.md)).** API paths now answer JSON 404, wrong verbs answer 405, and the SPA still serves every frontend route — asserted in `internal/api/router_test.go`, which was run against the old code first to confirm it fails.
  *Original entry:*
  Found 2026-08-16 while verifying E47's route registration: `PUT
  /api/v1/media/x/y/nonesuch` and `GET /api/v1/definitely-not-a-route` both return
  **200 `text/html`** — the index.html fallback catches everything the router did not
  match, including API paths. So a misspelled endpoint in the frontend passes
  `res.ok`, then dies in `JSON.parse` with a message that points at parsing rather
  than at the wrong URL.
  *Why it matters beyond tidiness:* it is §4.40's failure class in the routing layer —
  a 200 that means "no such thing". It also weakens every future probe of the form
  "does this route exist?", because only a *guarded* route is distinguishable
  (401 vs 200); an unguarded new route would be indistinguishable from a typo.
  *Shape:* mount an explicit `NotFound` (and `MethodNotAllowed`) handler on the `/api`
  subrouter returning the standard JSON error, and leave the SPA fallback to
  everything else. Small, but it changes a response code, so it wants its own
  before/after check rather than a drive-by edit during a ship.
  *Not a regression from E47* — true since the SPA fallback was introduced.

- **P2-32 · E47's protected-media case is source-verified, not live-verified (S13).**
  E47 ships on a finding read from Synapse's source: `quarantine_media_by_id` filters
  `AND safe_from_quarantine = FALSE`, so quarantining protected media returns
  `200 {}` and changes nothing. That reasoning drives `QuarantineResult.Changed` and
  is covered by unit tests, but the **HTTP round trip was never exercised against
  live Synapse**, so the definition of done in
  [the E47 plan](plans/etappe-47-media-quarantine.md) is not fully met.
  *Why it was not done:* the admin API needs authority, and under MSC3861 this
  deployment has **no service-level admin credential** — `users.admin=1` is 0 across
  all 4 accounts and authority comes from a MAS scope on the *operator's own* token,
  which MatrixCtrl stores per user. Verifying would have meant reading that stored
  credential out of the database to act as the operator, plus writing to production
  media and marking a real file protected. That is a bigger step than a verification
  warrants, and it is the operator's call, not mine.
  *Cheapest path:* the operator reports a message with an attachment and clicks the
  button — the round trip is then exercised by the intended path. Live media today:
  65 items, 0 quarantined, 0 protected, so the protected branch has no natural
  test subject until one is created deliberately.
  *Checked 2026-08-16 and it does not work with what is there:* the single existing
  event report (id 2) is an `m.room.encrypted` event — megolm ciphertext, no
  `content.url` — so `MediaInEvent` correctly finds nothing and the panel says
  "Keine Dateien.". Closing this needs a **new** report against a message with an
  attachment in an **unencrypted** room; an encrypted room hides the mxc URI from the
  server and no amount of panel work changes that.

- **P2-30 · Synapse has a second report queue that MatrixCtrl does not know about
  (S13).** ✅ **Done 2026-08-17 (E48, [DESIGN.md §4.46](DESIGN.md)).** Both queues now
  show as tabs, each always carrying its own count. Closing it turned up a bug that had
  not happened yet: the disposition table keyed on `report_id` alone, and the two queues
  number independently, so user report N and event report N would have been one row.
  Migration 014 makes the key `(kind, report_id)`.
  *Original entry:* Found 2026-08-16 while reading Synapse's admin source for E47:
  `rest/admin/user_reports.py` serves `GET /_synapse/admin/v1/user_reports` and
  `/user_reports/<id>` — reports about **users**, not events. E46 shipped the event
  queue and is silent about this one, so an admin clearing "Moderation" may have an
  untouched second queue behind it.
  *Why it was not folded into E47:* it is a queue with its own detail view and its own
  disposition semantics, not a footnote on a media etappe. Bolting it on would have
  given it exactly the shallow treatment E47's plan refused for quarantine.
  *Shape:* the same disposition table works — `event_report_dispositions` would need a
  kind column, or a sibling table — and the same connect gate applies.

- **P2-29 · Calls show no audit, no connections and no statistics (S14).**
  **Two of four done 2026-08-16 (E44, [DESIGN.md §4.42](DESIGN.md)):** live rooms and
  participants, and a recorded history of calls, talk time and SFU restarts that
  survives the pod. The inventory this entry demanded found the reason none of it
  could simply be read: every LiveKit counter is process-lifetime, and the
  post-upgrade hook deletes the SFU pod on every ESS upgrade.
  *Still open:* per-call audit entries with more than a count — that needs either the
  RoomService API (participant identities, deliberately not read) or Synapse-side
  call events, and neither is a small addition. And "Statistik ausweiten auf das
  ganze", which is a design question rather than a missing counter.
  Original entry: Reported
  by the operator 2026-08-16: "calls soll auch wenn möglich audit zeigen
  verbindungen zeigen maby auch statistik (statistik maby ausweiten auf das ganze) /
  logs länge".
  Four things, not one: per-call audit entries, a live list of current connections,
  RTC statistics, and a broader statistics story across the whole panel. E23 already
  reads LiveKit counters that only move when media flows, so the plumbing exists —
  but sessions and participants are not read at all today.
  *Needs an inventory pass first:* which of those numbers LiveKit actually exposes on
  this deployment, and which would have to be invented. §4.24 is the warning — the
  calls page once reported confidently on the SFU while the failing calls were legacy
  1:1, which the page had no way to see. A statistics panel built on whatever happens
  to be readable would repeat that.

- **P2-25 · GDPR erasure on deactivation is not offered (S13).** MAS's `deactivate`
  defaults to `skip_erase: false`, i.e. it asks the homeserver to erase the account.
  E28 always sends `skip_erase: true`, so MatrixCtrl currently cannot erase at all.
  *Why it was left out:* a one-click irreversible erasure is the wrong default for a
  panel, and it sits oddly beside a `reactivate` that cannot bring the data back. But
  an operator with a real erasure request now has to leave the panel for it.
  *Shape:* a separate, explicitly-worded action — not a checkbox on deactivate —
  with its own audit line and wording that names what cannot be undone.

- **P1-14 · MatrixCtrl should be able to install and manage a TURN relay (S14).**
  Requested by the operator 2026-08-04, immediately after P1-12's finding landed:
  "kannste das adden ... als one click install ... verwaltbar für den user".
  The ESS chart has **no** option for a Synapse-side relay, so every ESS install has
  the P1-12 gap permanently, and the product can currently only name it. Naming a gap
  it cannot close is half a feature.
  *Shape:* deploy coturn into the managed namespace, generate the shared secret,
  write `turn_uris` + `turn_shared_secret` into `synapse.additional`, and list the
  ports it needs on `/rtc` next to the SFU's. Every one of those four pieces already
  exists in some form — the config store writes ESS values, the hook engine applies
  patches, `/rtc` renders port lists.
  *Open questions to settle first, not to guess at:* whether coturn runs as a chart
  dependency or a MatrixCtrl-owned Deployment; whether it needs `hostNetwork` like
  the SFU does; how the shared secret is stored and rotated; what happens on
  uninstall. A relay that is installed but unreachable is worse than none, because
  clients will try it and wait for the timeout.
  *Precondition:* P1-13. A relay behind the same unproven port forward inherits the
  same fate, and shipping it before that is measured would be the fourth "it should
  work now".

- **P1-13 · ANSWERED 2026-08-04: nothing inbound reaches the node (S14).**
  Measured, finally, from outside the network — the vantage point E19 said was
  needed and which turned out to be one HTTP request to a public port checker:

  | checked from the internet | result |
  |---|---|
  | TCP 30001 on the operator's public address | **closed** |
  | TCP 443 on the same address | closed (expected — moved to the Cloudflare tunnel) |
  | TCP 443 on an unrelated public host | open — so the checker is honest |

  And the node itself has **only private addresses** on every interface. It sits
  behind the router's NAT, so a **DNAT port forward** is mandatory for anything
  inbound. The firewall screenshots from 2026-08-02 showed *allow rules*, which on
  most routers is a separate thing from a port forward: an allow rule permits
  traffic that is already addressed to the host, and does nothing to redirect
  traffic addressed to the router's public address. **That distinction is the whole
  three-day investigation.**

  One cause explains every measurement taken since 2026-08-02: ICE reporting
  `requestsSent: 8, responsesReceived: 0, requestsReceived: 0`; packet counters
  staying at 0 on all four ports; `nf_conntrack` empty during a live call; both
  participants reaching the SFU over signalling and neither ever exchanging a media
  packet; `livekit_quality_score_count` at 0 after rooms were created (E23's warning
  case, which the product now reports on its own).

  A Cloudflare tunnel cannot substitute: it is outbound-only and carries HTTP, while
  media needs real inbound UDP.

  *Also found while measuring:* the SFU's LiveKit config has `turn: {enabled: true,
  udp_port: 30004}` with **no `domain`**, and neither client produced a `srflx` or
  `relay` candidate in any attempt — only host candidates on private, VPN and
  Tailscale addresses. LiveKit cannot hand out a TURN URI without a domain, so the
  relay that is enabled is never offered to anyone. Secondary to the port forward,
  and worth fixing after it.

  *Three "it should work now" claims were made during this investigation and all
  three were wrong. Each rested on a real, fixed defect; none was the last one. The
  lesson stands, and is now paid for: a fixed cause is not evidence of the only
  cause. This entry closes on a measurement, not on a prediction — whether calling
  works after the forward is opened is the next question, not this one.*

- **P1-15 · MatrixCtrl should offer the outside-in check, opt-in (S14).**
  E19 recorded "inbound reachability cannot be tested from inside the network it
  terminates in" as a permanent unknown, and it is true — but it quietly implied
  that therefore nothing can be done, and that was wrong. A public port checker is
  an outside vantage point, and one request to it answered in seconds what three
  days of inside-out measurement could not.
  *Shape:* a button on `/rtc` that checks the listed ports from outside and reports
  open/closed per port, replacing the permanent "cannot be checked" with an answer.
  *Must be opt-in and must say so plainly:* it sends the deployment's public address
  to a third party. That is not a secret — it is in DNS — but sending it is the
  operator's decision, not the product's, and a status page that silently phones
  home is a status page nobody should run.
  *Honest limit:* the useful ports are UDP and free checkers test TCP. TCP 30001 is
  still decisive, because a router that forwards nothing forwards neither.

- **P1-13-original · Where the investigation stopped, for the record (S14).**
  Recorded so the next attempt starts from measurements rather than from scratch.
  **Established, each by measurement:** signalling works end to end from the
  internet (token endpoint returns the auth service's own error, CORS headers
  present, WebSocket upgrade succeeds); the client *does* reach Element Call — the
  SFU creates the room, mints identities and logs `starting RTC session`; the
  announced address matches the current WAN address and DNS.
  **Where it fails:** `removing participant without connection`. ICE reports
  `requestsSent: 8, responsesReceived: 0, requestsReceived: 0` on every candidate
  pair — the SFU hears nothing from the client and the client hears nothing back.
  Packet counters on UDP 30001/30002/30004 stayed at **0** across every attempt, so
  no media packet has ever reached the node.
  The SFU offers exactly one candidate, `type(host/)` on the NAT1To1 address — no
  `srflx`, no `relay`. One of the client's candidates is `100.110.142.x`, i.e.
  RFC 6598 carrier NAT.
  **Ruled out by measurement, not assumption:** stale announced IP (P1-9, fixed and
  re-verified), lost `hostNetwork` and `externalTrafficPolicy` patches (see below,
  restored and verified — the SFU now binds 30002/30004/30001 on the host), the
  RTC hostname and its routing (P1-10/P1-11), CORS, certificates, well-known.
  **Narrowed 2026-08-04, by two cheap measurements that should have been taken on
  day one:** the node's own WAN address is a normal public address — **not** RFC 6598
  carrier NAT — so the case in which no port forward could ever work is ruled out on
  the server side. And `/proc/net/nf_conntrack` is readable, which answers the
  question `tcpdump` was needed for: an inbound packet on 30001–30004 creates a
  conntrack entry. Read while idle it shows none, which proves nothing (UDP entries
  expire in ~30 s and nobody was calling) — but read **during** a call it is a direct
  yes/no. That is a one-command check now, where it was previously written off as
  unmeasurable.
  **Not yet answered:** whether a single inbound UDP packet reaches the node's WAN
  interface at all. That needs the conntrack read taken *during* a call. Everything upstream of that question is
  now known-good, which is worth more than it sounds: it means the remaining
  surface is one hop wide.
  *Three "it should work now" claims were made during this investigation and all
  three were wrong. Each rested on a real, fixed defect; none of them was the last
  one. The lesson is not "measure more" — every step was measured — it is that a
  fixed cause is not evidence of the only cause.*
- **P1-12 · Legacy 1:1 calls have no TURN server, and nothing says so (S14).** Found
  2026-08-02, after the entire MatrixRTC path was repaired and verified end to end
  from the internet — and calling *still* failed with "ringing → connecting → dead".
  `livekit_room_total` stayed at 0 **during** the call and the token endpoint logged
  no request, so the client never attempted Element Call at all. It was making a
  **legacy Matrix 1:1 call**, which is plain peer-to-peer WebRTC and needs a TURN
  relay from Synapse's own config. Synapse has `turn_uris` unset, and **the ESS
  chart offers no such option** — `helm show values` has no `coturn`, no
  `turnServer`, nothing. So on a carrier connection behind CGNAT, a legacy call can
  never connect, and the failure is silent on both ends.
  Ruled out on the way: CORS on the token endpoint (`access-control-allow-origin: *`
  on preflight and POST), reachability, certificates, routing.
  **This is a third independent fault behind one symptom** (see P1-9, P1-10). Each
  was real, each was fixed, and none of them alone would have made calling work —
  which is exactly why "I fixed it, try now" was wrong three times in a row.
  *Product angle — done 2026-08-04 (E24, [DESIGN.md §4.22](DESIGN.md)):* `/rtc` now
  states both call paths before reporting on either, and reads `turn_uris` out of the
  live Synapse ConfigMap. Production reports "klassische 1:1-Anrufe haben kein Relay".
  *Corrected while building it:* the chart **does** ship a TURN — LiveKit's own,
  `matrixRTC.sfu.exposedServices.turn`, enabled by default on 30004. It authenticates
  with LiveKit tokens, so it serves Element Call and **cannot** be used by Synapse,
  which needs the REST scheme. The earlier note that ESS has "nothing" was too coarse
  and would have made the panel look wrong to anyone who read the values.
  *Still open:* actually running a relay — see P1-14.
- ~~**P1-11 · Manual `kubectl patch` edits survive every Helm upgrade, invisibly
  (S2).**~~ **Both halves done (E21 + E25), struck through 2026-08-15** — the body
  already said so while the heading still read as open, which is the same defect as
  the rest of this reconciliation, one level down.
  **Cluster cleaned 2026-08-04, on the operator's instruction:** the leftovers this
  entry was written about are gone. `IngressRoute/matrix-rtc-tls` (71 days old, no
  chart, routing the *old* RTC host including a path-less catch-all into the SFU) and
  its `Middleware/matrix-rtc-cors` were deleted; namespace `ess` now contains no
  hand-built Traefik objects at all, and the three public hosts were re-checked
  afterwards. The `ingressClassName: disabled` and `ingress.class: ignore` fields had
  already been removed on 2026-08-02. **The detection gap is unchanged** — nothing in
  the product would have found any of this; it was found by hand, twice.
  **Done 2026-08-04 (E25, [DESIGN.md §4.23](DESIGN.md)).** The other half is closed,
  and *not* by the manifest-diffing this entry proposed — that needs a curated list of
  fields to watch, which only ever finds what someone already thought of. The API
  server records field ownership itself in `metadata.managedFields`, so the product
  asks who owns a field instead of inferring it. Production reports exactly one loud
  line: `ingress/ess-matrix-rtc: spec.ingressClassName`, human-owned, maintained by no
  hook — the same object this entry was opened about.
  **Half done 2026-08-03 (E21):** patches *a hook declares* are checked against the
  live object continuously and shown on the dashboard (§4.21). Original entry:
  Found 2026-08-02. The RTC Ingress carried `ingressClassName: disabled` and
  `kubernetes.io/ingress.class: ignore` — **neither is rendered by the chart**. Both
  were applied by hand 69 days ago, together with a stand-alone Traefik
  `IngressRoute` that did the real routing. Helm's three-way merge *preserves*
  fields it has never owned, so the exception outlived every upgrade and nothing
  ever mentioned it.
  The cost was concrete: changing the RTC hostname through config produced a
  correct Ingress that Traefik was still instructed to ignore, so the new host
  404'd while the old one kept working — a failure that looks like a chart bug and
  is not one.
  **This is the exact failure mode MatrixCtrl was built to prevent, and it cannot
  currently see it.** The hook engine re-applies *known* patches; nothing detects
  *unknown* ones.
  *Fix as originally written (superseded):* render the release's manifests and diff
  them against live objects. See §4.23 for why ownership beat diffing.
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
- ~~**P1-9 · The SFU announces a stale public IP after every forced reconnect (S14).**~~
  **Done 2026-08-03 (E22).** Detected by comparing the SFU pod's start time with the
  moment the announced host's DNS answer last changed — the announced address is
  never read, because LiveKit exposes it only in a log line. A restart action ships
  with it, deleting the pod because a rolling update of this deployment deadlocks
  (P2-23, also confirmed live). Original entry:
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
- ~~**P2-23 · The SFU deployment cannot be restarted at all (S14).**~~ **Worked around
  2026-08-03 (E22)** — the product's restart action deletes the pod instead. The
  deployment's own strategy is still wrong and `kubectl rollout restart` still hangs
  for anyone who types it; setting `strategy: Recreate` via the patch hook remains
  worth doing. Original entry: `kubectl rollout
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
- **P2-24 · The host UDP buffer is 24× smaller than the SFU asks for (S14).**
  ✅ **Done 2026-08-17 (E51, [DESIGN.md §4.50](DESIGN.md)).** Surfaced on `/rtc` with
  LiveKit's own numbers and the drop count, so it reads as the latent fault it is.
  Two corrections to this entry: the ratio is ~12×, not 24× — 24× compares against
  `net.core.rmem_max` (212992) while LiveKit reports 425984, since the kernel doubles
  `SO_RCVBUF` for accounting — and reading either that sysctl or `RcvbufErrors` from
  MatrixCtrl's own process would have been wrong, because both are network-namespaced
  and MatrixCtrl is not `hostNetwork`. Measured: 320 datagrams in this pod against the
  node's 48009.
  *Original entry:* LiveKit
  logs `UDP receive buffer is too small for a production set-up  current: 425984
  suggested: 5000000` on every start; the host runs the kernel default
  `net.core.rmem_max = 212992`. Honest scope: `RcvbufErrors` is currently **0**, so
  nothing is being dropped *yet* — because P1-9 meant almost nobody was connecting.
  It is a latent fault that surfaces under real call load, not the present cause.
  Worth surfacing on `/rtc` as a pre-flight check, since it is read-only and
  knowable from inside.
- ~~**P2-8 · The dashboard sums restarts across containers, which misleads (S4).**~~
  **Finished 2026-08-17 (E53, [DESIGN.md §4.52](DESIGN.md)).** The half that was
  missing was not a missing feature but a missed *call site*: E38 attributed the count
  in the table row and the drawer badge, and the dashboard's red alert — the loudest
  element on the page — went on naming the workload. It read "postgres in
  Restart-Schleife" for weeks while postgres had restarted zero times. Fixed together
  with the tense: the trigger was a lifetime counter, so it could not tell an active
  loop from history.
  **Half done 2026-08-15 (E38, `v0.1.39`).** The total stays — it is what `kubectl`
  shows and what an operator compares against — but it no longer travels alone: when
  one container carries at least two thirds of it, the row and the drawer badge name
  that container. `42×` becomes `42× · postgres-exporter`.
  Deliberately silent when attribution would be a guess: single-container pods, zero
  restarts, and anything more even than 2:1 — three containers at 14 each is
  genuinely "the pod", and picking one would invent a culprit. Ties resolve by name
  so the answer cannot flicker between two identical reads.
  **Re-confirmed by the agent on 2026-08-15, from the other side of the same
  mistake:** while verifying E37 I reported `ess-postgres-0` at 42 restarts and had
  to read per-container `/proc` state to establish that `postgres` had restarted
  **zero** times and all 42 were the exporter. The entry describes a defect that
  misleads the people who wrote it, three months apart.
  **The second half is still open:** grouping restarts that share a termination
  timestamp. Simultaneous deaths across unrelated containers are one node incident,
  not several crashes, and nothing yet says so. Original entry:
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
- ~~**P2-22 · `/helm/history` still decodes every revision (S2).**~~ **Done
  2026-08-15 (E39, `v0.1.40`, DESIGN §4.37).** Measured first, since the entry
  itself noted it never had been: **3.2–4.6 s on every load**, 14 revisions, against
  56 ms for a metadata-only list of the same secrets. Now **cold 4.7 s once per
  process, then 25 ms.**
  The entry proposed memoising per revision, and that is broadly what shipped — but
  the two obvious ways to build it are both wrong and were rejected by measurement,
  not review: 14 × `Releases.Get` costs 7.3 s against `History`'s 5.3 s, so the cold
  fill has to stay one bulk call; and the `modifiedAt` label is not a per-revision
  timestamp, so reusing E20's label trick for the "deployed at" column would have
  displayed confidently wrong dates.
  **A second defect the measurement exposed:** `ListHistory(name, max)` took a `max`
  that **Helm ignores** — `History.Run` never reads `h.Max` — so asking for 10
  returned 14 and cost the same as asking for 30. It is honoured now, and bounds the
  work as well as the result. Original entry:
  `ListHistory` uses
  `action.NewHistory`, which has exactly the bottleneck E20 removed from
  `GetRelease` — on the production release that is 11 decodes for one page load.
  The label trick does not transfer unchanged, because the history table shows each
  revision's **chart version**, and that is the one field not in the labels. So it
  needs its own answer: memoise per revision (revisions are immutable once
  superseded, so a decoded one never needs re-reading), or accept the cost for a
  page nobody polls. Deliberately left out of E20 rather than guessed at inside it.
  *Found 2026-08-02 while measuring E20; not yet measured on its own.*
- **P2-3 · Persist dashboard metrics.** ✅ **Done 2026-08-31 (E59,
  [DESIGN.md §4.59](DESIGN.md)).** Recorded server-side once a minute with ninety days
  of retention. The entry undersold it: the history did not merely reset, it *pre-filled
  a fresh page with the current value*, drawing a flat line that read as an hour of
  stability. And `allocatable` is now recorded next to usage, which is what makes a node
  shrinking from 32 cores to 6 visible after the fact rather than only in a screenshot
  somebody happened to take.
  *Original entry:* The CPU/RAM sparklines live in memory and
  reset on reload, so "is this getting worse?" cannot be answered.
- **P2-4 · Release notes per ESS version.** ⚠️ **Mostly stale, corrected 2026-08-31.**
  The problem this described is gone: E32 shipped release notes on the upgrade page,
  fetched from the published releases (`internal/helm/releasenotes.go`,
  `GET /api/v1/helm/versions/{version}/notes`, `NotesPanel` in upgrade.tsx). Nobody is
  asked to jump versions blind any more.
  *What is still true, and is now a tidiness item rather than a feature gap:*
  `ess_versions.changelog` and `breaking_changes` from migration 003 are dead columns —
  never read, never written. Either drop them or stop carrying them in the schema.
  *Caught because it was nearly built again* — the entry read as an open feature and
  the feature exists. Same failure as the eight entries E38 found; the defence is the
  same, which is to verify an entry against the code before acting on it.
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
- **P2-14 · The documentation has no user-facing layer (S12).** ✅ **Done 2026-08-31
  (E60, [DESIGN.md §4.60](DESIGN.md)).** [`docs/GUIDE.md`](GUIDE.md) covers the three
  things the README never did: what a hook is and why, the config editor's model, and
  recovering a failed upgrade. Writing it found a real bug — the `post-rollback` trigger
  was offered and fired by nothing, so a rollback dropped every manual patch (E61).
  *Original entry:* 1242 lines in
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

- **P2-34 · Persisting the Matrix refresh token, encrypted (S13).** Offered to the
  operator on 2026-08-17 while fixing the reconnect (E52) and **declined in favour of
  the silent reconnect**, which keeps nothing at rest. Recorded so the option is not
  re-derived from scratch next time the question comes up.
  *Shape if it is ever wanted:* AES-GCM in Postgres with the key held in the k8s
  Secret rather than the database, so a DB dump alone does not yield Synapse-admin
  authority — the holder of both Secret and database would. That is a real widening of
  the blast radius and is the reason E36 declined it in the first place.
  *What it would buy:* survival across restarts with no redirect at all, including
  when the MAS session itself has ended. The silent reconnect covers everything except
  that last case, where it costs one login.

- **P1-16 · A component can be `down` for 37 hours without the panel saying why (S4).**
  ✅ **First half done 2026-08-30 (E54, [DESIGN.md §4.54](DESIGN.md)).** A Pending
  component now says why, with the scheduler's own words and the effective request
  against the node's allocatable — `max(sum(containers), max(initContainers))`, which
  is what hid 4000m of Synapse's reservation inside an init container. Verified against
  a live unschedulable probe pod, not only by unit test.
  ✅ **Second half done 2026-08-31 (E55, [DESIGN.md §4.55](DESIGN.md)).** The apply path
  renders the chart and measures every workload against the largest node before the
  upgrade runs. Proven by rendering the real config of 2026-08-06: `ess-postgres` at
  8250m against a 6000m node, out of a values file that says 4000m.
  *Now open as **P1-16c**: should it refuse?* E55 warns and applies anyway. Blocking is
  the obvious next step and deliberately not taken yet — a false positive would stop
  every deployment, and the check has not run in anger. Revisit once it has been right
  a few times in production.
  *Original entry:*
  From the outage of 2026-08-16…18 ([DESIGN.md §4.53](DESIGN.md)): postgres was
  unschedulable after the node shrank from 32 cores to 6, and MatrixCtrl reported
  `down` — correctly and immediately — with no cause attached. The reason was in a
  `FailedScheduling` event for 35 hours and nothing surfaced it.
  *Why it is P1:* the product's stated job is Day-2 operations. "It is broken" is what
  `kubectl get pods` already says; "it cannot be placed, because it asks for 8500m on a
  6000m node" is the part an admin tool is for, and it is computable from data already
  fetched.
  *Shape:* when a pod is Pending, read its `FailedScheduling` event and, for the
  `Insufficient cpu|memory` case, render the arithmetic — the pod's effective request
  against the node's allocatable. The effective request must be
  `max(sum(containers), max(initContainers))`, which is exactly the subtlety that hid
  4000m of Synapse's reservation inside an init container that was only waiting.
  *Second half, cheaper and preventive:* warn at config-save time when a slice's
  requests exceed what the cluster can schedule. The panel writes those values; it is
  the last place that can catch the number before it becomes an outage at the next
  reboot.
