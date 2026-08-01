import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Card, Icon, Badge, Button, StatusDot, EmptyState } from "@/components/mc";

export const Route = createFileRoute("/helm/history")({
  component: HelmHistory,
});

interface UpgradeEntry {
  id: string;
  from_version: string;
  to_version: string;
  status: string;
  ts_initiated: string;
  helm_revision?: number;
}

const essVersion = (v: string) => v.replace(/^matrix-stack-/, "");

// Every status the backend can write. "running-hooks" and "interrupted" were
// missing and fell through to the "pending" styling, so an upgrade that had been
// dead for a day rendered as calmly in-progress (BACKLOG P2-16).
const STATUS_MAP: Record<
  string,
  { tone: "ok" | "err" | "warn" | "info"; dot: "ok" | "err" | "warn" | "info"; label: string }
> = {
  success: { tone: "ok", dot: "ok", label: "Erfolgreich" },
  failed: { tone: "err", dot: "err", label: "Fehlgeschlagen" },
  "hooks-failed": { tone: "warn", dot: "warn", label: "Hooks fehlgeschlagen" },
  interrupted: { tone: "warn", dot: "warn", label: "Abgebrochen" },
  pending: { tone: "info", dot: "info", label: "Wartet" },
  running: { tone: "info", dot: "info", label: "Läuft" },
  "running-hooks": { tone: "info", dot: "info", label: "Hooks laufen" },
};

function HelmHistory() {
  const navigate = useNavigate();
  const { data: history } = useQuery({
    queryKey: ["helm", "history"],
    queryFn: () => api.get<UpgradeEntry[]>("/api/v1/helm/releases/ess/history"),
  });

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 18 }}>
      <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
        <Button variant="ghost" size="sm" icon="chevLeft" onClick={() => navigate({ to: "/helm" })}>Release</Button>
      </div>

      <Card pad={false}>
        <div style={{ padding: "14px 18px 4px", fontSize: 10.5, fontWeight: 600, letterSpacing: "0.06em", textTransform: "uppercase", color: "var(--text-faint)" }}>
          {history?.length ? `${history.length} Upgrade${history.length > 1 ? "s" : ""}` : "Verlauf"}
        </div>
        <div style={{ padding: "0 8px 8px" }}>
          {history && history.length > 0 ? history.map((e, i) => {
            // An unknown status must not borrow "pending"'s calm blue — show it
            // raw and flag it, rather than reassure about something unrecognised.
            const st = STATUS_MAP[e.status] ?? { tone: "warn" as const, dot: "warn" as const, label: e.status };
            return (
              <div key={e.id} style={{ display: "flex", alignItems: "center", gap: 14, padding: "13px 14px", borderRadius: "var(--radius-sm)", borderBottom: i < history.length - 1 ? "1px solid var(--border-soft)" : "none" }}>
                <div style={{ display: "grid", placeItems: "center", width: 34, height: 34, borderRadius: "var(--radius-sm)", background: "var(--surface-2)", color: "var(--text-dim)" }}><Icon name="rotate" size={16} /></div>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div style={{ fontFamily: "var(--mono)", fontSize: 13.5, fontWeight: 600, color: "var(--text)", display: "flex", alignItems: "center", gap: 8 }}>
                    <span>{essVersion(e.from_version)}</span>
                    <Icon name="chevRight" size={13} style={{ color: "var(--text-faint)" }} />
                    <span style={{ color: "var(--accent)" }}>{essVersion(e.to_version)}</span>
                  </div>
                  <div style={{ fontSize: 11.5, color: "var(--text-faint)", marginTop: 2 }}>
                    {new Date(e.ts_initiated).toLocaleString("de-DE")}{e.helm_revision != null && ` · Revision #${e.helm_revision}`}
                  </div>
                </div>
                <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                  <StatusDot status={st.dot} pulse={e.status === "running" || e.status === "running-hooks"} />
                  <Badge tone={st.tone} size="sm">{st.label}</Badge>
                </div>
              </div>
            );
          }) : <EmptyState icon="rotate" title="Noch keine Upgrades" sub="Upgrades, die du über MatrixCtrl ausführst, erscheinen hier mit Status und Hook-Ergebnis." />}
        </div>
      </Card>
    </div>
  );
}
