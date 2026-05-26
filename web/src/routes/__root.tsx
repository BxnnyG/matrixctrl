import { createRootRouteWithContext, Outlet, redirect } from "@tanstack/react-router";
import type { QueryClient } from "@tanstack/react-query";
import { Shell } from "@/components/layout/Shell";

interface RouterContext {
  queryClient: QueryClient;
}

export const Route = createRootRouteWithContext<RouterContext>()({
  beforeLoad: async ({ location }) => {
    const isAuthRoute = location.pathname.startsWith("/auth");
    if (isAuthRoute) return;

    const token = localStorage.getItem("matrixctrl_token");
    if (!token) {
      throw redirect({ to: "/auth/login", search: { redirect: location.href } });
    }
  },
  component: () => (
    <Shell>
      <Outlet />
    </Shell>
  ),
});
