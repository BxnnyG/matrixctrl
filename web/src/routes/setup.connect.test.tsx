import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, cleanup, fireEvent } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const post = vi.fn();
vi.mock("@/lib/api", () => ({
  api: { get: vi.fn(), post: (path: string, body: unknown) => post(path, body) },
  ApiError: class extends Error {},
}));

import { ConnectCard } from "./setup";

function mount(node: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{node}</QueryClientProvider>);
}

afterEach(() => { cleanup(); post.mockReset(); });

describe("ConnectCard", () => {
  // Pressing "Verbinden" on an instance that is already connected answers at once, with
  // no run to follow. The card took every answer as a run and showed nothing: five
  // clicks on the new server, five answers, a button that "did nothing" (etappe 116b).
  it("shows an answer that starts no run", async () => {
    post.mockResolvedValue({ already_registered: true, changed: [], message: "Der MatrixCtrl-Client ist bereits vollständig registriert." });
    const onStart = vi.fn();
    const onDone = vi.fn();
    mount(<ConnectCard masHost="mas.example.com" onStart={onStart} onDone={onDone} />);
    fireEvent.click(screen.getByRole("button", { name: "Verbinden" }));
    expect(await screen.findByText(/bereits vollständig registriert/)).toBeTruthy();
    // Nothing is running, so nothing may hold the page.
    expect(onStart).not.toHaveBeenCalled();
    expect(onDone).toHaveBeenCalled();
  });

  it("holds the page when the answer is a run", async () => {
    post.mockResolvedValue({ upgrade_id: "run-1", client_id: "c" });
    const onStart = vi.fn();
    mount(<ConnectCard masHost="mas.example.com" onStart={onStart} onDone={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: "Verbinden" }));
    expect(await screen.findByText("Verbinde…")).toBeTruthy();
    expect(onStart).toHaveBeenCalledTimes(1);
  });
});
