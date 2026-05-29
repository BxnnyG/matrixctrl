-- Key/value store for instance-level secrets and settings that should be
-- generated once and persisted across restarts (e.g. the JWT signing key).
-- This lets MatrixCtrl self-configure on first boot instead of requiring the
-- operator to hand-set secrets via env vars.
CREATE TABLE instance_settings (
    key        TEXT        PRIMARY KEY,
    value      TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
