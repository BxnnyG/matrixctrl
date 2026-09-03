# Etappe 67 — The table everyone is told to read first

CLAUDE.md rule 2 says: *"Inventory instead of guessing. What already exists is looked
up, not assumed. Start at DESIGN.md §1."*

That table said **S13 User & room management — not started**. Users shipped in E27/E28,
rooms in E36/E41, moderation in E46/E48, media quarantine in E47, GDPR erasure in E65.
Roughly four months of work, invisible in the document every reader is pointed at first.

## What was wrong, measured

| row | said | is |
|---|---|---|
| S9 Verification | "26 frontend tests, 13 backend tests" | **33 frontend, 350 Go test functions** across 20 packages |
| S13 User & rooms | "not started" | eight etappes shipped |
| S14 Day-2 ops | "⅓ (E19)" | RTC has four more etappes; TLS is a config slice; backup/restore does not exist |
| S6 Setup | greenfield proven at E15 | still true, and re-verified today |

Counted rather than recalled. "Backup/restore" in particular looked half-present in a
grep until the matches turned out to be a one-off config-migration directory and a
comment — the difference between a word appearing and a feature existing.

## Why this is worth an etappe rather than a commit

This is the third document in this repo found describing a state the code had left:
§4.17's audit trail (documented two months before it was built), the backlog entries
E38 reconciled, and P2-4, which nearly got a feature built twice this week.

The pattern is not carelessness. Every one of them was written accurately and then
*stopped being true* while nobody re-read it, because nothing fails when a document goes
stale. That is the same shape as §4.49's committed build artefact and §4.62's dead
schema columns: state that looks authoritative, is checked by nothing, and drifts.

The defence used here is the only one that has worked elsewhere in this project —
**verify against the code while writing**, which is exactly how E60's guide found the
dead rollback trigger and E62's sweep found the unimplemented action type.

## Scope

**Ships:** the corrected table, an S13 section that describes what exists, and the
counts as measured today.

**Does not ship: an automated staleness check.** Tempting — a test that fails when the
table disagrees with the code — but the table's cells are prose about intent, and a
checker would either be trivially satisfiable or wrong. Recorded as an idea rather than
built badly.

## Definition of done

- No row claims a state contradicted by the code
- The test counts are the counts, on the day they were written
- S13 has a real section, not a placeholder among "not started"
- `make check` green
