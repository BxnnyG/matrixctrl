-- What the node looked like over time (etappe 59).
--
-- The dashboard's CPU/RAM sparklines were kept in a browser ref: one tab's worth of
-- history, gone on reload, and pre-filled with the current value so a fresh page drew
-- a flat line that read as "stable for an hour" and was one reading repeated forty
-- times (P2-3).
--
-- `allocatable` is recorded next to `used`, and that is the column this table exists
-- for. Usage answers "is it getting worse". Allocatable answers "did the machine change
-- under us" — which is what nobody could answer during the outage of 2026-08-16…18,
-- when the node went from 32 cores to 6 and the only surviving evidence was a
-- screenshot the operator happened to have taken beforehand (§4.53).
CREATE TABLE IF NOT EXISTS node_samples (
    id          BIGSERIAL   PRIMARY KEY,
    observed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- The node's name. Kept per node rather than aggregated: a two-node cluster whose
    -- totals stay flat while one node halves is exactly the case an average hides.
    node        TEXT        NOT NULL,
    cpu_used_millis   BIGINT NOT NULL,
    cpu_alloc_millis  BIGINT NOT NULL,
    mem_used_mi       BIGINT NOT NULL,
    mem_alloc_mi      BIGINT NOT NULL
);

-- Every read is "the recent samples, newest first", either for one node or for all.
CREATE INDEX IF NOT EXISTS node_samples_observed_idx
    ON node_samples (observed_at DESC);
CREATE INDEX IF NOT EXISTS node_samples_node_observed_idx
    ON node_samples (node, observed_at DESC);
