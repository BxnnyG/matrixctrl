import { describe, it, expect } from "vitest";
import { orderReports, claimForPage } from "./paging";

const r = (id: number, ts: number) => ({ id, received_ts: ts });

describe("orderReports", () => {
  it("puts the newest first", () => {
    expect(orderReports([r(1, 100), r(2, 300), r(3, 200)]).map((x) => x.id))
      .toEqual([2, 3, 1]);
  });

  // The reason this function exists: Synapse orders by received_ts alone, so equal
  // timestamps come back in whatever order the database felt like (etappe 64).
  it("breaks ties by id so the order is the same on every reload", () => {
    const burst = [r(7, 500), r(9, 500), r(8, 500)];
    expect(orderReports(burst).map((x) => x.id)).toEqual([9, 8, 7]);
    // Same input in a different order must produce the same output.
    expect(orderReports([r(8, 500), r(7, 500), r(9, 500)]).map((x) => x.id))
      .toEqual([9, 8, 7]);
  });

  it("does not mutate its input", () => {
    const rows = [r(1, 100), r(2, 300)];
    orderReports(rows);
    expect(rows.map((x) => x.id)).toEqual([1, 2]);
  });
});

describe("claimForPage", () => {
  it("drops a row that already appeared on an earlier page", () => {
    const seen = new Map<number, number>();
    const page0 = claimForPage(orderReports([r(1, 300), r(2, 200)]), 0, seen);
    expect(page0.map((x) => x.id)).toEqual([1, 2]);

    // The server returns row 2 again on the next page — the duplicate this fixes.
    const page1 = claimForPage(orderReports([r(2, 200), r(3, 100)]), 1, seen);
    expect(page1.map((x) => x.id)).toEqual([3]);
  });

  it("still shows a page when you go back to it", () => {
    const seen = new Map<number, number>();
    claimForPage(orderReports([r(1, 300), r(2, 200)]), 0, seen);
    claimForPage(orderReports([r(3, 100)]), 1, seen);
    // Returning to page 0 must not blank it, which a plain seen-set would do.
    const back = claimForPage(orderReports([r(1, 300), r(2, 200)]), 0, seen);
    expect(back.map((x) => x.id)).toEqual([1, 2]);
  });

  it("keeps the two queues apart when given separate maps", () => {
    // Synapse numbers event and user reports independently, so id 5 in one is
    // unrelated to id 5 in the other (E48).
    const events = new Map<number, number>();
    const users = new Map<number, number>();
    expect(claimForPage([r(5, 100)], 0, events).map((x) => x.id)).toEqual([5]);
    expect(claimForPage([r(5, 100)], 0, users).map((x) => x.id)).toEqual([5]);
  });

  it("leaves a single-page queue untouched", () => {
    const seen = new Map<number, number>();
    const rows = orderReports([r(1, 300), r(2, 200), r(3, 100)]);
    expect(claimForPage(rows, 0, seen).map((x) => x.id)).toEqual([1, 2, 3]);
  });
});
