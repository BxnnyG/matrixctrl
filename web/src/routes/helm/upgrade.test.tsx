import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, cleanup, fireEvent, waitFor, act } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const get = vi.fn();
const post = vi.fn();
vi.mock("@/lib/api", () => ({
  api: { get: (p: string) => get(p), post: (...a: unknown[]) => post(...a) },
  ApiError: class extends Error { status = 0; },
}));
vi.mock("@tanstack/react-router", () => ({
  createFileRoute: () => () => ({ useSearch: () => ({ version: "matrix-stack-26.9.0" }) }),
  useNavigate: () => vi.fn(),
}));
// The stream is driven by hand: the test decides how the upgrade ends.
let streamOpts: { onDone: (s: string, i?: { backupFailed: boolean }) => void } | null = null;
vi.mock("@/lib/ws", () => ({ useUpgradeStream: (_id: string | null, o: typeof streamOpts) => { streamOpts = o; } }));

import { UpgradeWizard } from "./upgrade";

function mount() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}><UpgradeWizard /></QueryClientProvider>);
}

function setup() {
  get.mockImplementation((p: string) => {
    if (p.startsWith("/api/v1/helm/upgrade-check")) return Promise.resolve({ pinned: [] });
    if (p === "/api/v1/helm/versions") return Promise.resolve([{ version: "matrix-stack-26.9.0" }]);
    if (p === "/api/v1/helm/releases/ess") return Promise.resolve({ chart_version: "matrix-stack-26.8.0", revision: 3 });
    if (p === "/api/v1/hooks") return Promise.resolve([]);
    return Promise.resolve({ available: false });
  });
  post.mockResolvedValue({ upgrade_id: "u1" });
}

afterEach(() => { cleanup(); get.mockReset(); post.mockReset(); streamOpts = null; });

async function start() {
  mount();
  const go = await screen.findByRole("button", { name: "Upgrade starten" });
  await waitFor(() => expect((go as HTMLButtonElement).disabled).toBe(false));
  fireEvent.click(go);
  await waitFor(() => expect(post).toHaveBeenCalledTimes(1));
}

describe("Upgrade: backup before the upgrade", () => {
  it("offers 'Ohne Backup fortfahren' when the backup failed, and repeats with skip_backup", async () => {
    setup();
    await start();
    expect(post.mock.calls[0][1]).toMatchObject({ to_version: "matrix-stack-26.9.0", skip_backup: false });
    await waitFor(() => expect(streamOpts).not.toBeNull());
    act(() => streamOpts!.onDone("failed", { backupFailed: true }));
    fireEvent.click(await screen.findByRole("button", { name: "Ohne Backup fortfahren" }));
    await waitFor(() => expect(post).toHaveBeenCalledTimes(2));
    expect(post.mock.calls[1][1]).toMatchObject({ to_version: "matrix-stack-26.9.0", skip_backup: true });
  });

  // The counter-case: a Helm failure ends in the same status and must not offer to skip a backup.
  it("does not offer it when Helm failed", async () => {
    setup();
    await start();
    await waitFor(() => expect(streamOpts).not.toBeNull());
    act(() => streamOpts!.onDone("failed", { backupFailed: false }));
    expect(await screen.findByText("Upgrade fehlgeschlagen.")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Ohne Backup fortfahren" })).toBeNull();
  });
});
