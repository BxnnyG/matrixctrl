import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { RouterProvider, createRouter } from "@tanstack/react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { routeTree } from "./routeTree.gen";
import "@fontsource-variable/geist";
import "@fontsource-variable/geist-mono";
import "./index.css";
import "./lib/theme";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { staleTime: 10_000, retry: 1 },
  },
});

const router = createRouter({
  routeTree,
  context: { queryClient },
  // Without this, an unhandled error in any route renders TanStack Router's default
  // CatchBoundary: the words "Something went wrong!", no cause, no navigation. An
  // operator met it on their first login to a new server and had to type /setup to
  // get anywhere. Whatever else is broken, the screen should say what and offer a way on.
  defaultErrorComponent: ({ error }) => (
    <div style={{ padding: 40, maxWidth: 640, margin: "0 auto", fontFamily: "var(--font)" }}>
      <h1 style={{ fontSize: 18, marginBottom: 8 }}>Diese Seite konnte nicht geladen werden</h1>
      <p style={{ fontSize: 13, color: "var(--text-faint)", marginBottom: 16 }}>
        Die übrigen Bereiche funktionieren möglicherweise weiter.
      </p>
      <pre style={{ fontSize: 12, fontFamily: "var(--mono)", background: "var(--surface-2)", border: "1px solid var(--border)", borderRadius: 8, padding: 12, overflowX: "auto", whiteSpace: "pre-wrap" }}>
        {error instanceof Error ? error.message : String(error)}
      </pre>
      <div style={{ display: "flex", gap: 10, marginTop: 16 }}>
        <a href="/" style={{ fontSize: 13 }}>Zur Übersicht</a>
        <a href="/setup" style={{ fontSize: 13 }}>Zu Setup</a>
        <a href="/system" style={{ fontSize: 13 }}>Zu System &amp; Logs</a>
      </div>
    </div>
  ),
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
);
