import { createFileRoute } from "@tanstack/react-router";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { ComponentCard } from "@/components/status/ComponentCard";
import { ReleaseCard } from "@/components/status/ReleaseCard";
import { Trash2, AlertTriangle, Cpu, MemoryStick } from "lucide-react";

export const Route = createFileRoute("/")({
  component: Dashboard,
});

interface ComponentStatus {
  name: string;
  status: string;
  ready: number;
  desired: number;
  restarts: number;
}

interface NodeInfo {
  name: string;
  cpu_used_millis: number;
  cpu_total_millis: number;
  mem_used_mi: number;
  mem_total_mi: number;
}

interface StatusResponse {
  release?: {
    name: string;
    chart_version: string;
    revision: number;
    status: string;
    deployed_at?: string;
  };
  components: ComponentStatus[];
  nodes: NodeInfo[];
  evicted_pods: number;
}

function pct(used: number, total: number) {
  if (!total) return 0;
  return Math.round((used / total) * 100);
}

function ResourceBar({ label, used, total, unit, icon: Icon }: {
  label: string; used: number; total: number; unit: string; icon: React.ElementType;
}) {
  const p = pct(used, total);
  const color = p >= 90 ? "bg-red-500" : p >= 70 ? "bg-yellow-500" : "bg-blue-500";
  const textColor = p >= 90 ? "text-red-500" : p >= 70 ? "text-yellow-500 dark:text-yellow-400" : "text-gray-700 dark:text-gray-300";
  return (
    <div className="space-y-1">
      <div className="flex items-center justify-between text-xs">
        <span className="flex items-center gap-1 text-gray-600 dark:text-gray-400">
          <Icon className="w-3.5 h-3.5" /> {label}
        </span>
        <span className={`font-mono font-medium ${textColor}`}>
          {used}{unit} / {total}{unit} ({p}%)
        </span>
      </div>
      <div className="h-1.5 bg-gray-200 dark:bg-gray-700 rounded-full overflow-hidden">
        <div className={`h-full rounded-full transition-all ${color}`} style={{ width: `${p}%` }} />
      </div>
    </div>
  );
}

function Dashboard() {
  const qc = useQueryClient();
  const { data: status, isLoading } = useQuery({
    queryKey: ["status"],
    queryFn: () => api.get<StatusResponse>("/api/v1/status"),
    refetchInterval: 15_000,
  });

  const cleanup = useMutation({
    mutationFn: () => api.delete<{ deleted: number }>("/api/v1/status/evicted-pods"),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["status"] }),
  });

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold text-gray-900 dark:text-gray-100">Dashboard</h1>

      {isLoading && <div className="text-sm text-gray-500">Lade Status...</div>}

      {status && (
        <>
          <ReleaseCard release={status.release} />

          {status.evicted_pods > 0 && (
            <div className="flex items-center gap-3 bg-yellow-50 dark:bg-yellow-950/40 border border-yellow-200 dark:border-yellow-800 rounded-xl px-4 py-3 text-sm">
              <AlertTriangle className="w-4 h-4 text-yellow-600 dark:text-yellow-400 shrink-0" />
              <span className="flex-1 text-yellow-800 dark:text-yellow-300">
                <strong>{status.evicted_pods}</strong> evicted {status.evicted_pods === 1 ? "Pod" : "Pods"} im{" "}
                <code className="bg-yellow-100 dark:bg-yellow-900 px-1 rounded">ess</code> Namespace
              </span>
              <button
                onClick={() => cleanup.mutate()}
                disabled={cleanup.isPending}
                className="flex items-center gap-1.5 px-3 py-1.5 bg-yellow-600 hover:bg-yellow-700 disabled:opacity-50 text-white text-xs font-medium rounded-lg transition-colors"
              >
                <Trash2 className="w-3.5 h-3.5" />
                {cleanup.isPending ? "Lösche..." : "Bereinigen"}
              </button>
              {cleanup.isSuccess && (
                <span className="text-xs text-green-600 dark:text-green-400">
                  {(cleanup.data as { deleted: number }).deleted} gelöscht
                </span>
              )}
            </div>
          )}

          {status.nodes?.length > 0 && (
            <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-xl p-4 space-y-4">
              {status.nodes.map((node) => (
                <div key={node.name} className="space-y-3">
                  <p className="text-xs font-medium text-gray-500 dark:text-gray-400 font-mono">{node.name}</p>
                  <ResourceBar label="CPU" used={node.cpu_used_millis} total={node.cpu_total_millis} unit="m" icon={Cpu} />
                  <ResourceBar label="Memory" used={node.mem_used_mi} total={node.mem_total_mi} unit=" MiB" icon={MemoryStick} />
                </div>
              ))}
            </div>
          )}

          <section>
            <h2 className="text-lg font-medium text-gray-900 dark:text-gray-100 mb-3">Komponenten</h2>
            <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
              {status.components?.map((c) => (
                <ComponentCard key={c.name} component={c} />
              ))}
            </div>
          </section>
        </>
      )}
    </div>
  );
}
