import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useEffect } from "react";

export const Route = createFileRoute("/auth/callback")({
  component: OIDCCallback,
});

function OIDCCallback() {
  const navigate = useNavigate();

  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const token = params.get("token");
    const error = params.get("error");

    if (token) {
      localStorage.setItem("matrixctrl_token", token);
      window.location.replace("/");
    } else {
      const dest = error
        ? `/auth/login?error=${encodeURIComponent(error)}`
        : "/auth/login";
      window.location.replace(dest);
    }
  }, [navigate]);

  return (
    <div className="relative min-h-screen flex items-center justify-center overflow-hidden bg-gray-50 dark:bg-gray-950">
      <div aria-hidden className="pointer-events-none absolute inset-0">
        <div className="absolute -top-40 -left-40 w-[32rem] h-[32rem] rounded-full bg-blue-400/20 dark:bg-blue-600/10 blur-3xl" />
        <div className="absolute -bottom-40 -right-40 w-[32rem] h-[32rem] rounded-full bg-[#0DBD8B]/20 dark:bg-[#0DBD8B]/10 blur-3xl" />
      </div>
      <div className="relative flex flex-col items-center gap-5">
        <div className="w-14 h-14 rounded-2xl bg-gradient-to-br from-blue-600 to-[#0DBD8B] flex items-center justify-center shadow-lg shadow-blue-600/20">
          <svg viewBox="0 0 32 32" className="w-7 h-7 fill-white animate-pulse" aria-hidden="true">
            <path d="M1 1v30h2.5V3.5H29v27H1V30l-1 1v1h32V0H0v1h1z" />
            <path d="M10.6 9.2v13.5h2.4v-5.1l4.8 5.1h3.2l-5.5-5.7 5.2-5.3h-3.1l-4.6 4.8V9.2h-2.4z" />
          </svg>
        </div>
        <p className="text-sm text-gray-500 dark:text-gray-400">Anmeldung wird abgeschlossen…</p>
      </div>
    </div>
  );
}
