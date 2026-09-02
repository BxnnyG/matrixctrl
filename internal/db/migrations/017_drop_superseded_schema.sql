-- Columns and a table that promised a feature nobody built (etappe 63).
--
-- Verified empty on the live database before writing this: config_snapshots held 0
-- rows, and every column dropped below was NULL in every row of its table (7 upgrades,
-- 175 versions). "Nothing in the repository writes it" and "it is empty" are different
-- claims, and only the second one licenses a DROP.
--
-- config_snapshots was a second home for config history. The real one is the git
-- repository on the config volume (E4/E5) — which is better, because it gives diffs and
-- rollback for free. Keeping an unused snapshot table beside it is the second source of
-- truth §4.49 warns about, waiting for someone to wire it up and disagree with git.
ALTER TABLE upgrade_history DROP COLUMN IF EXISTS values_snapshot;
DROP TABLE IF EXISTS config_snapshots;

-- helm_output was meant to hold the whole Helm log. error_message already carries what
-- a failed upgrade said, which is the part anyone reads; the rest is a large blob that
-- would be written once and never queried.
ALTER TABLE upgrade_history DROP COLUMN IF EXISTS helm_output;

-- Release notes come from the published releases (E32) and version dates from the
-- release index (E43), neither of which touches these. This is what P2-4 was actually
-- describing once its "the operator upgrades blind" half stopped being true.
ALTER TABLE ess_versions DROP COLUMN IF EXISTS changelog;
ALTER TABLE ess_versions DROP COLUMN IF EXISTS breaking_changes;
ALTER TABLE ess_versions DROP COLUMN IF EXISTS chart_digest;
ALTER TABLE ess_versions DROP COLUMN IF EXISTS published_at;

-- pre_flight is deliberately NOT dropped. It is the one column here worth filling, and
-- etappe 63 fills it: E55's capacity check currently reports into a WebSocket stream
-- that vanishes when it closes, so "were we warned before applying that?" has no answer
-- afterwards. NULL keeps meaning "not checked" — an upgrade from before the check
-- existed must not be made to look as though it passed one.
