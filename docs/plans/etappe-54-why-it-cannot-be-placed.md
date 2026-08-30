# Etappe 54 — `down` is not a diagnosis

P1-16, straight out of the outage of 2026-08-16…18 ([DESIGN.md §4.53](../DESIGN.md)).

MatrixCtrl reported postgres, Synapse, MAS and haproxy as `down`. Correctly, and
within seconds. For **37 hours**. What it never said was *why* — and the reason was in
a `FailedScheduling` event the whole time, one API call from data the panel was already
fetching.

`kubectl get pods` already says "it is broken". An admin tool exists to say
**"it cannot be placed, because it asks for 8500m on a 6000m node"**. That sentence is
the etappe.

## What makes this more than reading an event message

Kubernetes' own message is `0/1 nodes are available: 1 Insufficient cpu.` — true, and
not enough to act on. It does not say how much was asked for, or how much there is.
Both are knowable here, and the arithmetic is where the two traps live:

**A pod's request is not the sum of its containers.** It is
`max(sum(containers), max(initContainers))`. Synapse's `render-config` and `db-wait`
had inherited 4000m each, so Synapse reserved 4000m the entire time it was *only
waiting for the database* — while its own `synapse` container asked for 1000m. Summing
containers would have reported 1000m and made the diagnosis look wrong.

**One `resources` block can cover several containers.** `postgres.resources` applies to
postgres *and* postgres-ess-updater, so 4000m written once reserved 8000m. Reporting
the per-container number would understate it by half.

So the panel computes the *effective* request the scheduler actually uses, and shows it
against the node's allocatable. That is the number that explains the failure.

## Scope

**Ships:** for a component whose pods are Pending, why — the scheduler's own reason,
and for the `Insufficient cpu|memory` case the effective request against allocatable.
Rendered where the component already says `down`.

**Does not ship: warning at config-save time.** The valuable half, and the honest
reason it is not here: to know what a config *would* request, the chart has to be
rendered — the topology (which containers share a `resources` block, which init
containers inherit it) is a property of the chart, not of the YAML being edited.
MatrixCtrl has the Helm SDK and could dry-run it, but that is a second etappe with its
own failure modes, and guessing the topology from the values file is exactly the kind
of inference this project refuses. Stays P1-16b.

**Does not ship: suggesting a fix.** "Reduce postgres to 750m" depends on what else the
operator intends to run. Naming the arithmetic is the job; choosing the number is
theirs.

## Definition of done

- A Pending component says why, with the scheduler's reason
- For insufficient cpu/memory it shows effective request vs allocatable
- The effective request is `max(sum(containers), max(initContainers))` — asserted by a
  test built on the real shape that caused the outage
- A component that is `down` for any other reason is unaffected
- Nothing is claimed when there is no event to read: absent is not "fine"
- `make check` green
