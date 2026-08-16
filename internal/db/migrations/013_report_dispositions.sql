-- What an admin did about a report, kept here rather than in Synapse (etappe 46).
--
-- Synapse has no "resolved" state for an event report. It offers exactly one way to
-- empty the queue: DELETE /_synapse/admin/v1/event_reports/<id>, which destroys the
-- record. That is the wrong primitive to build a moderation queue on.
--
-- A report is a user's statement that something was wrong. Deleting it after acting
-- on it means the next admin cannot see that it existed, what it said, or that anyone
-- looked at it — and if one account is reported five times and each report is deleted
-- as it is handled, the pattern is erased one report at a time. The pattern is the
-- part that matters.
--
-- So the disposition lives here and Synapse's record is never touched. Marking is
-- reversible: reopening a report deletes this row and takes nothing away. That is the
-- §4.39 rule — an action with an inverse can ship early.
--
-- report_id is Synapse's integer id and is *not* a foreign key to anything here.
-- Nothing in this database owns reports; this table annotates rows that live in
-- another system, which is why a report that vanishes upstream simply stops being
-- joined rather than breaking a constraint.
CREATE TABLE IF NOT EXISTS event_report_dispositions (
    report_id  BIGINT      PRIMARY KEY,
    -- 'handled' or 'dismissed'. Both mean "off the open queue" and they differ in
    -- what they say to the next admin: handled means something was done, dismissed
    -- means the report was judged not to need it. Collapsing them into one flag
    -- would lose the only distinction an admin actually cares about later.
    state      TEXT        NOT NULL CHECK (state IN ('handled', 'dismissed')),
    note       TEXT        NOT NULL DEFAULT '',
    -- Who decided, as a MatrixCtrl user id. Nullable because a disposition made by
    -- an automated path later must not be attributed to a person.
    actor      TEXT,
    decided_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
