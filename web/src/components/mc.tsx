// mc.tsx — MatrixCtrl design-system primitives, ported from the Claude Design
// prototype. Inline styles on the CSS-var tokens (index.css) so the 3 directions
// re-theme everything live. Use these instead of ad-hoc Tailwind for the new look.
import { useEffect, useState, type CSSProperties, type ReactNode } from "react";

/* ── Icons (24x24 line set) ── */
export const ICONS: Record<string, string> = {
  dashboard: "M3 13h8V3H3zM13 21h8V3h-8zM3 21h8v-6H3z",
  sliders: "M4 21v-7M4 10V3M12 21v-9M12 8V3M20 21v-5M20 12V3M1 14h6M9 8h6M17 16h6",
  helm: "M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z M3.3 7L12 12l8.7-5 M12 22V12",
  hook: "M18 6.5a4.5 4.5 0 1 0-9 0v9a3 3 0 1 1-6 0v-1 M18 6.5V13",
  rocket: "M4.5 16.5c-1.5 1.3-2 5-2 5s3.7-.5 5-2c.7-.8.7-2 0-2.8a2 2 0 0 0-3 0z M12 15l-3-3a22 22 0 0 1 8-10c2.5 0 4 1.5 4 4a22 22 0 0 1-10 8z M9 12H4s.5-3 2-4 5 0 5 0 M12 15v5s3-.5 4-2 0-5 0-5",
  users: "M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2 M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8z M22 21v-2a4 4 0 0 0-3-3.87 M16 3.13A4 4 0 0 1 16 11",
  room: "M4 9h16M4 15h16M10 3 8 21M16 3l-2 18",
  shield: "M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z M9 12l2 2 4-4",
  phone: "M15.5 8.5a5 5 0 0 1 0 7 M18 6a9 9 0 0 1 0 12 M3 5a2 2 0 0 1 2-2h2.3a1 1 0 0 1 1 .75l1 4a1 1 0 0 1-.5 1.1L7 10a12 12 0 0 0 5 5l1.15-1.8a1 1 0 0 1 1.1-.5l4 1a1 1 0 0 1 .75 1V18a2 2 0 0 1-2 2A16 16 0 0 1 3 5z",
  lock: "M5 11h14v10H5z M8 11V7a4 4 0 0 1 8 0v4 M12 15v2",
  database: "M12 8c4.4 0 8-1.3 8-3s-3.6-3-8-3-8 1.3-8 3 3.6 3 8 3z M4 5v6c0 1.7 3.6 3 8 3s8-1.3 8-3V5 M4 11v6c0 1.7 3.6 3 8 3s8-1.3 8-3v-6",
  globe: "M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20z M2 12h20 M12 2a15 15 0 0 1 0 20 15 15 0 0 1 0-20z",
  audit: "M9 5H7a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V7a2 2 0 0 0-2-2h-2 M9 5a2 2 0 0 1 2-2h2a2 2 0 0 1 2 2 M9 5a2 2 0 0 0 2 2h2a2 2 0 0 0 2-2 M9 12h6 M9 16h4",
  server: "M4 4h16v6H4z M4 14h16v6H4z M8 7v0 M8 17v0",
  search: "M11 19a8 8 0 1 0 0-16 8 8 0 0 0 0 16z M21 21l-4.3-4.3",
  bell: "M18 8a6 6 0 0 0-12 0c0 7-3 9-3 9h18s-3-2-3-9 M13.7 21a2 2 0 0 1-3.4 0",
  chevDown: "M6 9l6 6 6-6",
  chevRight: "M9 6l6 6-6 6",
  chevLeft: "M15 6l-6 6 6 6",
  check: "M20 6L9 17l-5-5",
  x: "M18 6L6 18M6 6l12 12",
  plus: "M12 5v14M5 12h14",
  refresh: "M21 12a9 9 0 1 1-3-6.7L21 8 M21 3v5h-5",
  play: "M6 4l14 8-14 8z",
  clock: "M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20z M12 6v6l4 2",
  diff: "M6 3v12 M18 9v12 M6 21a3 3 0 1 0 0-6 3 3 0 0 0 0 6z M18 9a3 3 0 1 0 0-6 3 3 0 0 0 0 6z M6 15a9 9 0 0 0 9-9",
  file: "M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z M14 3v5h5 M9 13h6 M9 17h6",
  terminal: "M4 5h16v14H4z M8 9l3 3-3 3 M13 15h3",
  alert: "M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z M12 9v4 M12 17v0",
  info: "M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20z M12 16v-4 M12 8v0",
  ext: "M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6 M15 3h6v6 M10 14L21 3",
  copy: "M9 9h11v11H9z M5 15H4V4h11v1",
  download: "M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4 M7 10l5 5 5-5 M12 15V3",
  upload: "M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4 M17 8l-5-5-5 5 M12 3v12",
  cpu: "M6 6h12v12H6z M9 9h6v6H9z M9 2v3 M15 2v3 M9 19v3 M15 19v3 M2 9h3 M2 15h3 M19 9h3 M19 15h3",
  activity: "M22 12h-4l-3 9L9 3l-3 9H2",
  logout: "M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4 M16 17l5-5-5-5 M21 12H9",
  git: "M6 3v12 M18 9a3 3 0 1 0 0-6 3 3 0 0 0 0 6z M6 21a3 3 0 1 0 0-6 3 3 0 0 0 0 6z M15 6a9 9 0 0 1-9 9",
  sparkle: "M12 3l1.9 5.6L19.5 10l-5.6 1.9L12 17l-1.9-5.1L4.5 10l5.6-1.4z M19 15l.7 2 2 .7-2 .7-.7 2-.7-2-2-.7 2-.7z",
  trash: "M3 6h18 M8 6V4a1 1 0 0 1 1-1h6a1 1 0 0 1 1 1v2 M5 6l1 14a2 2 0 0 0 2 2h8a2 2 0 0 0 2-2l1-14 M10 11v6 M14 11v6",
  edit: "M12 20h9 M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4z",
  power: "M12 2v10 M18.4 6.6a9 9 0 1 1-12.8 0",
  rotate: "M3 12a9 9 0 1 0 9-9 9 9 0 0 0-6.4 2.6L3 8 M3 3v5h5",
  eye: "M2 12s4-7 10-7 10 7 10 7-4 7-10 7-10-7-10-7z M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6z",
  key: "M15 7a4 4 0 1 0-4 4 M21 2l-6 6 M17 6l2 2 M14 8l2 2",
  settings: "M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6z M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z",
};

type IconName = string;
export function Icon({ name, size = 18, stroke = 1.7, style, className }: { name: IconName; size?: number; stroke?: number; style?: CSSProperties; className?: string }) {
  const d = ICONS[name];
  if (!d) return null;
  const paths = d.split(" M").map((p, i) => (i === 0 ? p : "M" + p));
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={stroke} strokeLinecap="round" strokeLinejoin="round" style={{ flexShrink: 0, ...style }} className={className} aria-hidden="true">
      {paths.map((p, i) => <path key={i} d={p} fill={name === "play" ? "currentColor" : "none"} />)}
    </svg>
  );
}

export type Status = "ok" | "warn" | "err" | "info" | "idle" | "accent";
export function StatusDot({ status = "ok", size = 8, pulse = false }: { status?: Status; size?: number; pulse?: boolean }) {
  const c = status === "accent" ? "var(--accent)" : `var(--status-${status})`;
  return (
    <span style={{ position: "relative", display: "inline-flex", width: size, height: size, flexShrink: 0 }}>
      {pulse && <span style={{ position: "absolute", inset: 0, borderRadius: "50%", background: c, opacity: 0.55, animation: "mc-ping 1.8s cubic-bezier(0,0,.2,1) infinite" }} />}
      <span style={{ position: "relative", width: size, height: size, borderRadius: "50%", background: c, boxShadow: `0 0 0 3px color-mix(in oklch, ${c} 18%, transparent)` }} />
    </span>
  );
}

type Tone = "neutral" | "accent" | "ok" | "warn" | "err" | "info";
export function Badge({ children, tone = "neutral", icon, size = "md", style }: { children?: ReactNode; tone?: Tone; icon?: string; size?: "sm" | "md"; style?: CSSProperties }) {
  const tones: Record<Tone, { bg: string; fg: string; bd: string }> = {
    neutral: { bg: "var(--surface-2)", fg: "var(--text-dim)", bd: "var(--border)" },
    accent: { bg: "var(--accent-soft)", fg: "var(--accent)", bd: "color-mix(in oklch, var(--accent) 30%, transparent)" },
    ok: { bg: "color-mix(in oklch, var(--status-ok) 14%, transparent)", fg: "var(--status-ok)", bd: "color-mix(in oklch, var(--status-ok) 30%, transparent)" },
    warn: { bg: "color-mix(in oklch, var(--status-warn) 15%, transparent)", fg: "var(--status-warn)", bd: "color-mix(in oklch, var(--status-warn) 32%, transparent)" },
    err: { bg: "color-mix(in oklch, var(--status-err) 15%, transparent)", fg: "var(--status-err)", bd: "color-mix(in oklch, var(--status-err) 32%, transparent)" },
    info: { bg: "color-mix(in oklch, var(--status-info) 14%, transparent)", fg: "var(--status-info)", bd: "color-mix(in oklch, var(--status-info) 30%, transparent)" },
  };
  const tn = tones[tone];
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 5, padding: size === "sm" ? "2px 7px" : "3px 9px", fontSize: size === "sm" ? 11 : 12, fontWeight: 550, lineHeight: 1.5, borderRadius: 999, background: tn.bg, color: tn.fg, border: `1px solid ${tn.bd}`, whiteSpace: "nowrap", ...style }}>
      {icon && <Icon name={icon} size={size === "sm" ? 11 : 12} stroke={2} />}
      {children}
    </span>
  );
}

type ButtonVariant = "primary" | "outline" | "ghost" | "soft" | "danger" | "dangerGhost";
export function Button({ children, variant = "outline", size = "md", icon, iconRight, onClick, disabled, full, active, style, title, type }: {
  children?: ReactNode; variant?: ButtonVariant; size?: "sm" | "md" | "lg"; icon?: string; iconRight?: string;
  onClick?: () => void; disabled?: boolean; full?: boolean; active?: boolean; style?: CSSProperties; title?: string; type?: "button" | "submit";
}) {
  const [hover, setHover] = useState(false);
  const sizes = { sm: { p: "6px 11px", fs: 12.5, h: 30 }, md: { p: "8px 14px", fs: 13.5, h: 36 }, lg: { p: "11px 18px", fs: 14.5, h: 44 } };
  const s = sizes[size];
  const base: CSSProperties = { display: "inline-flex", alignItems: "center", justifyContent: "center", gap: 7, padding: s.p, minHeight: s.h, fontSize: s.fs, fontWeight: 550, fontFamily: "var(--font)", borderRadius: "var(--radius-sm)", cursor: disabled ? "not-allowed" : "pointer", border: "1px solid transparent", transition: "all .14s ease", whiteSpace: "nowrap", width: full ? "100%" : undefined, opacity: disabled ? 0.5 : 1, userSelect: "none" };
  const variants: Record<ButtonVariant, CSSProperties> = {
    primary: { background: hover && !disabled ? "color-mix(in oklch, var(--accent) 88%, white)" : "var(--accent)", color: "var(--accent-fg)", boxShadow: "0 1px 2px oklch(0 0 0 /0.3)" },
    outline: { background: hover && !disabled ? "var(--hover)" : "var(--surface)", color: "var(--text)", borderColor: "var(--border-strong)" },
    ghost: { background: hover && !disabled ? "var(--hover)" : "transparent", color: active ? "var(--text)" : "var(--text-dim)" },
    soft: { background: hover && !disabled ? "color-mix(in oklch, var(--accent) 22%, transparent)" : "var(--accent-soft)", color: "var(--accent)" },
    danger: { background: hover && !disabled ? "color-mix(in oklch, var(--status-err) 90%, white)" : "color-mix(in oklch, var(--status-err) 92%, black)", color: "white" },
    dangerGhost: { background: hover && !disabled ? "color-mix(in oklch, var(--status-err) 14%, transparent)" : "transparent", color: "var(--status-err)", borderColor: hover ? "color-mix(in oklch, var(--status-err) 35%, transparent)" : "var(--border)" },
  };
  return (
    <button type={type || "button"} title={title} onMouseEnter={() => setHover(true)} onMouseLeave={() => setHover(false)} onClick={disabled ? undefined : onClick} disabled={disabled} style={{ ...base, ...variants[variant], ...style }}>
      {icon && <Icon name={icon} size={s.fs + 2} stroke={1.9} />}
      {children}
      {iconRight && <Icon name={iconRight} size={s.fs + 1} stroke={1.9} />}
    </button>
  );
}

export function Card({ children, pad = true, style, hover = false, onClick, className }: { children?: ReactNode; pad?: boolean; style?: CSSProperties; hover?: boolean; onClick?: () => void; className?: string }) {
  const [h, setH] = useState(false);
  return (
    <div onClick={onClick} onMouseEnter={() => setH(true)} onMouseLeave={() => setH(false)} className={className}
      style={{ background: "var(--surface)", border: `1px solid ${hover && h ? "var(--border-strong)" : "var(--border)"}`, borderRadius: "var(--radius)", boxShadow: "var(--shadow)", padding: pad ? "var(--pad)" : 0, transition: "border-color .15s ease, transform .15s ease", cursor: onClick ? "pointer" : "default", transform: hover && h ? "translateY(-1px)" : "none", ...style }}>
      {children}
    </div>
  );
}

export function SectionTitle({ children, sub, right, icon }: { children?: ReactNode; sub?: ReactNode; right?: ReactNode; icon?: string }) {
  return (
    <div style={{ display: "flex", alignItems: "flex-start", justifyContent: "space-between", gap: 16, marginBottom: 16 }}>
      <div style={{ display: "flex", gap: 11, alignItems: "center" }}>
        {icon && <div style={{ display: "grid", placeItems: "center", width: 34, height: 34, borderRadius: "var(--radius-sm)", background: "var(--accent-soft)", color: "var(--accent)" }}><Icon name={icon} size={18} /></div>}
        <div>
          <h2 style={{ margin: 0, fontSize: 16.5, fontWeight: 600, letterSpacing: "var(--head-tracking)", color: "var(--text)" }}>{children}</h2>
          {sub && <p style={{ margin: "3px 0 0", fontSize: 13, color: "var(--text-faint)" }}>{sub}</p>}
        </div>
      </div>
      {right}
    </div>
  );
}

export function Toggle({ checked, onChange, size = 20 }: { checked: boolean; onChange: (v: boolean) => void; size?: number }) {
  return (
    <button onClick={() => onChange(!checked)} style={{ width: size * 1.8, height: size, borderRadius: 999, border: "none", background: checked ? "var(--accent)" : "var(--surface-2)", position: "relative", cursor: "pointer", transition: "background .18s ease", padding: 0, flexShrink: 0 }}>
      <span style={{ position: "absolute", top: 2, left: checked ? size * 0.8 + 2 : 2, width: size - 4, height: size - 4, borderRadius: "50%", background: checked ? "var(--accent-fg)" : "var(--text-faint)", transition: "left .18s ease, background .18s ease", boxShadow: "0 1px 2px rgba(0,0,0,.4)" }} />
    </button>
  );
}

export function Meter({ value, max = 100, tone = "accent", height = 6 }: { value: number; max?: number; tone?: "accent" | "auto" | "ok" | "warn" | "err" | "info"; height?: number }) {
  const pct = Math.min(100, (value / max) * 100);
  const color = tone === "accent" ? "var(--accent)" : tone === "auto" ? (pct > 85 ? "var(--status-err)" : pct > 65 ? "var(--status-warn)" : "var(--status-ok)") : `var(--status-${tone})`;
  return (
    <div style={{ height, background: "var(--surface-2)", borderRadius: 999, overflow: "hidden" }}>
      <div style={{ width: pct + "%", height: "100%", background: color, borderRadius: 999, transition: "width .5s ease" }} />
    </div>
  );
}

export function Sparkline({ data, w = 120, h = 34, color = "var(--accent)", fill = true, strokeW = 1.6 }: { data: number[]; w?: number; h?: number; color?: string; fill?: boolean; strokeW?: number }) {
  if (!data || data.length < 2) return <svg width={w} height={h} />;
  const min = Math.min(...data), max = Math.max(...data);
  const range = max - min || 1;
  const pts = data.map((v, i) => [(i / (data.length - 1)) * w, h - 3 - ((v - min) / range) * (h - 6)]);
  const line = pts.map((p, i) => (i === 0 ? "M" : "L") + p[0].toFixed(1) + " " + p[1].toFixed(1)).join(" ");
  const area = line + ` L${w} ${h} L0 ${h} Z`;
  const gid = "sg" + Math.random().toString(36).slice(2, 7);
  return (
    <svg width={w} height={h} style={{ display: "block", overflow: "visible" }}>
      <defs><linearGradient id={gid} x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stopColor={color} stopOpacity="0.28" /><stop offset="100%" stopColor={color} stopOpacity="0" /></linearGradient></defs>
      {fill && <path d={area} fill={`url(#${gid})`} />}
      <path d={line} fill="none" stroke={color} strokeWidth={strokeW} strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

export function Avatar({ name, size = 32, accent }: { name: string; size?: number; accent?: boolean }) {
  const initials = (name || "?").split(/[ @._-]/).filter(Boolean).slice(0, 2).map((s) => s[0]).join("").toUpperCase();
  const hue = [...(name || "x")].reduce((a, c) => a + c.charCodeAt(0), 0) % 360;
  return (
    <div style={{ width: size, height: size, borderRadius: "30%", flexShrink: 0, display: "grid", placeItems: "center", background: accent ? "var(--accent-soft)" : `oklch(0.40 0.08 ${hue})`, color: accent ? "var(--accent)" : `oklch(0.93 0.04 ${hue})`, fontSize: size * 0.36, fontWeight: 600, letterSpacing: "-0.02em" }}>{initials}</div>
  );
}

export function Tabs<T extends string>({ tabs, active, onChange, size = "md" }: { tabs: { id: T; label: string; icon?: string; count?: number }[]; active: T; onChange: (id: T) => void; size?: "sm" | "md" }) {
  return (
    <div style={{ display: "flex", gap: 2, padding: 3, background: "var(--panel)", borderRadius: "var(--radius-sm)", border: "1px solid var(--border-soft)", width: "fit-content" }}>
      {tabs.map((t) => {
        const on = active === t.id;
        return (
          <button key={t.id} onClick={() => onChange(t.id)} style={{ display: "inline-flex", alignItems: "center", gap: 6, padding: size === "sm" ? "5px 11px" : "7px 14px", fontSize: size === "sm" ? 12.5 : 13.5, fontWeight: 550, fontFamily: "var(--font)", border: "none", cursor: "pointer", borderRadius: "calc(var(--radius-sm) - 2px)", background: on ? "var(--surface-2)" : "transparent", color: on ? "var(--text)" : "var(--text-faint)", transition: "all .13s ease", boxShadow: on ? "0 1px 2px rgba(0,0,0,.25)" : "none" }}>
            {t.icon && <Icon name={t.icon} size={14} />}{t.label}{t.count != null && <span style={{ fontSize: 11, opacity: 0.7, fontFamily: "var(--mono)" }}>{t.count}</span>}
          </button>
        );
      })}
    </div>
  );
}

export function EmptyState({ icon = "sparkle", title, sub, action }: { icon?: string; title: string; sub?: string; action?: ReactNode }) {
  return (
    <div style={{ display: "flex", flexDirection: "column", alignItems: "center", justifyContent: "center", padding: "56px 24px", textAlign: "center", gap: 6 }}>
      <div style={{ display: "grid", placeItems: "center", width: 52, height: 52, borderRadius: "var(--radius)", background: "var(--surface-2)", color: "var(--text-faint)", marginBottom: 6 }}><Icon name={icon} size={24} /></div>
      <div style={{ fontSize: 15, fontWeight: 600, color: "var(--text)" }}>{title}</div>
      {sub && <div style={{ fontSize: 13, color: "var(--text-faint)", maxWidth: 340 }}>{sub}</div>}
      {action && <div style={{ marginTop: 10 }}>{action}</div>}
    </div>
  );
}

export function Kbd({ children }: { children: ReactNode }) {
  return <kbd style={{ fontFamily: "var(--mono)", fontSize: 11, padding: "2px 6px", borderRadius: 5, background: "var(--surface-2)", border: "1px solid var(--border)", color: "var(--text-dim)" }}>{children}</kbd>;
}

export function Spinner({ size = 16 }: { size?: number }) {
  return <span style={{ display: "inline-block", width: size, height: size, border: "2px solid var(--border-strong)", borderTopColor: "var(--accent)", borderRadius: "50%", animation: "mc-spin .7s linear infinite" }} />;
}

/** Confirmation for an action that cannot be casually undone.
 *
 *  The body is a slot rather than a string because "are you sure?" only asks the
 *  operator to confirm they pressed the button they pressed. What they need is the
 *  consequence — and for the MAS user actions the consequence is routinely *not*
 *  what the verb implies (locking does not end sessions; revoking admin does not
 *  end an admin session). That text is the whole point of the dialog, so it gets
 *  room rather than a fixed one-liner.
 *
 *  Extracted from config/history.tsx when a second caller appeared (CLAUDE.md rule 3).
 */
export function ConfirmDialog({ open, title, children, confirmLabel, confirmIcon, confirmDisabled, tone = "danger", busy, error, onConfirm, onCancel }: {
  open: boolean;
  title: string;
  children?: ReactNode;
  confirmLabel: string;
  confirmIcon?: string;
  /** Blocks confirmation while the dialog's own input is incomplete — an empty
   *  password field should not be able to reach the server at all. */
  confirmDisabled?: boolean;
  tone?: "danger" | "primary";
  busy?: boolean;
  error?: string | null;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  // Escape closes. A modal that can only be dismissed by finding the right pixel is
  // one people click through to get rid of, which defeats a confirmation.
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape" && !busy) onCancel(); };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, busy, onCancel]);

  if (!open) return null;

  return (
    <div
      style={{ position: "fixed", inset: 0, background: "oklch(0 0 0 / 0.55)", display: "flex", alignItems: "center", justifyContent: "center", zIndex: 60, backdropFilter: "blur(2px)", padding: 16 }}
      onClick={() => { if (!busy) onCancel(); }}
    >
      <div onClick={(e) => e.stopPropagation()} role="dialog" aria-modal="true"
        style={{ background: "var(--surface)", border: "1px solid var(--border)", borderRadius: "var(--radius-lg)", padding: 24, maxWidth: 480, width: "100%", boxShadow: "0 24px 60px -12px oklch(0 0 0 / 0.6)" }}>
        <div style={{ display: "flex", gap: 12, marginBottom: 18 }}>
          <Icon name={tone === "danger" ? "alert" : "info"} size={20} style={{ color: tone === "danger" ? "var(--status-warn)" : "var(--text-dim)", flexShrink: 0, marginTop: 1 }} />
          <div style={{ minWidth: 0 }}>
            <h2 style={{ margin: 0, fontSize: 15, fontWeight: 650, color: "var(--text)" }}>{title}</h2>
            <div style={{ margin: "6px 0 0", fontSize: 13, color: "var(--text-dim)", lineHeight: 1.6 }}>{children}</div>
          </div>
        </div>
        <div style={{ display: "flex", gap: 8, justifyContent: "flex-end" }}>
          <Button variant="ghost" size="sm" disabled={busy} onClick={onCancel}>Abbrechen</Button>
          <Button variant={tone === "danger" ? "danger" : "primary"} size="sm" icon={busy ? undefined : confirmIcon} disabled={busy || confirmDisabled} onClick={onConfirm}>
            {busy ? <><Spinner size={13} /> Läuft…</> : confirmLabel}
          </Button>
        </div>
        {error && <p style={{ margin: "10px 0 0", fontSize: 12, color: "var(--status-err)" }}>{error}</p>}
      </div>
    </div>
  );
}
