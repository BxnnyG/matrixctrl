// Drill-down behind a dashboard component row.
//
// The point of this panel is to answer "it restarted 1191 times — why?".
// Kubernetes keeps that answer in containerStatuses[].lastState.terminated
// (reason + exit code), which is what we surface first, followed by the pod's
// warning events and its logs.
import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Icon, Badge, Button, StatusDot, Spinner, EmptyState } from "@/components/mc";

export interface ContainerInfo {
  name: string;
  image: string;
  ready: boolean;
  restarts: number;
  state: string;
  state_reason?: string;
  state_message?: string;
  started_at?: string;
  last_exit_reason?: string;
  last_exit_code?: number;
  last_exit_signal?: number;
  last_exit_at?: string;
  last_exit_message?: string;
}
export interface PodDetail {
  name: string;
  phase: string;
  ready: boolean;
  restarts: number;
  /** Container carrying most of `restarts`, when one does (P2-8). */
  restarts_by?: string;
  started_at?: string;
  node: string;
  pod_ip?: string;
  reason?: string;
  message?: string;
  containers: ContainerInfo[];
  conditions?: Record<string, string>;
}
export interface EventInfo {
  type: string;
  reason: string;
  message: string;
  kind: string;
  name: string;
  component?: string;
  count: number;
  first_seen?: string;
  last_seen?: string;
}
interface ComponentDetailResponse {
  name: string;
  pods: PodDetail[];
  events: EventInfo[];
}

export function relTime(iso?: string): string {
  if (!iso) return "—";
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return "—";
  const secs = Math.max(0, Math.round((Date.now() - then) / 1000));
  if (secs < 60) return `vor ${secs}s`;
  const mins = Math.round(secs / 60);
  if (mins < 60) return `vor ${mins}min`;
  const hrs = Math.round(mins / 60);
  if (hrs < 48) return `vor ${hrs}h`;
  return `vor ${Math.round(hrs / 24)}d`;
}

// Plain-language explanation of a container exit. The raw reason/exit code is
// still shown next to it — this is the "what does that mean" line.
export function explainExit(reason?: string, code?: number): string | null {
  if (!reason && code == null) return null;
  switch (reason) {
    case "OOMKilled":
      return "Container hat sein Memory-Limit überschritten und wurde vom Kernel beendet. Limit erhöhen oder Leak suchen.";
    case "Error":
      return code === 2
        ? "Container ist mit Fehlercode 2 abgestürzt — meist ein Konfigurations- oder Startfehler. Siehe Logs."
        : `Container ist mit Fehlercode ${code} abgestürzt. Siehe Logs.`;
    case "Completed":
      return "Prozess hat sich sauber beendet (exit 0). Bei einem Dauerdienst heißt das: er läuft nicht durch.";
    case "ContainerStatusUnknown":
    case "Unknown":
      return "Zustand ging verloren — typischerweise ein Node-Neustart oder kubelet-Restart.";
    case "DeadlineExceeded":
      return "Container hat sein aktives Deadline-Limit überschritten.";
    default:
      return code != null ? `Beendet mit Exit-Code ${code}.` : null;
  }
}

const exitTone = (reason?: string): "err" | "warn" | "ok" | "idle" => {
  if (reason === "OOMKilled" || reason === "Error") return "err";
  if (reason === "Completed") return "ok";
  if (!reason) return "idle";
  return "warn";
};

function PodCard({ pod, onLogs, onRestart, restarting }: { pod: PodDetail; onLogs: (c: string) => void; onRestart: () => void; restarting: boolean }) {
  const phaseDot: "ok" | "warn" | "err" | "idle" =
    pod.phase === "Running" && pod.ready ? "ok" : pod.phase === "Running" ? "warn" : pod.phase === "Failed" ? "err" : "idle";
  return (
    <div style={{ border: "1px solid var(--border)", borderRadius: "var(--radius)", background: "var(--surface)", overflow: "hidden" }}>
      <div style={{ display: "flex", alignItems: "center", gap: 10, padding: "12px 14px", borderBottom: "1px solid var(--border-soft)" }}>
        <StatusDot status={phaseDot} pulse={pod.phase === "Pending"} />
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ fontFamily: "var(--mono)", fontSize: 12.5, fontWeight: 600, color: "var(--text)", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>{pod.name}</div>
          <div style={{ fontSize: 11, color: "var(--text-faint)", marginTop: 2 }}>
            {pod.phase} · Node {pod.node || "—"}{pod.started_at ? ` · gestartet ${relTime(pod.started_at)}` : ""}
          </div>
        </div>
        {/* Naming the container turns an alarming number into an actionable one:
            "42× Restart" reads as a failing database, "42× Restart ·
            postgres-exporter" reads as a failing sidecar (P2-8). */}
        {pod.restarts > 0 && (
          <Badge tone={pod.restarts > 20 ? "err" : "warn"} size="sm">
            {pod.restarts}× Restart{pod.restarts_by ? ` · ${pod.restarts_by}` : ""}
          </Badge>
        )}
        <Button variant="ghost" size="sm" icon="rotate" onClick={onRestart} disabled={restarting} title="Pod neu starten (löschen — Controller erstellt ihn neu)">
          {restarting ? <Spinner size={12} /> : "Neustart"}
        </Button>
      </div>

      <div style={{ padding: "4px 14px 12px" }}>
        {pod.containers.map((c) => {
          const explain = explainExit(c.last_exit_reason, c.last_exit_code);
          const tone = exitTone(c.last_exit_reason);
          return (
            <div key={c.name} style={{ padding: "10px 0", borderBottom: "1px solid var(--border-soft)" }}>
              <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
                <Icon name="server" size={13} style={{ color: "var(--text-faint)" }} />
                <span style={{ fontSize: 12.5, fontWeight: 600, color: "var(--text)", fontFamily: "var(--mono)" }}>{c.name}</span>
                <Badge tone={c.ready ? "ok" : "warn"} size="sm">{c.state}{c.state_reason ? `: ${c.state_reason}` : ""}</Badge>
                {c.restarts > 0 && <span style={{ fontSize: 11.5, color: "var(--text-faint)", fontFamily: "var(--mono)" }}>{c.restarts}×</span>}
                <div style={{ flex: 1 }} />
                <Button variant="ghost" size="sm" icon="terminal" onClick={() => onLogs(c.name)}>Logs</Button>
              </div>

              {c.state_message && (
                <div style={{ marginTop: 6, fontSize: 11.5, color: "var(--status-warn)", fontFamily: "var(--mono)", lineHeight: 1.5 }}>{c.state_message}</div>
              )}

              {/* The restart cause */}
              {(c.last_exit_reason || c.last_exit_code != null) && (
                <div style={{ marginTop: 8, padding: "9px 11px", borderRadius: "var(--radius-sm)", background: `color-mix(in oklch, var(--status-${tone === "idle" ? "info" : tone}) 9%, var(--surface-2))`, border: `1px solid color-mix(in oklch, var(--status-${tone === "idle" ? "info" : tone}) 26%, var(--border))` }}>
                  <div style={{ display: "flex", alignItems: "center", gap: 7, flexWrap: "wrap" }}>
                    <Icon name="alert" size={13} style={{ color: `var(--status-${tone === "idle" ? "info" : tone})` }} />
                    <span style={{ fontSize: 11.5, fontWeight: 600, color: "var(--text)" }}>Letzter Abbruch</span>
                    <code style={{ fontFamily: "var(--mono)", fontSize: 11.5, color: `var(--status-${tone === "idle" ? "info" : tone})` }}>
                      {c.last_exit_reason || "?"}{c.last_exit_code != null ? ` (exit ${c.last_exit_code})` : ""}
                    </code>
                    {c.last_exit_at && <span style={{ fontSize: 11, color: "var(--text-faint)" }}>· {relTime(c.last_exit_at)}</span>}
                  </div>
                  {explain && <div style={{ marginTop: 5, fontSize: 12, color: "var(--text-dim)", lineHeight: 1.5 }}>{explain}</div>}
                  {c.last_exit_message && (
                    <div style={{ marginTop: 5, fontSize: 11, fontFamily: "var(--mono)", color: "var(--text-faint)", whiteSpace: "pre-wrap", maxHeight: 80, overflow: "auto" }}>{c.last_exit_message}</div>
                  )}
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}

export function EventRow({ e, showComponent = false }: { e: EventInfo; showComponent?: boolean }) {
  const warn = e.type === "Warning";
  return (
    <div style={{ display: "flex", gap: 10, padding: "9px 0", borderBottom: "1px solid var(--border-soft)" }}>
      <Icon name={warn ? "alert" : "info"} size={14} style={{ color: warn ? "var(--status-warn)" : "var(--text-faint)", flexShrink: 0, marginTop: 2 }} />
      <div style={{ minWidth: 0, flex: 1 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 7, flexWrap: "wrap" }}>
          <span style={{ fontSize: 12, fontWeight: 600, color: warn ? "var(--status-warn)" : "var(--text)" }}>{e.reason}</span>
          {showComponent && e.component && <code style={{ fontFamily: "var(--mono)", fontSize: 10.5, color: "var(--text-faint)" }}>{e.component}</code>}
          {e.count > 1 && <span style={{ fontSize: 10.5, fontFamily: "var(--mono)", color: "var(--text-faint)", background: "var(--surface-2)", padding: "0 5px", borderRadius: 4 }}>{e.count}×</span>}
          <div style={{ flex: 1 }} />
          <span style={{ fontSize: 10.5, color: "var(--text-faint)", whiteSpace: "nowrap" }}>{relTime(e.last_seen)}</span>
        </div>
        <div style={{ fontSize: 11.5, color: "var(--text-dim)", lineHeight: 1.45, marginTop: 2, wordBreak: "break-word" }}>{e.message}</div>
      </div>
    </div>
  );
}

export function ComponentDrawer({ name, onClose }: { name: string; onClose: () => void }) {
  const qc = useQueryClient();
  const [logsFor, setLogsFor] = useState<{ pod: string; container: string } | null>(null);
  const [restartingPod, setRestartingPod] = useState<string | null>(null);

  const { data, isLoading } = useQuery({
    queryKey: ["component", name],
    queryFn: () => api.get<ComponentDetailResponse>(`/api/v1/status/components/${name}/pods`),
    refetchInterval: 15_000,
  });

  const { data: logs, isLoading: logsLoading } = useQuery({
    queryKey: ["podlogs", logsFor?.pod, logsFor?.container],
    queryFn: () => api.get<{ logs: string }>(`/api/v1/status/pods/${logsFor!.pod}/logs?tail=300&container=${encodeURIComponent(logsFor!.container)}`),
    enabled: !!logsFor,
  });

  const restart = useMutation({
    mutationFn: (pod: string) => { setRestartingPod(pod); return api.delete(`/api/v1/status/pods/${pod}`); },
    onSettled: () => {
      setRestartingPod(null);
      qc.invalidateQueries({ queryKey: ["component", name] });
      qc.invalidateQueries({ queryKey: ["status"] });
    },
  });

  const totalRestarts = data?.pods.reduce((n, p) => n + p.restarts, 0) ?? 0;
  const warnings = data?.events.filter((e) => e.type === "Warning") ?? [];

  return (
    <>
      <div onClick={onClose} style={{ position: "fixed", inset: 0, background: "oklch(0 0 0 / 0.5)", zIndex: 70, backdropFilter: "blur(2px)" }} />
      <aside className="mc-scroll" style={{ position: "fixed", top: 0, right: 0, height: "100vh", width: "min(680px, 100vw)", background: "var(--panel)", borderLeft: "1px solid var(--border)", zIndex: 71, overflowY: "auto", boxShadow: "-16px 0 50px -16px oklch(0 0 0 / 0.6)" }}>
        <div style={{ position: "sticky", top: 0, zIndex: 2, display: "flex", alignItems: "center", gap: 12, padding: "16px 20px", background: "var(--panel)", borderBottom: "1px solid var(--border)" }}>
          <div style={{ display: "grid", placeItems: "center", width: 38, height: 38, borderRadius: "var(--radius-sm)", background: "var(--accent-soft)", color: "var(--accent)", flexShrink: 0 }}><Icon name="server" size={18} /></div>
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ fontSize: 15, fontWeight: 650, color: "var(--text)", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>{name}</div>
            <div style={{ fontSize: 11.5, color: "var(--text-faint)" }}>
              {data ? `${data.pods.length} Pod${data.pods.length === 1 ? "" : "s"} · ${totalRestarts} Restarts · ${warnings.length} Warnungen` : "Lade…"}
            </div>
          </div>
          <button onClick={onClose} style={{ background: "transparent", border: "none", color: "var(--text-faint)", cursor: "pointer", padding: 4 }}><Icon name="x" size={18} /></button>
        </div>

        <div style={{ padding: 20, display: "flex", flexDirection: "column", gap: 18 }}>
          {isLoading && <div style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 13, color: "var(--text-faint)" }}><Spinner size={14} /> Lade Pod-Details…</div>}

          {data?.pods.map((p) => (
            <PodCard key={p.name} pod={p} restarting={restartingPod === p.name}
              onRestart={() => restart.mutate(p.name)}
              onLogs={(c) => setLogsFor({ pod: p.name, container: c })} />
          ))}
          {data && data.pods.length === 0 && <EmptyState icon="server" title="Keine Pods" sub="Dieses Workload hat aktuell keine laufenden Pods." />}

          {logsFor && (
            <div style={{ border: "1px solid var(--border)", borderRadius: "var(--radius)", overflow: "hidden" }}>
              <div style={{ display: "flex", alignItems: "center", gap: 8, padding: "9px 12px", background: "var(--surface-2)", borderBottom: "1px solid var(--border-soft)" }}>
                <Icon name="terminal" size={14} style={{ color: "var(--text-faint)" }} />
                <span style={{ fontFamily: "var(--mono)", fontSize: 11.5, color: "var(--text-dim)", flex: 1, minWidth: 0, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>{logsFor.pod} · {logsFor.container}</span>
                <button onClick={() => setLogsFor(null)} style={{ background: "transparent", border: "none", color: "var(--text-faint)", cursor: "pointer" }}><Icon name="x" size={15} /></button>
              </div>
              <pre className="mc-scroll" style={{ margin: 0, padding: 12, maxHeight: 320, overflow: "auto", background: "oklch(0.13 0.005 256)", fontFamily: "var(--mono)", fontSize: 11.5, lineHeight: 1.55, color: "var(--text-dim)", whiteSpace: "pre-wrap" }}>
                {logsLoading ? "Lade Logs…" : logs?.logs || "(keine Logs)"}
              </pre>
            </div>
          )}

          {data && data.events.length > 0 && (
            <div>
              <div style={{ fontSize: 10.5, fontWeight: 600, letterSpacing: "0.06em", textTransform: "uppercase", color: "var(--text-faint)", marginBottom: 6 }}>Events</div>
              <div style={{ background: "var(--surface)", border: "1px solid var(--border)", borderRadius: "var(--radius)", padding: "2px 14px 6px" }}>
                {data.events.map((e, i) => <EventRow key={i} e={e} />)}
              </div>
            </div>
          )}
        </div>
      </aside>
    </>
  );
}
