# Etappe 44 — The calls page reports on a counter that resets

P2-29, reported by the operator 2026-08-16:

> "calls soll auch wenn möglich audit zeigen verbindungen zeigen maby auch
> statistik (statistik maby ausweiten auf das ganze) / logs länge"

Four asks: per-call audit, live connections, RTC statistics, and a wider statistics
story. The backlog entry said this needed an inventory pass before any of it, because
§4.24 is the standing warning here — the calls page once reported confidently on the
SFU while the failing calls were legacy 1:1, which it had no way to see.

## Inventory

**What LiveKit exposes on this deployment**, read from the SFU's own metrics port:

| metric | kind | what it answers |
|---|---|---|
| `livekit_room_total` | gauge | rooms open **right now** |
| `livekit_participant_total` | gauge | participants **right now** |
| `livekit_room_duration_seconds_count` | counter | rooms that have completed |
| `livekit_room_duration_seconds_sum` | counter | seconds of room time |
| `livekit_quality_score_{count,sum}` | histogram | call-quality samples |
| `livekit_forward_latency_ns_count` | counter | forwarded-media samples |
| `livekit_node_packet_total` | counter | packets, rises without any call |

Three of these are already parsed — `internal/rtc/media.go` reads the counters that
prove media flowed (E23). The two **gauges** are not read by anything, and they are
exactly "Verbindungen zeigen".

**The fact that shapes everything else:** all of these are *process-lifetime*. Read
live, ten hours after the 26.8.0 upgrade, every single one is `0`:

    livekit_room_total                   0
    livekit_participant_total            0
    livekit_room_duration_seconds_count  0
    livekit_quality_score_count          0

That is not "this server has never carried a call". It is "this **process** has not",
and the process is younger than the counters suggest, because the post-upgrade hook
deletes the SFU pod on every ESS upgrade to restore `hostNetwork`. On this instance
that is several times a week.

So a statistics page built directly on these numbers would silently mean "since the
last upgrade" while looking like "ever" — the §4.24 mistake with a different subject.
**History has to be recorded by MatrixCtrl or it does not exist.**

**The precedent is already in the package.** `internal/rtc/watcher.go` + `store.go`
exist for exactly this reason, for a different quantity: DNS observations are sampled
on a timer and persisted, because "a history built from page views has gaps exactly
where nobody was looking, which is most of the time." The same sentence applies here.

**What is deliberately not built:** LiveKit's RoomService API (port 7880) would give
room names and participant identities. It needs the SFU's API secret and a minted
admin token, and what it returns is *who is in a call with whom*. That is a different
class of data from "three people are in a call", and it is not needed to answer any
of the four asks. The gauges answer "connections" without it.

## What this etappe does

**Sampling.** A `Sampler` alongside the existing `Watcher`: reads the metrics port on
a timer, writes one row per observation. The endpoint is already fetched by the RTC
handler, so this is a second caller of a known-good path, not a new integration.

**Counter resets are first-class.** When a counter comes back *lower* than the last
sample, the SFU restarted. The delta is then the new value, not a negative number —
and the restart itself is worth recording, because "the SFU restarted" explains a gap
in every other series on the page.

**Exact totals, coarse timing.** `room_duration_seconds_{count,sum}` are cumulative,
so the number of calls and the total minutes between two samples are exact *even for
calls that began and ended entirely between them*. What the sampling interval bounds
is only *when* — the same trade the Watcher documents for DNS, and it gets stated on
the page rather than left for someone to discover.

**The page gains two sections:** what is happening now (rooms, participants, and how
long the SFU has been up so the zero can be read correctly), and what has happened
(calls per day, total minutes, quality samples, restarts). The existing call-path
assessment stays exactly where it is — it answers a question neither of these does.

**"Statistik ausweiten auf das ganze" is not in scope**, and the plan says so rather
than half-doing it. A panel-wide statistics story is a design question about what an
operator wants to see over time, not a matter of finding more counters. This etappe
makes one series real; the shape of the general case should be argued from a working
example rather than in advance.

## Definition of done

- A sample is written on a timer and survives an SFU restart
- A restart is visible as a restart, never as a negative delta or a reset to zero
- The live section distinguishes "no calls" from "the SFU restarted a minute ago"
- Totals are exact across a restart, verified against a counter that actually moved
- The page states the interval, so "when" is never read as more precise than it is
- No participant identity is read or stored
- `make check` green, and a live test against the real SFU
