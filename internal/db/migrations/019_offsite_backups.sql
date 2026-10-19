-- Automatic backups outside the server (etappe 120).
--
-- One row per attempt, including the ones that did not happen: a scheduled run that was
-- skipped because the target is not set up, or because another backup was running, is a
-- fact the page has to be able to state. "Last run: never" and "last run: skipped, no
-- target" are different answers to the operator's real question, whether last night's
-- backup exists.
--
-- `status` is running | ok | failed | skipped. A row left `running` by a restart is closed
-- as failed at startup; no process owns it any more.
CREATE TABLE IF NOT EXISTS offsite_runs (
    id          BIGSERIAL   PRIMARY KEY,
    started_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ,
    occasion    TEXT        NOT NULL,                -- zeitplan | manuell | vor-upgrade-<version>
    status      TEXT        NOT NULL,
    object_key  TEXT        NOT NULL DEFAULT '',
    size_bytes  BIGINT      NOT NULL DEFAULT 0,      -- the stored (encrypted) size
    media       BOOLEAN     NOT NULL DEFAULT false,
    error       TEXT        NOT NULL DEFAULT '',
    note        TEXT        NOT NULL DEFAULT ''      -- e.g. a failed clean-up after a good upload
);

CREATE INDEX IF NOT EXISTS offsite_runs_started_idx ON offsite_runs (started_at DESC);

-- What happened to the backup before this upgrade: '' (none was asked for, e.g. rows
-- from before this etappe), ok, skipped (the operator chose to go on without), failed
-- (stopped the upgrade) or not_configured (no target, so none was possible).
ALTER TABLE upgrade_history ADD COLUMN IF NOT EXISTS backup_status TEXT NOT NULL DEFAULT '';
ALTER TABLE upgrade_history ADD COLUMN IF NOT EXISTS backup_object TEXT NOT NULL DEFAULT '';
