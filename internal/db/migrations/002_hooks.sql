CREATE TABLE hooks (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT        NOT NULL,
    description TEXT,
    trigger     TEXT        NOT NULL,
    enabled     BOOLEAN     NOT NULL DEFAULT TRUE,
    priority    INTEGER     NOT NULL DEFAULT 100,
    actions     JSONB       NOT NULL DEFAULT '[]',
    builtin     BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by  TEXT        NOT NULL DEFAULT 'system'
);
CREATE INDEX idx_hooks_trigger ON hooks(trigger, enabled, priority);

CREATE TABLE hook_run_log (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    hook_id         UUID        NOT NULL REFERENCES hooks(id),
    ts_start        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ts_end          TIMESTAMPTZ,
    trigger_type    TEXT        NOT NULL,
    trigger_ref     TEXT,
    status          TEXT        NOT NULL DEFAULT 'running',
    action_results  JSONB       NOT NULL DEFAULT '[]',
    triggered_by    TEXT        NOT NULL
);
CREATE INDEX idx_hook_run_log_hook ON hook_run_log(hook_id, ts_start DESC);
CREATE INDEX idx_hook_run_log_ts   ON hook_run_log(ts_start DESC);
