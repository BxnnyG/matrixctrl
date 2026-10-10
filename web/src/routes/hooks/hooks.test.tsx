import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, cleanup } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const get = vi.fn();
vi.mock("@/lib/api", () => ({ api: { get: (p: string) => get(p), post: vi.fn() }, ApiError: class extends Error {} }));
vi.mock("@tanstack/react-router", () => ({
  createFileRoute: () => (opts: { component: unknown }) => ({ options: opts }),
  useNavigate: () => vi.fn(),
}));

import { Route } from "./index";

afterEach(() => { cleanup(); get.mockReset(); });

const hook = (over: object) => ({ id: "1", name: "ESS RTC: SFU Host Network", trigger: "post-upgrade", enabled: true,
  priority: 10, builtin: true, actions: [{ type: "kubectl_patch" }], ...over });

function mount() {
  const List = (Route as unknown as { options: { component: () => React.ReactElement } }).options.component;
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}><List /></QueryClientProvider>);
}

describe("Hooks list", () => {
  // The production server of 2026-10-10: both built-ins enabled, both covered by the
  // chart. "2 Hooks laufen beim nächsten Deployment" was true and meant nothing.
  it("says there is nothing to do when every enabled hook is covered", async () => {
    get.mockResolvedValue([
      hook({ covered: "Nicht mehr nötig: Die ESS-Konfiguration setzt das bereits selbst — ess-matrix-rtc-sfu (dnsPolicy, hostNetwork)." }),
      hook({ id: "2", name: "ESS RTC: Service ExternalTrafficPolicy", covered: "Nicht mehr nötig: …" }),
    ]);
    mount();
    await screen.findByText("ESS RTC: Service ExternalTrafficPolicy");
    expect(screen.getByText(/gibt es für Hooks nichts zu tun/)).toBeTruthy();
    expect(screen.getAllByText("Nicht mehr nötig")).toHaveLength(2);
  });

  it("counts a hook that still has work", async () => {
    get.mockResolvedValue([hook({}), hook({ id: "2", covered: "Nicht mehr nötig: …" })]);
    mount();
    await screen.findAllByText("ESS RTC: SFU Host Network");
    expect(screen.getByText(/1 Hook hat beim nächsten Update etwas zu tun/)).toBeTruthy();
  });

  // An empty list is not "nothing to do" — it is "not loaded yet".
  it("claims nothing before the list is in", async () => {
    get.mockReturnValue(new Promise(() => {}));
    mount();
    expect(await screen.findByText("Lade…")).toBeTruthy();
    expect(screen.queryByText(/nichts zu tun/)).toBeNull();
  });
});
