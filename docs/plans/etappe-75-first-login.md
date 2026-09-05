# Etappe 75 — A first install you can actually log into

## The report

A fresh k3s box, a fresh `helm install`, the command the README gives:

    kubectl logs -n matrixctrl deploy/matrixctrl -c matrixctrl | grep "bootstrap admin password"

printed nothing. Twice. Then `rollout restart` — still nothing. The install was
unreachable and there was no way in.

## What actually happens

Three defects, each survivable alone, fatal together.

**1. The password exists for one pod lifetime.** It is written to the log and nowhere
else. Miss it — log rotated, pod restarted, terminal scrolled — and it is gone forever.
There is no reset path and no second chance.

**2. `helm uninstall` does not uninstall.** The PVCs stay (Kubernetes keeps them) and the
Secret is annotated `helm.sh/resource-policy: keep`. Postgres said so plainly:

    PostgreSQL Database directory appears to contain a database; Skipping initialization

So the reinstall found an existing `bootstrap_credentials` row, concluded there was
nothing to do, and said **nothing at all**. Reinstalling — the one thing anybody tries —
cannot fix this and does not explain why.

**3. The documented escape hatch is inert.** `secrets.adminPassword` is only ever read
when the admin row is *created*. Setting it on the second install would also have done
nothing. The one supported way to choose a password silently does not apply to any
install that already has one.

And a fourth, quieter: the existence check swallows its error —

    if err != nil { return nil }   // "table may not exist yet"

— so a real database failure is indistinguishable from success, which is how a bricked
install produces a clean-looking log.

Plus noise that makes a healthy greenfield install look broken:

    warning: config repo init: read /root/ess-config-values/values.yaml: permission denied

That path is the *previous* deployment's seed directory, hardcoded as the Go default. On
any machine that is not the original one it does not exist, and the operator is told
about it in the language of a failure.

## The fix

The password stops being a log line and becomes **state you can query at any time**,
using the machinery the chart already has for `db-password` and `jwt-secret`:

- The chart always generates `admin-password` into its Secret when the operator does not
  set one, reusing the existing `lookup` so `helm upgrade` keeps it stable.
- Therefore `MATRIXCTRL_ADMIN_PASSWORD` is always present in the pod, and the README's
  first step becomes a command that works on day one and on day four hundred:

      kubectl -n matrixctrl get secret matrixctrl -o jsonpath='{.data.admin-password}' | base64 -d

- `MATRIXCTRL_ADMIN_PASSWORD` becomes **authoritative on every start**, not only at
  creation: if it is set, the stored hash is brought in line with it. That is what makes
  the escape hatch real, and it means an install already in this broken state repairs
  itself on the next `helm upgrade` rather than needing psql.

Supporting changes:

- The existence check reports its error instead of returning `nil`.
- When an admin already exists and no password is configured, say so, and say how to
  reset it. Silence was the worst part of the original report.
- The password is never logged when it came from the environment — it is retrievable from
  the Secret, so putting it in the log only widens where it leaks.
- `MATRIXCTRL_CONFIG_SEED` defaults to empty. Seeding is opt-in; a missing seed is not a
  warning.

## Checks

- Unit tests for: password enforced on every start, generated only when absent, error not
  swallowed.
- `helm template` on a clean namespace produces `admin-password`; a second render with an
  existing Secret reuses it rather than rotating it.
- The four S11 regression checks.
