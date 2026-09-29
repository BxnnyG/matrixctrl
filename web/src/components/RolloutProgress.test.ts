import { describe, expect, it } from "vitest";
import { stepState } from "./RolloutProgress";

// Steps: 0 Konfiguration · 1 Anwenden · 2 Rollout · 3 Hooks · 4 Fertig
describe("stepState", () => {
  // It used to spin on "Fertig" after a finished apply, as if still working.
  it("ticks the last step once the operation has arrived there", () => {
    expect(stepState(4, "done", "success")).toBe("done");
    expect(stepState(4, "done", undefined)).toBe("done");
  });
  it("shows the step an operation failed on as failed, not as still running", () => {
    expect(stepState(1, "apply", "failed")).toBe("failed");
    expect(stepState(0, "apply", "failed")).toBe("done");
    expect(stepState(2, "apply", "failed")).toBe("pending");
  });
  it("still shows a running step as running", () => {
    expect(stepState(2, "rollout", undefined)).toBe("running");
  });
});
