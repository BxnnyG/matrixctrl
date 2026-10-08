import { describe, expect, it, vi, afterEach, beforeAll } from "vitest";
import { render, screen, cleanup, fireEvent } from "@testing-library/react";

const navigate = vi.fn();
vi.mock("@tanstack/react-router", () => ({
  createFileRoute: () => () => ({}),
  useNavigate: () => navigate,
}));
vi.mock("@/lib/api", () => ({ api: { get: vi.fn() }, ApiError: class extends Error {} }));

import { WorkersView, type WorkersResponse } from "./workers";

beforeAll(() => {
  // jsdom has no matchMedia; the layout hook asks it.
  window.matchMedia = ((q: string) => ({ matches: false, media: q, addEventListener() {}, removeEventListener() {} })) as unknown as typeof window.matchMedia;
});
afterEach(() => { cleanup(); navigate.mockReset(); });

function response(over: Partial<WorkersResponse>): WorkersResponse {
  return {
    verdict: { level: "fine", title: "Kein Worker nötig", detail: "Synapse nutzt in der Spitze 1 % eines Kerns." },
    range: "24h", processes: [{ pod: "ess-synapse-main-0", worker: "main", ready: true, restarts: 0, running: true, enabled: true,
      stats: { samples: 600, avg_cores: 0.004, p95_cores: 0.01, max_cores: 0.03, now_cores: 0.005, memory: 3.6e8, from: "", to: "" } }],
    areas: [{ id: "federation-in", label: "Föderation empfangen", worker: "federation-inbound", cores: 0.0006 }],
    main_cores: 0.004, areas_source: "range", enabled: {}, history: {}, interval_secs: 60,
    worker_labels: { main: "Hauptprozess", synchrotron: "Sync — Apps holen Neuigkeiten ab", "federation-inbound": "Föderation empfangen" },
    ...over,
  };
}

describe("WorkersView", () => {
  // The answer for nearly every self-hosted server. It has to be said, with the number —
  // and it must not come with a button that switches something on.
  it("says no worker is needed, without a call to switch one on", () => {
    render(<WorkersView data={response({})} />);
    expect(screen.getByText("Kein Worker nötig")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /einschalten/ })).toBeNull();
  });

  it("leads a recommendation straight to its switch", () => {
    render(<WorkersView data={response({ verdict: { level: "recommend", title: "Ein Worker würde helfen", detail: "…", worker: "synchrotron" } })} />);
    fireEvent.click(screen.getByRole("button", { name: /Sync — Apps holen Neuigkeiten ab.*einschalten/ }));
    expect(navigate).toHaveBeenCalledWith({ to: "/config", search: { mode: "tasks", card: "workers" } });
  });

  // Synapse labels only part of its CPU time. Showing the labelled part as if it were the
  // whole would make a 15 % slice look like the main load.
  it("shows the part Synapse does not attribute", () => {
    render(<WorkersView data={response({})} />);
    expect(screen.getByText(/Nicht zuzuordnen/)).toBeTruthy();
    expect(screen.getByText("85 %")).toBeTruthy();
    expect(screen.getByText("15 %")).toBeTruthy();
  });

  it("says when a switched-on worker does not run", () => {
    render(<WorkersView data={response({ processes: [...response({}).processes,
      { pod: "", worker: "synchrotron", ready: false, restarts: 0, running: false, enabled: true }] })} />);
    expect(screen.getByText(/eingeschaltet, läuft aber nicht/)).toBeTruthy();
  });
});
