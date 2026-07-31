# MatrixCtrl — rules for AI agents

**Before any change: read and follow [docs/PROZESS.md](docs/PROZESS.md).**
No feature without an entry in [docs/DESIGN.md](docs/DESIGN.md).

MatrixCtrl is an AGPL Day-2 admin layer for self-hosted Matrix / Element Server
Suite (ESS). Go backend + embedded React frontend, one container. It runs in the
k3s namespace `matrixctrl` and manages the ESS Helm release in namespace `ess`.

## Non-negotiable

1. **Plan first, then build.** The plan goes to `docs/plans/etappe-NN-<slug>.md`
   before the first line of code — ten lines is fine, zero lines is not.
2. **Inventory instead of guessing.** What already exists is looked up, not
   assumed. Start at [DESIGN.md §1](docs/DESIGN.md#1-current-inventory-what-already-exists-centrally).
3. **Centralisation check.** Does more than one place need this? → shared package
   under `internal/` or `web/src/components`. Never duplicated.
4. **Never shell out.** Helm via `helm.sh/helm/v3`, Kubernetes via `client-go`.
   No `exec("helm")`, no `exec("kubectl")` — ever (§4.1).
5. **Don't fix past the report.** Reproduce a bug first. If it cannot be
   reproduced, prove that instead of blind-editing a probably-correct file.
6. **Open decisions go to the operator**, not into your own assumption.
7. **Run the four regression checks before every ship** — the managed ESS stays
   reachable · saving config destroys no comments or values · admin login works ·
   the SFU patches survive a Helm upgrade ([DESIGN.md S11](docs/DESIGN.md#s11--regression-safety-net-)).

## Map

| I want to … | File |
|---|---|
| know where the project is going | [docs/VISION.md](docs/VISION.md) |
| know what already exists | [docs/DESIGN.md](docs/DESIGN.md) |
| know how to work here | [docs/PROZESS.md](docs/PROZESS.md) |
| know what comes next | [docs/ROADMAP.md](docs/ROADMAP.md) |
| know what is worth doing | [docs/BACKLOG.md](docs/BACKLOG.md) |
| understand the onboarding design | [docs/SETUP.md](docs/SETUP.md) |

## Commands

Go lives at `/usr/local/go/bin/go` and is **not** on the default PATH.

| Purpose | Command |
|---|---|
| Go tests | `go test ./...` |
| Typecheck | `cd web && ./node_modules/.bin/tsc --noEmit` |
| Build frontend | `make web-build` |
| Build binary (embeds frontend) | `make build` |
| Run locally | `make dev` (backend) + `make web-dev` (frontend) |
| Build image | `make docker VERSION=<x.y.z>` |
| Ship | see [PROZESS.md §4](docs/PROZESS.md#4-verify--ship) — build → `k3s ctr images import` → `helm upgrade` → verify running result |

## Things that will bite you

- Go embeds `cmd/matrixctrl/dist`, **not** `web/dist`. The Makefile copies it.
- `ComponentHealth.Status` is `healthy | degraded | down | scaled-zero` — never
  `"ok"`. Comparing against `"ok"` silently marks everything as a warning.
- ESS runs **StatefulSets** too (`ess-synapse-main`, `ess-postgres`) and Postgres
  is a multi-container pod. Listing only Deployments hides the two most important
  components; reading logs without a container name fails.
- Monaco is self-hosted and must stay behind `components/config/YamlEditor.tsx`
  (§4.10) — a top-level import puts 3.9 MB on every page, including login.
- Instance Helm values (`deploy/helm/matrixctrl/values.*.yaml`) are gitignored and
  excluded from the packaged chart. The repo is public: no secrets, no IPs, no
  personal names (§4.8).
