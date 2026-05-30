# Security Policy

MatrixCtrl runs with broad cluster privileges and manages authentication for a Matrix
deployment, so we take security seriously.

## Reporting a vulnerability

**Please do not open a public issue for security problems.**

Report privately via GitHub:

1. Go to the repository's **Security** tab → **Report a vulnerability**
   ([open a private advisory](https://github.com/BxnnyG/matrixctrl/security/advisories/new)).
2. Describe the issue, affected versions, and steps to reproduce.

You can expect an initial response within a few days. Once a fix is available we'll
coordinate a disclosure timeline with you and credit you (unless you prefer otherwise).

## Supported versions

Only the **latest** release receives security fixes. MatrixCtrl is still pre-1.0
(versions `0.x`), so there are no maintained older branches yet — please always run
the newest version.

| Version | Supported |
|---------|-----------|
| Latest release | ✅ |
| Older releases  | ❌ |

## Scope & hardening notes

- The container image is built multi-stage; only the compiled binary ships — no source,
  secrets, or instance config (`.dockerignore` enforces this).
- Instance Helm values (`values.*.yaml`) are gitignored and excluded from the packaged
  chart (`.helmignore`) so secrets never land in the repo or the published chart.
- DB password and JWT signing key are auto-generated and stored in-cluster.
- Admin login is OIDC via MAS, verified through the MAS Admin API.

If you're deploying MatrixCtrl, rotate any credential that has ever been committed or
shared, and keep your instance values out of version control.
