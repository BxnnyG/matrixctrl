import { describe, expect, it, vi, afterEach, beforeAll } from "vitest";
import { render, screen, cleanup, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

vi.mock("@tanstack/react-router", () => ({
  createFileRoute: () => () => ({}),
  Link: ({ children }: { children: React.ReactNode }) => <a href="/config">{children}</a>,
}));
const get = vi.fn();
const post = vi.fn();
const { FakeApiError } = vi.hoisted(() => ({ FakeApiError: class extends Error { status: number; constructor(m: string, s: number) { super(m); this.status = s; } } }));
vi.mock("@/lib/api", () => ({
  api: { get: (p: string) => get(p), post: (p: string, b: unknown) => post(p, b), put: vi.fn() },
  ApiError: FakeApiError,
}));

import { InvitesPage, type Invite } from "./invites";

beforeAll(() => {
  window.matchMedia = ((q: string) => ({ matches: false, media: q, addEventListener() {}, removeEventListener() {} })) as unknown as typeof window.matchMedia;
});
afterEach(() => { cleanup(); get.mockReset(); post.mockReset(); });

const inv = (over: Partial<Invite> = {}): Invite => ({
  id: "I1", token: "willkommen", valid: true, state: "valid", usage_limit: 5, times_used: 2,
  created_at: "2026-01-01T00:00:00Z", last_used_at: null, expires_at: null, revoked_at: null, ...over,
});
const tasks = (password: boolean, needCode: boolean) => ({
  values: { "mas.password_registration": { value: password }, "mas.registration_token_required": { value: needCode } },
});

function show(o: { invites?: Invite[]; password?: boolean; needCode?: boolean; mayWrite?: boolean; registerUrl?: string; listError?: Error } = {}) {
  get.mockImplementation((p: string) => {
    if (p === "/api/v1/invites") return o.listError ? Promise.reject(o.listError) : Promise.resolve({ configured: true, invites: o.invites ?? [inv()], register_url: o.registerUrl });
    if (p === "/api/v1/config/tasks") return Promise.resolve(tasks(o.password ?? true, o.needCode ?? true));
    return Promise.resolve({ may_write: o.mayWrite ?? true });
  });
  render(<QueryClientProvider client={new QueryClient()}><InvitesPage /></QueryClientProvider>);
}

describe("InvitesPage", () => {
  it("lists a code with its state and use count", async () => {
    show({ invites: [inv(), inv({ id: "I2", token: "alt", state: "revoked", usage_limit: null, times_used: 0 })] });
    expect(await screen.findByText("willkommen")).toBeTruthy();
    expect(screen.getByText(/2 von 5 benutzt/)).toBeTruthy();
    expect(screen.getByText("gültig")).toBeTruthy();
    expect(screen.getByText("widerrufen")).toBeTruthy();
    expect(screen.getByText(/0 von ∞ benutzt/)).toBeTruthy();
  });

  it("sends the new code with days turned into an RFC3339 date", async () => {
    post.mockResolvedValue(inv({ token: "neu" }));
    show({ invites: [] });
    fireEvent.change(await screen.findByLabelText("Code"), { target: { value: "neu" } });
    fireEvent.change(screen.getByLabelText("Nutzungen"), { target: { value: "3" } });
    fireEvent.change(screen.getByLabelText("Tage"), { target: { value: "7" } });
    const before = Date.now();
    fireEvent.click(screen.getByRole("button", { name: /Code anlegen/ }));
    await waitFor(() => expect(post).toHaveBeenCalled());
    const [path, body] = post.mock.calls[0] as [string, { token: string; usage_limit: number; expires_at: string }];
    expect(path).toBe("/api/v1/invites");
    expect(body.token).toBe("neu");
    expect(body.usage_limit).toBe(3);
    expect(body.expires_at).toMatch(/^\d{4}-\d\d-\d\dT[\d:.]+Z$/);
    expect(Math.abs(Date.parse(body.expires_at) - (before + 7 * 86_400_000))).toBeLessThan(60_000);
    expect(await screen.findByText(/Code angelegt/)).toBeTruthy();
  });

  it("shows a calm state when MAS is not connected", async () => {
    show({ listError: new FakeApiError("Einladungen brauchen MAS-Zugang.", 503) });
    expect(await screen.findByText("MAS nicht verbunden")).toBeTruthy();
    expect(screen.queryByText(/Einladungen brauchen MAS-Zugang/)).toBeNull();
  });

  it("warns when a code is required but password registration is off", async () => {
    show({ password: false, needCode: true });
    expect(await screen.findByRole("alert")).toBeTruthy();
  });

  it("does not warn when password registration is on", async () => {
    show({ password: true, needCode: true });
    await screen.findByText(/nur mit Einladungs-Code möglich/);
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("offers no write buttons without write permission, and no code", async () => {
    show({ mayWrite: false, invites: [inv({ token: "", token_hidden: true })] });
    await screen.findByText(/nur für Konten mit Schreibrecht/);
    expect(screen.queryByRole("button", { name: /Code anlegen|Widerrufen|Ändern/ })).toBeNull();
  });

  it("offers write buttons with write permission", async () => {
    show({});
    expect(await screen.findByRole("button", { name: /Widerrufen/ })).toBeTruthy();
  });

  it("shows the registration address next to the code, and nothing when it is unknown", async () => {
    show({ registerUrl: "https://mas.example.org/register" });
    expect(await screen.findByText("https://mas.example.org/register")).toBeTruthy();
    expect(screen.getByText(/Code im Formular eingeben/)).toBeTruthy();
    cleanup();
    show({});
    await screen.findByText("willkommen");
    expect(screen.queryByText(/Code im Formular eingeben/)).toBeNull();
  });
});
