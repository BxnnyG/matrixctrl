import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, cleanup, fireEvent } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const get = vi.fn();
vi.mock("@/lib/api", () => ({
  api: { get: (p: string) => get(p), post: vi.fn() },
  ApiError: class extends Error { status = 0; },
}));
vi.mock("@tanstack/react-router", () => ({
  createFileRoute: () => () => ({ useSearch: () => ({}) }),
  Link: () => null,
  useNavigate: () => vi.fn(),
}));

import { ReachCard, DestinationsSection } from "./federation";

function mount(node: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{node}</QueryClientProvider>);
}

afterEach(() => { cleanup(); get.mockReset(); });

describe("ReachCard", () => {
  // The symptom of every broken step is the same — invitations from elsewhere never
  // arrive. The verdict has to say "not reachable", and the step that broke has to say
  // why, in the card, not in a log (etappe 117).
  it("says when others cannot reach this server, and which step broke", async () => {
    get.mockResolvedValue({
      server_name: "example.com", reachable: false, target: "example.com:8448", target_from: "default",
      steps: [
        { key: "delegation", title: "Wegweiser", level: "warn", detail: "Gibt es nicht (404)." },
        { key: "key", title: "Verbindung und Schlüssel", level: "err", detail: "Keine Verbindung zu example.com:8448: Verbindung abgelehnt." },
      ],
    });
    mount(<ReachCard />);
    expect(await screen.findByText(/erreichen/)).toBeTruthy();
    expect(screen.getByText(/nicht$/)).toBeTruthy();
    expect(screen.getByText(/Verbindung abgelehnt/)).toBeTruthy();
    // The outside view is offered, and it is the operator's click.
    expect(screen.getByRole("link", { name: /Federation Tester/ }).getAttribute("href")).toContain("example.com");
  });
});

describe("DestinationsSection", () => {
  it("narrows to the failing servers on request", async () => {
    get.mockImplementation((p: string) => {
      if (p === "/api/v1/rooms/state") return Promise.resolve({ connected: true });
      if (p === "/api/v1/federation/destinations") return Promise.resolve({
        total: 2, failing: 1, cut: false,
        destinations: [
          { destination: "broken.example", failing: true, failure_ts: Date.now() - 3_600_000, retry_last_ts: null, retry_interval: 0 },
          { destination: "fine.example", failing: false, failure_ts: null, retry_last_ts: null, retry_interval: 0 },
        ],
      });
      return Promise.reject(new Error("unexpected " + p));
    });
    mount(<DestinationsSection />);
    expect(await screen.findByText("fine.example")).toBeTruthy();
    expect(screen.getByText(/scheitert seit/)).toBeTruthy();
    expect(screen.getByRole("button", { name: /Jetzt neu versuchen/ })).toBeTruthy();

    fireEvent.click(screen.getByLabelText(/nur Probleme/));
    expect(screen.queryByText("fine.example")).toBeNull();
    expect(screen.getByText("broken.example")).toBeTruthy();
  });
});
