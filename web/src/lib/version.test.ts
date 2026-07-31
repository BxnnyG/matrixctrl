import { describe, it, expect } from "vitest";
import { cmpVersion, essVersion } from "./version";

describe("cmpVersion", () => {
  // The regression: a plain string sort ranked 26.5.1 above 26.10.0, so the
  // upgrade picker hid every newer release.
  it("orders by number, not lexically", () => {
    expect(cmpVersion("26.10.0", "26.5.1")).toBeGreaterThan(0);
    expect(cmpVersion("26.5.1", "26.10.0")).toBeLessThan(0);
    expect(cmpVersion("0.10.0", "0.9.0")).toBeGreaterThan(0);
  });

  it("compares patch and minor levels", () => {
    expect(cmpVersion("26.7.2", "26.7.1")).toBeGreaterThan(0);
    expect(cmpVersion("26.7.0", "26.6.2")).toBeGreaterThan(0);
  });

  it("treats equal versions as equal, with or without a v prefix", () => {
    expect(cmpVersion("26.5.1", "26.5.1")).toBe(0);
    expect(cmpVersion("v26.5.1", "26.5.1")).toBe(0);
  });

  it("ranks a final release above its own prereleases", () => {
    expect(cmpVersion("1.0.0", "1.0.0-rc.1")).toBeGreaterThan(0);
    expect(cmpVersion("1.0.0-rc.1", "1.0.0")).toBeLessThan(0);
  });

  it("agrees with the backend on the deployed-vs-available case", () => {
    // 26.5.1 was deployed while 26.7.2 was the newest release.
    const available = ["26.7.2", "26.7.1", "26.6.0", "26.5.1", "26.4.0"];
    const newer = available.filter((v) => cmpVersion(v, "26.5.1") > 0);
    expect(newer).toEqual(["26.7.2", "26.7.1", "26.6.0"]);
  });
});

describe("essVersion", () => {
  it("strips the chart-name prefix", () => {
    expect(essVersion("matrix-stack-26.5.1")).toBe("26.5.1");
  });

  it("leaves a bare version untouched", () => {
    expect(essVersion("26.5.1")).toBe("26.5.1");
  });
});
