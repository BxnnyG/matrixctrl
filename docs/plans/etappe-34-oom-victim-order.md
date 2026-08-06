# Etappe 34 — The panel must not be first in line to be killed

**Date:** 2026-08-06 · **System:** S8 · **Found by:** measuring the OOM from E33
instead of assuming its cause

## The hypothesis was wrong

E33 closed with *"something spikes — I would measure that before raising the limit"*,
and named the Helm upgrade path as the likely culprit. The measurement says otherwise,
and it is worth writing down that the guess was wrong, because the fix that followed
from it would have been pure waste.

```
Out of memory: Killed process 1090258 (matrixctrl)
  total-vm:1333904kB, anon-rss:14084kB, oom_score_adj:997
oom-kill:constraint=CONSTRAINT_NONE, global_oom
```

**14 MB resident, against a 512Mi limit.** MatrixCtrl was not near its cgroup ceiling
and has no memory problem. `constraint=CONSTRAINT_NONE` with `global_oom` means the
*node* ran out, not the container.

The process that actually exhausted it is three lines further down: `node`, 18.2 GB
anon-rss, in `/user.slice/user-0.slice/session-7551.scope`, immediately after a line
reading `claude invoked oom-killer`. That was the agent's own session — external to
the product entirely. Raising MatrixCtrl's memory limit would have fixed nothing.

## What the log does show, which is a real defect

The kill order is not random. kubelet derives `oom_score_adj` from the memory
**request**:

```
oom_score_adj = 1000 − 1000 × memoryRequest / nodeCapacity
              = 1000 − 1000 × 128Mi / 35Gi ≈ 996
```

The kernel logged **997** for `matrixctrl` — the same mechanism, to within rounding.
With `requests.memory: 128Mi` against `limits.memory: 512Mi`, the pod is **Burstable**
and sits near the top of the kill list. Under node pressure the admin panel dies
before almost everything else.

That is backwards. The panel is the tool for diagnosing a node in trouble; being the
first casualty of that trouble makes it useless exactly when it is needed. It is the
same failure shape as E33 — a component that opts itself out at the worst moment —
and it is why these two etappes belong next to each other.

The cascade that night, in kill order: `nginx`, `postgres`, `postgres_exporter`,
`sh`, `matrixctrl` (14 MB), `livekit-server` (20 MB), and finally the 18 GB process
that caused it. Every victim was small; the cause was never touched until last.

## Approach

Set `requests == limits` for both containers, making the pod **Guaranteed**
(`oom_score_adj = -997`) — from near-first to near-last in the kill order.

**This does not create memory, it changes who dies instead**, and that trade is worth
stating rather than hiding. Two reasons it is the right trade here:

- The reservation is 512Mi + 256Mi on a 35 GiB node — about 2%. It barely moves the
  ordering for anything else.
- Actual usage is 81Mi against a 512Mi limit. The pod was never going to burst; the
  gap between request and limit was buying nothing and costing the kill order.

Deliberately **not** raising the limit: nothing has ever approached it.

## Mandatory edge-case walk ([PROZESS §1](../PROZESS.md))

1. **No ESS / ESS elsewhere.** Chart-local change.
2. **Helm release in a bad state.** Unaffected.
3. **Not just Deployments.** The Postgres sidecar shares the pod — QoS is a *pod*
   property, so both containers must have `requests == limits` or the class stays
   Burstable. This is the step that is easy to get half-right.
4. **Cluster slow or gone.** N/A.
5. **No outbound internet.** N/A.
6. **Both auth modes.** N/A.
7. **Config edge shapes.** A small node: the values stay overridable, and the chart
   default must not be so large that a modest node cannot schedule the pod. 512Mi+256Mi
   is schedulable anywhere the pod already ran.
8. **Helm succeeded, hooks failed.** Unrelated.

## Definition of done

- `kubectl get pod -n matrixctrl -o jsonpath='{...qosClass}'` reads **Guaranteed**
- Both containers carry equal requests and limits
- The pod still schedules and serves
- S11 green **after** the deploy

## Not in scope

**The ESS side.** `nginx`, `postgres` and `livekit-server` were killed in the same
cascade and are Burstable for the same reason, but they belong to the ESS chart. That
is the operator's call, not a change to make unilaterally on someone's live homeserver
([CLAUDE.md rule 6](../../CLAUDE.md)). Reported, not acted on.

**The 18 GB process.** Outside the product. Named here so the record is honest about
what actually caused the outage.
