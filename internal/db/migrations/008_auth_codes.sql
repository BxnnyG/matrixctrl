-- One-time codes that replace handing the session JWT to the browser in a URL.
--
-- The OIDC callback used to redirect to /auth/callback?token=<jwt>, and chi's
-- request logger writes the full URL — so the token was written to the application
-- log by the very request that delivered it (P0-5). Now the redirect carries a code
-- in the URL *fragment*, which browsers never send to a server, and the SPA trades
-- it for the token over a POST whose body is not logged.
--
-- Short-lived and single-use, so the copy left in browser history is already spent.
CREATE TABLE IF NOT EXISTS auth_codes (
    code       TEXT PRIMARY KEY,
    user_id    TEXT        NOT NULL,
    ip_addr    TEXT,
    user_agent TEXT,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Consumption is DELETE ... RETURNING, so the lookup is by primary key; this index
-- exists for the sweep of codes nobody ever redeemed.
CREATE INDEX IF NOT EXISTS auth_codes_expires_idx ON auth_codes (expires_at);

-- Failed login attempts, counted per key (IP or username).
--
-- Deliberately in Postgres rather than in memory: the pod restarts, and an
-- in-memory counter would make "restart it and try again" the attack.
CREATE TABLE IF NOT EXISTS login_attempts (
    key          TEXT PRIMARY KEY,
    failures     INT         NOT NULL DEFAULT 0,
    first_failed TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_failed  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
