# Etappe 74 — A restore that survives its own foreign keys

## The report

`TestFullArchiveRoundTrip`, the first test to take a real archive from a real database
and put it back, failed on its first run:

    RestoreDatabase: hook_run_log: ERROR: insert or update on table "hook_run_log"
    violates foreign key constraint "hook_run_log_hook_id_fkey" (SQLSTATE 23503)

So restoring any archive from an install that has ever run a hook fails outright. Not
partially — the whole restore is one transaction, so it rolls back and nothing lands.
Etappes 68 to 73 built a backup nobody could restore.

## Why it was invisible

`backup.go` says, in a comment above the table listing:

    // (this schema has no cross-table foreign keys — ...)

There are two, both older than that comment:

| child | parent | migration |
|---|---|---|
| `hook_run_log.hook_id` | `hooks.id` | 002 |
| `upgrade_history.values_snapshot` | `config_snapshots.id` | 003 |

Tables are captured and restored in alphabetical order. `config_snapshots` sorts before
`upgrade_history`, so that pair works — by luck, not design. `hook_run_log` sorts before
`hooks`, so that one always breaks. One bug hidden behind one coincidence.

The unit tests could not catch it because they build their own archives *and* their own
expectations: no fixture contains a foreign key, because the person writing the fixture
believed the comment.

## The fix

Order comes from the target database, not from the alphabet or the archive. The target
is the authority: it is the schema the rows have to satisfy, and an archive may predate
any given constraint.

Two phases inside the existing single transaction:

1. `TRUNCATE` every target table, **children first** — truncating a referenced parent
   while a child still holds rows is its own error, so reverse order is required here
   and `CASCADE` is not an option (it would silently empty tables outside the archive).
2. `COPY` the rows in, **parents first**.

Interleaving truncate-then-copy per table, which is what it does today, is unfixable by
ordering alone: any order that is right for one phase is wrong for the other.

Dependency order is computed with a topological sort over `pg_constraint`, so a
constraint added in a future migration is handled without anyone remembering this file.
Self-references are ignored (a row referencing its own table is satisfied within one
`COPY`), and a genuine cycle falls back to name order rather than refusing to restore.

## Checks

- New hermetic unit test for the ordering itself, with a fake graph including the real
  pair, a self-reference and a cycle.
- `TestFullArchiveRoundTrip` goes green against the live database.
- The four S11 regression checks.
