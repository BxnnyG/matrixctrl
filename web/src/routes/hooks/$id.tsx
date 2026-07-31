import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Card, Icon, Badge, Button, SectionTitle, StatusDot, Spinner, EmptyState } from "@/components/mc";

export const Route = createFileRoute("/hooks/$id")({
  component: HookDetail,
});

interface HookAction {
  type: string;
  description?: string;
  resource?: string;
  name?: string;
  namespace?: string;
  patch_type?: string;
  patch?: string;
  timeout_secs?: number;
}
interface HookDetailT {
  id: string;
  name: string;
  description?: string;
  trigger: string;
  enabled: boolean;
  priority: number;
  builtin: boolean;
  actions: HookAction[];
}
interface ActionResult { action_index: number; type: string; status: string; error?: string; duration_ms: number }
interface HookRun {
  id: string;
  status: string;
  ts_start: string;
  ts_end?: string;
  trigger_type: string;
  action_results: ActionResult[];
  triggered_by: string;
}

const RUN_DOT: Record<string, "ok" | "err" | "warn" | "info" | "idle"> = {
  success: "ok", failed: "err", partial: "warn", running: "info",
};

function HookDetail() {
  const { id } = Route.useParams();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [triggering, setTriggering] = useState(false);
  const [expandedRun, setExpandedRun] = useState<string | null>(null);

  const { data: hook } = useQuery({
    queryKey: ["hooks", id],
    queryFn: () => api.get<HookDetailT>(`/api/v1/hooks/${id}`),
  });
  const { data: runs } = useQuery({
    queryKey: ["hooks", id, "runs"],
    queryFn: () => api.get<HookRun[]>(`/api/v1/hooks/${id}/runs`),
    refetchInterval: 5_000,
  });

  const trigger = useMutation({
    mutationFn: () => { setTriggering(true); return api.post(`/api/v1/hooks/${id}/trigger`, {}); },
    onSettled: () => { setTriggering(false); qc.invalidateQueries({ queryKey: ["hooks", id, "runs"] }); },
  });

  if (!hook) return <div style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 13, color: "var(--text-faint)" }}><Spinner size={14} /> Lade…</div>;

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 20, maxWidth: 820 }}>
      <div style={{ display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap" }}>
        <Button variant="ghost" size="sm" icon="chevLeft" onClick={() => navigate({ to: "/hooks" })}>Hooks</Button>
        <h1 style={{ margin: 0, fontSize: 19, fontWeight: 650, letterSpacing: "-0.02em", color: "var(--text)" }}>{hook.name}</h1>
        {hook.builtin && <Badge tone="accent" size="sm">Built-in</Badge>}
        {!hook.enabled && <Badge tone="neutral" size="sm">Deaktiviert</Badge>}
      </div>

      {/* Meta */}
      <Card>
        <div style={{ display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: 18 }}>
          {[["Trigger", <code key="t" style={{ fontFamily: "var(--mono)", fontSize: 12, background: "var(--surface-2)", padding: "2px 7px", borderRadius: 5, color: "var(--text-dim)" }}>{hook.trigger}</code>],
            ["Priorität", <span key="p" style={{ fontWeight: 600, color: "var(--text)" }}>{hook.priority}</span>],
            ["Status", <span key="s" style={{ fontWeight: 600, color: hook.enabled ? "var(--status-ok)" : "var(--text-faint)" }}>{hook.enabled ? "Aktiv" : "Deaktiviert"}</span>]].map(([label, node]) => (
            <div key={label as string}>
              <div style={{ fontSize: 11, color: "var(--text-faint)", marginBottom: 4 }}>{label}</div>
              <div style={{ fontSize: 13 }}>{node}</div>
            </div>
          ))}
        </div>
        {hook.description && (
          <div style={{ marginTop: 16, paddingTop: 14, borderTop: "1px solid var(--border-soft)" }}>
            <div style={{ fontSize: 11, color: "var(--text-faint)", marginBottom: 4 }}>Beschreibung</div>
            <p style={{ margin: 0, fontSize: 12.5, color: "var(--text-dim)", lineHeight: 1.55 }}>{hook.description}</p>
          </div>
        )}
      </Card>

      {/* Actions */}
      <Card>
        <SectionTitle sub={`${hook.actions.length} ${hook.actions.length === 1 ? "Aktion" : "Aktionen"} in Reihenfolge`}>Aktionen</SectionTitle>
        <div style={{ display: "flex", flexDirection: "column", gap: 0 }}>
          {hook.actions.map((action, i) => (
            <div key={i} style={{ display: "flex", gap: 12, padding: "12px 0", borderTop: i > 0 ? "1px solid var(--border-soft)" : "none" }}>
              <span style={{ display: "grid", placeItems: "center", width: 22, height: 22, borderRadius: 6, background: "var(--surface-2)", color: "var(--text-faint)", fontSize: 11, fontWeight: 600, fontFamily: "var(--mono)", flexShrink: 0 }}>{i + 1}</span>
              <div style={{ minWidth: 0, display: "flex", flexDirection: "column", gap: 4 }}>
                <code style={{ fontFamily: "var(--mono)", fontSize: 12, background: "var(--surface-2)", padding: "2px 7px", borderRadius: 5, color: "var(--accent)", width: "fit-content" }}>{action.type}</code>
                {action.description && <p style={{ margin: 0, fontSize: 12.5, color: "var(--text-dim)" }}>{action.description}</p>}
                {action.resource && <p style={{ margin: 0, fontSize: 11.5, fontFamily: "var(--mono)", color: "var(--text-faint)" }}>{action.resource}/{action.namespace}/{action.name}{action.patch_type && ` (${action.patch_type})`}</p>}
                {action.timeout_secs && <p style={{ margin: 0, fontSize: 11.5, color: "var(--text-faint)" }}>Timeout: {action.timeout_secs}s</p>}
              </div>
            </div>
          ))}
        </div>
      </Card>

      {/* Trigger */}
      <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
        <Button variant="primary" icon={triggering ? undefined : "play"} disabled={!hook.enabled || triggering} onClick={() => trigger.mutate()}>
          {triggering ? <><Spinner size={14} /> Läuft…</> : "Jetzt ausführen"}
        </Button>
        {trigger.isError && <span style={{ fontSize: 13, color: "var(--status-err)" }}>{(trigger.error as Error).message}</span>}
        {trigger.isSuccess && !triggering && <span style={{ fontSize: 13, color: "var(--status-ok)" }}>Gestartet — siehe Ausführungen unten.</span>}
      </div>

      {/* Run history */}
      <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
        <h2 style={{ margin: "0 0 2px", fontSize: 14, fontWeight: 600, letterSpacing: "var(--head-tracking)", color: "var(--text)" }}>Letzte Ausführungen</h2>
        {runs?.length === 0 && <Card pad={false}><EmptyState icon="clock" title="Noch keine Ausführungen" sub="Triggere den Hook oben, um die erste Ausführung zu sehen." /></Card>}
        {runs?.map((run) => {
          const expanded = expandedRun === run.id;
          const durationSec = run.ts_end ? ((new Date(run.ts_end).getTime() - new Date(run.ts_start).getTime()) / 1000).toFixed(1) : null;
          return (
            <Card key={run.id} pad={false} style={{ overflow: "hidden" }}>
              <button onClick={() => setExpandedRun(expanded ? null : run.id)} className="mc-row"
                style={{ width: "100%", display: "flex", alignItems: "center", gap: 12, padding: "13px 16px", background: "transparent", border: "none", cursor: "pointer", textAlign: "left" }}>
                <StatusDot status={RUN_DOT[run.status] ?? "idle"} pulse={run.status === "running"} />
                <div style={{ flex: 1, fontSize: 13 }}>
                  <span style={{ fontWeight: 600, textTransform: "capitalize", color: "var(--text)" }}>{run.status}</span>
                  <span style={{ color: "var(--text-faint)", marginLeft: 8, fontSize: 12 }}>{new Date(run.ts_start).toLocaleString("de-DE")}</span>
                  {durationSec && <span style={{ color: "var(--text-faint)", marginLeft: 8, fontSize: 12, fontFamily: "var(--mono)" }}>{durationSec}s</span>}
                </div>
                <Badge tone="neutral" size="sm">{run.trigger_type}</Badge>
                <Icon name={expanded ? "chevDown" : "chevRight"} size={16} style={{ color: "var(--text-faint)" }} />
              </button>
              {expanded && (
                <div style={{ borderTop: "1px solid var(--border-soft)", padding: "10px 16px", display: "flex", flexDirection: "column", gap: 8, background: "var(--panel)" }}>
                  {run.action_results.length > 0 ? run.action_results.map((r) => (
                    <div key={r.action_index} style={{ display: "flex", gap: 9, fontSize: 12 }}>
                      <Icon name={r.status === "success" ? "check" : "x"} size={14} stroke={2.2} style={{ color: r.status === "success" ? "var(--status-ok)" : "var(--status-err)", flexShrink: 0, marginTop: 1 }} />
                      <div style={{ minWidth: 0 }}>
                        <span style={{ color: "var(--text-dim)" }}>Aktion {r.action_index + 1}</span>
                        <code style={{ marginLeft: 6, fontFamily: "var(--mono)", background: "var(--surface-2)", padding: "1px 5px", borderRadius: 4, color: "var(--text-dim)" }}>{r.type}</code>
                        <span style={{ color: "var(--text-faint)", marginLeft: 6, fontFamily: "var(--mono)" }}>{r.duration_ms}ms</span>
                        {r.error && <p style={{ margin: "3px 0 0", color: "var(--status-err)", lineHeight: 1.5 }}>{r.error}</p>}
                      </div>
                    </div>
                  )) : <div style={{ fontSize: 12, color: "var(--text-faint)" }}>Keine Aktions-Details verfügbar.</div>}
                </div>
              )}
            </Card>
          );
        })}
      </div>
    </div>
  );
}
