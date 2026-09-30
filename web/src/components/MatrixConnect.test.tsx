import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, cleanup } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const get = vi.fn();
const post = vi.fn();
vi.mock("@/lib/api", () => ({
  api: { get: (p: string) => get(p), post: (p: string, b: unknown) => post(p, b) },
  ApiError: class extends Error {},
}));
vi.mock("@tanstack/react-router", () => ({ useNavigate: () => vi.fn() }));

import { MatrixConnect } from "./MatrixConnect";

function mount(node: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{node}</QueryClientProvider>);
}

function answer(userId: string) {
  get.mockImplementation((p: string) => {
    if (p === "/api/v1/auth/oidc/available") return Promise.resolve({ enabled: true, retrying: false });
    if (p === "/api/v1/auth/me") return Promise.resolve({ user_id: userId });
    return Promise.reject(new Error("unexpected " + p));
  });
  post.mockResolvedValue({ url: "about:blank" });
}

afterEach(() => { cleanup(); get.mockReset(); post.mockReset(); try { sessionStorage.clear(); } catch { /* */ } });

describe("MatrixConnect", () => {
  // The tab that connected the Matrix login was still on the emergency login. Rooms
  // started the authorization, MAS granted it to the Matrix account, and this session
  // asked again: a loop with nothing on screen (etappe 116d).
  it("tells an emergency-login session to sign in with Matrix, and starts nothing", async () => {
    answer("admin");
    mount(<MatrixConnect auto returnTo="/rooms" />);
    expect(await screen.findByText(/Du bist mit dem Notzugang angemeldet/)).toBeTruthy();
    expect(screen.getByRole("button", { name: /über Matrix anmelden/ })).toBeTruthy();
    await new Promise((r) => setTimeout(r, 20));
    expect(post).not.toHaveBeenCalled();
  });

  it("connects a Matrix session as before", async () => {
    answer("@op:example.com");
    mount(<MatrixConnect returnTo="/rooms" />);
    expect(await screen.findByRole("button", { name: "Verbinden" })).toBeTruthy();
    expect(screen.queryByText(/Notzugang angemeldet/)).toBeNull();
  });
});
