# Etappe 48 — the second queue, and an id that means two different things

E46 shipped "Moderation" and it is silent about half of what Synapse holds. An admin
who empties that screen may still have an untouched queue behind it. This closes
P2-30, and the closing turned up a data bug that would have been silent.

## Inventory, from Synapse's own source

`rest/admin/user_reports.py` and `storage/databases/main/room.py`:

| endpoint | method | returns |
|---|---|---|
| `/user_reports` | GET | `{user_reports, total, next_token?}` |
| `/user_reports/<id>` | GET | `{id, received_ts, target_user_id, user_id, reason}` |
| `/user_reports/<id>` | DELETE | destroys the record — the only upstream way to clear |

## Three findings that change the design

### 1. The report id is not unique across the two queues

`event_report_dispositions` (migration 013) has `report_id BIGINT PRIMARY KEY`.
User report ids come from **their own sequence** — `user_reports.id` and
`event_reports.id` are independent, so both queues contain an id 1, an id 2, and so
on, meaning different reports.

Reusing the table as it stands would make event report 5 and user report 5 **the same
row**. Marking one handled would silently mark the other, and reopening one would
reopen the other. Nothing would error; the queue would just quietly lie — the same
shape as §4.43, recording one member of a set and reading it back as the set.

Migration 014 renames the table to `report_dispositions`, adds `kind`, and makes the
key `(kind, report_id)`. One table and one `Dispositions` type parameterised by kind,
not a sibling table: the storage rules — open is the absence of a row, reopen is a
delete, `handled`/`dismissed` are the only writable states — are identical for both
queues, and duplicating them is how the two copies start disagreeing (rule 3).

### 2. The detail endpoint returns nothing the list did not already have

`get_user_reports_paginate` selects `id, received_ts, target_user_id, user_id, reason`
per row. `get_user_report` returns **those same five fields**. Unlike event reports,
where the detail call is the only source of `event_json`, here it is a second round
trip for a byte-identical answer.

So there is **no detail fetch and no detail route**. A row expands in place with what
the list already carried. Building the symmetrical `GET /reports/users/{id}` because
the event queue has one would be the kind of mirroring that looks like consistency and
is really just an extra failure mode.

### 3. The filters are substring searches, not filters

```python
filters.append("user_id LIKE ?")
args.extend(["%" + user_id + "%"])
```

A filter for `@bob:example.org` also matches `@bobby:example.org`. That is Synapse's
behaviour and it is not worth fighting, but the field is labelled as a **search** so
nobody reads a filtered count as exact.

Also `ORDER BY received_ts` — not by id. Reports sharing a timestamp have no stable
order, so paging can repeat or skip one. Recorded, not fixed here: fixing it means
ordering client-side across pages, which changes what "page" means.

## Scope

**Ships:** the user-report queue listed alongside the event queue, with dispositions
that cannot collide, and a moderation screen that states both counts.

**Does not ship: acting on a reported user.** Deactivation erases the account;
suspension is reversible but it is an action *against a person* and belongs with the
permissions story that E47 already deferred protect/unprotect to.

**Also in scope, separately verified: P2-31.** An unmatched `/api/` path currently
answers `200 text/html` from the SPA fallback. It is included because this etappe adds
API routes and that behaviour is what makes "does this route exist?" unanswerable for
an unguarded route — and because it is a 200 that means "no such thing", which is the
same defect class as E47's. It gets its own before/after check rather than riding
along on the queue's.

## Definition of done

- Both queues appear, each with its own count, and neither implies the other is empty
- A disposition on user report N does not affect event report N — checked directly
  against the live database, since this is the bug the etappe exists to prevent
- Existing dispositions survive the migration with their kind set to `event`
- Reopening still removes the row
- An unmatched `/api/` path answers 404 JSON; the SPA still loads on every real route
- `make check` green
