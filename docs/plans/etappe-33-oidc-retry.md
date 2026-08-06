# Etappe 33 — A blip at boot must not lock the operator out

**Date:** 2026-08-06 · **Systems:** S3, S4 · **Found by:** the operator, locked out of
their own panel

## What happened

```
2026/08/05 21:43:10  matrixctrl OOMKilled (exit 137)
2026/08/05 21:43:32  WARNING: OIDC init failed (oidc discovery parse:
                     invalid character 'o' in literal null) — staying in
                     bootstrap mode; reconnect via Setup
```

Eleven hours later the operator wrote: *"idk jetzt ist login mit username password
statt mit mas"*.

The chain, where every link looks harmless on its own:

1. The container was OOMKilled. Limit 512Mi, steady state 81Mi — a spike, not a leak.
2. It restarted **before MAS was serving**. The discovery URL answered with a proxy
   error page, not JSON. The `'o'` in the parse error is the second character of
   `no healthy upstream`.
3. `NewOIDCService` failed once, and `oidcSvc` stayed `nil` **for the process's whole
   life**. MAS was healthy seconds later. Nothing tried again.

## Why this is the bug and not the OOM

The OOM is a separate problem (below). But an admin panel that permanently disables
its own login because a dependency was slow to start is broken independently of what
caused the restart. The operator could only get back in because someone else had
cluster access. **That is the worst property an admin panel can have: it fails at
exactly the moment you need it.**

There is a second, quieter effect. `BootstrapLogin` refuses to run while OIDC is
configured — so when OIDC latched off, the local username/password login **re-opened
on a public URL**. A transient IdP blip widens the attack surface. The Postgres-backed
throttle (E29) bounds it, and the 401 the operator hit took 2.2s, so it works. It
should still not be reachable at all.

## The trap this plan exists to avoid

`AuthHandler.ReloadOIDC` already loads config, builds the service and swaps it under a
lock, and is documented "safe to call repeatedly". Retrying by calling it in a loop is
the obvious move — **and on this instance it would do nothing at all, silently.**

`ReloadOIDC` reads `auth.LoadOIDCConfig(ctx, db)`: DB only. Startup prefers **env**
(main.go: *"Env always wins"*), and this deployment is env-configured —
`MATRIXCTRL_OIDC_ISSUER`, `CLIENT_ID`, `CLIENT_SECRET` are all set, and the DB has
nothing. `LoadOIDCConfig` would return `ok == false`, `ReloadOIDC` would return `nil`
— success — and OIDC would stay off forever while the logs claimed a retry was running.

Verified before writing this: `grep -c "OIDC config loaded from DB"` over the live
pod's logs returns **0**, and the deployment carries the three env vars.

So the retry must reuse the **effective startup config**, not re-derive it from one
source.

## Approach

- main.go computes the effective config (env-first, DB fallback) exactly once, as it
  does today. On failure it hands **that config** to a retrier.
- The retrier rebuilds with backoff and installs the result through the *existing*
  lock in `AuthHandler` — no second swap mechanism (CLAUDE.md rule 3).
- It stops when it succeeds, when the context ends, or when something else has already
  installed a service — the connect-OIDC setup flow must win over a retry in flight,
  because that one is a person acting deliberately.
- `ReloadOIDC` keeps its DB-first behaviour. The setup flow's whole job is to apply
  newly persisted config; making it env-first would stop it working on exactly the
  instances that need it.
- `/api/v1/auth/oidc/available` gains a distinct "trying" state, so the login page can
  say *Matrix-Login vorübergehend nicht erreichbar — Verbindung wird wiederhergestellt*
  instead of silently presenting a password box that looks like the normal way in.

## Mandatory edge-case walk ([PROZESS §1](../PROZESS.md))

1. **No ESS / ESS elsewhere.** OIDC config is independent of the ESS release.
2. **Helm release in a bad state.** Unrelated; MAS reachability is what matters.
3. **Not just Deployments.** N/A.
4. **Cluster slow or gone.** Precisely the case: retry, do not latch.
5. **No outbound internet.** MAS is in-cluster; a permanently unreachable issuer must
   degrade to bootstrap and keep saying so, not spin invisibly.
6. **Both auth modes.** The point of the etappe. Bootstrap must stay usable while a
   retry runs, and must close again the moment OIDC installs.
7. **Config edge shapes.** Env-only (this instance), DB-only, both, neither.
8. **Helm succeeded, hooks failed.** Unrelated.

## Definition of done

- A failed OIDC init retries and recovers without a restart
- An env-configured instance recovers — the case a DB-only retry would miss
- The setup flow beats an in-flight retry
- Bootstrap login closes again as soon as OIDC installs
- The login page distinguishes "bootstrap by design" from "OIDC down, retrying"
- No goroutine outlives the process context
- S11 green **after** the deploy

## Out of scope, deliberately

**The OOM itself.** 512Mi limit, 81Mi steady state, two kills — something spikes, and
raising the limit before knowing what would just move the failure. It gets its own
investigation; this etappe makes the restart survivable either way.
