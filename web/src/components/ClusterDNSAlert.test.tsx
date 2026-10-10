import { describe, expect, it, afterEach } from "vitest";
import { render, screen, cleanup } from "@testing-library/react";
import { ClusterDNSAlertView, type DNSState } from "./ClusterDNSAlert";

afterEach(cleanup);

const base: DNSState = {
  ok: false, checked: "2026-10-10T08:05:00Z", since: "2026-10-10T08:00:00Z", names: ["mas.example.com"],
  failures: [{ name: "mas.example.com", kind: "servfail", server: "10.43.0.10:53", error: "server misbehaving" }],
};

describe("ClusterDNSAlert", () => {
  // The case of 2026-10-10. The fix is on the host, not in ESS — so the card names the
  // resolver chain and offers the way to cut it, not a restart of something.
  it("names the host's resolver and offers the independence fix when outside answers", () => {
    render(<ClusterDNSAlertView state={{ ...base, outside: "answers" }} />);
    expect(screen.getByText(/nur die Namensauflösung im Cluster hängt/)).toBeTruthy();
    expect(screen.getByText(/resolv-conf/)).toBeTruthy();
    expect(screen.getByText(/mas\.example\.com — Auflösung gescheitert/)).toBeTruthy();
  });

  it("says there is no way out, without the k3s fix, when outside is silent too", () => {
    render(<ClusterDNSAlertView state={{ ...base, outside: "silent" }} />);
    expect(screen.getByText(/keine Verbindung nach draußen/)).toBeTruthy();
    expect(screen.queryByText(/resolv-conf/)).toBeNull();
  });
});
