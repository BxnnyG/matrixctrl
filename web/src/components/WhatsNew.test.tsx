import { describe, expect, it, vi, afterEach, beforeEach } from "vitest";
import { render, screen, cleanup, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const get = vi.fn();
vi.mock("@/lib/api", () => ({ api: { get: (p: string) => get(p) }, ApiError: class extends Error {} }));
import { WhatsNew } from "./WhatsNew";
import { Markdown } from "./Markdown";

const mount = () => render(<QueryClientProvider client={new QueryClient()}><WhatsNew /></QueryClientProvider>);
const answer = (version: string) => get.mockImplementation((p: string) =>
  p === "/api/v1/version" ? Promise.resolve({ version })
    : p.startsWith("/api/v1/whatsnew") ? Promise.resolve({ current: version, sections: [{ version, date: "2026-11-18", body: "- **neu** Sache" }] })
      : Promise.reject(new Error(p)));

beforeEach(() => localStorage.clear());
afterEach(() => { cleanup(); get.mockReset(); });

describe("WhatsNew", () => {
  it("first visit remembers the version and shows nothing", async () => {
    answer("0.1.130");
    mount();
    await waitFor(() => expect(localStorage.getItem("mc.whatsnew.seen")).toBe("0.1.130"));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(get).not.toHaveBeenCalledWith(expect.stringContaining("whatsnew"));
  });
  it("shows the dialog after a version change and closing marks it seen", async () => {
    localStorage.setItem("mc.whatsnew.seen", "0.1.129");
    answer("0.1.130");
    mount();
    expect(await screen.findByRole("dialog")).toBeTruthy();
    expect(get).toHaveBeenCalledWith("/api/v1/whatsnew?since=0.1.129");
    fireEvent.click(screen.getByText("Schließen"));
    expect(localStorage.getItem("mc.whatsnew.seen")).toBe("0.1.130");
    expect(screen.queryByRole("dialog")).toBeNull();
  });
  it("dev builds show nothing", async () => {
    localStorage.setItem("mc.whatsnew.seen", "0.1.129");
    answer("dev");
    mount();
    await new Promise((r) => setTimeout(r, 30));
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});

describe("Markdown", () => {
  it("renders lists with continuation lines, bold, and unsafe links as text", () => {
    const { container } = render(<Markdown text={"### Neu\n- **Fett** und `code`\n  weiter hier\n- [ok](https://a.example/x) [bad](javascript:alert(1)) [rel](docs/DESIGN.md)"} />);
    expect(container.querySelector("strong")?.textContent).toBe("Fett");
    expect(container.textContent).toContain("code weiter hier");
    const links = container.querySelectorAll("a");
    expect(links).toHaveLength(1);
    expect(links[0].getAttribute("href")).toBe("https://a.example/x");
    expect(container.textContent).toContain("bad");
    expect(container.textContent).toContain("rel");
    expect(container.innerHTML).not.toContain("javascript:");
  });
});
