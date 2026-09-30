import { describe, expect, it } from "vitest";
import { setupView, shownView, type SetupViewInput } from "./setupView";

const deployedNotConnected: SetupViewInput = { ess_state: "deployed", ess_installed: true, config_sections: 5, oidc_configured: false };
// What the status poll reports while the connect's own upgrade runs.
const midUpgrade: SetupViewInput = { ess_state: "busy", ess_installed: false, config_sections: 5, oidc_configured: false };

describe("setupView", () => {
  it("waits on an operation this page did not start", () => {
    expect(shownView(midUpgrade, null)).toBe("busy");
  });

  // The connect worked and the operator saw "another operation is in progress", then
  // the connect button again: the page's own upgrade had evicted the card watching it
  // (etappe 116b).
  it("keeps the card that started the upgrade while that upgrade runs", () => {
    const held = setupView(deployedNotConnected);
    expect(held).toBe("connect");
    expect(shownView(midUpgrade, held)).toBe("connect");
  });

  it("keeps the deploy wizard through its own install", () => {
    const fresh: SetupViewInput = { ess_state: "absent", ess_installed: false, config_sections: 0, oidc_configured: false };
    const installing: SetupViewInput = { ess_state: "busy", ess_installed: false, config_sections: 0, oidc_configured: false };
    expect(shownView(installing, setupView(fresh))).toBe("install");
  });

  it("follows the status again once nothing is held", () => {
    expect(shownView({ ...deployedNotConnected, oidc_configured: true }, null)).toBe("connected");
  });
});
