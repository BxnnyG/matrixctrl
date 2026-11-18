import { describe, expect, it } from "vitest";
import { NAV } from "./Shell";

describe("NAV", () => {
  it("has the 20 entries in six task groups, without roadmap phases or Compliance", () => {
    expect(NAV.map((g) => g.group)).toEqual(["Übersicht", "Server", "Menschen", "Vernetzung", "Betrieb", "Wartung"]);
    const items = NAV.flatMap((g) => g.items);
    expect(items).toHaveLength(20);
    const text = JSON.stringify(NAV);
    expect(text).not.toMatch(/P3|P4|P5|Compliance|phase/);
    expect(items.find((i) => i.id === "workers")).toMatchObject({ label: "Auslastung", to: "/workers" });
  });
});
