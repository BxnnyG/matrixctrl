CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE sessions (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL,
    last_seen   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ip_addr     INET,
    user_agent  TEXT,
    revoked     BOOLEAN     NOT NULL DEFAULT FALSE
);
CREATE INDEX idx_sessions_user_id ON sessions(user_id);

CREATE TABLE audit_log (
    id          BIGSERIAL   PRIMARY KEY,
    ts          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    user_id     TEXT        NOT NULL,
    action      TEXT        NOT NULL,
    resource    TEXT,
    detail      JSONB,
    result      TEXT        NOT NULL
);
CREATE INDEX idx_audit_log_ts   ON audit_log(ts DESC);
CREATE INDEX idx_audit_log_user ON audit_log(user_id, ts DESC);

CREATE TABLE config_snapshots (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    ts          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    user_id     TEXT        NOT NULL,
    git_commit  TEXT        NOT NULL,
    ess_version TEXT        NOT NULL,
    slices      JSONB       NOT NULL,
    validation  JSONB,
    description TEXT
);
CREATE INDEX idx_config_snapshots_ts ON config_snapshots(ts DESC);
