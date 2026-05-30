# CLAUDE.md — MatrixCtrl Development Context

## Project Summary
MatrixCtrl is an AGPL-licensed admin layer for self-hosted Matrix/ESS deployments.
Go backend + React 18 frontend, monorepo at `/opt/matrixctrl/`.
Runs in K3s namespace `matrixctrl`, manages the ESS Helm release in namespace `ess`.

## THE CORE PROBLEM THIS SOLVES
After every `helm upgrade ess`, four manual patches are required or WebRTC calling breaks:
```bash
kubectl patch deployment -n ess ess-matrix-rtc-sfu --type='json' \
  -p='[{"op":"add","path":"/spec/template/spec/hostNetwork","value":true}]'
kubectl patch svc -n ess ess-matrix-rtc-sfu-turn      -p '{"spec":{"externalTrafficPolicy":"Local"}}'
kubectl patch svc -n ess ess-matrix-rtc-sfu-muxed-udp -p '{"spec":{"externalTrafficPolicy":"Local"}}'
kubectl patch svc -n ess ess-matrix-rtc-sfu-tcp       -p '{"spec":{"externalTrafficPolicy":"Local"}}'
```
MatrixCtrl automates these via its post-upgrade hook system.
See `internal/hooks/builtin/ess_rtc_patches.go` for the seeded hook definitions.

Note: these CAN also be fixed via Helm values in `rtc.yaml`:
```yaml
matrixRTC:
  sfu:
    hostNetwork: true
    exposedServices:
      turn: {externalTrafficPolicy: Local}
      rtcTcp: {externalTrafficPolicy: Local}
      rtcMuxedUdp: {externalTrafficPolicy: Local}
```
The hooks serve as the Phase 0 bridge and long-term fallback.

## Repository Structure
```
cmd/matrixctrl/main.go     — binary entry, dependency wiring, go:embed web/dist
internal/api/              — HTTP layer (chi router, handlers, middleware) — no business logic here
internal/config/           — config slice management: read/write YAML, merge, JSON Schema validation
internal/git/              — go-git wrapper for config versioning (linear history, single branch)
internal/helm/             — helm.sh/helm/v3 SDK wrapper — NEVER exec("helm")
internal/hooks/            — post-upgrade hook engine, runs patches via k8s dynamic client
internal/k8s/              — client-go wrapper — NEVER exec("kubectl")
internal/auth/             — bootstrap mode (bcrypt+JWT) + OIDC via MAS (Phase 1)
internal/db/               — pgx/v5 pool, migration runner, query functions
internal/server/           — http.Server, graceful shutdown
web/                       — React 18, TanStack Router/Query, shadcn/ui, Monaco Editor
schemas/                   — ESS Helm values JSON Schemas per ESS version
deploy/helm/matrixctrl/    — MatrixCtrl's own Helm chart (for production K3s deployment)
deploy/dev/                — docker-compose for local dev (Postgres only)
```

## Key Architectural Decisions
1. **NO exec("kubectl") or exec("helm") anywhere.** Use client-go dynamic client and helm SDK.
2. **go-git not git2go** — pure Go, no CGO, no git binary needed in runtime image.
3. **Config is ONE YAML file per ESS section** (synapse.yaml, matrixRTC.yaml, … + general.yaml)
   in a git repo at `/data/config-repo/` (PVC), listed in `config-slices.json`. They merge into
   the helm values (disjoint top-level keys → order-independent). The legacy monolith
   (values/hostnames/rtc/tls) + easy.yaml overlay were migrated away by `Store.MigrateToSections`.
   Form edits are comment-preserving (yaml.v3 Node surgery in `internal/config/yamledit.go`).
4. **Helm release storage driver: "secret"** (default, secrets named `sh.helm.release.v1.ess.vN`).
5. **Auth: admin-only OIDC via MAS.** OIDC `sub` is a ULID; admin status is verified through
   the MAS Admin API (`/api/admin/v1/users/{sub}`) using a client_credentials token
   (`urn:mas:admin`) from MatrixCtrl's own client. Bootstrap mode (bcrypt+JWT) still exists and
   auto-activates when OIDC is unset (needed for greenfield — see docs/SETUP.md). JWT key is
   auto-generated and persisted in the DB (`instance_settings`, migration 006).
6. **PostgreSQL runs as sidecar** in same pod (single replica — acceptable for homelab).
7. **Frontend embedded in Go binary** via `//go:embed all:dist` reading `cmd/matrixctrl/dist/`
   (NOT web/dist) — the Makefile/Dockerfile copy web/dist → cmd/matrixctrl/dist before `go build`.
8. **Hook failure ≠ Helm rollback.** If Helm succeeds but hooks fail → status `hooks-failed`,
   alert in UI, allow re-trigger. Never roll back a good deployment over a patch failure.

## ESS Deployment Context
- K3s single node: `<k3s-node>` (<node-ip>), namespace `ess`, release `ess`
- Current ESS version: `matrix-stack-26.5.1`
- Config repo at `/data/config-repo/` (PVC): one YAML per ESS section (synapse.yaml,
  matrixAuthenticationService.yaml, elementWeb.yaml, matrixRTC.yaml, postgres.yaml, … +
  general.yaml for serverName/certManager/labels/ingress/matrixTools). `_backup-pre-sections/`
  holds the pre-migration monolith. Host backup: `/root/matrixctrl-config-backup-*`.
- MatrixCtrl's OWN MAS client: id `01KSPV9ZMR7NB4B2BBWMPYSD1P` (in ESS values
  `matrixAuthenticationService.additional` + `policy.data.admin_clients`).
- MAS (Matrix Auth Service): `http://ess-matrix-authentication-service.ess.svc.cluster.local.:8080`
- Synapse Admin API: `http://ess-synapse-main.ess.svc.cluster.local.:8008`
- Element Admin: `https://admin-matrix.bxnny.de` (existing, separate from MatrixCtrl)

## API Conventions
- Base path: `/api/v1`
- Auth: `Authorization: Bearer <jwt>` header OR session cookie
- All responses: `application/json`
- Paginated lists: `{items: [], total: int, page: int, per_page: int}`
- Errors: `{error: string, detail: string, code: string}`
- WebSocket streams: `/api/v1/*/stream` endpoints

## Database (PostgreSQL 16 via pgx/v5)
Migrations run sequentially at startup (`internal/db/migrations/`).
Tables:
- `sessions` — authenticated user sessions
- `audit_log` — every state-changing API call
- `config_snapshots` — merged config at each apply operation (git SHA + slice content)
- `hooks` — hook definitions (user-editable, built-ins seeded at startup)
- `hook_run_log` — execution history per hook run
- `upgrade_history` — Helm upgrade operations with status + hook results
- `ess_versions` — discovered ESS chart versions from OCI registry

## Frontend Conventions
- TanStack Router with file-based routing in `web/src/routes/`
- TanStack Query for all API calls — query/mutation keys in `web/src/lib/queries.ts`
- shadcn/ui components in `web/src/components/ui/` (generated — do not hand-edit)
- Custom components in `web/src/components/{domain}/`
- Typed fetch client in `web/src/lib/api.ts`
- WebSocket hook in `web/src/lib/ws.ts`

## Current Phase
Phase 1 complete + Phase 1.5 (setup/onboarding) mostly done; deployed in-cluster
(ns `matrixctrl`, image `ghcr.io/bxnnyg/matrixctrl`). Full phase breakdown in
[docs/ROADMAP.md](docs/ROADMAP.md); onboarding design in [docs/SETUP.md](docs/SETUP.md).
**Do NOT implement Phase 2+ features (user/room management, federation, bridges) yet.**

## Testing
- Unit tests: `internal/config`, `internal/hooks`, `internal/git` (pure logic, no k8s needed)
- Integration: `deploy/dev/` with real Postgres, in-cluster for k8s operations
- Frontend: Vitest + Testing Library

## Go Module: github.com/bxnnyg/matrixctrl (Go 1.24+)
## Node: 20 LTS
## Dev commands: `make dev` (backend), `make web-dev` (frontend), `make test`, `make build`
