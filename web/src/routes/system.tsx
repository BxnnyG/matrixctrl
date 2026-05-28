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

// Simple SVG sparkline: values is an array of 0-100 percentages
function Sparkline({ values, color = "blue" }: { values: number[]; color?: "blue" | "green" | "yellow" | "red" }) {
  if (values.length < 2) return <div className="h-12 bg-gray-100 dark:bg-gray-700 rounded" />;

  const w = 240;
  const h = 48;
  const pad = 2;
  const innerW = w - pad * 2;
  const innerH = h - pad * 2;

  const pts = values.map((v, i) => {
    const x = pad + (i / (values.length - 1)) * innerW;
    const y = pad + (1 - v / 100) * innerH;
    return `${x},${y}`;
  });

  const fillPts = [
    `${pad},${h - pad}`,
    ...pts,
    `${w - pad},${h - pad}`,
  ];

  const colors = {
    blue:   { stroke: "stroke-blue-500",   fill: "fill-blue-500/20"   },
    green:  { stroke: "stroke-green-500",  fill: "fill-green-500/20"  },
    yellow: { stroke: "stroke-yellow-500", fill: "fill-yellow-500/20" },
    red:    { stroke: "stroke-red-500",    fill: "fill-red-500/20"    },
  };

  const last = values[values.length - 1];
  const c = last >= 90 ? colors.red : last >= 70 ? colors.yellow : colors[color];

  return (
    <svg viewBox={`0 0 ${w} ${h}`} className="w-full h-12" preserveAspectRatio="none">
      <polygon points={fillPts.join(" ")} className={c.fill} />
      <polyline points={pts.join(" ")} className={`${c.stroke} fill-none`} strokeWidth="1.5" strokeLinejoin="round" strokeLinecap="round" />
    </svg>
  );
}

function pct(used: number, total: number) {
  if (!total) return 0;
  return Math.round((used / total) * 100);
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
  // Rolling history: { [nodeName]: { cpu: number[], mem: number[] } }
  const historyRef = useRef<Record<string, { cpu: number[]; mem: number[] }>>({});
  const [, forceUpdate] = useState(0);

  const { data: sysinfo } = useQuery({
    queryKey: ["sysinfo"],
    queryFn: () => api.get<SysInfoResponse>("/api/v1/status/sysinfo"),
    refetchInterval: 15_000,
  });

  // Append new data points to history on each fetch
  useEffect(() => {
    if (!sysinfo?.node_metrics) return;
    sysinfo.node_metrics.forEach((n) => {
      if (!historyRef.current[n.name]) {
        historyRef.current[n.name] = { cpu: [], mem: [] };
      }
      const h = historyRef.current[n.name];
      h.cpu.push(pct(n.cpu_used_millis, n.cpu_total_millis));
      h.mem.push(pct(n.mem_used_mi, n.mem_total_mi));
      if (h.cpu.length > MAX_HISTORY) h.cpu.splice(0, h.cpu.length - MAX_HISTORY);
      if (h.mem.length > MAX_HISTORY) h.mem.splice(0, h.mem.length - MAX_HISTORY);
    });
    forceUpdate((v) => v + 1);
  }, [sysinfo?.node_metrics]);

  const conditionOrder = ["Ready", "MemoryPressure", "DiskPressure", "PIDPressure"];

  return (
    <div className="space-y-8 max-w-4xl">
      <h1 className="text-2xl font-semibold text-gray-900 dark:text-gray-100">System</h1>

      {/* Node metrics + sparklines */}
      {sysinfo?.node_metrics?.map((node) => {
        const h = historyRef.current[node.name] ?? { cpu: [], mem: [] };
        const cpuPct = pct(node.cpu_used_millis, node.cpu_total_millis);
        const memPct = pct(node.mem_used_mi, node.mem_total_mi);
        const cond = sysinfo.nodes?.find((n) => n.name === node.name);

        return (
          <div key={node.name} className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-xl p-5 space-y-5">
            <div className="flex items-center justify-between">
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
              <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-gray-500 dark:text-gray-400">
                {cond.os_image && <span>{cond.os_image}</span>}
                {cond.kernel_version && <span>Kernel {cond.kernel_version}</span>}
                {cond.kube_version && <span>K8s {cond.kube_version}</span>}
                {cond.arch && <span>{cond.arch}</span>}
              </div>
            )}

            <div className="grid grid-cols-2 gap-4">
              {/* CPU */}
              <div className="space-y-1">
                <div className="flex items-center justify-between text-xs">
                  <span className="flex items-center gap-1 text-gray-600 dark:text-gray-400">
                    <Cpu className="w-3.5 h-3.5" /> CPU
                  </span>
                  <span className={`font-mono font-medium ${cpuPct >= 90 ? "text-red-500" : cpuPct >= 70 ? "text-yellow-500" : "text-gray-700 dark:text-gray-300"}`}>
                    {node.cpu_used_millis}m / {node.cpu_total_millis}m ({cpuPct}%)
                  </span>
                </div>
                <div className="h-1.5 bg-gray-200 dark:bg-gray-700 rounded-full overflow-hidden">
                  <div className={`h-full rounded-full transition-all ${cpuPct >= 90 ? "bg-red-500" : cpuPct >= 70 ? "bg-yellow-500" : "bg-blue-500"}`} style={{ width: `${cpuPct}%` }} />
                </div>
                <Sparkline values={h.cpu} color="blue" />
              </div>

              {/* Memory */}
              <div className="space-y-1">
                <div className="flex items-center justify-between text-xs">
                  <span className="flex items-center gap-1 text-gray-600 dark:text-gray-400">
                    <MemoryStick className="w-3.5 h-3.5" /> Memory
                  </span>
                  <span className={`font-mono font-medium ${memPct >= 90 ? "text-red-500" : memPct >= 70 ? "text-yellow-500" : "text-gray-700 dark:text-gray-300"}`}>
                    {node.mem_used_mi} / {node.mem_total_mi} MiB ({memPct}%)
                  </span>
                </div>
                <div className="h-1.5 bg-gray-200 dark:bg-gray-700 rounded-full overflow-hidden">
                  <div className={`h-full rounded-full transition-all ${memPct >= 90 ? "bg-red-500" : memPct >= 70 ? "bg-yellow-500" : "bg-green-500"}`} style={{ width: `${memPct}%` }} />
                </div>
                <Sparkline values={h.mem} color="green" />
              </div>
            </div>
          </div>
        );
      })}

      {/* Pod counts */}
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

      {/* PVCs */}
      {sysinfo?.pvcs && sysinfo.pvcs.length > 0 && (
        <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-xl p-5">
          <div className="flex items-center gap-2 mb-4">
            <HardDrive className="w-4 h-4 text-gray-400" />
            <h2 className="text-sm font-medium text-gray-700 dark:text-gray-300">Persistent Volumes</h2>
          </div>
          <div className="space-y-2">
            {sysinfo.pvcs.map((pvc) => (
              <div
                key={`${pvc.namespace}/${pvc.name}`}
                className="flex items-center gap-4 py-2 border-b border-gray-100 dark:border-gray-700 last:border-0"
              >
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
                  <span className={`${pvc.phase === "Bound" ? "text-green-600 dark:text-green-400" : pvc.phase === "Pending" ? "text-yellow-600 dark:text-yellow-400" : "text-red-500"}`}>
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
