import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { useQuery, useMutation, useQueryClient, keepPreviousData } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Card, Icon, StatusDot, Badge, Button, SectionTitle, Meter, Spinner, EmptyState } from "@/components/mc";
import { ComponentDrawer, EventRow, type EventInfo } from "@/components/status/ComponentDrawer";
import type { CSSProperties } from "react";

export const Route = createFileRoute("/")({
  component: Dashboard,
});

interface DriftFinding {
  hook: string;
  action: string;
  name: string;
  status: "satisfied" | "drifted" | "unknown";
  detail: string;
  paths?: string[];
}

/** A field owned by something other than the chart — see internal/drift/manual.go.
 *  `covered` means a hook maintains it: someone went around the product, but the
 *  intent is owned. Uncovered means nothing will ever put it back. */
interface ManualEdit {
  resource: string;
  name: string;
  manager: string;
  kind: "human" | "automation" | "foreign";
  paths: string[];
  covered: boolean;
}

interface DriftResponse {
  findings: DriftFinding[];
  satisfied: number;
  drifted: number;
  unknown: number;
  manual_edits: ManualEdit[] | null;
  manual_partial: boolean;
  manual_unmaintained: number;
  manual_by_hand: number;
  manual_foreign: number;
}

interface ComponentStatus {
  name: string;
  kind?: string;
  status: string;
  ready: number;
  desired: number;
  restarts: number;
  /** Container carrying most of `restarts`, when one does. Empty means the
   *  total is the honest answer — see internal/k8s/health.go podRestarts. */
  restarts_by?: string;
  /** Whether something is in CrashLoopBackOff *right now*. `restarts` cannot
   *  answer that — it only ever grows (E53). */
  looping?: boolean;
  /** When the most recent restart happened, absent if nothing has restarted. */
  last_restart?: string;
  /** Why a `down` component is down, when the answer is "the scheduler refused it".
   *  Absent for every other kind of failure (E54). */
  unschedulable?: {
    pod: string;
    reason: string;
    cpu_request_millis?: number;
    cpu_allocatable_millis?: number;
    mem_request_mi?: number;
    mem_allocatable_mi?: number;
    exceeds_node: boolean;
  };
}
interface NodeInfo { name: string; cpu_used_millis: number; cpu_total_millis: number; mem_used_mi: number; mem_total_mi: number }
interface StatusResponse {
  release?: { name: string; chart_version: string; revision: number; status: string; deployed_at?: string };
  components: ComponentStatus[];
  nodes: NodeInfo[];
  evicted_pods: number;
}
interface SysInfoResponse {
  pvcs?: { name: string; phase: string; capacity?: string }[];
  pod_counts?: Record<string, number>;
}

const pct = (u: number, t: number) => (t ? Math.round((u / t) * 100) : 0);
const shortName = (n: string) => n.replace(/^ess-/, "").replace(/-main$/, "");

/** "vor 17 Stunden" — coarse on purpose.
 *
 *  The banner's job is to let the operator judge whether a restart count is current
 *  or historical, and minute precision does not help with that while making a stable
 *  component look like it is being watched second by second (E53). */
function sinceText(iso: string): string {
  const then = new Date(iso).getTime();
  if (!Number.isFinite(then)) return "unbekannt";
  const mins = Math.max(0, Math.round((Date.now() - then) / 60000));
  if (mins < 60) return `vor ${mins} Minute${mins === 1 ? "" : "n"}`;
  const hours = Math.round(mins / 60);
  if (hours < 48) return `vor ${hours} Stunde${hours === 1 ? "" : "n"}`;
  const days = Math.round(hours / 24);
  return `vor ${days} Tagen`;
}

// Backend reports healthy | degraded | down | scaled-zero.
const isHealthy = (c: ComponentStatus) => c.status === "healthy";
const needsAttention = (c: ComponentStatus) => c.status === "degraded" || c.status === "down";

function compIcon(name: string): string {
  const n = name.toLowerCase();
  if (n.includes("postgres") || n.includes("redis")) return "database";
  if (n.includes("authentication") || n.includes("mas")) return "key";
  if (n.includes("rtc") || n.includes("sfu")) return "phone";
  if (n.includes("element")) return "room";
  if (n.includes("well") || n.includes("known")) return "globe";
  if (n.includes("synapse")) return "server";
  if (n.includes("haproxy")) return "activity";
  return "server";
}

const STATUS_LABEL: Record<string, string> = {
  healthy: "Healthy", degraded: "Degraded", down: "Down", "scaled-zero": "Skaliert auf 0",
};
const STATUS_DOT: Record<string, "ok" | "warn" | "err" | "idle"> = {
  healthy: "ok", degraded: "warn", down: "err", "scaled-zero": "idle",
};

function MiniStat({ label, value, unit, icon, tone }: { label: string; value: string | number; unit?: string; icon: string; tone?: "ok" | "warn" | "err" }) {
  return (
    <Card style={{ display: "flex", flexDirection: "column", gap: 10 }}>
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
        <span style={{ fontSize: 12.5, color: "var(--text-faint)", fontWeight: 500 }}>{label}</span>
        <Icon name={icon} size={15} style={{ color: tone ? `var(--status-${tone})` : "var(--text-faint)" }} />
      </div>
      <div style={{ fontSize: 26, fontWeight: 650, letterSpacing: "-0.03em", fontFamily: "var(--mono)", color: "var(--text)", lineHeight: 1 }}>
        {value}{unit && <span style={{ fontSize: 12.5, color: "var(--text-faint)", fontWeight: 500, marginLeft: 4 }}>{unit}</span>}
      </div>
    </Card>
  );
}

function ComponentRow({ c, last, onClick }: { c: ComponentStatus; last: boolean; onClick: () => void }) {
  const dot = STATUS_DOT[c.status] ?? "idle";
  const hot = c.restarts > 20;
  return (
    <button onClick={onClick} className="mc-row mc-comp-row"
      style={{ display: "grid", gridTemplateColumns: "1.8fr 1fr 0.7fr 0.9fr 26px", alignItems: "center", gap: 14, padding: "11px 16px", width: "100%", textAlign: "left", border: "none", background: "transparent", cursor: "pointer", borderBottom: last ? "none" : "1px solid var(--border-soft)", borderRadius: "var(--radius-sm)" }}>
      <div style={{ display: "flex", alignItems: "center", gap: 12, minWidth: 0 }}>
        <div style={{ display: "grid", placeItems: "center", width: 36, height: 36, borderRadius: "var(--radius-sm)", background: "var(--surface-2)", color: "var(--text-dim)", flexShrink: 0 }}><Icon name={compIcon(c.name)} size={17} /></div>
        <div style={{ minWidth: 0 }}>
          <div style={{ fontSize: 13.5, fontWeight: 600, color: "var(--text)", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>{shortName(c.name)}</div>
          <div style={{ fontSize: 11, color: "var(--text-faint)", fontFamily: "var(--mono)" }}>{c.kind || "Workload"}</div>
        </div>
      </div>
      <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
        <StatusDot status={dot} pulse={c.status === "degraded"} />
        <span style={{ fontSize: 12.5, color: dot === "ok" || dot === "idle" ? "var(--text-dim)" : `var(--status-${dot})`, fontWeight: 500 }}>{STATUS_LABEL[c.status] ?? c.status}</span>
      </div>
      <span style={{ fontSize: 12.5, fontFamily: "var(--mono)", color: isHealthy(c) ? "var(--text-dim)" : "var(--status-warn)" }}>{c.ready}/{c.desired}</span>
      {/* The count alone reads as "this component is crash-looping". For a
          multi-container workload that is often false: ess-postgres showed 42
          while the database itself had restarted zero times and a monitoring
          sidecar carried all of them (P2-8). */}
      <span style={{ display: "inline-flex", alignItems: "baseline", gap: 5, fontSize: 12.5, fontFamily: "var(--mono)", color: hot ? "var(--status-err)" : c.restarts > 0 ? "var(--status-warn)" : "var(--text-faint)", minWidth: 0 }}>
        <span style={{ display: "inline-flex", alignItems: "center", gap: 4, flexShrink: 0 }}>
          {c.restarts}×{hot && <Icon name="alert" size={12} />}
        </span>
        {c.restarts_by && (
          <span title={`${c.restarts} Restarts, überwiegend im Container ${c.restarts_by}`}
            style={{ fontSize: 10.5, color: "var(--text-faint)", fontFamily: "var(--font)", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>
            {c.restarts_by}
          </span>
        )}
      </span>
      <Icon name="chevRight" size={16} style={{ color: "var(--text-faint)", opacity: 0.5 }} />
    </button>
  );
}

const skel: CSSProperties = { background: "var(--surface-2)", borderRadius: "var(--radius)" };

function Dashboard() {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [drawer, setDrawer] = useState<string | null>(null);

  const { data, isLoading, isFetching } = useQuery({
    queryKey: ["status"],
    queryFn: () => api.get<StatusResponse>("/api/v1/status"),
    refetchInterval: 15_000,
    staleTime: 10_000,
    placeholderData: keepPreviousData,
  });
  const { data: events } = useQuery({
    queryKey: ["events"],
    queryFn: () => api.get<EventInfo[]>("/api/v1/status/events?limit=25"),
    refetchInterval: 20_000,
  });
  // Whether the hooks' patches are still in effect — a different question from
  // whether the hooks are enabled, and the one that was silently "no" while every
  // other panel on this page was green (see internal/drift).
  const { data: drift } = useQuery({
    queryKey: ["drift"],
    queryFn: () => api.get<DriftResponse>("/api/v1/drift"),
    refetchInterval: 60_000,
  });
  const { data: sysinfo } = useQuery({
    queryKey: ["sysinfo"],
    queryFn: () => api.get<SysInfoResponse>("/api/v1/status/sysinfo"),
    refetchInterval: 60_000,
  });
  const cleanup = useMutation({
    mutationFn: () => api.delete<{ deleted: number }>("/api/v1/status/evicted-pods"),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["status"] }),
  });

  if (isLoading) {
    return (
      <div style={{ display: "flex", flexDirection: "column", gap: 22 }}>
        <div style={{ ...skel, height: 96 }} />
        <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(190px,1fr))", gap: 14 }}>{[0, 1, 2, 3].map((i) => <div key={i} style={{ ...skel, height: 96 }} />)}</div>
        <div style={{ ...skel, height: 320 }} />
      </div>
    );
  }
  if (!data) return null;

  const node = data.nodes?.[0];
  const cpu = node ? pct(node.cpu_used_millis, node.cpu_total_millis) : 0;
  const mem = node ? pct(node.mem_used_mi, node.mem_total_mi) : 0;
  const attention = data.components.filter(needsAttention);
  const totalRestarts = data.components.reduce((n, c) => n + c.restarts, 0);
  // Two different questions, and the banner used to ask only the first.
  //
  // `restarts > 20` is a lifetime count: it cannot tell a container dying every
  // thirty seconds from one that misbehaved a fortnight ago and has been fine since.
  // Measured on the live cluster: postgres-exporter at 64 restarts over twelve days,
  // stable for the last seventeen hours, rendered as "postgres in Restart-Schleife"
  // — a red alert claiming the database was crash-looping, when postgres itself has
  // restarted exactly zero times (E53, §4.42 and §4.43 together).
  const loopingComponents = data.components.filter((c) => c.looping);
  const hotComponents = data.components.filter((c) => c.restarts > 20 && !c.looping);

  // What actually restarted. `restarts_by` is deliberately empty when no single
  // container carries the count, and the workload name is then the honest answer.
  const culprit = (c: ComponentStatus) => c.restarts_by || shortName(c.name);

  // Components the scheduler refused. "down" is what kubectl already says; this is
  // the sentence that was missing for 37 hours during the outage of 2026-08-16…18.
  const unplaceable = data.components.filter((c) => c.unschedulable);
  const warnEvents = (events ?? []).filter((e) => e.type === "Warning");
  const rel = data.release;
  const ver = rel?.chart_version?.replace(/^matrix-stack-/, "") || "—";
  const essPods = sysinfo?.pod_counts?.ess;
  const boundPvcs = sysinfo?.pvcs?.filter((p) => p.phase === "Bound").length ?? 0;

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 22 }}>
      {/* Hero */}
      <Card style={{ padding: 0, overflow: "hidden", position: "relative" }}>
        <div style={{ position: "absolute", inset: 0, background: "radial-gradient(120% 140% at 100% 0%, var(--accent-soft), transparent 55%)", pointerEvents: "none" }} />
        <div style={{ position: "relative", display: "flex", alignItems: "center", justifyContent: "space-between", gap: 24, padding: "22px 24px", flexWrap: "wrap" }}>
          <div style={{ display: "flex", alignItems: "center", gap: 18 }}>
            <div style={{ display: "grid", placeItems: "center", width: 52, height: 52, borderRadius: "var(--radius)", background: "var(--accent-soft)", color: "var(--accent)" }}><Icon name="server" size={26} /></div>
            <div>
              <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 4, flexWrap: "wrap" }}>
                <span style={{ fontSize: 19, fontWeight: 650, letterSpacing: "-0.02em" }}>{rel?.name || "ESS"}</span>
                {attention.length === 0 ? <Badge tone="ok" icon="check">Alle Komponenten healthy</Badge> : <Badge tone="warn" icon="alert">{attention.length} brauchen Aufmerksamkeit</Badge>}
              </div>
              <div style={{ display: "flex", gap: 16, flexWrap: "wrap", fontSize: 12.5, color: "var(--text-faint)" }}>
                <span style={{ fontFamily: "var(--mono)" }}>matrix-stack {ver}</span>
                <span>·</span><span>Revision #{rel?.revision ?? "—"}</span>
                <span>·</span><span>{data.components.length} Komponenten</span>
                {node && <><span>·</span><span style={{ fontFamily: "var(--mono)" }}>{node.name}</span></>}
              </div>
            </div>
          </div>
          <div style={{ display: "flex", gap: 10 }}>
            <Button variant="outline" icon="refresh" onClick={() => { qc.invalidateQueries({ queryKey: ["status"] }); qc.invalidateQueries({ queryKey: ["events"] }); }}>{isFetching ? <Spinner size={14} /> : "Sync"}</Button>
            <Button variant="primary" icon="helm" onClick={() => navigate({ to: "/helm" })}>Updates</Button>
          </div>
        </div>
      </Card>

      {/* Cluster stats */}
      <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(180px, 1fr))", gap: "var(--gap)" }}>
        <Card style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}><span style={{ fontSize: 12.5, color: "var(--text-faint)", fontWeight: 500 }}>CPU</span><Icon name="cpu" size={15} style={{ color: "var(--text-faint)" }} /></div>
          <div style={{ fontSize: 26, fontWeight: 650, fontFamily: "var(--mono)", letterSpacing: "-0.03em", lineHeight: 1 }}>{cpu}<span style={{ fontSize: 13, color: "var(--text-faint)", marginLeft: 3 }}>%</span></div>
          <Meter value={cpu} tone="auto" />
          {node && <span style={{ fontSize: 10.5, color: "var(--text-faint)", fontFamily: "var(--mono)" }}>{node.cpu_used_millis}m / {node.cpu_total_millis}m</span>}
        </Card>
        <Card style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}><span style={{ fontSize: 12.5, color: "var(--text-faint)", fontWeight: 500 }}>Arbeitsspeicher</span><Icon name="activity" size={15} style={{ color: "var(--text-faint)" }} /></div>
          <div style={{ fontSize: 26, fontWeight: 650, fontFamily: "var(--mono)", letterSpacing: "-0.03em", lineHeight: 1 }}>{mem}<span style={{ fontSize: 13, color: "var(--text-faint)", marginLeft: 3 }}>%</span></div>
          <Meter value={mem} tone="auto" />
          {node && <span style={{ fontSize: 10.5, color: "var(--text-faint)", fontFamily: "var(--mono)" }}>{node.mem_used_mi} / {node.mem_total_mi} MiB</span>}
        </Card>
        <MiniStat label="Pods (ess)" value={essPods ?? data.components.reduce((n, c) => n + c.ready, 0)} unit={`/ ${boundPvcs} PVCs`} icon="server" />
        {/* "kritisch" means looping now, not merely high. A lifetime total above a
            threshold is not a critical condition, and colouring it red spends the
            operator's attention on something that already stopped (E53). */}
        <MiniStat label="Restarts gesamt" value={totalRestarts}
          unit={loopingComponents.length ? `· ${loopingComponents.length} kritisch` : ""}
          icon="rotate"
          tone={loopingComponents.length ? "err" : totalRestarts > 0 ? "warn" : undefined} />
        <MiniStat label="Warn-Events" value={warnEvents.length} icon="alert" tone={warnEvents.length > 0 ? "warn" : undefined} />
      </div>

      {/* Cannot be placed at all. Above the restart banners on purpose: a component
          the scheduler refused is not going to recover on its own, and the arithmetic
          is the whole diagnosis (E54). */}
      {unplaceable.map((c) => {
        const u = c.unschedulable!;
        const cpuReq = u.cpu_request_millis ?? 0;
        const cpuCap = u.cpu_allocatable_millis ?? 0;
        return (
          <Card key={c.name} style={{ display: "flex", alignItems: "flex-start", gap: 12, flexWrap: "wrap", background: "color-mix(in oklch, var(--status-err) 9%, var(--surface))", borderColor: "color-mix(in oklch, var(--status-err) 28%, var(--border))" }}>
            <Icon name="alert" size={18} style={{ color: "var(--status-err)", marginTop: 2 }} />
            <div style={{ flex: 1, minWidth: 240 }}>
              <div style={{ fontSize: 13, color: "var(--text)" }}>
                <strong>{shortName(c.name)}</strong> kann nicht eingeplant werden.
                {cpuReq > 0 && cpuCap > 0 && (
                  <> Der Pod fordert <strong>{cpuReq}m CPU</strong> an, der größte Node hat <strong>{cpuCap}m</strong>.</>
                )}
              </div>
              {u.exceeds_node && (
                <div style={{ fontSize: 12.5, color: "var(--status-err)", marginTop: 4, lineHeight: 1.55 }}>
                  Mehr, als ein einzelner Node bereitstellen kann — das löst sich nicht
                  von selbst, weder durch Warten noch durch Verschieben anderer Pods.
                </div>
              )}
              {/* The scheduler's own words, verbatim. It is what a search engine and
                  every Kubernetes answer will match on. */}
              <div style={{ fontSize: 11.5, color: "var(--text-faint)", marginTop: 6, fontFamily: "var(--mono)", overflowWrap: "anywhere", lineHeight: 1.5 }}>
                {u.reason}
              </div>
            </div>
            <Button variant="soft" size="sm" iconRight="chevRight" onClick={() => setDrawer(c.name)}>Diagnose</Button>
          </Card>
        );
      })}

      {/* An actual restart loop: something is in CrashLoopBackOff right now. Red,
          because this one is happening as the page is being read. */}
      {loopingComponents.length > 0 && (
        <Card style={{ display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap", background: "color-mix(in oklch, var(--status-err) 9%, var(--surface))", borderColor: "color-mix(in oklch, var(--status-err) 28%, var(--border))" }}>
          <Icon name="alert" size={18} style={{ color: "var(--status-err)" }} />
          <span style={{ flex: 1, minWidth: 220, fontSize: 13, color: "var(--text)" }}>
            <strong>{loopingComponents.map(culprit).join(", ")}</strong> startet gerade wiederholt neu — Ursache ansehen.
          </span>
          <Button variant="soft" size="sm" iconRight="chevRight" onClick={() => setDrawer(loopingComponents[0].name)}>Diagnose</Button>
        </Card>
      )}

      {/* A high count that is *not* looping. Worth knowing, not worth alarming: 64
          restarts over twelve days is history, and rendering history in red is how
          red banners come to be ignored. Names the container, and says when — a
          count with no time in it cannot be judged (E53). */}
      {hotComponents.length > 0 && (
        <Card style={{ display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap", background: "color-mix(in oklch, var(--status-warn) 8%, var(--surface))", borderColor: "color-mix(in oklch, var(--status-warn) 26%, var(--border))" }}>
          <Icon name="rotate" size={18} style={{ color: "var(--status-warn)" }} />
          <span style={{ flex: 1, minWidth: 220, fontSize: 13, color: "var(--text)" }}>
            <strong>{hotComponents.map(culprit).join(", ")}</strong>{" "}
            {hotComponents.length === 1 ? "ist" : "sind"} häufig neu gestartet
            {hotComponents.length === 1 && hotComponents[0].restarts ? ` (${hotComponents[0].restarts}×)` : ""}
            {hotComponents.length === 1 && hotComponents[0].last_restart
              ? `, zuletzt ${sinceText(hotComponents[0].last_restart)}`
              : ""}
            {" "}— läuft aktuell stabil.
          </span>
          <Button variant="ghost" size="sm" iconRight="chevRight" onClick={() => setDrawer(hotComponents[0].name)}>Ansehen</Button>
        </Card>
      )}

      {/* Drift banner. A patch that is no longer applied is invisible everywhere
          else: the pod is healthy, the release is deployed, the hook says enabled.
          Only "would this patch still change something?" gives it away. */}
      {(drift?.drifted ?? 0) > 0 && (
        <Card style={{ display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap", background: "color-mix(in oklch, var(--status-err) 9%, var(--surface))", borderColor: "color-mix(in oklch, var(--status-err) 28%, var(--border))" }}>
          <Icon name="alert" size={18} style={{ color: "var(--status-err)" }} />
          <div style={{ flex: 1, minWidth: 240 }}>
            <div style={{ fontSize: 13, color: "var(--text)" }}>
              <strong>{drift!.drifted} Hook-Patch{drift!.drifted === 1 ? "" : "es"} nicht mehr aktiv.</strong>{" "}
              Meist die Folge eines Helm-Upgrades, das an MatrixCtrl vorbei lief.
            </div>
            <div style={{ fontSize: 12, color: "var(--text-dim)", marginTop: 3, fontFamily: "var(--mono)" }}>
              {drift!.findings.filter((f) => f.status === "drifted").slice(0, 3)
                .map((f) => `${f.name}: ${(f.paths ?? []).join(", ")}`).join(" · ")}
            </div>
          </div>
          <Button variant="soft" size="sm" iconRight="chevRight" onClick={() => navigate({ to: "/hooks" })}>Hooks ausführen</Button>
        </Card>
      )}

      {/* Hand-edits nothing maintains. This is the failure MatrixCtrl was built to
          prevent and could not see until now: Helm's three-way merge preserves
          fields it has never owned, so an exception applied once outlives every
          upgrade in silence. The API server tracks ownership itself, so this is
          read, not inferred (P1-11). */}
      {(drift?.manual_unmaintained ?? 0) > 0 && (
        <Card style={{ display: "flex", alignItems: "flex-start", gap: 12, flexWrap: "wrap", background: "color-mix(in oklch, var(--status-warn) 10%, var(--surface))", borderColor: "color-mix(in oklch, var(--status-warn) 28%, var(--border))" }}>
          <Icon name="alert" size={18} style={{ color: "var(--status-warn)", marginTop: 1 }} />
          <div style={{ flex: 1, minWidth: 240 }}>
            <div style={{ fontSize: 13, color: "var(--text)" }}>
              <strong>{drift!.manual_unmaintained} Feld{drift!.manual_unmaintained === 1 ? "" : "er"} von Hand gesetzt, ohne dass ein Hook es trägt.</strong>{" "}
              Helm überschreibt solche Felder nicht — sie überleben jedes Upgrade unbemerkt.
            </div>
            <div style={{ fontSize: 12, color: "var(--text-dim)", marginTop: 4, fontFamily: "var(--mono)", lineHeight: 1.6 }}>
              {(drift!.manual_edits ?? []).filter((e) => e.kind === "human" && !e.covered).slice(0, 4)
                .map((e) => `${e.resource}/${e.name}: ${e.paths.join(", ")}`).join(" · ")}
            </div>
          </div>
        </Card>
      )}

      {/* Quieter: the field is hand-set but a hook owns the same intent, so it will
          be re-applied. Worth knowing — someone bypassed the product — but not an
          alarm, and mixing it into the loud banner would bury the loud one. */}
      {(drift?.manual_by_hand ?? 0) > 0 && (drift?.manual_unmaintained ?? 0) === 0 && (
        <Card style={{ display: "flex", alignItems: "center", gap: 12, background: "var(--surface-2)" }}>
          <Icon name="info" size={17} style={{ color: "var(--text-dim)" }} />
          <span style={{ flex: 1, fontSize: 12.5, color: "var(--text-dim)" }}>
            {drift!.manual_by_hand} von Hand gesetzte{drift!.manual_by_hand === 1 ? "s Feld" : " Felder"} — jeweils von einem Hook getragen, wird also wieder angewendet.
          </span>
        </Card>
      )}

      {/* Unknown is shown too, quieter. It means the check could not answer — which
          must not look like an all-clear, and must not look like an alarm either. */}
      {(drift?.unknown ?? 0) > 0 && (drift?.drifted ?? 0) === 0 && (
        <Card style={{ display: "flex", alignItems: "center", gap: 12, background: "var(--surface-2)" }}>
          <Icon name="info" size={17} style={{ color: "var(--text-dim)" }} />
          <span style={{ flex: 1, fontSize: 12.5, color: "var(--text-dim)" }}>
            {drift!.unknown} Hook-Patch{drift!.unknown === 1 ? "" : "es"} nicht prüfbar — der Zustand ist unbekannt, nicht bestätigt.
          </span>
        </Card>
      )}

      {data.evicted_pods > 0 && (
        <Card style={{ display: "flex", alignItems: "center", gap: 12, background: "color-mix(in oklch, var(--status-warn) 10%, var(--surface))" }}>
          <Icon name="alert" size={18} style={{ color: "var(--status-warn)" }} />
          <span style={{ flex: 1, fontSize: 13, color: "var(--text)" }}><strong>{data.evicted_pods}</strong> evicted Pods im <code style={{ fontFamily: "var(--mono)" }}>ess</code>-Namespace.</span>
          <Button variant="soft" size="sm" icon="trash" onClick={() => cleanup.mutate()} disabled={cleanup.isPending}>{cleanup.isPending ? "Lösche…" : "Bereinigen"}</Button>
        </Card>
      )}

      {/* Components + events */}
      <div style={{ display: "grid", gridTemplateColumns: "minmax(0, 1.9fr) minmax(300px, 1fr)", gap: 22, alignItems: "start" }} className="mc-dash-grid">
        <Card pad={false}>
          <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", padding: "16px 18px 10px" }}>
            <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
              <h2 style={{ margin: 0, fontSize: 15, fontWeight: 600, letterSpacing: "var(--head-tracking)" }}>ESS-Komponenten</h2>
              {attention.length > 0 && <Badge tone="warn">{attention.length} auffällig</Badge>}
            </div>
            <span style={{ fontSize: 11, color: "var(--text-faint)" }}>Zeile klicken für Pods, Restart-Grund & Logs</span>
          </div>
          <div className="mc-comp-row" style={{ padding: "0 6px 8px", display: "grid", gridTemplateColumns: "1.8fr 1fr 0.7fr 0.9fr 26px", gap: 14, fontSize: 10.5, fontWeight: 600, letterSpacing: "0.05em", textTransform: "uppercase", color: "var(--text-faint)" }}>
            <span style={{ paddingLeft: 16 }}>Komponente</span><span>Status</span><span>Ready</span><span>Restarts</span><span />
          </div>
          <div style={{ padding: "0 6px 8px" }}>
            {data.components.map((c, i) => <ComponentRow key={c.name} c={c} last={i === data.components.length - 1} onClick={() => setDrawer(c.name)} />)}
            {data.components.length === 0 && <div style={{ padding: "24px 16px", fontSize: 13, color: "var(--text-faint)" }}>Keine Komponenten gefunden.</div>}
          </div>
        </Card>

        <div style={{ display: "flex", flexDirection: "column", gap: 22 }}>
          <Card pad={false}>
            <div style={{ padding: "16px 18px 4px" }}>
              <SectionTitle sub="Kubernetes-Events im ess-Namespace" icon="bell">Ereignisse</SectionTitle>
            </div>
            <div className="mc-scroll" style={{ padding: "0 18px 10px", maxHeight: 420, overflowY: "auto" }}>
              {events && events.length > 0
                ? events.map((e, i) => <EventRow key={i} e={e} showComponent />)
                : <EmptyState icon="bell" title="Keine Events" sub="Der Cluster meldet aktuell nichts." />}
            </div>
          </Card>

          <Card>
            <SectionTitle sub="Helm-Release">Deployment</SectionTitle>
            {[["Chart", `matrix-stack ${ver}`], ["Revision", `#${rel?.revision ?? "—"}`], ["Status", rel?.status || "—"], ["Deployed", rel?.deployed_at ? new Date(rel.deployed_at).toLocaleDateString("de-DE", { dateStyle: "medium" }) : "—"]].map(([k, v], i) => (
              <div key={k} style={{ display: "flex", justifyContent: "space-between", gap: 16, padding: "9px 0", borderBottom: i < 3 ? "1px solid var(--border-soft)" : "none" }}>
                <span style={{ fontSize: 13, color: "var(--text-faint)" }}>{k}</span>
                <span style={{ fontSize: 13, color: "var(--text)", fontFamily: "var(--mono)" }}>{v}</span>
              </div>
            ))}
            <Button variant="ghost" size="sm" full iconRight="chevRight" onClick={() => navigate({ to: "/helm" })} style={{ marginTop: 10 }}>Verlauf & Upgrade</Button>
          </Card>
        </div>
      </div>

      {drawer && <ComponentDrawer name={drawer} onClose={() => setDrawer(null)} />}
    </div>
  );
}
