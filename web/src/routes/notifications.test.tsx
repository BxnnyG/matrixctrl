import { describe, expect, it, vi, afterEach, beforeAll } from "vitest";
import { render, screen, cleanup, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

vi.mock("@tanstack/react-router", () => ({ createFileRoute: () => () => ({}), useNavigate: () => vi.fn() }));
const get = vi.fn();
vi.mock("@/lib/api", () => ({ api: { get: (p: string) => get(p), put: vi.fn(), post: vi.fn() }, ApiError: class extends Error {} }));

import { NotificationsPage, type NotifySettings, type NotifyState } from "./notifications";

beforeAll(() => {
  window.matchMedia = ((q: string) => ({ matches: false, media: q, addEventListener() {}, removeEventListener() {} })) as unknown as typeof window.matchMedia;
});
afterEach(() => { cleanup(); get.mockReset(); });

const settings = (over: Partial<NotifySettings> = {}): NotifySettings => ({
  webhook: { enabled: false, urlSet: false }, email: { enabled: false }, recipients: [], smtpSource: "own",
  own: { from: "", host: "", port: 587, encryption: "starttls", username: "", passwordSet: false },
  reminder: "off", triggers: { dns: true }, masAvailable: true, saved: true, secretsUsable: true, ...over,
});
const state = (over: Partial<NotifyState> = {}): NotifyState => ({ alerts: [], channels: {}, lastRun: null, ...over });

function show(s: NotifySettings, st: NotifyState) {
  get.mockImplementation((p: string) => Promise.resolve(p.endsWith("/settings") ? s : st));
  render(<QueryClientProvider client={new QueryClient()}><NotificationsPage /></QueryClientProvider>);
}

describe("NotificationsPage", () => {
  it("lists active problems with their title", async () => {
    show(settings(), state({ alerts: [{ key: "dns:a", trigger: "dns", title: "Name löst nicht auf", detail: "", since: "2026-01-01T10:00:00Z", lastSent: null }] }));
    expect(await screen.findByText("Name löst nicht auf")).toBeTruthy();
    expect(screen.queryByText("Alles ruhig")).toBeNull();
  });
  it("says all is quiet without problems", async () => {
    show(settings(), state({ lastRun: { at: "2026-01-01T10:00:00Z", unknown: [] } }));
    expect(await screen.findByText("Alles ruhig")).toBeTruthy();
  });
  it("greys out the MAS option when it is not available", async () => {
    show(settings({ masAvailable: false }), state());
    await waitFor(() => screen.getByText("Wie Matrix (MAS)"));
    expect((screen.getByLabelText("Wie Matrix (MAS)") as HTMLInputElement).disabled).toBe(true);
  });
  it("leaves the MAS option usable when it is available", async () => {
    show(settings(), state());
    await waitFor(() => screen.getByText("Wie Matrix (MAS)"));
    expect((screen.getByLabelText("Wie Matrix (MAS)") as HTMLInputElement).disabled).toBe(false);
  });
});
