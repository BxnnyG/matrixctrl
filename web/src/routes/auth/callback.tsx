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
      window.location.replace(error ? `/auth/login?error=${encodeURIComponent(error)}` : "/auth/login");
    }
  }, [navigate]);

  return (
    <div style={{ position: "relative", minHeight: "100vh", display: "flex", alignItems: "center", justifyContent: "center", overflow: "hidden", background: "var(--bg)" }}>
      <div aria-hidden style={{ pointerEvents: "none", position: "absolute", inset: 0 }}>
        <div style={{ position: "absolute", top: -160, left: -160, width: 512, height: 512, borderRadius: "50%", background: "var(--accent)", opacity: 0.12, filter: "blur(80px)" }} />
        <div style={{ position: "absolute", bottom: -160, right: -160, width: 512, height: 512, borderRadius: "50%", background: "var(--status-ok)", opacity: 0.1, filter: "blur(80px)" }} />
      </div>
      <div style={{ position: "relative", display: "flex", flexDirection: "column", alignItems: "center", gap: 20 }}>
        <div style={{ width: 56, height: 56, borderRadius: "var(--radius-lg)", background: "var(--accent)", display: "grid", placeItems: "center", boxShadow: "0 8px 28px -6px var(--accent-soft)" }}>
          <svg viewBox="0 0 32 32" width={28} height={28} fill="var(--accent-fg)" aria-hidden="true" style={{ animation: "mc-ping 1.5s ease infinite" }}>
            <path d="M1 1v30h2.5V3.5H29v27H1V30l-1 1v1h32V0H0v1h1z" />
            <path d="M10.6 9.2v13.5h2.4v-5.1l4.8 5.1h3.2l-5.5-5.7 5.2-5.3h-3.1l-4.6 4.8V9.2h-2.4z" />
          </svg>
        </div>
        <p style={{ fontSize: 13.5, color: "var(--text-faint)" }}>Anmeldung wird abgeschlossen…</p>
      </div>
    </div>
  );
}
