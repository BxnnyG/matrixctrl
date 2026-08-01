# Etappe 15 — Greenfield, as far as it can honestly go

**Date:** 2026-08-01 · **System:** S6 · **Addresses:** P1-2 (partly)

## Scope, and what it deliberately does not cover

The full claim is *deploy-ess → connect-oidc on a fresh cluster*. The second half
cannot run here: connect-OIDC fetches `/.well-known/openid-configuration` from
**MAS's public URL** (`internal/auth/oidc.go:52`), and the deploy wizard derives
public hostnames from the server name. A local cluster has neither resolvable DNS
nor a certificate the Go client accepts.

So this etappe is **option (c)** from the backlog: everything up to that wall, on a
throwaway k3d cluster with no DNS at all. It proves the largest untested chunk and
is explicit about the rest.

| Covered | Not covered |
|---|---|
| Installing MatrixCtrl from the **published** chart, exactly as the README says | connect-OIDC |
| First start on an empty cluster: migrations, bootstrap admin, no ESS present | Real TLS / DNS / cert-manager |
| The greenfield wizard's precondition checks and ESS chart pull | ESS actually serving federated traffic |
| Seeding per-section config from chart defaults | Element/MAS reachable over the internet |

A stranger's first fifteen minutes are exactly this path, so it is worth proving
even without the final step.

## Why this is worth doing even partially

Two months of "works for anyone" rests on a code path nobody has executed. Our own
instance **cannot** execute it: ESS already exists, so the wizard's guards
short-circuit. Until a fresh cluster has run it, every install instruction in the
README is unverified.

It also re-tests `v0.1.15` from the outside — the same artefact, on a machine that
has never seen this project.

## Approach

1. **k3d**, not kind: it runs k3s, which is what the project targets and documents.
2. **Install from `oci://ghcr.io/bxnnyg/charts/matrixctrl` with no `--version`** —
   the exact README command, so the docs are under test too.
3. `ingress.enabled=false` and reach the UI by port-forward. No DNS is available
   and pretending otherwise would only test a fiction.
4. Drive the API directly with a bootstrap token rather than the browser: the point
   is the backend path, and the UI is already covered by `verify-ui.mjs`.
5. **Tear down completely** and reclaim the disk.

## Risk, and why it is acceptable now

The throwaway cluster shares this host's single 32 GB root filesystem with the
**live** ESS volumes. That was the reason to refuse earlier; after reclaiming
caches there is 15 GB free and the whole ESS image set is ~1.2 GiB, so the test
needs roughly 4 GB. Disk is checked before starting and after each heavy step, and
the run is abandoned rather than pushed if free space falls below 6 GB.

Everything lives in Docker, separate from the production k3s.

## Definition of done

- A fresh cluster, MatrixCtrl installed from the published chart, pod healthy
- Bootstrap admin login works on an empty database
- `/api/v1/setup/status` correctly reports "no ESS here" — the branch our own
  instance can never reach
- The greenfield deploy path exercised to the point where it genuinely needs DNS,
  with that point documented precisely
- Cluster destroyed, disk reclaimed, findings written down

## Outcome (2026-08-01)

**The greenfield deploy had never worked.** Not "worked and regressed" — it failed
on the first line of the first real run, and had done so since the feature shipped
two months earlier. Our own instance cannot reach the code path (ESS exists, the
guards short-circuit), so nothing had ever executed it.

Four defects, each only visible because the previous one was fixed:

### 1. `helm install` rejected every deploy (the headline claim, broken)

```
values don't meet the specifications of the schema(s):
matrix-stack:
- wellKnownDelegation.ingress: Additional property host is not allowed
```

The wizard seeded `wellKnownDelegation.ingress.host`. Checked against
matrix-stack 26.7.2's `values.schema.json`: that ingress block has **no** `host`
property and sets `additionalProperties: false` — well-known is served at the
server name itself, which the chart derives from `serverName`. All six seeded keys
were checked; exactly one was wrong. Extracted to `greenfieldHostnames()` with
tests that pin the key set and explicitly forbid `wellKnownDelegation`.

### 2. The first container of every new install crashed

```
database: ping (after 1m0s): ... connection refused
```

`internal/db/db.go` waited 60 s for Postgres and exited 1. On a **fresh** PVC
Postgres must run `initdb` first, which takes longer. Kubernetes restarted it and
the second start succeeded, so a brand-new install greeted its operator with
`restarts=1` — from a tool whose own dashboard flags restarts as a problem. Raised
to 5 minutes: a slow start costs time, giving up early looks like a broken install.

### 3. A failed deploy could never be retried

```
ERROR: seed config: config repo already has 22 sections — refusing to overwrite
```

The failed attempt left the config repo populated, and the guard then blocked every
retry. Since *every* greenfield deploy failed, this is where every operator would
have ended up: stuck, with no way forward from the UI. Now existing config is kept
and the deploy continues. Deliberately **not** `force: true` — config may have been
prepared on purpose, and destroying it to retry would be a worse failure than the
one being fixed.

### 4. Fixing 1 and 3 cancelled each other out

With 3 fixed, the deploy kept the config from the failed attempt — including the
schema-invalid key from 1. The retry failed identically. The fix stops the bad
value being *written*; it did nothing about repos that already had it, which is
precisely everyone who had tried. `SetSectionValues` already took a removals
argument that was being passed `nil`; `greenfieldRemovals()` now strips the key on
every deploy, healing repos written by older builds.

**The lesson is about sequencing, not about any one bug.** Each defect was hidden
behind the one before it. No amount of code reading would have found #3 or #4 —
they only exist once #1 is fixed and someone tries again. An end-to-end run on a
genuinely empty cluster was the only thing that could surface them.

## Confirmed working on a cluster that had never seen this project

- `helm install oci://ghcr.io/bxnnyg/charts/matrixctrl` **exactly as the README
  says**, no `--version` — resolved to 0.1.15 and came up
- First start on an empty database: migrations, bootstrap admin, login
- `setup/status` correctly reports `ess_installed: false` — the branch our instance
  can never reach
- ESS version discovery from GHCR: 78 versions, correctly sorted (etappe 12's fix,
  first run outside our own cluster)
- **The README's password-recovery procedure**, executed verbatim after the
  bootstrap password was genuinely lost. Documentation tested, not asserted.
- **Etappe 14's progress emission**, which the plan had explicitly listed as
  unproven because there was nothing left to upgrade: `Waiting for install…
  (30s elapsed)` during the real, minutes-long install.

## The deploy then succeeded

With all four fixed, the same poisoned config repo deployed a complete ESS:

```
Config repo already has 22 sections — keeping them and continuing.
Server name set to test.invalid with derived hostnames.
Installing ESS 26.7.2 — this can take several minutes…
Waiting for install… (30s elapsed) … (6m 00s elapsed)
ESS installed (revision 1). Running post-install hooks…
ESS deployed successfully.
```

All eight components ready: postgres 3/3, synapse-main, MAS, element-web,
element-admin, haproxy, rtc-sfu, rtc-authorisation. Helm release `ess` revision 1,
`matrix-stack-26.7.2`, status deployed. And Synapse answered its own API:

```
GET /_matrix/client/versions
{"versions":["r0.0.1",…,"v1.12"],"unstable_features":{…}}
```

A working Matrix homeserver, built from an empty cluster by MatrixCtrl, using a
chart pulled from the public registry. That is the product claim, executed rather
than asserted, for the first time.

Cluster destroyed afterwards; disk went from 7.8 GB to 16 GB free. The production
cluster was untouched throughout — verified after teardown: 9 ESS pods running,
Matrix API and MatrixCtrl both answering 200.

## Still not covered

connect-OIDC. It fetches `/.well-known/openid-configuration` from MAS's **public**
URL, which needs real DNS and a certificate the Go client accepts. That remains
option (a) — a throwaway VM with a public subdomain — and is unchanged as the last
untested step of the product claim.
