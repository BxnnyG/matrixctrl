# Etappe 53 — The banner named the wrong thing and called an old number a loop

Spotted in the operator's dashboard screenshot on 2026-08-17. The red alert at the top
of the page reads:

> **postgres** in Restart-Schleife — Ursache ansehen.

while the component row six lines below it correctly reads `63× ⚠ postgres-exporter`.
Two different claims about the same fact, on the same screen, and the louder one is
wrong twice.

## Measured first

```
$ kubectl -n ess get pod ess-postgres-0 -o json | …
  postgres                 restarts=0   ready=True
  postgres-ess-updater     restarts=0   ready=True
  postgres-exporter        restarts=64  ready=True
```

**Postgres has restarted zero times.** It has never restarted. The banner tells the
operator that the database underneath their entire homeserver is crash-looping.

And the recency:

```
  postgres-exporter: last terminated 2026-08-18T02:38:10Z, running since then (~17 h)
  pod age: 12 days
```

64 restarts spread over twelve days, stable for the last seventeen hours.

## Two defects, both already documented in this file

**It names the workload, not the container.** This is P2-8 / §4.43 exactly, and E38
already fixed it — in the table row and the drawer badge. `ComponentHealth.RestartsBy`
is populated, correct, and rendered two elements away. The banner simply never read it.
A fix applied to some of the places that render a value is a fix with a half-life.

**"Schleife" is a present-tense claim built from a lifetime counter.** The condition is
`c.restarts > 20` — a number that only ever increases, with no time in it anywhere.
It cannot distinguish a container dying every thirty seconds right now from one that
misbehaved a fortnight ago and has been fine since. That is §4.42 again: a counter that
resets is not a history, and a counter that only accumulates is not a present state.

Kubernetes answers the present-tense question directly and it was never asked:
`state.waiting.reason == "CrashLoopBackOff"` is *the* signal for "in a loop right now",
and `lastState.terminated.finishedAt` says when the last one actually was.

## Scope

**Ships:** `LastRestart` and `Looping` on `ComponentHealth`, and a banner that names the
container and distinguishes an active loop from a stale total.

An active loop keeps the red banner. A high count that is not currently looping gets a
calmer line that says what restarted, how often, and when it last happened — because
64 restarts is still worth knowing about, it is just not an emergency, and rendering it
as one is why alarming banners get ignored.

**Does not ship: a restart-rate history.** "Five per day for twelve days" is the truly
useful framing and it needs samples over time, which is a table and a sampler (E44's
shape). `lastState` answers "is this happening now" without one, and that is the
question the banner asks.

**Does not ship: changing the >20 threshold.** It is a reasonable trigger for *looking*;
the defect is what the banner then claims, not that it noticed.

## Definition of done

- The banner names `postgres-exporter`, never `postgres`, when the exporter is what
  restarted
- A component in CrashLoopBackOff still reads as an active loop, in red
- A high count with no current loop reads as history, with the time of the last restart
- A component whose restarts have no dominant container still names the workload —
  `RestartsBy` is deliberately empty there and inventing a culprit is the bug it avoids
- `make check` green
