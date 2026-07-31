import { describe, it, expect } from "vitest";
import { explainExit, relTime } from "./ComponentDrawer";

describe("explainExit", () => {
  // The case that motivated the drill-down: ess-matrix-rtc-authorisation-service
  // had 1191 restarts with reason=Error, exit=2, and the UI only showed a count.
  it("explains a crash-looping container", () => {
    const out = explainExit("Error", 2);
    expect(out).toMatch(/Konfigurations- oder Startfehler/);
  });

  it("names the exit code for other Error exits", () => {
    expect(explainExit("Error", 137)).toMatch(/137/);
  });

  it("explains OOMKilled in terms of the memory limit", () => {
    expect(explainExit("OOMKilled")).toMatch(/Memory-Limit/);
  });

  it("flags a clean exit as a problem for a long-running service", () => {
    expect(explainExit("Completed", 0)).toMatch(/nicht durch/);
  });

  it("explains the Unknown state as a node or kubelet restart", () => {
    expect(explainExit("Unknown", 255)).toMatch(/Node-Neustart|kubelet/);
  });

  it("returns null when there is nothing to explain", () => {
    expect(explainExit(undefined, undefined)).toBeNull();
  });

  it("still reports a bare exit code without a reason", () => {
    expect(explainExit(undefined, 1)).toMatch(/Exit-Code 1/);
  });
});

describe("relTime", () => {
  const ago = (ms: number) => new Date(Date.now() - ms).toISOString();

  it("formats seconds, minutes, hours and days", () => {
    expect(relTime(ago(5_000))).toMatch(/^vor \d+s$/);
    expect(relTime(ago(5 * 60_000))).toMatch(/^vor \d+min$/);
    expect(relTime(ago(5 * 3_600_000))).toMatch(/^vor \d+h$/);
    expect(relTime(ago(5 * 86_400_000))).toMatch(/^vor \d+d$/);
  });

  it("falls back to a dash for missing or unparsable input", () => {
    expect(relTime(undefined)).toBe("—");
    expect(relTime("not a date")).toBe("—");
  });
});
