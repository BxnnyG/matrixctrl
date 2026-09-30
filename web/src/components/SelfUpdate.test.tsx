import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, cleanup, fireEvent } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const get = vi.fn();
vi.mock("@/lib/api", () => ({
  api: { get: (path: string) => get(path), post: vi.fn() },
  ApiError: class extends Error {},
}));

import { MatrixCtrlUpdateCard } from "./SelfUpdate";

function mount() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}><MatrixCtrlUpdateCard /></QueryClientProvider>);
}

const upToDate = { version: "0.1.116", commit: "x", may_write: true, update: { current: "0.1.116", latest: "0.1.116", available: false, checked_at: new Date().toISOString() } };
const behind = { ...upToDate, version: "0.1.115", update: { current: "0.1.115", latest: "0.1.116", available: true, checked_at: new Date().toISOString() } };

function answer(version: unknown, ready: boolean, refreshed?: unknown) {
  get.mockImplementation((path: string) => {
    if (path === "/api/v1/version?refresh=1") return Promise.resolve(refreshed ?? version);
    if (path === "/api/v1/version") return Promise.resolve(version);
    if (path === "/api/v1/self-update") return Promise.resolve({ current: "0.1.115", ready, missing: ready ? [] : ["jobs create"] });
    return Promise.reject(new Error("unexpected " + path));
  });
}

afterEach(() => { cleanup(); get.mockReset(); });

// The one-click update existed and could not be found: it sat behind a pill that only
// appears once an update is known, and nothing said the feature was there (etappe 116c).
describe("MatrixCtrlUpdateCard", () => {
  it("offers the update in one click when one is out and the rights are there", async () => {
    answer(behind, true);
    mount();
    expect(await screen.findByRole("button", { name: /Auf 0\.1\.116 aktualisieren/ })).toBeTruthy();
    expect(screen.queryByText(/install\.sh/)).toBeNull();
  });

  it("gives the command, not a button that would be refused, when the rights are missing", async () => {
    answer(behind, false);
    mount();
    expect(await screen.findByText(/install\.sh/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: /aktualisieren/ })).toBeNull();
  });

  it("says that updates go from here even when there is none", async () => {
    answer(upToDate, true);
    mount();
    expect(await screen.findByText(/installierst du sie hier mit einem Klick/)).toBeTruthy();
  });

  it("checks now when asked, and shows what it found", async () => {
    answer({ ...behind, version: "0.1.115", update: { ...upToDate.update, current: "0.1.115", latest: "0.1.115" } }, true, behind);
    mount();
    fireEvent.click(await screen.findByRole("button", { name: /Jetzt prüfen/ }));
    expect(await screen.findByRole("button", { name: /Auf 0\.1\.116 aktualisieren/ })).toBeTruthy();
    expect(get).toHaveBeenCalledWith("/api/v1/version?refresh=1");
  });
});
