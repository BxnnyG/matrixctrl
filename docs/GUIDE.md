# Using MatrixCtrl

The [README](../README.md) covers installing and running the container. This covers
the three things it does that a Kubernetes dashboard does not, and that nothing else
explains: **hooks**, the **config editor**, and **what to do when an upgrade goes
wrong**.

Everything below describes the software as it is. Where something is deliberately not
offered, it says so and why.

---

## 1. Hooks — why this project exists

### The problem

Element Server Suite is a Helm chart. Some things it does not let you configure, and
the only way to change them is to patch the running object by hand:

```
kubectl -n ess patch deployment ess-matrix-rtc-sfu --type=merge \
  -p '{"spec":{"template":{"spec":{"hostNetwork":true}}}}'
```

That works. It also lasts exactly until the next `helm upgrade`, which recreates the
Deployment from the chart and silently drops your patch. Calling breaks. Nothing turns
red — the pods are healthy, the release is `deployed`, and the only symptom is that
media stops flowing.

This happened, was not noticed for a day, and is the reason the project exists.

### What a hook is

A hook is a patch that gets **re-applied automatically after every upgrade**. You
describe it once; MatrixCtrl replays it whenever the chart would have undone it.

Each hook has:

- a **trigger** — `post-upgrade`, which fires after Helm reports success, or
  `post-rollback`, which fires after a release is rolled back. A hook can also be run
  by hand at any time, whatever its trigger.
- a **priority** — hooks run in ascending order, because patching a Deployment and
  then waiting for its rollout only makes sense in that order
- one or more **actions**

Three action types:

| type | what it does |
|---|---|
| `kubectl_patch` | applies a patch to a named object (Deployment, Service, …) |
| `wait_rollout` | waits for a workload to become ready before the next action |
| `http_request` | calls a URL — for notifying something outside the cluster |

### What ships built in

Two hooks are seeded on first start, both for Element Call's media path:

- **ESS RTC: SFU Host Network** (priority 10) — puts the SFU on the host network, then
  waits for the rollout
- **ESS RTC: Service ExternalTrafficPolicy** (priority 20) — sets
  `externalTrafficPolicy: Local` on the SFU's NodePort services, so the SFU sees the
  real client address instead of a node address

The second one deliberately covers three of the four services and not the fourth. The
RTC page explains which and why, rather than the hook silently covering everything.

### Checking they are still in effect

"Enabled" and "applied" are different questions. A hook can be enabled and its patch
still be gone — that is precisely the failure above. The **drift** check answers the
second question by asking whether the patch would still change something, and the
dashboard shows a banner when the answer is yes.

A field changed by hand that no hook carries also shows up there. That is a warning,
not an error: it means the change will be lost at the next upgrade unless you make it
a hook or put it in the config.

---

## 2. The config editor

### The model

ESS is configured by one large Helm values file. MatrixCtrl splits it into **sections**
— one file per area (`synapse`, `postgres`, `matrixRTC`, …) — and keeps them in a git
repository on its own volume.

Each section can be edited two ways:

- **Standard** — a form built from the chart's schema, with the chart's own
  documentation as help text
- **YAML** — the raw file, in an editor with schema validation

Both write the same file. The form is not a lossy view of the YAML: writes go through
YAML node surgery rather than a parse-and-re-serialise, so **comments survive**. That
matters more than it sounds, because in the ESS chart the `##` comments *are* the field
documentation, and a round trip that dropped them would delete the explanation of every
setting you did not touch.

### Applying a change

Editing a section writes the file. Nothing reaches the cluster until you apply, and
applying does three things in this order:

1. **commits** the change to the config repository, so the change exists in history
   before it exists in the cluster
2. runs a **`helm upgrade`** with the same chart version and the new values
3. runs the **post-upgrade hooks**, so patches the chart just undid come back

Before the upgrade starts, MatrixCtrl renders the chart and checks whether every
workload would still fit the cluster. A pod that would ask for more CPU or memory than
any node has is reported in the log, with both numbers, before anything is applied. It
warns rather than refusing — see the note at the end of chapter 3 for why.

### History and rollback

Every apply is a commit. The history view lists them, shows the diff of each, and can
reset the working tree back to any of them. A rollback is therefore two steps and both
are visible: restore the old values, then apply them.

### What is deliberately not offered

- **Bulk edits across sections.** One section at a time keeps a diff readable.
- **Editing the merged file.** It is generated; editing it would create a second source
  of truth that disagrees with the sections.

---

## 3. When an upgrade goes wrong

### While it runs

An upgrade streams its log live. It reports which phase it is in — applying the config,
waiting for the rollout, running hooks — and, during the rollout, what it is waiting
for: which workloads are updated, which are pulling images, which are still starting.

If your browser disconnects, reconnecting asks the server what actually happened rather
than assuming the worst. A closed tab does not abort an upgrade.

### The three ways it can end

**`deployed`** — Helm succeeded and the hooks ran.

**`failed`** — Helm itself failed. The release is unchanged or partially applied; Helm's
own error is in the log. The usual causes are a values file the chart rejects and an
image that cannot be pulled.

**`hooks-failed`** — and this one is worth understanding. **Helm succeeded**; a
post-upgrade hook did not. The new version is running, but one of the patches that
makes it work on your cluster is missing. Calling is the usual casualty, because the
built-in hooks are the ones that fix Element Call's media path. The upgrade is not
undone: look at which hook failed, fix the cause, and trigger it again.

### Getting back

Two different rollbacks, for two different mistakes:

- **The values were wrong** → config history: reset to the previous commit, apply.
- **The version was wrong** → Helm rollback on the release, which returns the chart and
  its values to the previous revision.

### A pod that will not start

If a component shows as `down`, the panel says *why* when the reason is that Kubernetes
refused to schedule it — including the arithmetic: what the pod asks for against what
the largest node has. If that number is larger than the node, no amount of waiting will
place it; the request has to come down or the node has to grow.

This is worth knowing because it is not obvious from the outside. A pod's CPU request is
not the sum of its containers — it is the larger of "all containers together" and "the
greediest init container alone" — and one `resources:` block in the values file can
apply to several containers at once. A value that reads as 4 cores can reserve 8.

The system page records node CPU and memory over time, capacity as well as usage, and
says so when a node's capacity changes. A machine that quietly loses cores will
otherwise look like a cluster that mysteriously stopped fitting.

### Why the preflight warns instead of refusing

A configuration that can schedule nothing is exactly the thing to block. It is not
blocked, because a false positive would stop every deployment, and the check has not yet
been wrong or right often enough to know. It warns loudly and applies. If it proves
reliable, refusing becomes the better default — that decision is tracked, not forgotten.

---

## Where the rest is written down

This guide is for using the product. The reasoning behind it — every design decision,
what was tried and rejected, and what is still missing — is in
[DESIGN.md](DESIGN.md), [ROADMAP.md](ROADMAP.md) and [BACKLOG.md](BACKLOG.md). Those are
written for maintainers and are unusually candid about what does not work yet.
