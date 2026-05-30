# MatrixCtrl — Onboarding & Setup (Design)

> Status: **design recorded, not yet implemented.** This captures the "a colleague installs
> it and it just works" goal and the `matrixctrl setup` command that gets us there.

## The goal

A new operator (k3s + ESS already running) should go from zero to a working
MatrixCtrl with **one command** and **no manual secret/OIDC fiddling**:

```bash
helm install matrixctrl oci://ghcr.io/bxnny/matrixctrl --namespace matrixctrl --create-namespace
```

Everything that can be derived or generated must be — the operator should never
type a DB password, JWT secret, or hand-register an OIDC client.

## What's already solved (done)

- **DB password + JWT secret**: auto-generated. Helm chart generates them
  (`lookup` + `randAlphaNum`, `resource-policy: keep`); the app also self-generates
  and persists the JWT key in `instance_settings` if no env is set.
- **In-cluster deploy**: chart deploys app + Postgres sidecar + RBAC + ingress.

## The hard part — the real friction (not yet solved)

The painful manual steps today are **not** the secrets. They are:

1. **MAS OIDC client registration** — generating a ULID client_id, adding it to
   `policy.data.admin_clients`, setting `client_secret_basic`, patching the MAS
   ConfigMap, restarting MAS. This is the step that makes "just works" fail.
2. **ESS discovery** — knowing the namespace, release name, Synapse/MAS URLs.
3. **Config seed** — getting the current ESS values into the config repo.

## The key enabler (discovered 2026-05-29)

**MatrixCtrl can register its own MAS client via the MAS Admin API.** The same
`client_credentials` + `urn:mas:admin` flow we use for the admin-login check can
also create/manage OAuth clients. This means client registration can be automated
instead of hand-patched.

## `matrixctrl setup` — proposed command

A one-shot bootstrap, runnable as a Helm post-install Job or `kubectl exec`:

```
matrixctrl setup
  1. Discover ESS:
     - scan namespaces for a Helm release whose chart is matrix-stack
     - read its values (helm get values) → serverName, MAS/Synapse service URLs
  2. Seed config:
     - write the live release values into the config-repo as the base slices
  3. Register OIDC client in MAS:
     - obtain an admin token (operator provides a one-time MAS admin client cred,
       OR we use an existing admin OAuth session)
     - create the MatrixCtrl client (redirect_uri from the ingress host)
     - add it to admin_clients policy
     - store client_id/secret in the matrixctrl Secret
  4. Generate remaining secrets (done by chart already)
  5. Verify: discovery, DB, k8s RBAC, MAS reachability → print a health summary
```

### Open design questions

- **Bootstrapping the MAS admin token**: registering a client needs admin access
  to MAS. Chicken-and-egg. Options: (a) operator pastes a short-lived MAS admin
  token once; (b) MatrixCtrl is granted a MAS admin client cred via ESS values at
  install; (c) first-run uses the existing bootstrap (bcrypt) login, then the admin
  links MAS interactively. Leaning towards (a) for the CLI and (c) for the UI.
- **Idempotency**: re-running setup must detect an existing client and not dupe.
- **Non-ESS / generic Matrix**: v1 targets ESS; keep discovery pluggable.

## THE BOOTSTRAP PARADOX (greenfield: deploy Matrix *with* a tool that needs Matrix)

If MatrixCtrl is meant to **deploy** ESS from scratch, it cannot depend on MAS for
login during that first deployment — MAS doesn't exist yet. OIDC-admin login is a
post-ESS capability. So setup must handle two distinct states:

```
State A — GREENFIELD (no ESS yet):
  - MatrixCtrl runs in BOOTSTRAP auth (local bcrypt admin + JWT, already implemented;
    auto-generated admin password printed on first start). OIDC is OFF.
  - k8s + Helm operations DO NOT need MAS — so MatrixCtrl can fully deploy ESS in
    this state: pick ESS version → seed an initial config (wizard / chart defaults)
    → helm install ess.
  - This is the bootstrap; the operator logs in with the local admin password.

State B — POST-ESS (MAS now running):
  - Run `matrixctrl setup` (or a UI "Connect Matrix login" flow):
      register the MatrixCtrl OIDC client in MAS via the Admin API,
      write OIDC config, flip oidc.enabled=true.
  - MatrixCtrl restarts into OIDC/admin-only mode; bootstrap login auto-disables
    (router already drops /bootstrap/login when OIDC is configured).
```

**Implication for the code today:** the OIDC-or-bootstrap switch already exists, but
the *transition* (B) must be runtime-reconfigurable without hand-editing env/secrets,
and greenfield (A) needs an "initial ESS config" wizard (seed the section files from
the chart's default values.yaml, not from a live release). Both are setup-phase work.

**Greenfield config seed:** State A can't `helm get values` (no release). Instead seed
the section files from the ESS chart's bundled `values.yaml` (pull chart → split via
the existing migrator) so the user starts from the documented defaults and edits down.

## Phasing

This is **Phase 1.5** — between the current Phase 1 (config + helm + OIDC, done)
and Phase 2 (user/room management). It's the difference between "works for bxnny"
and "works for a colleague", so it's high-leverage but not a Phase-1 blocker.

Setup-phase task list:
1. ✅ Greenfield deploy flow — `POST /api/v1/setup/deploy-ess` (helm.Install) + /setup wizard.
2. ✅ Initial-config seed from the chart's default values.yaml — `config.Store.SeedSections`.
3. ✅ Auto-register OIDC client — `POST /api/v1/setup/connect-oidc` writes the client +
   admin_clients into the matrixAuthenticationService config and helm-upgrades ESS (we
   register via the config we manage, NOT the MAS Admin API, because admin_clients is
   static MAS policy the API can't change).
4. ✅ Runtime bootstrap→OIDC switch — DB-backed OIDC config + AuthHandler.ReloadOIDC
   hot-reload; bootstrap login always-registered but 403s once OIDC is active.
5. ⬜ ESS discovery for the "manage existing" path (scan namespaces, helm get values → seed).
6. ⬜ End-to-end greenfield live test (needs a throwaway cluster/namespace).

NOTE: items 1–4 are built + deployed but the greenfield/connect happy-path is not yet
live-tested (the bxnny instance already has ESS + an env-configured OIDC client, so the
guards short-circuit). Logic verified to the guard boundary; building blocks are proven.
