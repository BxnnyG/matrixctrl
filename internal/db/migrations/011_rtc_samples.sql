-- What the SFU reported, over time, because it does not remember (etappe 44).
--
-- Every LiveKit counter on the metrics port is process-lifetime. Read ten hours
-- after the 26.8.0 upgrade, all of them were 0 — not because the server has never
-- carried a call, but because the post-upgrade hook deletes the SFU pod on every
-- ESS upgrade to restore hostNetwork, and on this instance that happens several
-- times a week.
--
-- So a "calls statistics" page built directly on those numbers would silently mean
-- "since the last upgrade" while reading as "ever". History exists only if
-- MatrixCtrl records it. This is the same argument migration 007 makes for DNS
-- observations, applied to a different quantity.
--
-- What is stored is deliberately aggregate: rooms, participants and durations, and
-- no identity of any kind. LiveKit's RoomService API would give room names and
-- participants, and that is a different class of data — "who is in a call with
-- whom" — which none of the four questions this page answers actually needs.
CREATE TABLE IF NOT EXISTS rtc_samples (
    id              BIGSERIAL   PRIMARY KEY,
    observed_at     TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Gauges: the state at the moment of the read.
    rooms_live        INTEGER   NOT NULL,
    participants_live INTEGER   NOT NULL,

    -- Counters, as read. Kept raw rather than pre-differenced so a bug in the
    -- delta logic can be corrected later against the original observations —
    -- a derived-only table would have destroyed the evidence.
    rooms_completed INTEGER     NOT NULL,
    room_seconds    INTEGER     NOT NULL,
    quality_samples INTEGER     NOT NULL,

    -- The deltas since the previous sample, with a counter reset resolved. On a
    -- reset the delta is the new value rather than a negative number: the SFU
    -- started counting again from zero, and whatever it had counted before was
    -- already recorded by earlier samples.
    d_rooms_completed INTEGER   NOT NULL,
    d_room_seconds    INTEGER   NOT NULL,
    d_quality_samples INTEGER   NOT NULL,

    -- True when this sample's counters came back lower than the previous one.
    -- Recorded rather than inferred at read time, because it explains a
    -- discontinuity in every other series on the page and a reader should not have
    -- to re-derive it.
    sfu_restarted   BOOLEAN     NOT NULL DEFAULT false
);

-- Every query on this table is "the recent samples" or "samples in a window".
CREATE INDEX IF NOT EXISTS rtc_samples_observed_at_idx
    ON rtc_samples (observed_at DESC);
