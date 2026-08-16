# Etappe 45 — The staleness warning has been false for twelve days

Found 2026-08-16 while checking whether E44's new table would grow without bound
(P2-19's class). `rtc_address_history` had **1778 rows**, which is not what a table
that records address *changes* should look like.

## What the data says

    address           count   first        last
    172.67.188.105     889    2026-08-04   2026-08-16
    104.21.33.5        889    2026-08-04   2026-08-16

Two addresses, 889 rows each. That symmetry is a coin flip, not a history. 152 rows
a day, every day, for twelve days.

Both addresses are Cloudflare's. The announced RTC host is proxied, so DNS returns
**two A records**, and the resolver rotates their order per query. `Store.Record` is
handed `ips[0]`:

```go
var resolved string
if len(ips) > 0 {
    resolved = ips[0]
}
```

So every poll samples one member of a set at random, and `NextObservation` — which
is correct for what it was given — sees a different answer roughly half the time and
duly inserts a new row.

## What it cost

Not just rows. `AssessFreshness` compares the newest observation's `first_seen`
against the SFU pod's start time, and the newest observation is never more than a few
minutes old:

    SFU pod started       2026-08-16 00:49:53Z
    last "address change" 2026-08-16 15:24:19Z

`obs.FirstSeen.After(podStart)` is therefore true, permanently. The calls page has
been showing this since 2026-08-04:

> **Die SFU kündigt eine veraltete Adresse an** … Medien laufen ins Leere, während
> Signalisierung und Pods gesund aussehen.
> *Action:* SFU-Pod ersetzen.

A `WARN` finding, continuously false, immediately above a button that replaces the
SFU pod — which drops any call in progress. E22 built this to catch a real failure
(P1-9, the SFU announcing a stale address after a forced reconnect). For twelve days
it has been reporting on DNS round-robin.

## The second cause, which is worse

Fixing the set/member bug would stop the noise and would still leave the verdict
meaningless *on this deployment*.

E22's premise is stated in `address.go` and is sound in itself:

> the announcement equals the public address at the moment the pod started, so it is
> stale exactly when the address changed after that moment

It depends on the announced host's A record **being the node's public address**. When
that host is proxied through a CDN, its A records are the CDN's anycast addresses.
They do not change when the operator's WAN address changes, and they change for
reasons that have nothing to do with this deployment. The comparison cannot answer the
question it is asked, whatever it is fed.

This is the same shape as §4.40's table, one more time: the check answered *its own*
question correctly — "did `addrs[0]` change?" — and never the one being asked.

## What this etappe does

**Record the set, not a member.** A host with several A records has an address *set*;
a change is a change to that set. Sorted and joined, so a rotation is not a change.
This is the correctness fix and it is small.

**Refuse the verdict when the premise does not hold.** More than one A record means
something sits in front of the node — a home connection has one WAN address — so the
DNS answer is no longer a proxy for what the SFU discovered by STUN. That returns
`Unknown` **with the reason**, never `ok` and never `stale`. `Unknown` already exists
for precisely this and the page already renders it; what was missing was recognising
this as a case of it.

Stating it plainly is the product: an operator behind a CDN learns that this
particular check cannot see their setup, rather than being told to restart the SFU
every day.

**Retention, for both tables.** The 1778 rows are noise and are deleted. Beyond that,
both `rtc_address_history` and E44's `rtc_samples` grow forever by design, on a
single-node cluster sharing a disk with Synapse's database and media (P2-19).
`rtc_samples` adds 1440 rows a day — I shipped that yesterday without a bound, which
is the same defect one table over.

Retention here is *not* the policy decision P2-19 correctly refuses to guess at. An
audit trail answers "who did what" and its retention is a compliance question. These
two tables are operational telemetry with no such duty, so a default is appropriate
and the number is documented where it is set.

## Definition of done

- A rotating multi-address answer records **no** change
- The freshness verdict on this deployment is `unknown` with the CDN reason, not
  `stale`
- A genuine single-address change is still detected — proven by test, since it cannot
  be provoked live
- The 1778 noise rows are gone and both tables are bounded
- Verified against the live database, before and after
- `make check` green
