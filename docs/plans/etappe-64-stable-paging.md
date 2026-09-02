# Etappe 64 — Paging over an order that does not exist

P2-33, found while reading Synapse's source for E48 and deferred because E48 was already
carrying a migration and a routing change.

## The defect

Synapse orders both report queues by `received_ts` **alone**:

```python
ORDER BY received_ts {order}
LIMIT ? OFFSET ?
```

Rows sharing a timestamp therefore have no defined order between two queries. Paging is
offset-based, so if two such rows swap places between the request for page 1 and the
request for page 2, one row is shown twice and another is never shown at all.

The case that produces equal timestamps is a burst of reports about a single incident —
which is precisely the case where an admin is paging through a queue looking for
something. The bug waits for the moment it matters most.

## Why this is not simply fixed

The ordering is Synapse's and cannot be changed from here. Three things *can* be done,
and they are not equally honest:

1. **Sort each page by `(received_ts, id)` locally.** Makes what is displayed
   deterministic. Does nothing about the boundary between pages.
2. **Remember which ids have been shown and drop repeats.** Eliminates duplicates
   completely, because a duplicate is by definition an id already seen.
3. **Skips.** A row that the server never returned cannot be recovered by the client
   without walking the whole queue, which is what `limit` exists to avoid.

So this etappe fixes two of the three and says so, rather than claiming to have fixed
paging. A duplicate is visible and confusing; a skip is invisible and worse — and it is
the one that cannot be closed here.

## What that leaves

An admin paging a queue no longer sees the same report twice, and the order within a
page is stable across reloads. On a queue with a burst of same-second reports, a row can
still be missed at a page boundary. The honest mitigation for that is to raise the page
size until the queue fits in one page, which removes boundaries altogether — and at
`limit=50` against queues of a handful of reports, that is already the normal case.

## Scope

**Does not ship: client-side full traversal.** Fetching every page and sorting would make
the order exact, and would also mean loading a queue of unknown size to render its first
screen. The failure mode it introduces (an admin panel that hangs on a large server) is
worse than the one it fixes.

**Does not ship: a warning banner.** "Rows may have been skipped" on every paged view
would be true, unactionable, and ignored within a week — E56's argument against a memory
warning tuned to cry wolf.

## Definition of done

- Two reports with the same timestamp appear in the same order on every reload
- Paging forward and back never shows the same report id twice
- The dedupe survives switching between the two queues, since their ids are unrelated
- A single-page queue behaves exactly as before
- `make check` green
