import { Link, useRouterState } from "@tanstack/react-router";
import { LayoutDashboard, Zap, Package, LogOut, Moon, Sun } from "lucide-react";
import type { ReactNode } from "react";
import { useTheme } from "@/lib/theme";

const nav = [
  { to: "/", label: "Dashboard", icon: LayoutDashboard, exact: true },
  { to: "/helm/", label: "Helm", icon: Package, exact: false },
  { to: "/hooks/", label: "Hooks", icon: Zap, exact: false },
];

export function Shell({ children }: { children: ReactNode }) {
  const state = useRouterState();
  const { theme, toggle } = useTheme();
  const isAuth = state.location.pathname.startsWith("/auth");

  if (isAuth) return <>{children}</>;

  return (
    <div className="flex min-h-screen bg-gray-50 dark:bg-gray-950">
      <aside className="w-56 bg-white dark:bg-gray-900 border-r border-gray-200 dark:border-gray-800 flex flex-col">
        <div className="px-4 py-5 border-b border-gray-200 dark:border-gray-800">
          <h1 className="text-base font-bold text-gray-900 dark:text-gray-100">MatrixCtrl</h1>
          <p className="text-xs text-gray-500 mt-0.5">ESS Admin</p>
        </div>
        <nav className="flex-1 px-3 py-4 space-y-1">
          {nav.map(({ to, label, icon: Icon, exact }) => {
            const active = exact
              ? state.location.pathname === to
              : state.location.pathname.startsWith(to) && to !== "/";
            return (
              <Link
                key={to}
                to={to}
                className={`flex items-center gap-2.5 px-3 py-2 rounded-lg text-sm transition-colors ${
                  active
                    ? "bg-blue-50 dark:bg-blue-950 text-blue-700 dark:text-blue-400 font-medium"
                    : "text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-800"
                }`}
              >
                <Icon className="w-4 h-4" />
                {label}
              </Link>
            );
          })}
        </nav>
        <div className="px-3 py-4 border-t border-gray-200 dark:border-gray-800 space-y-1">
          <button
            onClick={toggle}
            className="flex items-center gap-2.5 px-3 py-2 w-full rounded-lg text-sm text-gray-500 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors"
          >
            {theme === "dark" ? <Sun className="w-4 h-4" /> : <Moon className="w-4 h-4" />}
            {theme === "dark" ? "Light Mode" : "Dark Mode"}
          </button>
          <button
            onClick={() => {
              localStorage.removeItem("matrixctrl_token");
              window.location.href = "/auth/login";
            }}
            className="flex items-center gap-2.5 px-3 py-2 w-full rounded-lg text-sm text-gray-500 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors"
          >
            <LogOut className="w-4 h-4" />
            Abmelden
          </button>
        </div>
      </aside>

      <main className="flex-1 overflow-auto">
        <div className="max-w-5xl mx-auto px-6 py-8">
          {children}
        </div>
      </main>
    </div>
  );
}
