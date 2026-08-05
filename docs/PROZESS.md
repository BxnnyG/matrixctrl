# MatrixCtrl — Working process

> Binding for EVERY change. No exception for "small" features — the small ones
> are precisely what tore the holes listed in [BACKLOG.md](BACKLOG.md).
>
> Goal: [VISION.md](VISION.md) · Systems: [DESIGN.md](DESIGN.md) ·
> Timeline: [ROADMAP.md](ROADMAP.md)

## 0. Check issues (at the start of EVERY session)

The `gh` CLI is not installed on the build host, so use the API directly:

```bash
curl -s "https://api.github.com/repos/BxnnyG/matrixctrl/issues?state=open" \
  | python3 -c "import sys,json;[print('#%s %s'%(i['number'],i['title'])) for i in json.load(sys.stdin)]"
```

- Small, clear bug → fix it now, own commit referencing `Fixes #<n>`, plus a
  regression test. Bigger → plan it as an etappe.
- **Never fix past the report.** Reproduce first. If it cannot be reproduced,
  prove that and say so — do not blind-edit a file that is probably correct.

## 1. Plan (what & why)

- What is the problem **from the operator's point of view**? Not from the code's.
- **Centralisation check:** does more than one place need this? → shared package
  under `internal/` or `web/src/components`, never duplicated (S12).
- What already exists? Read [DESIGN.md §1](DESIGN.md#1-current-inventory-what-already-exists-centrally).
  Do not guess — the inventory exists so you don't have to.
- Walk the **mandatory edge-case list** below.
- Play the flow through: which states exist, and what does each role see?
- Open decisions go to the operator, not into your own assumption.

### Mandatory edge-case list

Derived from failures this project has actually had. Walk all eight, every time.

1. **No ESS / ESS elsewhere.** Greenfield (no release at all), adopted release in
   a non-default namespace, and release name ≠ `ess`. The guards short-circuit
   differently in each — S6's untested path lives here.
2. **Helm release in a bad state.** `failed`, `pending-upgrade`, or a revision
   history that does not go back far enough to roll back to.
3. **Not just Deployments.** Synapse and Postgres are **StatefulSets**, and
   Postgres is a **multi-container** pod. Anything that lists workloads or reads
   logs must handle both, or it silently omits the two most important components.
4. **The cluster is slow or gone.** Metrics API unavailable, a node NotReady, a
   list call that hangs. Every k8s call needs a timeout and a nil-guard.
5. **No outbound internet.** GHCR unreachable (version list), any CDN unreachable
   (this is how the config editor shipped broken). A self-hosted tool must degrade,
   not blank out.
6. **Both auth modes.** Bootstrap (greenfield, OIDC off) and OIDC-admin. Every new
   route must be reachable in the mode it is meant for and refused in the other.
7. **Config edge shapes.** A section file that is empty, absent, or heavily
   commented; a value that only exists in the chart defaults. Comments must
   survive (S11 #2).
8. **Helm succeeded, hooks failed.** The `hooks-failed` state is not an error
   state for the release — the UI must offer a re-run rather than implying the
   deployment is broken (§4.6).

## 2. Implementation plan (how)

Write it to `docs/plans/etappe-NN-<slug>.md` **before the first line of code**,
using the fixed chapter order (see any existing plan). A plan may be ten lines
long, but it must exist.

## 3. Build

- Logic with tests first, then the HTTP layer, then the UI.
- Small commits. Schema changes always land with their migration.
- New UI uses the primitives in `web/src/components/mc.tsx`. New editor usage
  goes through `YamlEditor` (§4.10).

## 4. Verify & ship

The definition of done (§4.12). **The agent performs all of it autonomously —
nothing here is a manual step for the operator.**

```bash
go test ./...                      # Go unit tests
cd web && ./node_modules/.bin/tsc --noEmit && npm run build
make docker VERSION=<next>         # build the image
docker save ghcr.io/bxnnyg/matrixctrl:<next> -o /tmp/mc.tar
k3s ctr images import /tmp/mc.tar  # no registry pull; chart uses IfNotPresent
helm upgrade matrixctrl deploy/helm/matrixctrl -n matrixctrl \
  -f deploy/helm/matrixctrl/values.bxnny.yaml \
  --set image.tag=<next>
kubectl -n matrixctrl rollout status deploy/matrixctrl --timeout=180s
```

> **`--set image.tag` is not optional.** The chart's committed default is
> `tag: "latest"`, and CI rewrites it to the exact version only when it *packages a
> released chart* (`release.yml`). A local deploy from the working tree therefore
> renders `:latest` — whatever stale build containerd happens to hold under that
> name. On 2026-08-05 this deployed a **0.1.32** image while `helm list` reported
> `APP VERSION 0.1.33` and `rollout status` reported success. Neither was lying about
> its own subject; neither was answering the question "what code is running?".
>
> **`rollout status` is not proof that anything changed.** If the upgrade never
> applied — a bad `-f` path, a values error — the deployment is untouched, and
> `kubectl rollout status` cheerfully reports the *old* one as successfully rolled
> out. That happened on 2026-08-01 and looked exactly like success. Always read
> back the image tag and the Helm revision, not just the rollout:
>
> ```bash
> helm list -n matrixctrl   # CHART/APP VERSION must be the new one
> kubectl get deploy matrixctrl -n matrixctrl \
>   -o jsonpath='{.spec.template.spec.containers[0].image}'
> # The one that settles it — the binary names its own version and commit:
> kubectl logs -n matrixctrl deploy/matrixctrl -c matrixctrl | grep starting
> # → "MatrixCtrl 0.1.33 (2f671ca) starting"
> ```
>
> The log line is the only one of the three that reads the *artefact* rather than a
> declaration about it. A tag can point anywhere; `helm list` reports the chart's
> `appVersion`, which is a string in a file. The commit hash cannot be wrong.

Then verify the **running** result, not the build output:

- `curl` the public URL for a 200, and check the served bundle actually contains
  the change (asset hashes, endpoint registration → `401` means registered,
  `404` means missing).
- New API routes: confirm each is reachable inside the pod.

**Then the four regression checks (S11) — do not skip:**

1. The managed ESS instance is still reachable.
2. Saving config destroyed no comments or values.
3. Admin login (OIDC via MAS) still works.
4. The SFU patches are still in place after an upgrade
   (`kubectl get deployment -n ess ess-matrix-rtc-sfu -o yaml | grep hostNetwork`).

Finally, pull the docs along — this is part of shipping, not paperwork:

- [ROADMAP.md](ROADMAP.md): etappe row → ✅ with the date.
- [DESIGN.md §1b](DESIGN.md#1b-status-overview--maintain-this-in-every-etappe): status table.
- [DESIGN.md §4](DESIGN.md#4-decisions): any decision taken, numbered and dated.
- README / CLAUDE.md if the developer-facing truth changed.

> **Not yet automatable:** the operator asked for a screenshot/browser check as
> part of verification. No headless browser is installed on the build host, so
> this step cannot run today — it is tracked in [BACKLOG.md](BACKLOG.md) (P1) and
> planned in etappe 13. Until then, verification stops at HTTP-level checks.

## Entry rule

**No feature is built that does not already exist as an entry in
[DESIGN.md](DESIGN.md)** — even if the entry is three lines long. If it is worth
building, it is worth one paragraph saying what it is and why.

## How this dies (and how to notice)

Two failure modes, both seen in real projects:

- **The status table (DESIGN §1b) stops being maintained.** After three etappes
  it is wrong, after five nobody believes it, after ten the whole documentation is
  dead. That is why it is step 4 of this process and not a request.
- **"Just this once without a plan."** The small changes are the ones that tear
  the holes.

If you are an agent and you notice either rule has been broken: **say so before
you build anything else.**
