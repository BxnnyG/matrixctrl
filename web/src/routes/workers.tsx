import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { api } from "@/lib/api";
import { Button, Card, Icon, SectionTitle, Spinner, useIsMobile } from "@/components/mc";

export const Route = createFileRoute("/workers")({ component: WorkersPage });

interface Verdict { level: "measuring" | "fine" | "watch" | "recommend" | "busy"; title: string; detail: string; worker?: string; area?: string; notes?: string[] }
interface Stats { samples: number; avg_cores: number; p95_cores: number; max_cores: number; now_cores: number; memory: number; from: string; to: string }
interface Process {
  pod: string; worker: string; ready: boolean; restarts: number; running: boolean; enabled: boolean;
  stats?: Stats; lifetime?: { cores: number; memory: number; since: string };
}
interface Area { id: string; label: string; worker?: string; cores: number }
export interface WorkersResponse {
  verdict: Verdict; range: "24h" | "7d"; processes: Process[]; areas: Area[] | null; main_cores: number;
  areas_source: "range" | "lifetime" | ""; enabled: Record<string, boolean>;
  history: Record<string, [number, number, number][]>; interval_secs: number; worker_labels: Record<string, string>;
  read_errors?: Record<string, string>;
}

const TONE: Record<Verdict["level"], string> = {
  measuring: "var(--text-faint)", fine: "var(--status-ok)", watch: "var(--status-warn)", recommend: "var(--accent)", busy: "var(--status-warn)",
};

const pct = (cores: number) => `${(cores * 100).toFixed(cores < 0.1 ? 1 : 0)} %`;
const mb = (bytes: number) => `${Math.round(bytes / 1e6)} MB`;

/** Does Synapse need workers, and which (etappe 118).
 *
 *  For almost every self-hosted server the answer is no, and the page has to be able to
 *  say that with a number rather than leave a list of 23 switches nobody can judge. */
function WorkersPage() {
  const [range, setRange] = useState<"24h" | "7d">("24h");
  const { data, isLoading, isFetching, error, refetch } = useQuery({
    queryKey: ["workers", range],
    queryFn: () => api.get<WorkersResponse>(`/api/v1/workers?range=${range}`),
    refetchInterval: 60_000,
  });

  const right = (
    <span style={{ display: "flex", gap: 6, alignItems: "center" }}>
      {(["24h", "7d"] as const).map((r) => (
        <Button key={r} size="sm" variant={range === r ? "soft" : "ghost"} onClick={() => setRange(r)}>{r === "24h" ? "24 Stunden" : "7 Tage"}</Button>
      ))}
      <Button size="sm" variant="outline" icon="refresh" disabled={isFetching} onClick={() => void refetch()}>{isFetching ? <Spinner size={13} /> : "Neu laden"}</Button>
    </span>
  );

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 18, maxWidth: 980 }}>
      <SectionTitle icon="activity" sub="Braucht Synapse eigene Prozesse für Teile seiner Arbeit — und welchen?" right={right}>Worker-Insights</SectionTitle>
      {isLoading && <Card style={{ fontSize: 13, color: "var(--text-faint)" }}><Spinner size={13} /> Lade Messwerte…</Card>}
      {error && <Card style={{ fontSize: 13, color: "var(--status-err)" }}>{(error as Error).message}</Card>}
      {data && <WorkersView data={data} />}
    </div>
  );
}

const COLS = "minmax(170px, 1.4fr) repeat(4, minmax(64px, 0.6fr)) minmax(120px, 1fr)";

export function WorkersView({ data }: { data: WorkersResponse }) {
  const navigate = useNavigate();
  const mobile = useIsMobile();
  const v = data.verdict;
  const toSwitches = () => navigate({ to: "/config", search: { mode: "tasks", card: "workers" } });
  const label = (w: string) => data.worker_labels[w] ?? w;

  const errors = Object.entries(data.read_errors ?? {});
  return (
    <>
      {/* Said before the verdict: "too little data" from a sampler that cannot reach
          Synapse would otherwise read as patience required. */}
      {errors.length > 0 && (
        <Card style={{ display: "flex", flexDirection: "column", gap: 6, borderColor: "var(--status-warn)" }}>
          <span style={{ display: "flex", gap: 8, alignItems: "center", fontSize: 13.5, fontWeight: 600 }}><Icon name="alert" size={15} style={{ color: "var(--status-warn)" }} /> Synapse lässt sich nicht ablesen</span>
          {errors.map(([pod, e]) => <span key={pod} style={{ fontSize: 12.5, color: "var(--text-dim)" }}><code style={{ fontFamily: "var(--mono)" }}>{pod}</code>: {e}</span>)}
          <span style={{ fontSize: 12, color: "var(--text-faint)" }}>MatrixCtrl liest Port 9001 jedes Synapse-Pods direkt. Eine NetworkPolicy im ESS-Namespace kann das verhindern.</span>
        </Card>
      )}
      <Card style={{ display: "flex", flexDirection: "column", gap: 10 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 10, flexWrap: "wrap" }}>
          <span style={{ width: 11, height: 11, borderRadius: 999, background: TONE[v.level], flexShrink: 0 }} />
          <span style={{ fontSize: 15.5, fontWeight: 650 }}>{v.title}</span>
        </div>
        <p style={{ margin: 0, fontSize: 13, color: "var(--text-dim)", lineHeight: 1.65, maxWidth: "80ch" }}>{v.detail}</p>
        {(v.notes ?? []).map((n) => (
          <div key={n} style={{ display: "flex", gap: 8, fontSize: 12.5, color: "var(--text-dim)" }}><Icon name="info" size={14} style={{ flexShrink: 0, marginTop: 2 }} />{n}</div>
        ))}
        <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
          {v.level === "recommend" && v.worker && (
            <Button variant="primary" size="sm" icon="activity" onClick={toSwitches}>„{label(v.worker)}“ in den Einstellungen einschalten</Button>
          )}
          <Button variant={v.level === "recommend" ? "ghost" : "outline"} size="sm" icon="sliders" onClick={toSwitches}>Worker-Schalter</Button>
        </div>
      </Card>

      <Card style={{ padding: 0, overflow: "hidden" }}>
        <div style={{ padding: "14px 18px 8px", fontSize: 14, fontWeight: 650 }}>Prozesse</div>
        {!mobile && (
          <div style={{ display: "grid", gridTemplateColumns: COLS, gap: "0 12px", padding: "0 18px 6px", fontSize: 11, color: "var(--text-faint)", textTransform: "uppercase", letterSpacing: "0.04em" }}>
            <span>Prozess</span><span>jetzt</span><span>Schnitt</span><span title="95 % der Minuten lagen darunter">Spitze</span><span>Speicher</span><span>Verlauf (eines Kerns)</span>
          </div>
        )}
        {data.processes.map((p) => <ProcessRow key={p.pod || p.worker} p={p} label={label(p.worker)} points={data.history[p.pod] ?? []} mobile={mobile} />)}
        <div style={{ padding: "8px 18px 12px", fontSize: 12, color: "var(--text-faint)", lineHeight: 1.6 }}>
          Prozent eines CPU-Kerns. Ein Synapse-Prozess kann höchstens einen Kern ausnutzen — die 100-%-Linie ist seine Grenze.
          Gemessen jede {data.interval_secs === 60 ? "Minute" : `${data.interval_secs} s`}, direkt bei Synapse.
        </div>
      </Card>

      <AreasCard data={data} />
    </>
  );
}

function ProcessRow({ p, label, points, mobile }: { p: Process; label: string; points: [number, number, number][]; mobile?: boolean }) {
  const s = p.stats;
  const cell = (v?: number, name?: string) => (
    <span style={{ fontFamily: "var(--mono)", fontSize: 12.5 }}>
      {mobile && name && <span style={{ fontFamily: "var(--font)", fontSize: 11, color: "var(--text-faint)" }}>{name} </span>}
      {v === undefined ? "—" : pct(v)}
    </span>
  );
  return (
    <div style={{ display: "grid", gridTemplateColumns: mobile ? "repeat(2, minmax(0, 1fr))" : COLS, gap: "4px 12px", alignItems: "center", padding: "9px 18px", borderTop: "1px solid var(--border-soft)" }}>
      <div style={{ minWidth: 0, gridColumn: mobile ? "1 / -1" : undefined }}>
        <div style={{ fontSize: 13, fontWeight: 600, color: "var(--text)" }}>{p.worker === "main" ? "Hauptprozess" : label}</div>
        <div style={{ fontSize: 11.5, color: p.running ? "var(--text-faint)" : "var(--status-warn)", fontFamily: p.running ? "var(--mono)" : undefined }}>
          {p.running
            ? <>{p.pod}{p.restarts > 0 ? ` · ${p.restarts} Neustarts` : ""}{!p.ready ? " · nicht bereit" : ""}</>
            : "eingeschaltet, läuft aber nicht — noch nicht übernommen?"}
        </div>
      </div>
      {cell(s?.now_cores, "jetzt")}
      {s ? cell(s.avg_cores, "Schnitt") : <span style={{ fontSize: 12, color: "var(--text-faint)" }} title="Durchschnitt seit dem Start des Prozesses">{p.lifetime ? `⌀ ${pct(p.lifetime.cores)} seit Start` : "—"}</span>}
      {cell(s?.p95_cores, "Spitze")}
      <span style={{ fontFamily: "var(--mono)", fontSize: 12.5 }}>{mobile && <span style={{ fontFamily: "var(--font)", fontSize: 11, color: "var(--text-faint)" }}>Speicher </span>}{s ? mb(s.memory) : p.lifetime ? mb(p.lifetime.memory) : "—"}</span>
      <span style={{ gridColumn: mobile ? "1 / -1" : undefined }}><Spark points={points} /></span>
    </div>
  );
}

/** Average as a filled area, the busiest minute as a line, one core as a dashed line
 *  when the scale reaches it. */
function Spark({ points }: { points: [number, number, number][] }) {
  if (points.length < 2) return <span style={{ fontSize: 11.5, color: "var(--text-faint)" }}>noch kein Verlauf</span>;
  const W = 160, H = 34;
  const t0 = points[0][0], t1 = points[points.length - 1][0];
  const top = Math.max(0.05, ...points.map((p) => p[2])) * 1.1;
  const x = (t: number) => ((t - t0) / Math.max(1, t1 - t0)) * W;
  const y = (v: number) => H - (Math.min(v, top) / top) * H;
  const avg = points.map((p) => `${x(p[0]).toFixed(1)},${y(p[1]).toFixed(1)}`).join(" ");
  const max = points.map((p) => `${x(p[0]).toFixed(1)},${y(p[2]).toFixed(1)}`).join(" ");
  return (
    <svg width={W} height={H} viewBox={`0 0 ${W} ${H}`} style={{ maxWidth: "100%", overflow: "visible" }} aria-label="CPU-Verlauf">
      {top >= 1 && <line x1={0} x2={W} y1={y(1)} y2={y(1)} stroke="var(--status-err)" strokeDasharray="3 3" strokeWidth={1} />}
      <polygon points={`0,${H} ${avg} ${W},${H}`} fill="color-mix(in oklch, var(--accent) 22%, transparent)" />
      <polyline points={max} fill="none" stroke="var(--accent)" strokeWidth={1.2} />
    </svg>
  );
}

export function AreasCard({ data }: { data: WorkersResponse }) {
  const areas = data.areas ?? [];
  const total = data.main_cores;
  if (!data.areas_source || total <= 0) return null;
  const attributed = areas.reduce((s, a) => s + a.cores, 0);
  const rest = Math.max(0, total - attributed);
  const rows: { key: string; label: string; worker?: string; cores: number; muted?: boolean }[] = [
    ...areas.map((a) => ({ key: a.id, label: a.label, worker: a.worker, cores: a.cores })),
    { key: "rest", label: "Nicht zuzuordnen — Replikation, Datenbank, Event-Schleife", cores: rest, muted: true },
  ];
  return (
    <Card style={{ display: "flex", flexDirection: "column", gap: 10 }}>
      <div>
        <div style={{ fontSize: 14, fontWeight: 650 }}>Wohin die Zeit des Hauptprozesses geht</div>
        <div style={{ fontSize: 12, color: "var(--text-faint)", marginTop: 2 }}>
          {data.areas_source === "range" ? "Im gewählten Zeitraum" : "Seit dem Start des Prozesses — der Verlauf ist noch zu kurz"}, im Schnitt {pct(total)} eines Kerns.
          Synapse ordnet nur einen Teil seiner Rechenzeit selbst zu; der Rest steht unten, statt die Aufteilung vollständiger aussehen zu lassen, als sie ist.
        </div>
      </div>
      {rows.map((r) => {
        const share = total > 0 ? r.cores / total : 0;
        return (
          <div key={r.key} style={{ display: "grid", gridTemplateColumns: "minmax(180px, 1.3fr) 2fr 56px", gap: 10, alignItems: "center" }}>
            <span style={{ fontSize: 12.5, color: r.muted ? "var(--text-faint)" : "var(--text)", minWidth: 0 }}>
              {r.label}{r.worker && <span style={{ marginLeft: 6, fontSize: 11, fontFamily: "var(--mono)", color: "var(--text-faint)" }}>{r.worker}</span>}
            </span>
            <span style={{ height: 8, borderRadius: 999, background: "var(--surface-2)", overflow: "hidden" }}>
              <span style={{ display: "block", height: "100%", width: `${Math.min(100, share * 100)}%`, background: r.muted ? "var(--text-faint)" : "var(--accent)", opacity: r.muted ? 0.4 : 1 }} />
            </span>
            <span style={{ fontSize: 12, fontFamily: "var(--mono)", color: "var(--text-dim)", textAlign: "right" }}>{(share * 100).toFixed(0)} %</span>
          </div>
        );
      })}
    </Card>
  );
}
