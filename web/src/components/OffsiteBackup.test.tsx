import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, cleanup, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const get = vi.fn();
const post = vi.fn();
const put = vi.fn();
vi.mock("@/lib/api", () => {
  class ApiError extends Error {
    status: number; data?: Record<string, unknown>;
    constructor(m: string, s: number, d?: Record<string, unknown>) { super(m); this.status = s; this.data = d; }
  }
  return { api: { get: (p: string) => get(p), post: (...a: unknown[]) => post(...a), put: (...a: unknown[]) => put(...a) }, ApiError };
});

import { ApiError } from "@/lib/api";
import { KeyPairBlock, OffsiteObjects } from "./OffsiteBackup";

function mount(node: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{node}</QueryClientProvider>);
}

afterEach(() => { cleanup(); get.mockReset(); post.mockReset(); put.mockReset(); localStorage.clear(); });

const KEY = "AGE-SECRET-KEY-1TESTTESTTEST";

describe("KeyPairBlock", () => {
  // The private key exists in exactly one place: this view. It must be readable, carry
  // the warning, and be gone after hiding — and nowhere in localStorage while it is up.
  it("shows the private key once, warns, and drops it on hiding", async () => {
    post.mockResolvedValue({ public_key: "age1pub", private_key: KEY });
    mount(<KeyPairBlock publicKey="" onChanged={vi.fn()} />);
    fireEvent.click(screen.getByRole("button", { name: /Schlüsselpaar erzeugen/ }));
    expect(await screen.findByText(KEY)).toBeTruthy();
    expect(screen.getByText(/ohne ihn ist kein Backup lesbar/)).toBeTruthy();
    expect(JSON.stringify({ ...localStorage })).not.toContain("AGE-SECRET-KEY");
    // Hiding is only offered after the acknowledgement.
    expect(screen.queryByRole("button", { name: "Ausblenden" })).toBeNull();
    fireEvent.click(screen.getByLabelText(/gesichert/));
    fireEvent.click(screen.getByRole("button", { name: "Ausblenden" }));
    expect(screen.queryByText(KEY)).toBeNull();
  });

  it("asks before replacing an existing key (409), then replaces", async () => {
    post.mockRejectedValueOnce(new ApiError("key_exists", 409))
      .mockResolvedValueOnce({ public_key: "age1new", private_key: KEY });
    const changed = vi.fn();
    mount(<KeyPairBlock publicKey="age1old" onChanged={changed} />);
    fireEvent.click(screen.getByRole("button", { name: /Schlüsselpaar erzeugen/ }));
    expect(await screen.findByText("Schlüssel ersetzen?")).toBeTruthy();
    // Nothing is replaced until the operator says so.
    expect(post).toHaveBeenCalledTimes(1);
    expect(post.mock.calls[0][1]).toEqual({});
    fireEvent.click(screen.getByRole("button", { name: /^Ersetzen/ }));
    expect(await screen.findByText(KEY)).toBeTruthy();
    expect(post.mock.calls[1][1]).toEqual({ replace: true });
    expect(changed).toHaveBeenCalled();
  });

  it("refuses a private key pasted as the public one and says why", async () => {
    put.mockRejectedValue(new ApiError("das ist ein privater Schlüssel", 422));
    mount(<KeyPairBlock publicKey="" onChanged={vi.fn()} />);
    const field = screen.getByPlaceholderText("age1…") as HTMLInputElement;
    expect(field.getAttribute("autocomplete")).toBe("off");
    fireEvent.change(field, { target: { value: KEY } });
    fireEvent.click(screen.getByRole("button", { name: "Übernehmen" }));
    expect(await screen.findByText(/privater Schlüssel/)).toBeTruthy();
  });
});

describe("OffsiteObjects", () => {
  const objects = { objects: [{ key: "p/a.tar.gz.age", name: "a.tar.gz.age", time: "2026-10-19T03:30:00Z", occasion: "zeitplan", size: 5 * 1024 * 1024 }] };

  it("restores only with a key, sends it, and shows a refused key", async () => {
    get.mockResolvedValue(objects);
    const onRestore = vi.fn().mockRejectedValue(new ApiError("Falscher Schlüssel", 422));
    mount(<OffsiteObjects onRestore={onRestore} />);
    fireEvent.click(await screen.findByRole("button", { name: /Wiederherstellen/ }));
    const confirm = screen.getAllByRole("button", { name: /Wiederherstellen/ }).pop()!;
    expect((confirm as HTMLButtonElement).disabled).toBe(true);
    const field = screen.getByPlaceholderText("AGE-SECRET-KEY-1…") as HTMLInputElement;
    expect(field.getAttribute("autocomplete")).toBe("off");
    expect(field.type).toBe("password");
    fireEvent.change(field, { target: { value: KEY } });
    fireEvent.click(screen.getAllByRole("button", { name: /Wiederherstellen/ }).pop()!);
    await waitFor(() => expect(onRestore).toHaveBeenCalledWith("a.tar.gz.age", KEY));
    expect(await screen.findByText("Falscher Schlüssel")).toBeTruthy();
  });
});
