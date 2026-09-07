import { describe, expect, it, vi, afterEach } from "vitest";
import { render, screen, cleanup } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

// The api module is the seam: everything under test asks the server through it, and
// nothing else in these components touches the network.
const get = vi.fn();
vi.mock("@/lib/api", () => ({
  api: { get: (path: string) => get(path) },
  ApiError: class extends Error {},
}));

import { MatrixLoginHealth } from "./setup";

function mount(node: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{node}</QueryClientProvider>);
}

afterEach(() => { cleanup(); get.mockReset(); });

describe("MatrixLoginHealth", () => {
  // "This install uses local login" and "Matrix login exists but its issuer is down"
  // look identical on screen and lead to opposite actions. The backend has told them
  // apart for a long time; nothing displayed it, and an operator whose MAS had moved
  // met a login screen with no explanation (§4.90).
  it("says nothing when Matrix login is simply not in use", async () => {
    get.mockResolvedValue({ enabled: false, retrying: false });
    mount(<MatrixLoginHealth />);
    // Nothing to assert positively: the point is that no warning appears.
    await new Promise((r) => setTimeout(r, 0));
    expect(screen.queryByText(/antwortet aber nicht/i)).toBeNull();
  });

  it("says so, and how to get back in, when the issuer is unreachable", async () => {
    get.mockResolvedValue({ enabled: false, retrying: true });
    mount(<MatrixLoginHealth />);
    expect(await screen.findByText(/antwortet aber nicht/i)).toBeTruthy();
    // The way out belongs next to the diagnosis, not in a document elsewhere.
    expect(screen.getByText(/recover-login/)).toBeTruthy();
  });

  it("says nothing while Matrix login is working", async () => {
    get.mockResolvedValue({ enabled: true, retrying: false });
    mount(<MatrixLoginHealth />);
    await new Promise((r) => setTimeout(r, 0));
    expect(screen.queryByText(/antwortet aber nicht/i)).toBeNull();
  });
});
