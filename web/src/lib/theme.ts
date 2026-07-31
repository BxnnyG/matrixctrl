import { useSyncExternalStore } from "react";

// Theme tweaks store — direction / accent / density + detail toggles.
// Persisted to localStorage and applied as data-attributes on <html> so the CSS
// token system (index.css) re-themes the whole app live.

export type Direction = "aura" | "carbon" | "graphite";
export type Accent = "default" | "blue" | "cyan" | "violet" | "green" | "amber";
export type Density = "compact" | "regular" | "comfy";

export interface Tweaks {
  direction: Direction;
  accent: Accent;
  density: Density;
  gridBg: boolean;
  monoLabels: boolean;
  showPhaseTags: boolean;
}

export const DIRECTIONS: { id: Direction; label: string; blurb: string }[] = [
  { id: "aura", label: "Aura", blurb: "Ruhig & luftig · UniFi/Vercel" },
  { id: "carbon", label: "Carbon", blurb: "Dicht & technisch · Ops-Konsole" },
  { id: "graphite", label: "Graphite", blurb: "Premium & warm · Enterprise" },
];
export const ACCENTS: Accent[] = ["default", "blue", "cyan", "violet", "green", "amber"];
export const DENSITIES: Density[] = ["compact", "regular", "comfy"];

const DEFAULTS: Tweaks = {
  direction: "aura",
  accent: "default",
  density: "regular",
  gridBg: true,
  monoLabels: false,
  showPhaseTags: true,
};

const KEY = "matrixctrl_tweaks";

function load(): Tweaks {
  try {
    const raw = localStorage.getItem(KEY);
    if (raw) return { ...DEFAULTS, ...JSON.parse(raw) };
  } catch {
    /* ignore */
  }
  return { ...DEFAULTS };
}

let state: Tweaks = load();
const listeners = new Set<() => void>();

function apply(t: Tweaks) {
  const el = document.documentElement;
  el.dataset.direction = t.direction;
  el.dataset.accent = t.accent;
  el.dataset.density = t.density;
}

// Apply immediately on module load (before first paint).
apply(state);

export function setTweak<K extends keyof Tweaks>(key: K, value: Tweaks[K]) {
  state = { ...state, [key]: value };
  apply(state);
  try {
    localStorage.setItem(KEY, JSON.stringify(state));
  } catch {
    /* ignore */
  }
  listeners.forEach((l) => l());
}

function subscribe(cb: () => void) {
  listeners.add(cb);
  return () => listeners.delete(cb);
}

export function useTweaks(): [Tweaks, typeof setTweak] {
  const t = useSyncExternalStore(subscribe, () => state, () => state);
  return [t, setTweak];
}

// Back-compat shim: the app is dark-only now, so Monaco etc. always use dark.
export function useTheme() {
  return { theme: "dark" as const, toggle: () => {} };
}
