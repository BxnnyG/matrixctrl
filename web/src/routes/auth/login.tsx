import { createFileRoute } from "@tanstack/react-router";
import { useState, useEffect } from "react";
import { api } from "@/lib/api";
import { ShieldCheck, Loader2, Moon, Sun, LockKeyhole } from "lucide-react";
import { useTheme } from "@/lib/theme";

export const Route = createFileRoute("/auth/login")({
  component: Login,
});

interface LoginResponse {
  token: string;
}

function Login() {
  const { theme, toggle } = useTheme();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [oidcEnabled, setOidcEnabled] = useState<boolean | null>(null);
  const [redirecting, setRedirecting] = useState(false);

  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const err = params.get("error");
    if (err) {
      setError(decodeURIComponent(err));
      window.history.replaceState({}, "", "/auth/login");
    }
  }, []);

  useEffect(() => {
    api.get<{ enabled: boolean }>("/api/v1/auth/oidc/available")
      .then((r) => setOidcEnabled(r.enabled))
      .catch(() => setOidcEnabled(false));
  }, []);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setLoading(true);
    try {
      const res = await api.post<LoginResponse>("/api/v1/auth/bootstrap/login", { username, password });
      localStorage.setItem("matrixctrl_token", res.token);
      window.location.replace("/");
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Login fehlgeschlagen");
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="relative min-h-screen flex items-center justify-center overflow-hidden bg-gray-50 dark:bg-gray-950">
      {/* Ambient gradient backdrop */}
      <div aria-hidden className="pointer-events-none absolute inset-0">
        <div className="absolute -top-40 -left-40 w-[32rem] h-[32rem] rounded-full bg-blue-400/20 dark:bg-blue-600/10 blur-3xl" />
        <div className="absolute -bottom-40 -right-40 w-[32rem] h-[32rem] rounded-full bg-[#0DBD8B]/20 dark:bg-[#0DBD8B]/10 blur-3xl" />
      </div>

      {/* Theme toggle */}
      <button
        onClick={toggle}
        className="absolute top-5 right-5 p-2 rounded-lg text-gray-400 hover:text-gray-600 dark:hover:text-gray-200 hover:bg-white/60 dark:hover:bg-gray-800/60 transition-colors"
        title={theme === "dark" ? "Light Mode" : "Dark Mode"}
      >
        {theme === "dark" ? <Sun className="w-4 h-4" /> : <Moon className="w-4 h-4" />}
      </button>

      <div className="relative w-full max-w-sm px-4">
        {/* Logo mark */}
        <div className="flex flex-col items-center mb-7">
          <div className="w-14 h-14 rounded-2xl bg-gradient-to-br from-blue-600 to-[#0DBD8B] flex items-center justify-center shadow-lg shadow-blue-600/20 mb-4">
            <svg viewBox="0 0 32 32" className="w-7 h-7 fill-white" aria-hidden="true">
              <path d="M1 1v30h2.5V3.5H29v27H1V30l-1 1v1h32V0H0v1h1z" />
              <path d="M10.6 9.2v13.5h2.4v-5.1l4.8 5.1h3.2l-5.5-5.7 5.2-5.3h-3.1l-4.6 4.8V9.2h-2.4z" />
            </svg>
          </div>
          <h1 className="text-2xl font-bold tracking-tight text-gray-900 dark:text-gray-100">MatrixCtrl</h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">ESS Admin Interface</p>
        </div>

        {/* Card */}
        <div className="bg-white/80 dark:bg-gray-900/80 backdrop-blur-xl rounded-2xl shadow-xl shadow-gray-900/5 dark:shadow-black/30 border border-gray-200/80 dark:border-gray-800/80 p-7">
          {error && (
            <div className="flex items-start gap-2 text-sm text-red-700 dark:text-red-400 bg-red-50 dark:bg-red-950/40 border border-red-200 dark:border-red-900 rounded-lg px-3 py-2.5 mb-5">
              <LockKeyhole className="w-4 h-4 shrink-0 mt-0.5" />
              <span>{error}</span>
            </div>
          )}

          {oidcEnabled === null && (
            <div className="flex items-center justify-center gap-2 py-6 text-sm text-gray-400 dark:text-gray-500">
              <Loader2 className="w-4 h-4 animate-spin" /> Laden…
            </div>
          )}

          {oidcEnabled === true && (
            <div className="space-y-4">
              <button
                type="button"
                disabled={redirecting}
                onClick={() => { setRedirecting(true); window.location.href = "/api/v1/auth/oidc/redirect"; }}
                className="group w-full flex items-center justify-center gap-2.5 py-3 px-4 bg-[#0DBD8B] hover:bg-[#0aa87b] disabled:opacity-70 text-white text-sm font-semibold rounded-xl shadow-sm transition-all active:scale-[0.99]"
              >
                {redirecting ? (
                  <Loader2 className="w-4 h-4 animate-spin" />
                ) : (
                  <svg viewBox="0 0 32 32" className="w-4 h-4 fill-white" aria-hidden="true">
                    <path d="M1 1v30h2.5V3.5H29v27H1V30l-1 1v1h32V0H0v1h1z" />
                    <path d="M10.6 9.2v13.5h2.4v-5.1l4.8 5.1h3.2l-5.5-5.7 5.2-5.3h-3.1l-4.6 4.8V9.2h-2.4z" />
                  </svg>
                )}
                {redirecting ? "Weiterleitung zu Matrix…" : "Mit Matrix anmelden"}
              </button>
              <div className="flex items-center justify-center gap-1.5 text-xs text-gray-400 dark:text-gray-500">
                <ShieldCheck className="w-3.5 h-3.5" />
                Nur für Administratoren
              </div>
            </div>
          )}

          {oidcEnabled === false && (
            <form onSubmit={handleSubmit} className="space-y-4">
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1.5">Benutzer</label>
                <input
                  type="text"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  autoComplete="username"
                  className="w-full px-3 py-2.5 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 rounded-xl text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-shadow"
                  required
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1.5">Passwort</label>
                <input
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  autoComplete="current-password"
                  className="w-full px-3 py-2.5 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 rounded-xl text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-shadow"
                  required
                />
              </div>
              <button
                type="submit"
                disabled={loading}
                className="w-full flex items-center justify-center gap-2 py-2.5 px-4 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-semibold rounded-xl shadow-sm transition-all active:scale-[0.99]"
              >
                {loading && <Loader2 className="w-4 h-4 animate-spin" />}
                {loading ? "Anmelden…" : "Anmelden"}
              </button>
            </form>
          )}
        </div>

        <p className="text-center text-xs text-gray-400 dark:text-gray-600 mt-6">
          MatrixCtrl · AGPL · Element Server Suite
        </p>
      </div>
    </div>
  );
}
