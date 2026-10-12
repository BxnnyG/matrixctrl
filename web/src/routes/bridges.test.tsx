import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, cleanup } from "@testing-library/react";

vi.mock("@tanstack/react-router", () => ({ createFileRoute: () => () => ({}), useNavigate: () => vi.fn() }));
vi.mock("@/lib/api", () => ({ api: { get: vi.fn() }, ApiError: class extends Error {} }));

import { BridgesView, type BridgesResponse } from "./bridges";

afterEach(cleanup);

const base: BridgesResponse = {
  hookshot: { enabled: true, pods: [{ name: "ess-hookshot-0", ready: true, restarts: 0, image: "x:7.5.0" }], version: "7.5.0",
    bot: "@hookshot:example.com", webhook_prefix: "https://matrix.example.com/_matrix/hookshot/webhook", webhooks: false, feeds: false },
  appservices: [],
};

describe("BridgesView", () => {
  // ESS turns Hookshot on without webhooks or feeds: a bridge that accepts nothing,
  // with nothing on screen to say so (etappe 119).
  it("says when Hookshot runs but accepts nothing", () => {
    render(<BridgesView data={base} />);
    expect(screen.getByText(/nimmt aber noch nichts an/)).toBeTruthy();
    expect(screen.queryByText("!hookshot webhook name")).toBeNull();
  });

  it("shows how to make a webhook, with the address it lands under", () => {
    render(<BridgesView data={{ ...base, hookshot: { ...base.hookshot, webhooks: true } }} />);
    expect(screen.getByText("!hookshot webhook name")).toBeTruthy();
    expect(screen.getByText("@hookshot:example.com")).toBeTruthy();
    expect(screen.getByText(/_matrix\/hookshot\/webhook/)).toBeTruthy();
  });

  it("is honest that other bridges run elsewhere", () => {
    render(<BridgesView data={base} />);
    expect(screen.getByText(/Betreiben musst du sie selbst/)).toBeTruthy();
  });

  it("marks an unreachable bridge and names why", () => {
    render(<BridgesView data={{ ...base, appservices: [{ source: "Secret wa / registration.yaml", reach: "der Name wa.bridges.svc ist im Cluster unbekannt",
      registration: { id: "whatsapp", url: "http://wa.bridges.svc:29318", sender_localpart: "whatsappbot" } }] }} />);
    expect(screen.getByText("whatsapp")).toBeTruthy();
    expect(screen.getByText(/im Cluster unbekannt/)).toBeTruthy();
  });
});
