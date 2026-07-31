# MatrixCtrl — Vision

> As of 2026-07-31. This file changes rarely. When it does, the change goes into
> the log at the bottom as a dated entry — never as a silent correction.
>
> What exists today: [DESIGN.md](DESIGN.md) · How we work: [PROZESS.md](PROZESS.md)

## Who this is for

A person who self-hosts **Element Server Suite (ESS) Community** on their own
Kubernetes cluster — a homelab, a small organisation, a school, a non-profit.
They are technical enough to run k3s and Helm, but they are not a full-time
Matrix operator.

Today that person edits a 5,000-line `values.yaml` in `vim`, has no validation
beyond a crashing pod, and re-applies the same four `kubectl patch` commands by
hand after every `helm upgrade` — because otherwise WebRTC calling breaks.

MatrixCtrl is the Day-2 admin layer that ESS Pro keeps proprietary, as free
software.

> *What UniFi is for networks, MatrixCtrl wants to be for Matrix.*

## How we know it works

The operator administers their **production** ESS entirely through MatrixCtrl,
and a Helm upgrade never breaks the SFU/hostNetwork config again.

## How it gets there

1. **Fix the config + Helm story first** — the part nobody else builds. Versioned,
   validated, comment-preserving config plus upgrades whose manual patches survive.
2. **Wrap the existing APIs, don't replace them.** Synapse and MAS keep owning
   their domains; MatrixCtrl is the operator's UI on top.
3. **Then grow into admin parity** — users, rooms, moderation — so MatrixCtrl can
   stand alone instead of sitting next to element-admin.
4. **Work for anyone, not just for us.** Greenfield deploy and adopt-existing are
   what turn "works on my cluster" into a product.

## Deliberately NOT

The reasoning matters more than the item — it is what shortcuts the same idea
being proposed again in six months.

- **Reimplementing Synapse or MAS** — they are large, well-maintained, and moving.
  MatrixCtrl wraps their admin APIs. Owning a fork of that surface would consume
  the entire project.
- **Bare-metal / non-Kubernetes installs** — the whole value proposition is built
  on the Helm SDK and client-go. Without a cluster there is no release to upgrade
  and no patch to preserve, so there is nothing left of the product.
- **A hosted SaaS** — MatrixCtrl is self-hosted first. Running other people's
  Matrix servers is a different business with a different threat model, and it
  would conflict with the AGPL positioning.
- **Phase 2+ features before Phase 1.5 is proven** — *parked, not rejected.*
  User/room management is the obvious next thing to want, but shipping it while
  the greenfield install has never been tested end-to-end on a fresh cluster
  would add surface to something that is not yet known to work for anyone else.
  Unparks when S6 (Setup & onboarding) is verified — see [DESIGN.md](DESIGN.md).

## Architecture decisions that shape everything

Only the heavy forks in the road live here. Per-system detail is in
[DESIGN.md §4](DESIGN.md#4-decisions), which is where new decisions get recorded.

- **Go backend + embedded React frontend, one container.** A single artefact to
  ship and a single thing to upgrade (2026-05-27).
- **Never shell out.** Helm goes through `helm.sh/helm/v3`, Kubernetes through
  `client-go` — no `exec("helm")`, no `exec("kubectl")` (2026-05-27, §4.1).
- **Config is git.** Every change is a commit, so diff, history and rollback come
  from the storage layer instead of being features (2026-05-27, §4.2).
- **AGPL-3.0.** If someone runs a modified MatrixCtrl as a network service, their
  users get the source. This is the deliberate counterweight to ESS Pro
  (2026-05-30, §4.7).

## Changes to the vision

| Date | What changed | Why |
|------|--------------|-----|
| 2026-07-31 | Vision extracted into its own file, out of `ROADMAP.md` | Roadmap answers *when*, vision answers *where to*. Mixing them meant the vision was only ever read while planning. |
