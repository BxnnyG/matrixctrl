-- Delete twelve days of recorded coin flips, and stop them recurring (etappe 45).
--
-- rtc_address_history is meant to hold one row per *change* of the announced host's
-- address. It held 1778, split 889/889 between two Cloudflare addresses: the host is
-- proxied, so DNS returns two A records, the resolver rotates their order, and the
-- writer recorded addrs[0]. Every other poll looked like a change.
--
-- The cost was not the rows. AssessFreshness compares the newest observation against
-- the SFU pod's start time, so a "change" every few minutes meant the calls page
-- reported "Die SFU kündigt eine veraltete Adresse an" continuously from 2026-08-04
-- — a warning telling the operator to replace the SFU pod, which drops any call in
-- progress.
--
-- The writer now records the sorted set (rtc.AddressKey), so a rotation is no longer
-- a change. These rows cannot be repaired into that shape — each one holds a single
-- member of a set whose other members were never written down — so they are deleted
-- rather than migrated. Deleting them is also what restores the verdict: with the
-- noise gone the newest observation is the one written under the new rule.
DELETE FROM rtc_address_history
WHERE address NOT LIKE '%,%'
  AND host IN (
      -- Only hosts that show the flapping signature: at least three recorded runs
      -- over at most two distinct addresses. A host with a genuinely changing
      -- address has many distinct values, and its history is real.
      SELECT host FROM rtc_address_history
      GROUP BY host
      HAVING count(*) >= 3 AND count(DISTINCT address) <= 2
  );
