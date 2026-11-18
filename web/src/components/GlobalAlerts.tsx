import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { api } from "@/lib/api";
import { useSession } from "@/lib/session";
import { Icon } from "@/components/mc";

export interface AlertItem {
  key: string;
  trigger?: string;
  severity: "error" | "warning" | "info";
  title: string;
  detail?: string;
  link?: string;
}
interface AlertsResp { alerts: AlertItem[]; checked_at: string | null; unknown: string[] }

const RANK = { error: 0, warning: 1, info: 2 } as const;
const COLOR = { error: "var(--status-err)", warning: "var(--status-warn)", info: "var(--accent)" } as const;
const DISMISS_KEY = "mc.alerts.dismissed";

/** Errors first, then warnings, then info; stable inside one severity. */
export function sortAlerts(a: AlertItem[]): AlertItem[] {
  return a.map((x, i) => [x, i] as const)
    .sort((p, q) => (RANK[p[0].severity] ?? 3) - (RANK[q[0].severity] ?? 3) || p[1] - q[1])
    .map(([x]) => x);
}

function readDismissed(): string[] {
  try {
    const v = JSON.parse(localStorage.getItem(DISMISS_KEY) || "[]");
    return Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : [];
  } catch { return []; }
}
function writeDismissed(keys: string[]) {
  try { localStorage.setItem(DISMISS_KEY, JSON.stringify(keys)); } catch { /* private mode: dismissal lasts until reload */ }
}

const short = (s: string, n = 140) => (s.length > n ? s.slice(0, n - 1) + "…" : s);

export const EMERGENCY_ALERT: AlertItem = {
  key: "emergency-login", severity: "warning", title: "Angemeldet per Notzugang",
  detail: "Der Notzugang ist kein Matrix-Konto. Mit dem Matrix-Login anmelden, sobald er eingerichtet ist.", link: "/setup",
};

/** One compact row per current problem, on every page. Only the update hint (info)
 *  can be dismissed — problems stay until they are gone. */
export function GlobalAlerts() {
  const navigate = useNavigate();
  const session = useSession();
  const [dismissed, setDismissed] = useState<string[]>(readDismissed);
  const { data, isError } = useQuery({
    queryKey: ["alerts"],
    queryFn: () => api.get<AlertsResp>("/api/v1/alerts"),
    refetchInterval: 60_000,
    retry: false,
  });
  return <AlertsBar alerts={data?.alerts ?? []} failed={isError && !data} emergency={session.bootstrap}
    dismissed={dismissed} onDismiss={(k) => { const n = [...dismissed, k]; setDismissed(n); writeDismissed(n); }}
    onOpen={(to) => navigate({ to })} />;
}

export function AlertsBar({ alerts, failed, emergency, dismissed, onDismiss, onOpen }: {
  alerts: AlertItem[]; failed: boolean; emergency: boolean; dismissed: string[];
  onDismiss: (key: string) => void; onOpen: (to: string) => void;
}) {
  const all = [...alerts, ...(emergency ? [EMERGENCY_ALERT] : [])];
  const rows = sortAlerts(all.filter((a) => a.severity !== "info" || !dismissed.includes(a.key)));
  if (!failed && rows.length === 0) return null;
  const row = (color: string, key: string, children: React.ReactNode) => (
    <div key={key} role="alert" style={{ display: "flex", alignItems: "center", gap: 10, padding: "7px 26px", fontSize: 12.5, color: "var(--text)",
      background: `color-mix(in oklch, ${color} 12%, var(--bg))`, borderBottom: "1px solid var(--border)", borderLeft: `3px solid ${color}` }}>{children}</div>
  );
  return (
    <div data-testid="global-alerts">
      {failed && row("var(--status-warn)", "_failed", <><Icon name="alert" size={14} style={{ color: "var(--status-warn)" }} /><span>Warnungen nicht abrufbar</span></>)}
      {rows.map((a) => row(COLOR[a.severity] ?? COLOR.info, a.key, <>
        <Icon name={a.severity === "info" ? "info" : "alert"} size={14} style={{ color: COLOR[a.severity] ?? COLOR.info, flexShrink: 0 }} />
        <span style={{ fontWeight: 600, flexShrink: 0 }}>{a.title}</span>
        {a.detail && <span style={{ color: "var(--text-dim)", flex: 1, minWidth: 0, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }} title={a.detail}>{short(a.detail)}</span>}
        {!a.detail && <span style={{ flex: 1 }} />}
        {a.link && <button onClick={() => onOpen(a.link!)} style={{ background: "none", border: "none", color: "var(--accent)", cursor: "pointer", fontSize: 12.5, flexShrink: 0 }}>Ansehen</button>}
        {a.severity === "info" && <button aria-label="Ausblenden" onClick={() => onDismiss(a.key)} style={{ background: "none", border: "none", color: "var(--text-faint)", cursor: "pointer", flexShrink: 0 }}>×</button>}
      </>))}
    </div>
  );
}
