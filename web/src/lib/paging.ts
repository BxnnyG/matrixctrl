/** Stable ordering and duplicate removal over a server order that has neither.
 *
 *  Synapse orders both report queues by `received_ts` alone, so rows sharing a
 *  timestamp have no defined order between two queries. With offset paging a row can
 *  then appear on two consecutive pages while another is never returned — and a burst
 *  of reports about one incident is exactly what produces equal timestamps (etappe 64).
 *
 *  Extracted from the component so it can be tested without rendering one. */

export interface Pageable {
  id: number;
  received_ts: number;
}

/** Newest first, with the id breaking ties.
 *
 *  The tiebreak is the whole point: without it two reports filed in the same second
 *  swap places between reloads. Descending id, because a higher id is the later row. */
export function orderReports<T extends Pageable>(rows: T[]): T[] {
  return [...rows].sort((a, b) => b.received_ts - a.received_ts || b.id - a.id);
}

/** Rows belonging to `page`, given where each id was first seen.
 *
 *  `firstSeen` is mutated: an id not yet recorded is claimed by the page currently
 *  rendering it. Keyed on *which* page rather than on "seen at all", because paging
 *  back to an earlier page must show its rows again — a plain seen-set would blank it. */
export function claimForPage<T extends Pageable>(
  sorted: T[],
  page: number,
  firstSeen: Map<number, number>,
): T[] {
  for (const r of sorted) {
    if (!firstSeen.has(r.id)) firstSeen.set(r.id, page);
  }
  return sorted.filter((r) => firstSeen.get(r.id) === page);
}
