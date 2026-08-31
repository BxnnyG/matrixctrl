# Etappe 59 — The sparkline that invented its own past

P2-3: "The CPU/RAM sparklines live in memory and reset on reload, so *is this getting
worse?* cannot be answered."

The outage of 2026-08-16…18 added a second question the same table answers, and the
one that actually cost 37 hours: **did the machine change under us?** The node went
from 32 cores to 6 and nothing anywhere recorded it. The only trace was a screenshot
the operator happened to have taken beforehand.

## What is there now

`system.tsx` keeps history in a `useRef`, so it lives in one browser tab and dies on
reload. That is the entry as filed. Reading it turned up something worse:

```js
historyRef.current[n.name] = { cpu: new Array(MAX_HISTORY).fill(cpuP), ... }
```

A freshly loaded page **pre-fills the history with the current value**. The chart shows
a flat line that reads as "stable for the last hour" and is in fact one reading
repeated forty times. It is not empty-looking, it is confidently wrong — the §4.42
family again, a display that looks like evidence and is not.

## What ships

The shape E44 and E45 already proved for the RTC counters, applied to the node:
a table, a sampler on a timer, and retention with a real number.

Sampled every minute, and **allocatable is recorded alongside usage**. That is the
part the incident argues for: usage answers "is it getting worse", capacity answers
"did the machine change", and only one of those was ever going to be asked at three in
the morning.

A capacity change is then detectable rather than merely recorded: when the newest
sample's allocatable differs from the previous one, the dashboard says so, with both
numbers and the time. On a single-node cluster that sentence is the entire diagnosis of
this month's outage.

## Scope

**Does not ship: downsampling.** Same reasoning as E45 — keeping hourly averages beyond
the retention window is a second representation of the same data, with its own bugs, to
save a few megabytes on a disk measured in gigabytes.

**Does not ship: alerting on a capacity change.** It is shown where the operator already
looks. A capacity change is not always a fault — this one was deliberate, as the
operator confirmed — and paging someone about an intentional resize is how alerts get
muted.

**Does not ship: per-pod history.** The node is what changed and the node is what the
sparklines already show. Per-workload usage over time is a different feature with a
much larger table behind it.

## Definition of done

- Node CPU/memory usage *and* allocatable are sampled every minute and survive a restart
- The sparkline is drawn from recorded samples, never from a repeated current value
- With fewer samples than the window, the chart shows what exists rather than padding it
- A change in allocatable between two samples is surfaced with both values and a time
- Retention bounds the table, with the number stated in code
- `make check` green
