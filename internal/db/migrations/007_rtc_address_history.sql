-- Observations of what the announced RTC host resolves to, over time.
--
-- The SFU discovers its public address by STUN once at startup and offers it in
-- every ICE candidate for the life of the process. A consumer line is re-addressed
-- roughly daily, so the announcement goes stale on a schedule and calling breaks
-- until the pod is replaced (P1-9, observed twice, 22 hours apart).
--
-- The announced address itself is not readable from LiveKit — not on its HTTP port,
-- not in its metrics, only in a log line, and a log format is not an API. So
-- staleness is derived from times instead: the announcement equals the public
-- address at the moment the pod started, therefore it is stale exactly when the
-- address changed after that moment.
--
-- One row per (host, address) run. An unchanged answer extends last_seen; a changed
-- answer starts a new row, whose first_seen is the moment of change.
CREATE TABLE rtc_address_history (
    id         BIGSERIAL   PRIMARY KEY,
    host       TEXT        NOT NULL,
    address    TEXT        NOT NULL,
    first_seen TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- The only query this table serves: the newest observation for a host.
CREATE INDEX rtc_address_history_host_seen ON rtc_address_history (host, first_seen DESC);
