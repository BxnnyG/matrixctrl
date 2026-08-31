import { useRouterState, useNavigate } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { useState, useEffect, type ReactNode } from "react";
import { useTweaks } from "@/lib/theme";
import { api } from "@/lib/api";
import { Icon, StatusDot, Avatar, Kbd, useIsMobile } from "@/components/mc";
import { TweaksButton } from "@/components/layout/Tweaks";

interface NavItem { id: string; label: string; icon: string; to?: string; phase?: string }
interface NavGroup { group: string; phase?: string; items: NavItem[] }

const NAV: NavGroup[] = [
  { group: "Übersicht", items: [{ id: "dashboard", label: "Dashboard", icon: "dashboard", to: "/" }] },
  { group: "Konfiguration", items: [
    { id: "config", label: "Config", icon: "sliders", to: "/config" },
    { id: "history", label: "Versionen & Diff", icon: "git", to: "/config/history" },
  ] },
  { group: "Deployment", items: [
    { id: "helm", label: "Updates", icon: "helm", to: "/helm" },
    { id: "hooks", label: "Hooks", icon: "hook", to: "/hooks" },
    { id: "setup", label: "Setup", icon: "rocket", to: "/setup" },
  ] },
  { group: "Betrieb", items: [
    { id: "system", label: "System", icon: "cpu", to: "/system" },
    { id: "audit", label: "Audit-Log", icon: "audit", to: "/audit" },
    { id: "rtc", label: "Calls / RTC", icon: "phone", to: "/rtc" },
  ] },
  // Users is real as of E27, rooms as of E36, moderation as of E46 — which completes
  // the Verwaltung group and, with it, Phase 2's feature list.
  { group: "Verwaltung", items: [
    { id: "users", label: "Benutzer", icon: "users", to: "/users" },
    { id: "rooms", label: "Räume", icon: "room", to: "/rooms" },
    { id: "moderation", label: "Moderation", icon: "shield", to: "/reports" },
  ] },
  // Future phases — shown as disabled roadmap entries (no backend yet).
  { group: "Betrieb · Day-2", phase: "3", items: [
    { id: "tls", label: "TLS & DNS", icon: "lock" }, { id: "backup", label: "Backup", icon: "database" },
  ] },
  { group: "Netzwerk", phase: "4", items: [
    { id: "federation", label: "Föderation", icon: "globe" }, { id: "bridges", label: "Bridges", icon: "audit" },
  ] },
  { group: "Compliance", phase: "5", items: [
    { id: "workers", label: "Worker-Insights", icon: "activity" },
  ] },
];

const TITLES: Record<string, [string, string]> = {
  "/": ["Dashboard", "Komponenten-Status & Cluster-Health"],
  "/config": ["Konfiguration", "Versionierte YAML pro ESS-Sektion"],
  "/config/history": ["Versionen & Diff", "Git-Historie der Config · Rollback"],
  "/helm": ["Updates", "Helm-Upgrades mit Patch-erhaltenden Hooks"],
  "/helm/history": ["Upgrade-Verlauf", "Vergangene Upgrades · Revision & Hook-Ergebnis"],
  "/hooks": ["Hooks", "Post-Upgrade Patch-Engine"],
  "/setup": ["Setup", "Onboarding · Deploy · Adopt · Matrix-Login"],
  "/system": ["System", "Node, PVCs, Pods & Metriken"],
  "/audit": ["Audit-Log", "Wer hat was geändert · nur ändernde Zugriffe"],
  "/users": ["Benutzer", "Konten aus dem Matrix Authentication Service"],
  "/rooms": ["Räume", "Räume auf diesem Homeserver, aus der Synapse-Admin-API"],
  "/rtc": ["Calls / RTC", "Was Calling braucht — und was von hier aus nicht prüfbar ist"],
};

function Logo() {
  return (
    <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
      <div style={{ position: "relative", width: 30, height: 30, borderRadius: "calc(var(--radius-sm) + 1px)", background: "linear-gradient(145deg, var(--accent), color-mix(in oklch, var(--accent) 60%, black))", display: "grid", placeItems: "center", boxShadow: "0 2px 8px -2px color-mix(in oklch, var(--accent) 60%, transparent)" }}>
        <svg width={18} height={18} viewBox="0 0 24 24" fill="none" stroke="var(--accent-fg)" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round">
          <path d="M7 4L3 12l4 8" /><path d="M17 4l4 8-4 8" /><circle cx="12" cy="12" r="1.6" fill="var(--accent-fg)" stroke="none" />
        </svg>
      </div>
      <div style={{ lineHeight: 1 }}>
        <div style={{ fontSize: 15.5, fontWeight: 650, letterSpacing: "-0.02em", color: "var(--text)" }}>MatrixCtrl</div>
        <div style={{ fontSize: 10.5, color: "var(--text-faint)", fontFamily: "var(--mono)", marginTop: 2 }}>ess admin</div>
      </div>
    </div>
  );
}

function NavRow({ item, active, mono, navigate }: { item: NavItem; active: boolean; mono: boolean; navigate: (to: string) => void }) {
  const [h, setH] = useState(false);
  const disabled = !item.to;
  return (
    <button onClick={() => item.to && navigate(item.to)} disabled={disabled} onMouseEnter={() => setH(true)} onMouseLeave={() => setH(false)}
      style={{ display: "flex", alignItems: "center", gap: 10, width: "100%", padding: "8px 10px", border: "none", cursor: disabled ? "default" : "pointer",
        borderRadius: "var(--radius-sm)", background: active ? "var(--accent-soft)" : !disabled && h ? "var(--hover)" : "transparent",
        color: active ? "var(--accent)" : disabled ? "var(--text-faint)" : h ? "var(--text)" : "var(--text-dim)", transition: "all .12s ease", textAlign: "left",
        fontFamily: mono ? "var(--mono)" : "var(--font)", fontSize: mono ? 12.5 : 13.5, fontWeight: active ? 600 : 500, position: "relative", opacity: disabled ? 0.55 : 1 }}>
      {active && <span style={{ position: "absolute", left: -8, top: "50%", transform: "translateY(-50%)", width: 3, height: 16, borderRadius: 2, background: "var(--accent)" }} />}
      <Icon name={item.icon} size={17} stroke={active ? 2 : 1.7} />
      <span style={{ flex: 1 }}>{item.label}</span>
    </button>
  );
}

function activeId(path: string): string {
  if (path === "/") return "dashboard";
  if (path === "/config/history") return "history";
  if (path === "/config" || path.startsWith("/config/")) return "config";
  if (path.startsWith("/helm")) return "helm";
  if (path.startsWith("/hooks")) return "hooks";
  if (path.startsWith("/setup")) return "setup";
  if (path.startsWith("/system")) return "system";
  if (path.startsWith("/audit")) return "audit";
  if (path.startsWith("/rtc")) return "rtc";
  return "";
}

function Sidebar({ path, onNavigate }: { path: string; onNavigate?: () => void }) {
  const [t] = useTweaks();
  const navigate = useNavigate();
  const cur = activeId(path);
  const go = (to: string) => { navigate({ to }); onNavigate?.(); };
  return (
    // 100dvh, not 100vh: on a phone the browser chrome slides away and 100vh keeps
    // reserving the space it used to occupy, leaving a strip that scrolls nowhere.
    <aside style={{ width: "var(--rail-w)", flexShrink: 0, background: "var(--rail)", borderRight: "1px solid var(--border)", display: "flex", flexDirection: "column", height: "100dvh", position: "sticky", top: 0, paddingTop: "env(safe-area-inset-top)" }}>
      <div style={{ padding: "18px 16px 14px" }}><Logo /></div>
      <nav className="mc-scroll" style={{ flex: 1, overflowY: "auto", padding: "4px 14px 14px", display: "flex", flexDirection: "column", gap: 2 }}>
        {NAV.filter((g) => !g.phase || t.showPhaseTags).map((g) => (
          <div key={g.group} style={{ marginTop: 12 }}>
            <div style={{ display: "flex", alignItems: "center", gap: 7, padding: "0 10px 6px" }}>
              <span style={{ fontSize: 10.5, fontWeight: 600, letterSpacing: "0.06em", textTransform: "uppercase", color: "var(--text-faint)" }}>{g.group}</span>
              {g.phase && <span style={{ fontSize: 9.5, fontWeight: 600, padding: "1px 5px", borderRadius: 4, background: "var(--surface-2)", color: "var(--text-faint)", fontFamily: "var(--mono)" }}>P{g.phase}</span>}
            </div>
            {g.items.map((it) => <NavRow key={it.id} item={it} active={cur === it.id} mono={t.monoLabels} navigate={go} />)}
          </div>
        ))}
      </nav>
      <div style={{ padding: "12px 16px", borderTop: "1px solid var(--border)", display: "flex", alignItems: "center", gap: 10 }}>
        <Avatar name="Admin" size={32} accent />
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ fontSize: 12.5, fontWeight: 600, color: "var(--text)", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>Admin</div>
          <div style={{ fontSize: 11, color: "var(--text-faint)", fontFamily: "var(--mono)" }}>Matrix-Login</div>
        </div>
        <button title="Abmelden" onClick={() => { localStorage.removeItem("matrixctrl_token"); window.location.href = "/auth/login"; }}
          style={{ background: "transparent", border: "none", color: "var(--text-faint)", cursor: "pointer", padding: 4, display: "grid", placeItems: "center" }}><Icon name="logout" size={16} /></button>
      </div>
    </aside>
  );
}

interface StatusResp { release?: { chart_version?: string }; components?: { status: string }[] }

// Backend reports healthy | degraded | down | scaled-zero — anything that is
// neither healthy nor deliberately scaled to zero needs the operator's attention.
const isDegraded = (s: string) => s === "degraded" || s === "down";

function Topbar({ path, onMenu }: { path: string; onMenu?: () => void }) {
  const [title, sub] = TITLES[path] || (path.startsWith("/config/") ? ["Konfiguration", "YAML-Editor"] : ["MatrixCtrl", ""]);
  const { data } = useQuery({ queryKey: ["status"], queryFn: () => api.get<StatusResp>("/api/v1/status"), staleTime: 15_000, refetchInterval: 30_000 });
  const warns = (data?.components || []).filter((c) => isDegraded(c.status)).length;
  const healthy = !!data && warns === 0;
  const ver = data?.release?.chart_version?.replace(/^matrix-stack-/, "") || "—";
  return (
    <header style={{ position: "sticky", top: 0, zIndex: 20, display: "flex", alignItems: "center", justifyContent: "space-between", gap: onMenu ? 10 : 16, padding: onMenu ? "0 14px" : "0 26px", height: 60, background: "color-mix(in oklch, var(--bg) 82%, transparent)", backdropFilter: "blur(12px)", borderBottom: "1px solid var(--border)" }}>
      {onMenu && (
        // 40px square: the smallest reliable touch target, and the reason the rail
        // can disappear at all.
        <button aria-label="Menü" onClick={onMenu}
          style={{ flexShrink: 0, width: 40, height: 40, display: "grid", placeItems: "center", background: "var(--surface)", border: "1px solid var(--border)", borderRadius: "var(--radius-sm)", color: "var(--text)", cursor: "pointer" }}>
          <Icon name="menu" size={18} />
        </button>
      )}
      <div style={{ minWidth: 0, flex: 1 }}>
        <h1 style={{ margin: 0, fontSize: 17, fontWeight: 600, letterSpacing: "var(--head-tracking)", color: "var(--text)", whiteSpace: "nowrap" }}>{title}</h1>
        <div style={{ fontSize: 12, color: "var(--text-faint)", marginTop: 1, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>{sub}</div>
      </div>
      <div style={{ display: "flex", alignItems: "center", gap: 10, flexShrink: 0 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 8, padding: "6px 12px", borderRadius: 999, background: "var(--surface)", border: "1px solid var(--border)", whiteSpace: "nowrap" }}>
          <StatusDot status={healthy ? "ok" : "warn"} pulse size={7} />
          {/* On a phone the dot and the warning count carry the message; the words and
              the chart version are what has to give when 360px is all there is. */}
          <span style={{ fontSize: 12.5, color: "var(--text-dim)" }}>
            {onMenu
              ? (healthy ? "OK" : warns ? String(warns) : "…")
              : (healthy ? "Cluster gesund" : warns ? `${warns} Warnung${warns > 1 ? "en" : ""}` : "Status…")}
          </span>
          {!onMenu && <span style={{ width: 1, height: 14, background: "var(--border)" }} />}
          {!onMenu && <span style={{ fontSize: 12, fontFamily: "var(--mono)", color: "var(--text-faint)" }}>{ver}</span>}
        </div>
        {/* A keyboard shortcut hint on a device with no keyboard is pure noise. */}
        {!onMenu && (
          <div style={{ display: "flex", alignItems: "center", gap: 7, padding: "6px 10px", borderRadius: "var(--radius-sm)", background: "var(--surface)", border: "1px solid var(--border)", color: "var(--text-faint)", cursor: "default" }}>
            <Icon name="search" size={15} /><Kbd>⌘K</Kbd>
          </div>
        )}
        {!onMenu && <TweaksButton />}
      </div>
    </header>
  );
}

export function Shell({ children }: { children: ReactNode }) {
  const state = useRouterState();
  const [t] = useTweaks();
  const mobile = useIsMobile();
  const [drawer, setDrawer] = useState(false);
  const path = state.location.pathname;

  // Closing on navigation is handled by Sidebar's onNavigate; this closes it when the
  // viewport grows back, so a drawer left open on a rotated phone does not reappear as
  // a floating panel next to the rail it turned back into.
  useEffect(() => {
    if (!mobile) setDrawer(false);
  }, [mobile]);

  if (path.startsWith("/auth")) return <>{children}</>;
  // Full-bleed: settings + per-section YAML editor manage their own height. History stays centered.
  const fullBleed = path === "/config" || (path.startsWith("/config/") && path !== "/config/history");

  return (
    <div style={{ display: "flex", minHeight: "100dvh", background: "var(--bg)", color: "var(--text)", fontFamily: "var(--font)", letterSpacing: "var(--tracking)", fontSize: "var(--fs)", backgroundImage: t.gridBg ? "radial-gradient(var(--bg-grid) 1px, transparent 1px)" : "none", backgroundSize: "22px 22px" }}>
      {/* On a phone the rail is a drawer: 240px of permanent navigation on a 360px
          screen leaves no room for the thing being navigated to (etappe 57). */}
      {!mobile && <Sidebar path={path} />}
      {mobile && drawer && (
        <>
          <div onClick={() => setDrawer(false)}
            style={{ position: "fixed", inset: 0, zIndex: 40, background: "color-mix(in oklch, black 55%, transparent)", backdropFilter: "blur(2px)" }} />
          <div style={{ position: "fixed", insetInlineStart: 0, top: 0, bottom: 0, zIndex: 41, boxShadow: "0 0 40px -8px black" }}>
            <Sidebar path={path} onNavigate={() => setDrawer(false)} />
          </div>
        </>
      )}

      <div style={{ flex: 1, minWidth: 0, display: "flex", flexDirection: "column", height: "100dvh", overflow: "hidden" }}>
        <Topbar path={path} onMenu={mobile ? () => setDrawer(true) : undefined} />
        {fullBleed ? (
          <main style={{ flex: 1, overflow: "hidden" }}>{children}</main>
        ) : (
          <main className="mc-scroll" style={{
            flex: 1, overflowY: "auto",
            padding: mobile ? 0 : 26,
            // The home-screen app paints under the rounded corners and the home
            // indicator; without this the last row of every page sits under it.
            paddingBottom: mobile ? "env(safe-area-inset-bottom)" : 26,
          }}>
            <div style={{ maxWidth: 1340, margin: "0 auto" }}>{children}</div>
          </main>
        )}
      </div>
    </div>
  );
}
