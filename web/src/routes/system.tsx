import { createFileRoute } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { api } from "@/lib/api";
import { Card, Icon, Badge, SectionTitle, Meter, EmptyState, Button } from "@/components/mc";

export const Route = createFileRoute("/system")({
  component: SystemPage,
});

interface NodeConditionInfo {
  name: string;
  conditions: Record<string, string>;
  kernel_version?: string;
  os_image?: string;
  kube_version?: string;
  arch?: string;
}
interface NodeInfo { name: string; cpu_used_millis: number; cpu_total_millis: number; mem_used_mi: number; mem_total_mi: number }
interface PVCInfo {
  name: string;
  namespace: string;
  phase: string;
  storage_class?: string;
  capacity?: string;
  access_modes: string[];
  volume_name?: string;
}
interface SysInfoResponse {
  nodes: NodeConditionInfo[];
  node_metrics: NodeInfo[];
  pvcs: PVCInfo[];
  pod_counts: Record<string, number>;
}


function toneColor(v: number, base: string) {
  return v >= 90 ? "var(--status-err)" : v >= 70 ? "var(--status-warn)" : base;
}

function Sparkline({ values, base, label }: { values: number[]; base: string; label: string }) {
  const [hoverIdx, setHoverIdx] = useState<number | null>(null);
  const w = 400, h = 56, padX = 2, padY = 6, innerW = w - padX * 2, innerH = h - padY * 2;
  const safePts = values.length >= 2 ? values : values.length === 1 ? [values[0], values[0]] : [0, 0];
  const pts = safePts.map((v, i) => ({ x: padX + (i / (safePts.length - 1)) * innerW, y: padY + (1 - Math.max(0, Math.min(100, v)) / 100) * innerH, v }));
  const polyline = pts.map((p) => `${p.x},${p.y}`).join(" ");
  const fillPoly = [`${padX},${h - padY}`, ...pts.map((p) => `${p.x},${p.y}`), `${w - padX},${h - padY}`].join(" ");
  const last = values[values.length - 1] ?? 0;
  const color = toneColor(last, base);

  function onMouseMove(e: React.MouseEvent<SVGSVGElement>) {
    const rect = e.currentTarget.getBoundingClientRect();
    const relX = (e.clientX - rect.left) / rect.width;
    const idx = Math.round(relX * (safePts.length - 1));
    setHoverIdx(Math.max(0, Math.min(safePts.length - 1, idx)));
  }

  const hoverPt = hoverIdx !== null ? pts[hoverIdx] : null;
  const hoverVal = hoverIdx !== null ? safePts[hoverIdx] : null;
  const tipW = 44;
  const tipX = hoverPt ? Math.max(0, Math.min(w - tipW, hoverPt.x - tipW / 2)) : 0;

  return (
    <div style={{ position: "relative" }}>
      <svg viewBox={`0 0 ${w} ${h}`} style={{ width: "100%", height: 56 }} preserveAspectRatio="none" onMouseMove={onMouseMove} onMouseLeave={() => setHoverIdx(null)}>
        {[25, 50, 75].map((p) => {
          const y = padY + (1 - p / 100) * innerH;
          return <line key={p} x1={padX} y1={y} x2={w - padX} y2={y} stroke="var(--border-soft)" strokeWidth="0.5" strokeDasharray="3,3" />;
        })}
        <polygon points={fillPoly} fill={color} fillOpacity={0.14} />
        <polyline points={polyline} fill="none" stroke={color} strokeWidth="1.5" strokeLinejoin="round" strokeLinecap="round" />
        {hoverPt && (
          <>
            <line x1={hoverPt.x} y1={padY} x2={hoverPt.x} y2={h - padY} stroke="var(--text-faint)" strokeWidth="0.75" strokeDasharray="2,2" />
            <circle cx={hoverPt.x} cy={hoverPt.y} r="3" fill={color} />
            <rect x={tipX} y={1} width={tipW} height={14} rx="3" fill="var(--text)" opacity="0.92" />
            <text x={tipX + tipW / 2} y={11} textAnchor="middle" fontSize="9" fill="var(--bg)" fontFamily="var(--mono)">{hoverVal?.toFixed(1)}%</text>
          </>
        )}
      </svg>
      <div style={{ display: "flex", justifyContent: "space-between", fontSize: 10, color: "var(--text-faint)", padding: "0 2px", marginTop: 2 }}>
        <span>{label}</span>
        <span style={{ fontFamily: "var(--mono)" }}>{values.length > 0 ? `${(values[values.length - 1] ?? 0).toFixed(0)}%` : "—"}</span>
      </div>
    </div>
  );
}

const pct = (used: number, total: number) => (total ? Math.round((used / total) * 100) : 0);
const pctF = (used: number, total: number) => (total ? (used / total) * 100 : 0);

function ConditionBadge({ type, status }: { type: string; status: string }) {
  const isOK = (type === "Ready" && status === "True") || (type !== "Ready" && status === "False");
  return <Badge tone={isOK ? "ok" : "err"} size="sm" icon={isOK ? "check" : "x"}>{type}</Badge>;
}

/** One recorded reading, as the server stored it. */
interface NodeSample {
  at: string; node: string;
  cpu_used_millis: number; cpu_alloc_millis: number;
  mem_used_mi: number; mem_alloc_mi: number;
}
interface CapacityChange {
  node: string;
  from_cpu_millis: number; to_cpu_millis: number;
  from_mem_mi: number; to_mem_mi: number;
  at: string;
}
interface NodeHistory {
  samples: NodeSample[] | null;
  capacity_changes: CapacityChange[] | null;
}

function SystemPage() {
  const { data: sysinfo } = useQuery({
    queryKey: ["sysinfo"],
    queryFn: () => api.get<SysInfoResponse>("/api/v1/status/sysinfo"),
    refetchInterval: 15_000,
  });

  // Recorded server-side (etappe 59). This used to be a useRef that died on reload
  // and, on a fresh page, pre-filled itself with the current value — a flat line that
  // read as an hour of stability and was one reading repeated forty times.
  const { data: history } = useQuery({
    queryKey: ["node-history"],
    queryFn: () => api.get<NodeHistory>("/api/v1/status/nodes/history?hours=6"),
    refetchInterval: 60_000,
  });

  const seriesFor = (node: string) => {
    const rows = (history?.samples ?? []).filter((s) => s.node === node);
    return {
      cpu: rows.map((s) => pctF(s.cpu_used_millis, s.cpu_alloc_millis)),
      mem: rows.map((s) => pctF(s.mem_used_mi, s.mem_alloc_mi)),
    };
  };

  const conditionOrder = ["Ready", "MemoryPressure", "DiskPressure", "PIDPressure"];

  const changes = history?.capacity_changes ?? [];

  // fetch + blob rather than a plain link. A navigation cannot carry an Authorization
  // header, and the alternatives were both worse: putting the session token in the URL
  // is exactly the leak E35 removed, and widening the single-use WebSocket ticket to
  // cover ordinary downloads would loosen a mechanism built narrow on purpose.
  //
  // The cost is that the browser holds the archive in memory. That is a property of
  // authenticated downloads, not a design choice — the server still streams it.
  const [downloading, setDownloading] = useState(false);
  const [downloadErr, setDownloadErr] = useState<string | null>(null);
  const download = async () => {
    setDownloading(true);
    setDownloadErr(null);
    try {
      const res = await fetch("/api/v1/status/backup", {
        headers: { Authorization: `Bearer ${localStorage.getItem("matrixctrl_token") ?? ""}` },
      });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = (res.headers.get("Content-Disposition")?.match(/filename="([^"]+)"/)?.[1])
        ?? "matrixctrl-backup.tar.gz";
      a.click();
      URL.revokeObjectURL(url);
    } catch (e) {
      // A backup that failed must say so. A button that flickers and produces no file
      // is indistinguishable from one that worked, which is the worst outcome here.
      setDownloadErr(e instanceof Error ? e.message : "unbekannter Fehler");
    } finally {
      setDownloading(false);
    }
  };

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 22, maxWidth: 920 }}>
      {/* The reason the capacity columns are recorded at all. On 2026-08-16 this node
          went from 32 cores to 6; every reservation on it became unschedulable at the
          next reboot, and nothing in the panel could say the machine had changed. */}
      {changes.map((c) => (
        <Card key={c.node} style={{ display: "flex", alignItems: "flex-start", gap: 12, background: "color-mix(in oklch, var(--status-warn) 8%, var(--surface))", borderColor: "color-mix(in oklch, var(--status-warn) 26%, var(--border))" }}>
          <Icon name="alert" size={18} style={{ color: "var(--status-warn)", marginTop: 2 }} />
          <div style={{ fontSize: 13, color: "var(--text)", lineHeight: 1.6 }}>
            <strong>Die Kapazität von {c.node} hat sich geändert.</strong>
            <div style={{ marginTop: 4, color: "var(--text-dim)" }}>
              {c.from_cpu_millis !== c.to_cpu_millis && (
                <>CPU: {c.from_cpu_millis}m → <strong>{c.to_cpu_millis}m</strong>. </>
              )}
              {c.from_mem_mi !== c.to_mem_mi && (
                <>Speicher: {c.from_mem_mi} Mi → <strong>{c.to_mem_mi} Mi</strong>. </>
              )}
              Reservierungen, die vorher passten, passen danach womöglich nicht mehr —
              und laufende Pods merken das erst beim nächsten Neustart.
            </div>
          </div>
        </Card>
      ))}

      {/* Backup. The card states what the archive does NOT hold, because an operator
          who believes they have a backup of their homeserver and discovers otherwise
          during a restore is the failure this whole feature exists to avoid. */}
      <Card style={{ display: "flex", flexDirection: "column", gap: 12 }}>
        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12, flexWrap: "wrap" }}>
          <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
            <Icon name="download" size={17} />
            <h2 style={{ margin: 0, fontSize: 15, fontWeight: 600 }}>Backup</h2>
          </div>
          <Button variant="primary" icon="download" onClick={() => void download()} disabled={downloading}>
            {downloading ? "Wird erstellt…" : "Herunterladen"}
          </Button>
        </div>
        <div style={{ fontSize: 12.5, color: "var(--text-dim)", lineHeight: 1.65 }}>
          <strong style={{ color: "var(--text)" }}>Enthalten:</strong> das Konfigurations-Repository
          mit vollständiger Git-Historie — also jeder ESS-Wert samt Änderungsverlauf — und die
          Datenbank von MatrixCtrl: Hooks, Upgrade-Verlauf, Melde-Entscheidungen, Node-Verlauf.
        </div>
        {downloadErr && (
          <div style={{ fontSize: 12.5, color: "var(--status-err)" }}>
            Das Backup konnte nicht erstellt werden: {downloadErr}
          </div>
        )}
        <div style={{ fontSize: 12.5, color: "var(--status-warn)", lineHeight: 1.65 }}>
          <strong>Nicht enthalten: der Homeserver selbst.</strong> Weder Synapses Datenbank
          (Konten, Räume, Nachrichten) noch die hochgeladenen Dateien. Beide liegen auf Volumes,
          die dieser Pod nicht einbindet. Dieses Archiv ersetzt kein Backup von Synapse.
        </div>
      </Card>

      {sysinfo?.node_metrics?.map((node) => {
        const h = seriesFor(node.name);
        const cpuPct = pct(node.cpu_used_millis, node.cpu_total_millis);
        const memPct = pct(node.mem_used_mi, node.mem_total_mi);
        const cond = sysinfo.nodes?.find((n) => n.name === node.name);
        return (
          <Card key={node.name} style={{ display: "flex", flexDirection: "column", gap: 18 }}>
            <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", flexWrap: "wrap", gap: 8 }}>
              <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
                <div style={{ display: "grid", placeItems: "center", width: 34, height: 34, borderRadius: "var(--radius-sm)", background: "var(--accent-soft)", color: "var(--accent)" }}><Icon name="server" size={17} /></div>
                <span style={{ fontFamily: "var(--mono)", fontSize: 13.5, fontWeight: 600, color: "var(--text)" }}>{node.name}</span>
              </div>
              <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
                {conditionOrder.map((t) => cond?.conditions[t] && <ConditionBadge key={t} type={t} status={cond.conditions[t]} />)}
              </div>
            </div>

            {cond && (
              <div style={{ display: "flex", flexWrap: "wrap", gap: "4px 16px", fontSize: 11.5, color: "var(--text-faint)", paddingBottom: 14, borderBottom: "1px solid var(--border-soft)" }}>
                {cond.os_image && <span>{cond.os_image}</span>}
                {cond.kernel_version && <span>Kernel {cond.kernel_version}</span>}
                {cond.kube_version && <span>K8s {cond.kube_version}</span>}
                {cond.arch && <span>{cond.arch}</span>}
              </div>
            )}

            <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 24 }} className="mc-dash-grid">
              <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
                <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", fontSize: 12, marginBottom: 2 }}>
                  <span style={{ display: "inline-flex", alignItems: "center", gap: 5, color: "var(--text-dim)" }}><Icon name="cpu" size={13} /> CPU</span>
                  <span style={{ fontFamily: "var(--mono)", fontWeight: 600, color: toneColor(cpuPct, "var(--text-dim)") }}>{node.cpu_used_millis}m / {node.cpu_total_millis}m</span>
                </div>
                <Meter value={cpuPct} tone="auto" height={4} />
                <Sparkline values={h.cpu} base="var(--accent)" label="CPU % (Verlauf)" />
              </div>
              <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
                <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", fontSize: 12, marginBottom: 2 }}>
                  <span style={{ display: "inline-flex", alignItems: "center", gap: 5, color: "var(--text-dim)" }}><Icon name="activity" size={13} /> Memory</span>
                  <span style={{ fontFamily: "var(--mono)", fontWeight: 600, color: toneColor(memPct, "var(--text-dim)") }}>{node.mem_used_mi} / {node.mem_total_mi} MiB</span>
                </div>
                <Meter value={memPct} tone="auto" height={4} />
                <Sparkline values={h.mem} base="var(--status-ok)" label="RAM % (Verlauf)" />
              </div>
            </div>
          </Card>
        );
      })}

      {sysinfo?.pod_counts && Object.keys(sysinfo.pod_counts).length > 0 && (
        <Card>
          <SectionTitle icon="server" sub="Live aus dem Cluster">Pods nach Namespace</SectionTitle>
          <div style={{ display: "flex", flexWrap: "wrap", gap: 10 }}>
            {Object.entries(sysinfo.pod_counts).map(([ns, count]) => (
              <div key={ns} style={{ display: "flex", alignItems: "center", gap: 8, background: "var(--surface-2)", borderRadius: "var(--radius-sm)", padding: "8px 12px" }}>
                <span style={{ fontSize: 12, fontFamily: "var(--mono)", color: "var(--text-dim)" }}>{ns}</span>
                <span style={{ fontSize: 14, fontWeight: 650, color: "var(--text)", fontFamily: "var(--mono)" }}>{count}</span>
              </div>
            ))}
          </div>
        </Card>
      )}

      {sysinfo?.pvcs && sysinfo.pvcs.length > 0 && (
        <Card>
          <SectionTitle icon="database" sub="Persistent Volume Claims">Storage</SectionTitle>
          <div style={{ display: "flex", flexDirection: "column" }}>
            {sysinfo.pvcs.map((pvc, i) => (
              <div key={`${pvc.namespace}/${pvc.name}`} style={{ display: "flex", alignItems: "center", gap: 14, padding: "11px 0", borderBottom: i < sysinfo.pvcs.length - 1 ? "1px solid var(--border-soft)" : "none" }}>
                <span style={{ width: 8, height: 8, borderRadius: "50%", flexShrink: 0, background: pvc.phase === "Bound" ? "var(--status-ok)" : pvc.phase === "Pending" ? "var(--status-warn)" : "var(--status-err)" }} />
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                    <span style={{ fontSize: 12.5, fontFamily: "var(--mono)", color: "var(--text)" }}>{pvc.name}</span>
                    <Badge tone="neutral" size="sm">{pvc.namespace}</Badge>
                  </div>
                  {pvc.volume_name && <p style={{ margin: "2px 0 0", fontSize: 11, fontFamily: "var(--mono)", color: "var(--text-faint)", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>{pvc.volume_name}</p>}
                </div>
                <div style={{ display: "flex", alignItems: "center", gap: 12, flexShrink: 0, fontSize: 12, color: "var(--text-faint)" }}>
                  {pvc.capacity && <span style={{ fontFamily: "var(--mono)", fontWeight: 600, color: "var(--text-dim)" }}>{pvc.capacity}</span>}
                  {pvc.storage_class && <span>{pvc.storage_class}</span>}
                  <span style={{ fontWeight: 500, color: pvc.phase === "Bound" ? "var(--status-ok)" : pvc.phase === "Pending" ? "var(--status-warn)" : "var(--status-err)" }}>{pvc.phase}</span>
                </div>
              </div>
            ))}
          </div>
        </Card>
      )}

      {!sysinfo && <Card pad={false}><EmptyState icon="server" title="Lade Systemdaten…" sub="Node-Metriken, Pods und Volumes werden aus dem Cluster gelesen." /></Card>}
    </div>
  );
}
