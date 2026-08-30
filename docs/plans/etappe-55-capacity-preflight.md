# Etappe 55 — Catching the number before it becomes an outage

P1-16b, the preventive half of E54. E54 explains an outage that has already happened;
this one is about the ten seconds before it starts.

The values that took the homeserver down for 37 hours were written *through this
panel*. MatrixCtrl is the last thing that sees them before they reach the cluster, and
it had nothing to say about `cpu: 4000m` on a node with 6 cores.

## Why this needs the chart and not the values file

The obvious implementation reads the edited YAML and checks the numbers in it. It would
have missed both halves of the real fault:

- `postgres.resources` is one block covering **postgres and postgres-ess-updater**, so
  `4000m` in the file is `8000m` in the cluster
- Synapse's `render-config` and `db-wait` **inherit** the same block, and a pod's
  request is `max(sum(containers), max(initContainers))` — so `4000m` in the file is
  what Synapse reserves while it is only waiting for the database

Neither fact is in the values file. Both are properties of the chart. So the check
renders the chart — `action.NewUpgrade` with `DryRun`, which produces the manifest and
touches nothing — and measures the pods that would actually exist.

That is the whole reason this was split off from E54 rather than guessed at there.

## What it reports

Per workload: the effective request of the pod the chart would create, against the
largest node's allocatable. Reusing `k8s.EffectiveRequest`, which E54 already built and
tested on exactly these two traps (rule 3 — one implementation, not two that drift).

Two levels, and the distinction is the same one E54 draws:

- **exceeds any node** — no node can ever run this pod. Not a warning about pressure;
  a statement that applying this config schedules nothing.
- **exceeds what is currently free** — the cluster is full *now*. Real, but it can
  resolve itself, so it is not the same sentence.

## Scope

**Ships:** the render-and-measure check, run before an apply so the result is in the
apply stream the operator is already watching, and available on demand while editing.

**Does not ship: refusing the apply.** Tempting — a config that can schedule nothing is
exactly the thing to block, and E49 set the precedent that a skipped check is a failure
unless you say otherwise. It is left out because a false positive here blocks *all*
deployment, and the check has never run in anger. Warn first, watch it be right, then
decide. Recorded so the decision is made deliberately rather than by omission.

**Does not ship: memory.** The same arithmetic applies and the plumbing carries it, but
memory pressure behaves differently — overcommit is normal, the kernel reclaims, and a
warning tuned like the CPU one would cry wolf. CPU is what caused the outage and CPU is
what ships checked.

## Definition of done

- The check renders the chart rather than reading the values file
- It reports the postgres pod as 8500m when the config says 4000m — the case that
  proves it sees through the chart, asserted against the real values
- "Larger than any node" is distinguished from "larger than what is free"
- An apply emits the result into its stream before the upgrade runs
- Rendering failure is reported as *not checked*, never as *fine*
- `make check` green
