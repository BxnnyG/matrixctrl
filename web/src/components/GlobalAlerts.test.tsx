import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, cleanup, fireEvent } from "@testing-library/react";
import { AlertsBar, sortAlerts, type AlertItem } from "./GlobalAlerts";

afterEach(cleanup);
const A = (key: string, severity: AlertItem["severity"]): AlertItem => ({ key, severity, title: "T-" + key, detail: "d", link: "/x" });
const mount = (p: Partial<Parameters<typeof AlertsBar>[0]> = {}) => {
  const onDismiss = vi.fn();
  render(<AlertsBar alerts={[]} failed={false} emergency={false} dismissed={[]} onDismiss={onDismiss} onOpen={vi.fn()} {...p} />);
  return onDismiss;
};

describe("GlobalAlerts", () => {
  it("sorts errors before warnings before info", () => {
    expect(sortAlerts([A("i", "info"), A("w", "warning"), A("e", "error")]).map((a) => a.key)).toEqual(["e", "w", "i"]);
    mount({ alerts: [A("i", "info"), A("e", "error")] });
    const rows = screen.getAllByRole("alert").map((r) => r.textContent);
    expect(rows[0]).toContain("T-e");
    expect(rows[1]).toContain("T-i");
  });
  it("lets only info be dismissed", () => {
    const onDismiss = mount({ alerts: [A("e", "error"), A("w", "warning"), A("i", "info")] });
    const btns = screen.getAllByLabelText("Ausblenden");
    expect(btns).toHaveLength(1);
    fireEvent.click(btns[0]);
    expect(onDismiss).toHaveBeenCalledWith("i");
  });
  it("hides a dismissed info but never a dismissed error", () => {
    mount({ alerts: [A("e", "error"), A("i", "info")], dismissed: ["e", "i"] });
    expect(screen.queryByText("T-i")).toBeNull();
    expect(screen.getByText("T-e")).toBeTruthy();
  });
  it("shows one line when the query fails, nothing when there is nothing", () => {
    mount({ failed: true });
    expect(screen.getByText("Warnungen nicht abrufbar")).toBeTruthy();
    cleanup();
    const { container } = render(<AlertsBar alerts={[]} failed={false} emergency={false} dismissed={[]} onDismiss={vi.fn()} onOpen={vi.fn()} />);
    expect(container.innerHTML).toBe("");
  });
  it("adds the emergency-login warning", () => {
    mount({ emergency: true });
    expect(screen.getByText("Angemeldet per Notzugang")).toBeTruthy();
  });
});
