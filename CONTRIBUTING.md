# Contributing to MatrixCtrl

Thanks for your interest! MatrixCtrl is AGPL-3.0 software and contributions are welcome.

## Ground rules

- By contributing you agree your work is licensed under **AGPL-3.0**.
- Be respectful — see the [Code of Conduct](CODE_OF_CONDUCT.md).
- For anything security-related, **do not** open a public issue — see [SECURITY.md](SECURITY.md).

## Project layout

```
cmd/matrixctrl/      Go entry point + embedded frontend
internal/            Go backend (api, config, helm, hooks, k8s, auth, db)
web/                 React 18 + Vite frontend (TanStack Router/Query, Monaco)
deploy/helm/         the MatrixCtrl Helm chart
docs/                ROADMAP, SETUP design
```

See [`CLAUDE.md`](CLAUDE.md) for the full architecture and conventions, and
[`docs/ROADMAP.md`](docs/ROADMAP.md) for what's planned.

## Development

```bash
make web-build      # build the React frontend
make build          # copy web/dist into cmd/matrixctrl/dist + build the Go binary
make test           # Go unit tests
make dev            # run against a local Postgres (deploy/dev/docker-compose.yaml)
```

Requirements: Go 1.26, Node 20.

The frontend dev server proxies `/api` to `localhost:8080`:

```bash
cd web && npm install && npm run dev
```

## Conventions (please follow)

- **No `exec("kubectl")` or `exec("helm")`** — use the client-go and Helm SDKs.
- Config edits must be **comment-preserving** (operate on yaml.v3 nodes, not maps).
- Backend code in `internal/`; HTTP handlers stay thin (no business logic in handlers).
- Frontend: TanStack Query for all API calls; typed client in `web/src/lib/api.ts`.
- Keep secrets and instance-specific values out of git (`values.*.yaml` is gitignored).
- Match the surrounding code's style; comments in **English**.

## Pull requests

1. Fork and branch from `master`.
2. Keep PRs focused; add tests for backend logic where practical.
3. Run `make test` and ensure `go build ./...` and `npm run build` pass.
4. Fill out the PR template; reference any related issue.

## Releasing (maintainers)

**Cutting a release is pushing a tag** — see [docs/RELEASING.md](docs/RELEASING.md).

```bash
# bump version + appVersion in deploy/helm/matrixctrl/Chart.yaml, then:
git tag v0.1.15
git push origin master --tags
```

`.github/workflows/release.yml` publishes the image and the OCI chart to GHCR and
fails if the tag and `Chart.yaml` disagree.

> This used to be a block of commands to run by hand, and it was skipped: images
> 0.1.10–0.1.14 never reached GHCR while the README told people to install
> `latest`. Please do not reintroduce a manual path.

## Reporting bugs / requesting features

Use the issue templates. Include your ESS version, MatrixCtrl version, and relevant
logs (`kubectl logs -n matrixctrl …`) for bugs.
