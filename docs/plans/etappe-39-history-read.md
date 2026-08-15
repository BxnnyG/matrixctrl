# Etappe 39 — The history page pays 4 seconds, every time (P2-22)

**Status:** planned · 2026-08-15

## The entry said it was never measured. It is now.

P2-22 has sat open since 2026-08-02 with the note *"not yet measured on its own"*.
Measured against the production release (14 revisions):

| call | cost |
|---|---|
| `ListHistory` (`action.NewHistory`) | **3.2 – 4.6 s**, every load |
| `GetRelease` (E20's fast path, for comparison) | 672 ms |
| metadata-only list of the same secrets | **56 ms** |

Every visit to the upgrade-history page pays that, and it is the page an operator
opens *because something has gone wrong* — so the latency lands exactly when
patience is shortest.

## A second defect the measurement exposed

`ListHistory(name, max)` takes a `max`. **Helm ignores it.**

```go
func (h *History) Run(name string) ([]*release.Release, error) {
	…
	return h.cfg.Releases.History(name)   // h.Max is never read
}
```

`Max` exists on the action struct for the CLI's output formatting. Asking for 10
returns 14 and costs the same as asking for 30 — confirmed by measurement, not by
reading the field name. Our API has carried a parameter that does nothing.

## Two things I assumed, measured, and had to discard

Both would have shipped as plausible-looking code.

**1. "Decode each revision individually and cache it."** The obvious refinement of
E20's trick. Measured on the same 14 revisions:

```
14 × Releases.Get   →  7.328 s
1  × Releases.History →  5.270 s
```

Fetching revisions one at a time is **40 % slower** than the call being replaced,
because each is its own round trip. So the cold fill has to stay a single
`History` call; only the *incremental* case — one new revision after an upgrade —
is better served by a targeted `Get` (~520 ms). Break-even is around ten
revisions, and the constant is derived from that measurement rather than chosen.

**2. "The `modifiedAt` label can supply the timestamp."** E20 reads that label, so
reusing it for the history rows looks free. It is not a per-revision timestamp:

```
rev 16  LastDeployed=2026-05-24T21:21:29Z   modifiedAt=1769459689
rev 24  LastDeployed=2026-08-05T16:31:55Z   modifiedAt=1769459689   ← identical
rev 29  LastDeployed=2026-08-06T16:35:25Z   modifiedAt=1785949355
```

Nine revisions spanning ten weeks share one `modifiedAt`. E20 uses it correctly —
as a cache-invalidation key for the newest revision, never as a displayed time —
and copying it into a "deployed at" column would have put confidently wrong dates
in front of the operator. The timestamp has to come from a decode.

## Design

What the labels *can* answer, in 56 ms: which revisions exist, and each one's
**current status**. What needs a decode: chart version and `LastDeployed`.

The saving move is that those two are **immutable**. A revision's chart and its
deployment time never change once written; only its status does (`deployed` →
`superseded`). So:

```
metadata list (56 ms)  →  revisions + current status
        ↓
per-revision cache of {chart, deployedAt}, keyed by (release, revision)
        ↓
misses ≤ 10  →  targeted Releases.Get each (~520 ms)
misses > 10  →  one Releases.History (5.3 s), because N × Get is slower
        ↓
status always from the label, never from the cache
```

Steady state: **56 ms and zero decodes**. After an upgrade: 56 ms plus one `Get`.
After a restart: one full fill, once.

`max` becomes real: the metadata list is sorted descending and truncated before
anything is decoded, so asking for 10 also *costs* 10.

Fallback, as in E20: any failure of the fast path ends in the current
`action.NewHistory` code. The worst case is today's latency, not a wrong answer.

## What could go wrong, and what catches it

- **A cached chart version going stale.** It cannot: the cache is only ever
  written for a revision whose payload was decoded, and a revision's payload is
  immutable. Status — the one mutable field — is deliberately not cached.
- **Revision numbers reused after a rollback.** Helm's rollback creates a *new*
  highest revision rather than rewriting an old one, so `(release, revision)` stays
  a stable key. Asserted in a test rather than trusted.
- **Unbounded growth.** Helm keeps at most `--history-max` revisions (10 by
  default; this release has 14). The cache is bounded by that per release, which is
  small enough not to need eviction — but entries for revisions that no longer
  exist are dropped on each read, so a long-lived process does not accumulate them.

## Definition of done

- History page measured before and after, on the live release, in the same way
- `max` is honoured, and a test asserts it bounds both the result and the work
- Fallback proven by forcing the probe to fail
- Signing in still works, S11 green after the deploy
- P2-22 struck through with the numbers; DESIGN records the two discarded
  assumptions, since both looked correct
