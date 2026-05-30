# MatrixCtrl

**An open-source admin layer for self-hosted Matrix / Element Server Suite (ESS) deployments.**

> *What UniFi is for networks, MatrixCtrl wants to be for Matrix.*

MatrixCtrl gives ESS Community a real Day-2 admin UI: edit config with validation
and versioning, run Helm upgrades that don't lose your manual patches, deploy a
fresh ESS, and manage it all behind admin-only Matrix login — without `vim`-ing
5,000-line YAML files or hand-patching MAS.

[![AGPL v3](https://img.shields.io/badge/license-AGPL--3.0-blue)](LICENSE)
`Go 1.26 · React 18 · Helm SDK · client-go · PostgreSQL`

---

## Why

Self-hosting ESS today is a YAML desert:

- Helm values are edited by hand, with no validation beyond a pod crash.
- Every `helm upgrade ess` overwrites manual `kubectl patch`es (hostNetwork,
  `externalTrafficPolicy`, …) and WebRTC calling breaks until you re-apply them.
- No config history, no audit, no UI for routine operations.

MatrixCtrl fixes the config + Helm story first (the part nobody else builds), then
grows into full admin parity.

## Features

- **Config management** — every ESS section as its own versioned YAML file, edited
  either as a **Standard** form (schema-driven, with help text pulled from the
  chart's `##` comments) or as raw **YAML** (Monaco editor). Edits preserve
  comments. Backed by a git repo: diff, history, rollback.
- **Helm upgrades** — pick an ESS version, see live logs, and **post-upgrade hooks**
  re-apply the SFU patches automatically so calling never breaks.
- **Config → Deploy** — apply config changes to the cluster with one click.
- **Greenfield deploy & adopt** — deploy a fresh ESS from the chart defaults, or
  adopt an existing release (auto-discovered across namespaces).
- **Admin-only login via MAS (OIDC)** — verified through the MAS Admin API. Starts
  in local bootstrap mode and connects Matrix login in one click (registers its own
  MAS client — no manual policy patching).
- **Self-configuring** — DB password and JWT key are auto-generated.

## Quick start

### Prerequisites
- A Kubernetes cluster (k3s works great) with an ingress controller (Traefik).
- An existing ESS (`matrix-stack`) release, *or* let MatrixCtrl deploy one.

### Install (recommended) — OCI chart

The chart and image are published to GHCR, so one command is all you need:

```bash
helm install matrixctrl oci://ghcr.io/bxnnyg/charts/matrixctrl --version 0.1.0 \
  --namespace matrixctrl --create-namespace \
  --set ingress.host=matrixctrl.example.com \
  --set ingress.certIssuer=letsencrypt-prod
```

The image is pulled from `ghcr.io/bxnnyg/matrixctrl`. Secrets (DB password, JWT key)
auto-generate on first install — nothing to set.

> Note: `helm install` can't read a GitHub URL — `github.com/bxnnyg/matrixctrl` is the
> source repo. Use the OCI chart above, or a local path / image import below.

<details>
<summary><b>Alternative — from a clone (local chart path)</b></summary>

```bash
git clone https://github.com/bxnnyg/matrixctrl
cd matrixctrl
helm install matrixctrl ./deploy/helm/matrixctrl \
  -n matrixctrl --create-namespace --set ingress.host=matrixctrl.example.com
```
</details>

<details>
<summary><b>Alternative — single-node k3s without pulling from a registry</b></summary>

Build and import the image straight into k3s containerd:

```bash
make docker            # or: docker build -t ghcr.io/bxnnyg/matrixctrl:dev .
docker save ghcr.io/bxnnyg/matrixctrl:dev | sudo k3s ctr images import -
helm install matrixctrl oci://ghcr.io/bxnnyg/charts/matrixctrl --version 0.1.0 \
  -n matrixctrl --create-namespace \
  --set image.tag=dev --set image.pullPolicy=IfNotPresent \
  --set ingress.host=matrixctrl.example.com
```
</details>

## First run

1. Open `https://matrixctrl.example.com` → log in with the bootstrap admin
   (password printed in the pod log on first start).
2. Go to **Setup**. MatrixCtrl auto-discovers your ESS:
   - **No ESS yet?** → *Deploy ESS* (pick a version + server name).
   - **ESS already running?** → *Adopt existing ESS* (seeds config from the release).
3. Click **Connect Matrix Login** → MatrixCtrl registers its own MAS OIDC client,
   upgrades ESS so MAS picks it up, and switches to admin-only Matrix login.

## Configuration (Helm values)

| Key | Default | Notes |
|-----|---------|-------|
| `image.repository` / `image.tag` | `ghcr.io/bxnnyg/matrixctrl` / `latest` | |
| `ingress.host` | `matrixctrl.example.com` | your hostname |
| `ingress.certIssuer` | `""` | cert-manager ClusterIssuer, or empty if TLS is external |
| `ess.namespace` / `ess.release` | `ess` / `ess` | auto-discovered if not found |
| `secrets.dbPassword` / `secrets.jwtSecret` | `""` | empty = auto-generate |
| `oidc.*` | disabled | leave empty; wire via Setup → Connect Matrix Login |

## Architecture

```
Go backend (chi) + embedded React frontend, single container + Postgres sidecar.
  internal/config  — per-section YAML, comment-preserving edits, git versioning
  internal/helm    — helm.sh/helm/v3 SDK (no exec("helm")); install/upgrade/discover
  internal/hooks   — post-upgrade patch engine via client-go (no exec("kubectl"))
  internal/auth    — bootstrap (bcrypt+JWT) + OIDC via MAS, runtime hot-reload
```

## Development

```bash
make web-build      # build the React frontend
make build          # embed frontend + build the Go binary
make test           # unit tests
make dev            # run against a local Postgres (docker compose)
```

Go 1.26, Node 20. See [`docs/ROADMAP.md`](docs/ROADMAP.md) for status and phases,
[`docs/SETUP.md`](docs/SETUP.md) for the onboarding design, and [`CLAUDE.md`](CLAUDE.md)
for full developer context.

## License

[AGPL-3.0](LICENSE). MatrixCtrl is free software — if you run a modified version as
a network service, you must offer your users its source.
