-- Make a disposition say *which* queue its report is in (etappe 48).
--
-- Migration 013 created event_report_dispositions with `report_id BIGINT PRIMARY KEY`
-- because there was one queue. There are two. Synapse's user_reports.id and
-- event_reports.id are independent sequences, so both queues contain an id 1 and an
-- id 2 that refer to entirely different reports.
--
-- Storing user-report decisions in the old table would therefore have made event
-- report 5 and user report 5 the same row: marking one handled would have marked the
-- other, and reopening one would have reopened the other. No error, no constraint
-- violation — the queue would simply have shown a decision nobody made. That is worth
-- a migration rather than a second table, because the rules the table encodes (open is
-- the absence of a row, reopen is a delete, only 'handled' and 'dismissed' are
-- writable) are the same for both queues and must not drift apart.
--
-- The rename is safe for existing rows: everything written before this migration was
-- an event report by construction, so the backfill is a constant.

ALTER TABLE event_report_dispositions RENAME TO report_dispositions;

-- DEFAULT 'event' backfills existing rows correctly and is then dropped, so future
-- inserts must state the kind rather than silently inheriting the older queue.
ALTER TABLE report_dispositions
    ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'event'
        CHECK (kind IN ('event', 'user'));

ALTER TABLE report_dispositions ALTER COLUMN kind DROP DEFAULT;

-- The key is the pair. Dropping the old single-column key first is what actually
-- removes the collision; adding the column without this would leave report_id unique
-- across both queues and the bug fully intact.
ALTER TABLE report_dispositions DROP CONSTRAINT event_report_dispositions_pkey;
ALTER TABLE report_dispositions ADD PRIMARY KEY (kind, report_id);

-- Postgres does not rename constraints when their table is renamed, so without this
-- the surviving check is still called event_report_dispositions_state_check on a table
-- that serves both queues — a name that would tell the next reader something false.
-- The old name is deterministic: migration 013 declared the check inline on the
-- column, which is what Postgres derives it from.
ALTER TABLE report_dispositions
    RENAME CONSTRAINT event_report_dispositions_state_check TO report_dispositions_state_check;
