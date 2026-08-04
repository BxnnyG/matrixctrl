# Etappe 23 — Has a call ever worked?

**Date:** 2026-08-04 · **System:** S14 · **Addresses:** part of P1-13

## The problem

Two days were spent asking whether calling works. Every component answered about
itself — pods healthy, ports listed, patches applied, signalling reachable — and
none of them answered the question. The one number that would have was on the SFU's
own metrics port the whole time:

```
livekit_quality_score_count         0
livekit_forward_latency_ns_count    0
livekit_room_duration_seconds_count 0
```

Those move only when media actually flows. Zero, after hours of uptime and rooms
being created, is not an opinion.

## What this can and cannot say

E19 established that inbound reachability is not knowable from inside, and that
stands. But "**has** media ever arrived" is a different question from "**can** it",
and the first one is answerable from here.

| Answerable | Not answerable |
|---|---|
| Media has flowed since this SFU started | Whether the ports are open right now |
| No media has flowed since this SFU started | Whether the next call will work |

The second row is where the honesty has to live: **zero samples with zero rooms
means nobody called**, and that is not evidence of a fault. Zero samples *after*
rooms were created is evidence of one. The finding must distinguish those, or it
becomes an alarm that fires on every quiet night.

## Approach

Read `http://<sfu-service>:6789/metrics` — the metrics port already exists on the
Service — and parse four counters. No new exposure, no log parsing.

Findings:

- **rooms > 0 and media samples == 0** → warn. Calls reached the SFU and no media
  followed: the signalling half works and the media half does not. That sentence
  alone would have redirected this week.
- **media samples > 0** → ok, with when.
- **rooms == 0** → unknown, explicitly: nothing has been tried since this SFU
  started, so there is nothing to report. Not "fine".

Counters reset when the pod restarts, so every statement is scoped "since the SFU
started" and says so. A number without its window is a number that will be
misread.

## Mandatory edge-case walk ([PROZESS §1](../PROZESS.md))

1. **No ESS / ESS elsewhere.** Service looked up from the release name; absent →
   unknown.
2. **Helm release in a bad state.** Untouched.
3. **Not just Deployments.** Untouched — one HTTP read.
4. **Cluster slow or gone.** Timeout → unknown, never ok.
5. **No outbound internet.** In-cluster read only.
6. **Both auth modes.** No new route; extends `/api/v1/rtc/status`.
7. **Config edge shapes.** Untouched.
8. **Helm succeeded, hooks failed.** Unrelated.

## Definition of done

- The current production state (`rooms created, zero media`) produces the warning
- A quiet SFU produces `unknown`, not a warning and not an all-clear
- Metrics parsing tested without a cluster, including a malformed body
- S11 green **after** the deploy

## Outcome (2026-08-04)

Shipped in `0.1.24`.

Production at the time of writing: rooms created earlier in the day, **zero**
quality samples and zero forward samples. The page states that in one line —
signalling works, media does not — which is the conclusion two days of manual
measurement arrived at.

### The trap the tests caught

A truncated metrics body (`livekit_quality_score_count{unclosed 5`) parsed
successfully and set a real counter, which would have produced a **false all-clear**
from a half-delivered response. Label sets must now close before the value is taken.
That is the only failure mode of this feature that really matters: reporting media
where there is none sends the operator away from a real fault.

`livekit_node_packet_total{type="out"}` was the obvious candidate and is a trap of
its own — it climbed by 2400 in two minutes on an SFU with zero participants. Using
it as evidence would have produced a permanent green tick. It is reported for
context, and a test pins that it never counts as media.

### Found while shipping it

The tag was pushed with `Chart.yaml` still on the previous version, because a `cd`
in a build command persisted and the version bump silently ran in the wrong
directory. **E16's release guard caught it**: the workflow compares the tag against
the chart and failed before anything reached GHCR. That guard was written for
exactly this and had never fired in anger until now.

### What it still does not say

Whether the ports are open, or whether the *next* call will work. E19's permanent
unknown stands. This narrows it: "has media ever arrived" is answerable from inside,
"can it arrive" is not, and keeping those apart is the whole point.
