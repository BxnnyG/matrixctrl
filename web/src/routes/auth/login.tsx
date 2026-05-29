import { createFileRoute } from "@tanstack/react-router";
import { useState, useEffect } from "react";
import { api } from "@/lib/api";

export const Route = createFileRoute("/auth/login")({
  component: Login,
});

interface LoginResponse {
  token: string;
}

function Login() {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [oidcEnabled, setOidcEnabled] = useState<boolean | null>(null);

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
    <div className="min-h-screen flex items-center justify-center bg-gray-50 dark:bg-gray-950">
      <div className="w-full max-w-sm">
        <div className="bg-white dark:bg-gray-900 rounded-xl shadow-sm border border-gray-200 dark:border-gray-800 p-8">
          <div className="mb-8 text-center">
            <h1 className="text-2xl font-bold text-gray-900 dark:text-gray-100">MatrixCtrl</h1>
            <p className="text-sm text-gray-500 mt-1">ESS Admin Interface</p>
          </div>

          {error && (
            <p className="text-sm text-red-600 bg-red-50 dark:bg-red-950/40 dark:text-red-400 rounded-lg px-3 py-2 mb-4">
              {error}
            </p>
          )}

          {oidcEnabled === null && (
            <p className="text-sm text-gray-400 text-center">Laden…</p>
          )}

          {oidcEnabled === true && (
            <button
              type="button"
              onClick={() => { window.location.href = "/api/v1/auth/oidc/redirect"; }}
              className="w-full flex items-center justify-center gap-2.5 py-2.5 px-4 bg-[#0DBD8B] hover:bg-[#0aa87b] text-white text-sm font-medium rounded-lg transition-colors"
            >
              <svg viewBox="0 0 32 32" className="w-4 h-4 fill-white" aria-hidden="true">
                <path d="M1 1v30h2.5V3.5H29v27H1V30l-1 1v1h32V0H0v1h1z"/>
                <path d="M10.6 9.2v13.5h2.4v-5.1l4.8 5.1h3.2l-5.5-5.7 5.2-5.3h-3.1l-4.6 4.8V9.2h-2.4z"/>
              </svg>
              Mit Matrix anmelden
            </button>
          )}

          {oidcEnabled === false && (
            <form onSubmit={handleSubmit} className="space-y-4">
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Benutzer</label>
                <input
                  type="text"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  autoComplete="username"
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                  required
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Passwort</label>
                <input
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  autoComplete="current-password"
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                  required
                />
              </div>
              <button
                type="submit"
                disabled={loading}
                className="w-full py-2 px-4 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-medium rounded-lg transition-colors"
              >
                {loading ? "Anmelden..." : "Anmelden"}
              </button>
            </form>
          )}
        </div>
      </div>
    </div>
  );
}
