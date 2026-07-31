import { useState } from "react";
import { useTweaks, DIRECTIONS, ACCENTS, DENSITIES, setTweak, type Accent } from "@/lib/theme";
import { Icon, Toggle } from "@/components/mc";

const ACCENT_PREVIEW: Record<Accent, string> = {
  default: "var(--accent)",
  blue: "oklch(0.64 0.155 256)",
  cyan: "oklch(0.74 0.135 197)",
  violet: "oklch(0.64 0.150 286)",
  green: "oklch(0.70 0.155 152)",
  amber: "oklch(0.76 0.145 70)",
};
const DIR_SWATCH: Record<string, [string, string, string]> = {
  aura: ["oklch(0.205 0.011 256)", "oklch(0.64 0.155 256)", "oklch(0.40 0.015 256)"],
  carbon: ["oklch(0.183 0 0)", "oklch(0.74 0.135 197)", "oklch(0.36 0 0)"],
  graphite: ["oklch(0.232 0.009 64)", "oklch(0.64 0.145 286)", "oklch(0.42 0.013 64)"],
};

function Sec({ label }: { label: string }) {
  return <div style={{ fontSize: 10.5, fontWeight: 600, letterSpacing: "0.06em", textTransform: "uppercase", color: "var(--text-faint)", margin: "18px 0 9px" }}>{label}</div>;
}

export function TweaksButton() {
  const [open, setOpen] = useState(false);
  const [t] = useTweaks();

  return (
    <>
      <button onClick={() => setOpen(true)} title="Design anpassen"
        style={{ background: "var(--surface)", border: "1px solid var(--border)", borderRadius: "var(--radius-sm)", width: 36, height: 36, display: "grid", placeItems: "center", color: "var(--text-dim)", cursor: "pointer" }}>
        <Icon name="settings" size={17} />
      </button>

      {open && (
        <>
          <div onClick={() => setOpen(false)} style={{ position: "fixed", inset: 0, background: "oklch(0 0 0 / 0.5)", zIndex: 60, backdropFilter: "blur(2px)" }} />
          <aside className="mc-scroll" style={{ position: "fixed", top: 0, right: 0, height: "100vh", width: 320, background: "var(--panel)", borderLeft: "1px solid var(--border)", zIndex: 61, overflowY: "auto", padding: "18px 18px 40px", boxShadow: "-12px 0 40px -12px oklch(0 0 0 / 0.5)" }}>
            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: 4 }}>
              <span style={{ fontSize: 15, fontWeight: 650, letterSpacing: "var(--head-tracking)", color: "var(--text)" }}>Design</span>
              <button onClick={() => setOpen(false)} style={{ background: "transparent", border: "none", color: "var(--text-faint)", cursor: "pointer", padding: 4 }}><Icon name="x" size={18} /></button>
            </div>

            <Sec label="Visuelle Richtung" />
            <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
              {DIRECTIONS.map((d) => {
                const on = t.direction === d.id;
                return (
                  <button key={d.id} onClick={() => setTweak("direction", d.id)} style={{ display: "flex", alignItems: "center", gap: 10, padding: "9px 11px", borderRadius: 9, cursor: "pointer", textAlign: "left", border: `1px solid ${on ? "var(--accent)" : "var(--border)"}`, background: on ? "var(--accent-soft)" : "var(--surface)" }}>
                    <span style={{ display: "flex", gap: 3 }}>{DIR_SWATCH[d.id].map((c, i) => <span key={i} style={{ width: 12, height: 20, borderRadius: 3, background: c, border: "1px solid rgba(255,255,255,.08)" }} />)}</span>
                    <span style={{ flex: 1 }}><span style={{ display: "block", fontSize: 13, fontWeight: 600, color: "var(--text)" }}>{d.label}</span><span style={{ display: "block", fontSize: 11, color: "var(--text-faint)" }}>{d.blurb}</span></span>
                    {on && <Icon name="check" size={15} style={{ color: "var(--accent)" }} />}
                  </button>
                );
              })}
            </div>

            <Sec label="Akzentfarbe" />
            <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
              {ACCENTS.map((a) => {
                const on = t.accent === a;
                return <button key={a} title={a} onClick={() => setTweak("accent", a)} style={{ width: 28, height: 28, borderRadius: 8, cursor: "pointer", background: ACCENT_PREVIEW[a], border: on ? "2px solid var(--text)" : "2px solid transparent", boxShadow: on ? "0 0 0 2px var(--bg)" : "none", outline: "1px solid var(--border)" }} />;
              })}
            </div>

            <Sec label="Dichte" />
            <div style={{ display: "flex", gap: 6, background: "var(--surface)", border: "1px solid var(--border)", borderRadius: 9, padding: 3 }}>
              {DENSITIES.map((d) => (
                <button key={d} onClick={() => setTweak("density", d)} style={{ flex: 1, padding: "7px 0", borderRadius: 6, border: "none", cursor: "pointer", fontSize: 12.5, fontWeight: 550, textTransform: "capitalize", background: t.density === d ? "var(--surface-2)" : "transparent", color: t.density === d ? "var(--text)" : "var(--text-faint)" }}>{d}</button>
              ))}
            </div>

            <Sec label="Details" />
            {([["gridBg", "Hintergrund-Raster"], ["monoLabels", "Nav-Labels monospace"], ["showPhaseTags", "Phasen-Tags zeigen"]] as const).map(([k, label]) => (
              <div key={k} style={{ display: "flex", alignItems: "center", justifyContent: "space-between", padding: "9px 0", borderBottom: "1px solid var(--border-soft)" }}>
                <span style={{ fontSize: 13, color: "var(--text-dim)" }}>{label}</span>
                <Toggle checked={t[k]} onChange={(v) => setTweak(k, v)} />
              </div>
            ))}
          </aside>
        </>
      )}
    </>
  );
}
