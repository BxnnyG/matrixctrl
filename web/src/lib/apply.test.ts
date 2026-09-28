import { describe, expect, it } from "vitest";
import { refusal, restartSummary, serviceName, type ConfigVerdict } from "./apply";

const base: ConfigVerdict = { rendered: true, restarts: [], findings: [], blocking: false, stuck: false };

describe("restartSummary", () => {
  it("names the services that restart, without the release prefix", () => {
    const v = { ...base, restarts: [{ kind: "StatefulSet", name: "ess-synapse-main" }, { kind: "StatefulSet", name: "ess-postgres" }] };
    expect(restartSummary(v)).toBe("startet neu: synapse-main, postgres");
  });
  it("tells a new service apart from a restarted one", () => {
    const v = { ...base, restarts: [{ kind: "Deployment", name: "ess-hookshot", new: true }] };
    expect(restartSummary(v)).toBe("startet erstmals: hookshot");
  });
  it("says so when nothing restarts", () => {
    expect(restartSummary(base)).toBe("kein Dienst startet neu");
  });
  // Unknown is not "nothing restarts" (§4.55).
  it("does not pass an unchecked config off as harmless", () => {
    expect(restartSummary({ ...base, rendered: false })).toBe("Auswirkung nicht prüfbar");
  });
});

describe("refusal", () => {
  it("refuses a config that does not fit", () => {
    expect(refusal({ ...base, blocking: true })).toMatch(/passt nicht/);
  });
  it("refuses while the release is stuck, and says in which state", () => {
    expect(refusal({ ...base, stuck: true, release_status: "pending-upgrade" })).toMatch(/pending-upgrade/);
  });
  it("lets a clean verdict through", () => {
    expect(refusal(base)).toBeNull();
  });
});

describe("serviceName", () => {
  it("keeps names of other releases intact", () => {
    expect(serviceName("essential-thing")).toBe("essential-thing");
  });
});
