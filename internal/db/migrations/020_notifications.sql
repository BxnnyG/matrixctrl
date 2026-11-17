-- Notifications (etappe 123).
--
-- One row per problem the operator has been (or is about to be) told about. It is here, not
-- in memory, so a restart sends nothing twice: a row with last_sent set is a problem already
-- announced. `resolved_at` marks a problem that is gone while the "resolved" message could
-- not be delivered yet; the row stays until it was.
CREATE TABLE IF NOT EXISTS notify_state (
    key         TEXT        PRIMARY KEY,           -- "dns", "pod:ess-postgres", "update:0.1.130"
    kind        TEXT        NOT NULL,              -- dns | cert | update | pod | backup
    title       TEXT        NOT NULL DEFAULT '',
    detail      TEXT        NOT NULL DEFAULT '',
    once        BOOLEAN     NOT NULL DEFAULT false, -- a notice: no reminder, no "resolved"
    since       TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_sent   TIMESTAMPTZ,
    resolved_at TIMESTAMPTZ
);

-- The latest outcome per delivery channel (webhook | email), for the page.
CREATE TABLE IF NOT EXISTS notify_channels (
    channel       TEXT        PRIMARY KEY,
    last_sent_at  TIMESTAMPTZ,
    last_error    TEXT        NOT NULL DEFAULT '',
    last_error_at TIMESTAMPTZ
);
