-- The two fields the upgrade-history page needs that require decoding a Helm
-- release secret, kept because they never change again (etappe 42).
--
-- E39 established that a revision's chart version and deployment time are fixed the
-- moment Helm writes it — only its status changes afterwards — and cached them in a
-- map. That took the page from 3.2–4.6 s to 25 ms, and the framing "cold once per
-- process" turned out to matter more than it sounded: the map dies with the pod, and
-- this project deploys several times a day, so the operator met the cold path far
-- more often than "once". They measured it at 7.7 s.
--
-- A table survives restarts, so the cost is paid once per revision rather than once
-- per process. The in-memory map stays in front of it: Postgres is fast, but not
-- 25 ms fast for fourteen rows plus a metadata list.
--
-- No deployed_at NOT NULL: Helm can write a revision whose LastDeployed is zero, and
-- refusing to record such a revision would send every later read back to the slow
-- path forever.
CREATE TABLE IF NOT EXISTS helm_revision_facts (
    release_name TEXT        NOT NULL,
    revision     INTEGER     NOT NULL,
    chart        TEXT        NOT NULL,
    deployed_at  TIMESTAMPTZ,
    recorded_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (release_name, revision)
);
