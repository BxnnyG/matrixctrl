CREATE TABLE upgrade_history (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    ts_initiated    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ts_completed    TIMESTAMPTZ,
    user_id         TEXT        NOT NULL,
    from_version    TEXT        NOT NULL DEFAULT '',
    to_version      TEXT        NOT NULL,
    helm_revision   INTEGER,
    status          TEXT        NOT NULL DEFAULT 'pending',
    values_snapshot UUID        REFERENCES config_snapshots(id),
    helm_output     TEXT,
    error_message   TEXT,
    pre_flight      JSONB,
    hooks_run       UUID[]
);
CREATE INDEX idx_upgrade_history_ts ON upgrade_history(ts_initiated DESC);

CREATE TABLE ess_versions (
    version         TEXT        PRIMARY KEY,
    chart_digest    TEXT,
    published_at    TIMESTAMPTZ,
    changelog       TEXT,
    breaking_changes JSONB,
    discovered_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
