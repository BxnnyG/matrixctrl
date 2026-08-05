# Etappe 31 — Say what the rollout is waiting for

**Date:** 2026-08-05 · **Systems:** S2, S4 · **Reported by:** the operator, watching
a 26.8.0 upgrade fail

## What the operator saw

```
Starting upgrade to 26.8.0...
Loaded 18 config slices from config store.
Waiting for Helm rollout… (30s elapsed)
Waiting for Helm rollout… (1m 00s elapsed)
…
Waiting for Helm rollout… (7m 30s elapsed)
```

Then `pending-upgrade`, then `failed`. Their question — *"kein Balken oder was
passiert da genau?"* — is the right one, and the honest answer is that
`startProgress` is a **clock**. It never looks at the cluster. It cannot say what it
is waiting for, because it does not know.

## What was actually happening

One pod, one line, the whole time:

```
ess-matrix-authentication-service-…  Init:CrashLoopBackOff
  database-migrate: password authentication failed for user "matrixauthenticationservice_user"
```

Finding that took reading pod states, per-container init statuses and container
logs. All of it was available to the process that was printing "30s elapsed".

## The root cause underneath, which is the second half of this etappe

The credentials were correct — verified against the live database from a pod. Chart
26.8.0 renders the MAS config with `database.password_file`, which needs MAS ≥ 1.22.
The operator's config **pins** `matrixAuthenticationService.image.tag: "1.15.0"`, so
MAS 1.15 received a field it does not understand, ignored it, connected with no
password, and Postgres refused it.

The pin is not a one-off. Comparing every image tag in the config against chart
26.8.0's defaults:

| component | config | chart 26.8.0 |
|---|---|---|
| matrixAuthenticationService | 1.15.0 | **1.22.0** |
| elementWeb | v1.12.14 | **v1.12.25** |
| elementAdmin | 0.1.11 | **0.1.12** |
| matrixRTC | 0.4.4 | 0.4.4 |

The config migration froze the image tags at the time it ran. Every chart upgrade
since has upgraded *templates* while keeping *old images* — so upgrades were
partially inert, and nobody was told. 26.8.0 is simply the first version where the
mismatch became fatal instead of merely stale.

## What gets built

**1. The progress line reports the cluster, not the clock.** On each tick, list the
namespace's pods, find the ones that are not ready, and say why — including the
error text from the failing container. One line like

> `matrix-authentication-service: Init:CrashLoopBackOff (database-migrate) — password authentication failed for user "…"`

replaces seven minutes of `(30s elapsed)`.

Design constraints:

- **The probe must never fail the upgrade.** It is diagnostics. A read that errors
  degrades to the plain elapsed line rather than aborting a running Helm operation.
- **Bounded.** One list plus at most a couple of log tails per tick, on a short
  timeout, so a struggling cluster does not get a poll storm on top.
- **Quiet when normal.** Pods that are merely young and starting are the expected
  state during a rollout; they are counted, not narrated. Only a pod that is
  *failing* — CrashLoopBackOff, ImagePullBackOff, a non-zero exit — gets its error
  printed.

**2. Pinned image tags are detected and reported.** Compare each tag in the config
against the chart's default for the version being deployed. Older than the chart →
say so, by name, with both versions.

Deliberately a **report, not an auto-fix**: unpinning is an upgrade decision with
consequences (here, a seven-minor-version MAS jump with database migrations), and
CLAUDE.md rule 6 puts that with the operator.

## Mandatory edge-case walk ([PROZESS §1](../PROZESS.md))

1. **No ESS / ESS elsewhere.** Probe reads the namespace it is deploying into; empty
   is fine.
2. **Helm release in a bad state.** This is exactly the case it is for.
3. **Not just Deployments.** Pods are read directly, so StatefulSets are included —
   Synapse and Postgres are StatefulSets, and today's failure was on a Deployment
   only by luck.
4. **Cluster slow or gone.** The probe degrades to the elapsed line; it never turns
   a diagnostic failure into an upgrade failure.
5. **No outbound internet.** Chart defaults come from the chart already being pulled
   for the upgrade, not from a fetch.
6. **Both auth modes.** No new route.
7. **Config edge shapes.** A tag that is absent, non-semver, a digest instead of a
   tag, or *newer* than the chart — each covered by a test. Newer is not a warning:
   an operator who deliberately runs ahead is not making a mistake.
8. **Helm succeeded, hooks failed.** Unrelated, and already covered by E21.

## Definition of done

- A failing pod's reason and error text appear in the upgrade log within one tick
- A healthy-but-slow rollout does not spam errors
- A probe failure never aborts an upgrade
- The MAS pin is reported against chart 26.8.0 with both versions named
- A tag newer than the chart's is not reported
- S11 green **after** the deploy

## Outcome (2026-08-05)

Shipped in `0.1.32`. S11 all four green after the deploy (revision 34), container
still non-root.

Pin detection run against the operator's **live** config and chart 26.8.0:

```
elementAdmin                 config 0.1.11          chart 0.1.12
elementWeb                   config v1.12.14        chart v1.12.25
matrixAuthenticationService  config 1.15.0          chart 1.22.0    ← failed the upgrade
synapse                      config v1.151.0-ess.1  chart v1.158.0
```

Four components behind, one of them fatally. Synapse was seven minor versions back
and nothing had ever mentioned it.

### What the tests caught

`shortPod` was meant to strip Kubernetes' generated suffixes and keep the readable
name. The first rule required a digit — and `jkvwf`, a real pod suffix from the very
failure this etappe is about, has none. The working discriminator is **vowels**:
Kubernetes generates these suffixes from an alphabet that deliberately omits a, e,
i, o and u, while `service`, `main` and `postgres` all have them.

Number agreement was wrong in the pin warning (*"4 Image-Tags … ist älter"*). Small,
and it appears at the moment an upgrade is about to go wrong, which is a bad moment
to look unmaintained.

### What is deliberately absent

Any automatic unpinning. The report names the versions and stops. A seven-minor MAS
jump carries database migrations, and choosing to run them is not a decision a
progress log should make on someone's behalf.
