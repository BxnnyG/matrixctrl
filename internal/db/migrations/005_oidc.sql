-- Short-lived OIDC state tokens for the authorization code flow.
-- Each row is created on redirect and consumed (deleted) on callback.
CREATE TABLE oidc_states (
    state      TEXT        PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL
);
