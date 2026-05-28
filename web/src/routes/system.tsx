import { createFileRoute } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { useRef, useEffect, useState } from "react";
import { api } from "@/lib/api";
import { HardDrive, Server, Cpu, MemoryStick, Package, CheckCircle, XCircle } from "lucide-react";

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

interface NodeInfo {
  name: string;
  cpu_used_millis: number;
  cpu_total_millis: number;
  mem_used_mi: number;
  mem_total_mi: number;
}

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

const MAX_HISTORY = 40;

function Sparkline({ values, color = "blue", label }: { values: number[]; color?: "blue" | "green"; label: string }) {
  const [hoverIdx, setHoverIdx] = useState<number | null>(null);
  const svgRef = useRef<SVGSVGElement>(null);

  const w = 400;
  const h = 56;
  const padX = 2;
  const padY = 6;
  const innerW = w - padX * 2;
  const innerH = h - padY * 2;

  const safePts = values.length >= 2 ? values : values.length === 1 ? [values[0], values[0]] : [0, 0];

  const pts = safePts.map((v, i) => {
    const x = padX + (i / (safePts.length - 1)) * innerW;
    const y = padY + (1 - Math.max(0, Math.min(100, v)) / 100) * innerH;
    return { x, y, v };
  });

  const polyline = pts.map((p) => `${p.x},${p.y}`).join(" ");
  const fillPoly = [`${padX},${h - padY}`, ...pts.map((p) => `${p.x},${p.y}`), `${w - padX},${h - padY}`].join(" ");

  const last = values[values.length - 1] ?? 0;
  const isHigh = last >= 90;
  const isMid = last >= 70;

  const stroke = isHigh ? "stroke-red-500" : isMid ? "stroke-yellow-500" : color === "blue" ? "stroke-blue-500" : "stroke-green-500";
  const fill = isHigh ? "fill-red-500/15" : isMid ? "fill-yellow-500/15" : color === "blue" ? "fill-blue-500/15" : "fill-green-500/15";
  const dotColor = isHigh ? "fill-red-500" : isMid ? "fill-yellow-500" : color === "blue" ? "fill-blue-500" : "fill-green-500";

  function onMouseMove(e: React.MouseEvent<SVGSVGElement>) {
    const rect = e.currentTarget.getBoundingClientRect();
    const relX = (e.clientX - rect.left) / rect.width;
    const idx = Math.round(relX * (safePts.length - 1));
    setHoverIdx(Math.max(0, Math.min(safePts.length - 1, idx)));
  }

  const hoverPt = hoverIdx !== null ? pts[hoverIdx] : null;
  const hoverVal = hoverIdx !== null ? safePts[hoverIdx] : null;

  // Tooltip x: clamp so it doesn't go off-screen
  const tipW = 44;
  const tipX = hoverPt ? Math.max(0, Math.min(w - tipW, hoverPt.x - tipW / 2)) : 0;

  return (
    <div className="relative">
      <svg
        ref={svgRef}
        viewBox={`0 0 ${w} ${h}`}
        className="w-full"
        style={{ height: 56 }}
        preserveAspectRatio="none"
        onMouseMove={onMouseMove}
        onMouseLeave={() => setHoverIdx(null)}
      >
        {/* Grid lines at 25, 50, 75% */}
        {[25, 50, 75].map((pct) => {
          const y = padY + (1 - pct / 100) * innerH;
          return (
            <line key={pct} x1={padX} y1={y} x2={w - padX} y2={y}
              className="stroke-gray-200 dark:stroke-gray-700" strokeWidth="0.5" strokeDasharray="3,3" />
          );
        })}

        {/* Area fill */}
        <polygon points={fillPoly} className={fill} />

        {/* Line */}
        <polyline points={polyline} className={`${stroke} fill-none`} strokeWidth="1.5"
          strokeLinejoin="round" strokeLinecap="round" />

        {/* Hover dot + vertical line */}
        {hoverPt && (
          <>
            <line x1={hoverPt.x} y1={padY} x2={hoverPt.x} y2={h - padY}
              className="stroke-gray-400 dark:stroke-gray-500" strokeWidth="0.75" strokeDasharray="2,2" />
            <circle cx={hoverPt.x} cy={hoverPt.y} r="3" className={dotColor} />
            {/* Tooltip bg */}
            <rect x={tipX} y={1} width={tipW} height={14} rx="3"
              className="fill-gray-800 dark:fill-gray-200" opacity="0.85" />
            <text x={tipX + tipW / 2} y={11} textAnchor="middle" fontSize="9"
              className="fill-white dark:fill-gray-900" fontFamily="monospace">
              {hoverVal?.toFixed(1)}%
            </text>
          </>
        )}
      </svg>
      <div className="flex justify-between text-[10px] text-gray-400 dark:text-gray-600 px-0.5 mt-0.5">
        <span>{label}</span>
        <span>{values.length > 0 ? `${(values[values.length - 1] ?? 0).toFixed(0)}%` : "—"}</span>
      </div>
    </div>
  );
}

function pct(used: number, total: number) {
  if (!total) return 0;
  return Math.round((used / total) * 100);
}

function pctF(used: number, total: number) {
  if (!total) return 0;
  return (used / total) * 100;
}

function ConditionBadge({ type, status }: { type: string; status: string }) {
  const isOK = (type === "Ready" && status === "True") ||
    (type !== "Ready" && status === "False");
  return (
    <span className={`inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded-full font-medium ${
      isOK
        ? "bg-green-100 dark:bg-green-950 text-green-700 dark:text-green-300"
        : "bg-red-100 dark:bg-red-950 text-red-700 dark:text-red-300"
    }`}>
      {isOK ? <CheckCircle className="w-3 h-3" /> : <XCircle className="w-3 h-3" />}
      {type}
    </span>
  );
}

function SystemPage() {
  const historyRef = useRef<Record<string, { cpu: number[]; mem: number[] }>>({});
  const [, forceUpdate] = useState(0);

  const { data: sysinfo } = useQuery({
    queryKey: ["sysinfo"],
    queryFn: () => api.get<SysInfoResponse>("/api/v1/status/sysinfo"),
    refetchInterval: 15_000,
  });

  useEffect(() => {
    if (!sysinfo?.node_metrics) return;
    sysinfo.node_metrics.forEach((n) => {
      const cpuP = pctF(n.cpu_used_millis, n.cpu_total_millis);
      const memP = pctF(n.mem_used_mi, n.mem_total_mi);

      if (!historyRef.current[n.name]) {
        // Pre-fill so the graph shows on first load instead of waiting 2 polls
        historyRef.current[n.name] = {
          cpu: new Array(MAX_HISTORY).fill(cpuP),
          mem: new Array(MAX_HISTORY).fill(memP),
        };
      } else {
        const h = historyRef.current[n.name];
        h.cpu.push(cpuP);
        h.mem.push(memP);
        if (h.cpu.length > MAX_HISTORY) h.cpu.splice(0, h.cpu.length - MAX_HISTORY);
        if (h.mem.length > MAX_HISTORY) h.mem.splice(0, h.mem.length - MAX_HISTORY);
      }
    });
    forceUpdate((v) => v + 1);
  }, [sysinfo?.node_metrics]);

  const conditionOrder = ["Ready", "MemoryPressure", "DiskPressure", "PIDPressure"];

  return (
    <div className="space-y-8 max-w-4xl">
      <h1 className="text-2xl font-semibold text-gray-900 dark:text-gray-100">System</h1>

      {sysinfo?.node_metrics?.map((node) => {
        const h = historyRef.current[node.name] ?? { cpu: [], mem: [] };
        const cpuPct = pct(node.cpu_used_millis, node.cpu_total_millis);
        const memPct = pct(node.mem_used_mi, node.mem_total_mi);
        const cond = sysinfo.nodes?.find((n) => n.name === node.name);

        return (
          <div key={node.name} className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-xl p-5 space-y-5">
            <div className="flex items-center justify-between flex-wrap gap-2">
              <div className="flex items-center gap-2">
                <Server className="w-4 h-4 text-gray-400" />
                <span className="font-mono text-sm font-medium text-gray-900 dark:text-gray-100">{node.name}</span>
              </div>
              <div className="flex flex-wrap gap-1">
                {conditionOrder.map((t) => cond?.conditions[t] && (
                  <ConditionBadge key={t} type={t} status={cond.conditions[t]} />
                ))}
              </div>
            </div>

            {cond && (
              <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-gray-500 dark:text-gray-400 pb-1 border-b border-gray-100 dark:border-gray-700">
                {cond.os_image && <span>{cond.os_image}</span>}
                {cond.kernel_version && <span>Kernel {cond.kernel_version}</span>}
                {cond.kube_version && <span>K8s {cond.kube_version}</span>}
                {cond.arch && <span>{cond.arch}</span>}
              </div>
            )}

            <div className="grid grid-cols-2 gap-6">
              <div className="space-y-1">
                <div className="flex items-center justify-between text-xs mb-1">
                  <span className="flex items-center gap-1 text-gray-600 dark:text-gray-400">
                    <Cpu className="w-3.5 h-3.5" /> CPU
                  </span>
                  <span className={`font-mono font-medium ${cpuPct >= 90 ? "text-red-500" : cpuPct >= 70 ? "text-yellow-500" : "text-gray-700 dark:text-gray-300"}`}>
                    {node.cpu_used_millis}m / {node.cpu_total_millis}m
                  </span>
                </div>
                <div className="h-1 bg-gray-200 dark:bg-gray-700 rounded-full overflow-hidden">
                  <div className={`h-full rounded-full transition-all ${cpuPct >= 90 ? "bg-red-500" : cpuPct >= 70 ? "bg-yellow-500" : "bg-blue-500"}`} style={{ width: `${cpuPct}%` }} />
                </div>
                <Sparkline values={h.cpu} color="blue" label="CPU % (letzte 10 Min.)" />
              </div>

              <div className="space-y-1">
                <div className="flex items-center justify-between text-xs mb-1">
                  <span className="flex items-center gap-1 text-gray-600 dark:text-gray-400">
                    <MemoryStick className="w-3.5 h-3.5" /> Memory
                  </span>
                  <span className={`font-mono font-medium ${memPct >= 90 ? "text-red-500" : memPct >= 70 ? "text-yellow-500" : "text-gray-700 dark:text-gray-300"}`}>
                    {node.mem_used_mi} / {node.mem_total_mi} MiB
                  </span>
                </div>
                <div className="h-1 bg-gray-200 dark:bg-gray-700 rounded-full overflow-hidden">
                  <div className={`h-full rounded-full transition-all ${memPct >= 90 ? "bg-red-500" : memPct >= 70 ? "bg-yellow-500" : "bg-green-500"}`} style={{ width: `${memPct}%` }} />
                </div>
                <Sparkline values={h.mem} color="green" label="RAM % (letzte 10 Min.)" />
              </div>
            </div>
          </div>
        );
      })}

      {sysinfo?.pod_counts && Object.keys(sysinfo.pod_counts).length > 0 && (
        <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-xl p-5">
          <div className="flex items-center gap-2 mb-4">
            <Package className="w-4 h-4 text-gray-400" />
            <h2 className="text-sm font-medium text-gray-700 dark:text-gray-300">Pods nach Namespace</h2>
          </div>
          <div className="flex flex-wrap gap-3">
            {Object.entries(sysinfo.pod_counts).map(([ns, count]) => (
              <div key={ns} className="flex items-center gap-2 bg-gray-50 dark:bg-gray-900 rounded-lg px-3 py-2">
                <span className="text-xs font-mono text-gray-600 dark:text-gray-400">{ns}</span>
                <span className="text-sm font-semibold text-gray-900 dark:text-gray-100">{count}</span>
              </div>
            ))}
          </div>
        </div>
      )}

      {sysinfo?.pvcs && sysinfo.pvcs.length > 0 && (
        <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-xl p-5">
          <div className="flex items-center gap-2 mb-4">
            <HardDrive className="w-4 h-4 text-gray-400" />
            <h2 className="text-sm font-medium text-gray-700 dark:text-gray-300">Persistent Volumes</h2>
          </div>
          <div className="space-y-2">
            {sysinfo.pvcs.map((pvc) => (
              <div key={`${pvc.namespace}/${pvc.name}`} className="flex items-center gap-4 py-2 border-b border-gray-100 dark:border-gray-700 last:border-0">
                <span className={`w-2 h-2 rounded-full shrink-0 ${pvc.phase === "Bound" ? "bg-green-500" : pvc.phase === "Pending" ? "bg-yellow-500" : "bg-red-500"}`} />
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="text-xs font-mono text-gray-900 dark:text-gray-100">{pvc.name}</span>
                    <span className="text-xs text-gray-400 dark:text-gray-500 bg-gray-100 dark:bg-gray-700 px-1 rounded">{pvc.namespace}</span>
                  </div>
                  {pvc.volume_name && (
                    <p className="text-xs text-gray-400 dark:text-gray-500 font-mono mt-0.5 truncate">{pvc.volume_name}</p>
                  )}
                </div>
                <div className="flex items-center gap-3 shrink-0 text-xs text-gray-500 dark:text-gray-400">
                  {pvc.capacity && <span className="font-mono font-medium text-gray-700 dark:text-gray-300">{pvc.capacity}</span>}
                  {pvc.storage_class && <span>{pvc.storage_class}</span>}
                  <span className={pvc.phase === "Bound" ? "text-green-600 dark:text-green-400" : pvc.phase === "Pending" ? "text-yellow-600 dark:text-yellow-400" : "text-red-500"}>
                    {pvc.phase}
                  </span>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {!sysinfo && (
        <p className="text-sm text-gray-500 dark:text-gray-400">Lade Systemdaten...</p>
      )}
    </div>
  );
}
