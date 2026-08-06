import { createFileRoute } from "@tanstack/react-router";
import { useState, useEffect } from "react";
import { api } from "@/lib/api";
import { Icon, Spinner } from "@/components/mc";

export const Route = createFileRoute("/auth/login")({
  component: Login,
});

interface LoginResponse { token: string }

const LogoMark = ({ size = 28 }: { size?: number }) => (
  <svg viewBox="0 0 32 32" width={size} height={size} fill="var(--accent-fg)" aria-hidden="true">
    <path d="M1 1v30h2.5V3.5H29v27H1V30l-1 1v1h32V0H0v1h1z" />
    <path d="M10.6 9.2v13.5h2.4v-5.1l4.8 5.1h3.2l-5.5-5.7 5.2-5.3h-3.1l-4.6 4.8V9.2h-2.4z" />
  </svg>
);

function Login() {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [oidcEnabled, setOidcEnabled] = useState<boolean | null>(null);
  const [oidcRetrying, setOidcRetrying] = useState(false);
  const [redirecting, setRedirecting] = useState(false);

  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const err = params.get("error");
    if (err) { setError(decodeURIComponent(err)); window.history.replaceState({}, "", "/auth/login"); }
  }, []);

  // While the backend is retrying a failed OIDC init (E33), keep asking. Without this
  // the operator sits on a password box until they think to reload — which is what
  // being locked out felt like the first time, even though recovery was seconds away.
  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout>;

    const poll = () => {
      api.get<{ enabled: boolean; retrying?: boolean }>("/api/v1/auth/oidc/available")
        .then((r) => {
          if (cancelled) return;
          setOidcEnabled(r.enabled);
          setOidcRetrying(!!r.retrying);
          if (!r.enabled && r.retrying) timer = setTimeout(poll, 5000);
        })
        .catch(() => { if (!cancelled) setOidcEnabled(false); });
    };
    poll();

    return () => { cancelled = true; clearTimeout(timer); };
  }, []);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(""); setLoading(true);
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

  const inputStyle: React.CSSProperties = { width: "100%", padding: "10px 12px", border: "1px solid var(--border)", background: "var(--surface-2)", color: "var(--text)", borderRadius: "var(--radius-sm)", fontSize: 13.5, fontFamily: "var(--font)" };

  return (
    <div style={{ position: "relative", minHeight: "100vh", display: "flex", alignItems: "center", justifyContent: "center", overflow: "hidden", background: "var(--bg)" }}>
      {/* Ambient gradient backdrop */}
      <div aria-hidden style={{ pointerEvents: "none", position: "absolute", inset: 0 }}>
        <div style={{ position: "absolute", top: -160, left: -160, width: 512, height: 512, borderRadius: "50%", background: "var(--accent)", opacity: 0.12, filter: "blur(80px)" }} />
        <div style={{ position: "absolute", bottom: -160, right: -160, width: 512, height: 512, borderRadius: "50%", background: "var(--status-ok)", opacity: 0.1, filter: "blur(80px)" }} />
      </div>

      <div style={{ position: "relative", width: "100%", maxWidth: 380, padding: "0 16px" }}>
        {/* Logo */}
        <div style={{ display: "flex", flexDirection: "column", alignItems: "center", marginBottom: 28 }}>
          <div style={{ width: 56, height: 56, borderRadius: "var(--radius-lg)", background: "var(--accent)", display: "grid", placeItems: "center", boxShadow: "0 8px 28px -6px var(--accent-soft)", marginBottom: 16 }}>
            <LogoMark size={28} />
          </div>
          <h1 style={{ margin: 0, fontSize: 24, fontWeight: 700, letterSpacing: "var(--head-tracking)", color: "var(--text)" }}>MatrixCtrl</h1>
          <p style={{ margin: "4px 0 0", fontSize: 13.5, color: "var(--text-faint)" }}>ESS Admin Interface</p>
        </div>

        {/* Card */}
        <div style={{ background: "var(--surface)", borderRadius: "var(--radius-lg)", boxShadow: "var(--shadow)", border: "1px solid var(--border)", padding: 28 }}>
          {error && (
            <div style={{ display: "flex", alignItems: "flex-start", gap: 8, fontSize: 13, color: "var(--status-err)", background: "color-mix(in oklch, var(--status-err) 10%, var(--surface))", border: "1px solid color-mix(in oklch, var(--status-err) 30%, var(--border))", borderRadius: "var(--radius-sm)", padding: "10px 12px", marginBottom: 20 }}>
              <Icon name="lock" size={16} style={{ flexShrink: 0, marginTop: 1 }} />
              <span>{error}</span>
            </div>
          )}

          {oidcEnabled === null && (
            <div style={{ display: "flex", alignItems: "center", justifyContent: "center", gap: 8, padding: "24px 0", fontSize: 13, color: "var(--text-faint)" }}><Spinner size={15} /> Laden…</div>
          )}

          {oidcEnabled === true && (
            <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
              <button type="button" disabled={redirecting} onClick={() => { setRedirecting(true); window.location.href = "/api/v1/auth/oidc/redirect"; }}
                style={{ width: "100%", display: "flex", alignItems: "center", justifyContent: "center", gap: 10, padding: "12px 16px", background: "var(--accent)", color: "var(--accent-fg)", fontSize: 14, fontWeight: 600, borderRadius: "var(--radius-sm)", border: "none", cursor: redirecting ? "default" : "pointer", opacity: redirecting ? 0.7 : 1 }}>
                {redirecting ? <Spinner size={15} /> : <LogoMark size={16} />}
                {redirecting ? "Weiterleitung zu Matrix…" : "Mit Matrix anmelden"}
              </button>
              <div style={{ display: "flex", alignItems: "center", justifyContent: "center", gap: 6, fontSize: 12, color: "var(--text-faint)" }}>
                <Icon name="shield" size={14} /> Nur für Administratoren
              </div>
            </div>
          )}

          {/* An unreachable issuer and an install that simply uses local login look
              identical on screen, and lead to opposite actions: wait, versus go find
              your password. Say which one this is. */}
          {oidcEnabled === false && oidcRetrying && (
            <div style={{ display: "flex", alignItems: "flex-start", gap: 8, fontSize: 13, color: "var(--status-warn)", background: "color-mix(in oklch, var(--status-warn) 10%, var(--surface))", border: "1px solid color-mix(in oklch, var(--status-warn) 30%, var(--border))", borderRadius: "var(--radius-sm)", padding: "10px 12px", marginBottom: 20 }}>
              <Spinner size={15} />
              <span>
                <strong style={{ fontWeight: 650 }}>Matrix-Login vorübergehend nicht erreichbar.</strong>{" "}
                Die Verbindung wird im Hintergrund wiederhergestellt — diese Seite schaltet automatisch um.
              </span>
            </div>
          )}

          {oidcEnabled === false && (
            <form onSubmit={handleSubmit} style={{ display: "flex", flexDirection: "column", gap: 16 }}>
              <div>
                <label style={{ display: "block", fontSize: 13, fontWeight: 600, color: "var(--text-dim)", marginBottom: 6 }}>Benutzer</label>
                <input type="text" value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" required style={inputStyle} />
              </div>
              <div>
                <label style={{ display: "block", fontSize: 13, fontWeight: 600, color: "var(--text-dim)", marginBottom: 6 }}>Passwort</label>
                <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" required style={inputStyle} />
              </div>
              <button type="submit" disabled={loading}
                style={{ width: "100%", display: "flex", alignItems: "center", justifyContent: "center", gap: 8, padding: "11px 16px", background: "var(--accent)", color: "var(--accent-fg)", fontSize: 14, fontWeight: 600, borderRadius: "var(--radius-sm)", border: "none", cursor: loading ? "default" : "pointer", opacity: loading ? 0.6 : 1 }}>
                {loading && <Spinner size={15} />}{loading ? "Anmelden…" : "Anmelden"}
              </button>
            </form>
          )}
        </div>

        <p style={{ textAlign: "center", fontSize: 11.5, color: "var(--text-faint)", marginTop: 24 }}>MatrixCtrl · AGPL · Element Server Suite</p>
      </div>
    </div>
  );
}
