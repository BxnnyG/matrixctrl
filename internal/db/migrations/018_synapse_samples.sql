-- How much each Synapse process works, minute by minute (etappe 118).
--
-- Rates, not raw counters: a counter is only meaningful against the previous reading of
-- the same process life, and that pairing is made once, in the sampler, where a restart
-- is visible. Stored counters would make every reader redo it and get restarts wrong.
--
-- `areas` is cores per area of work (sync, federation, media …) as far as Synapse labels
-- its own CPU time — on the production server about 15 % of the total. The remainder is
-- never stored as an area: it is `cores` minus their sum, and saying so is the reader's
-- job, not a column's.
CREATE TABLE IF NOT EXISTS synapse_samples (
    id              BIGSERIAL        PRIMARY KEY,
    observed_at     TIMESTAMPTZ      NOT NULL DEFAULT now(),
    process         TEXT             NOT NULL,  -- the pod
    worker          TEXT             NOT NULL,  -- "main" or the ESS worker type
    cores           DOUBLE PRECISION NOT NULL,  -- 1.0 = one core busy
    mem_bytes       BIGINT           NOT NULL,
    areas           JSONB            NOT NULL DEFAULT '{}',
    fed_pending_destinations DOUBLE PRECISION NOT NULL DEFAULT 0,
    inbound_staging          DOUBLE PRECISION NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS synapse_samples_observed_idx
    ON synapse_samples (observed_at DESC);
