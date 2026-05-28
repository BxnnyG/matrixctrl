import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { CheckCircle, XCircle, AlertTriangle, Clock, ChevronDown, ChevronRight, RotateCcw, ScrollText, Loader2, X } from "lucide-react";
import { api } from "@/lib/api";

interface Component {
  name: string;
  status: string;
  ready: number;
  desired: number;
  restarts: number;
}

interface PodInfo {
  name: string;
  phase: string;
  ready: boolean;
  restarts: number;
  started_at?: string;
  node: string;
}

function restartClass(n: number) {
  if (n === 0) return "";
  if (n <= 3) return "text-yellow-500 dark:text-yellow-400";
  return "text-red-500 dark:text-red-400";
}

function shortName(name: string) {
  return name.replace(/^ess-/, "");
}

function phaseClass(phase: string, ready: boolean) {
  if (phase === "Running" && ready) return "text-green-600 dark:text-green-400";
  if (phase === "Running") return "text-yellow-600 dark:text-yellow-400";
  if (phase === "Pending") return "text-blue-500 dark:text-blue-400";
  return "text-red-500 dark:text-red-400";
}

function LogsModal({ namespace: _ns, podName, onClose }: { namespace: string; podName: string; onClose: () => void }) {
  const { data, isLoading, error } = useQuery({
    queryKey: ["pod-logs", podName],
    queryFn: () => api.get<{ logs: string }>(`/api/v1/status/pods/${podName}/logs?tail=300`),
    refetchInterval: 10_000,
  });

  return (
    <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50 p-4">
      <div className="bg-white dark:bg-gray-900 rounded-xl w-full max-w-4xl h-[70vh] flex flex-col shadow-2xl">
        <div className="flex items-center justify-between px-4 py-3 border-b border-gray-200 dark:border-gray-700 shrink-0">
          <div className="flex items-center gap-2">
            <ScrollText className="w-4 h-4 text-gray-400" />
            <span className="text-sm font-medium text-gray-900 dark:text-gray-100 font-mono">{podName}</span>
            <span className="text-xs text-gray-400">— letzte 300 Zeilen (refresh 10s)</span>
          </div>
          <button onClick={onClose} className="p-1 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 rounded">
            <X className="w-4 h-4" />
          </button>
        </div>
        <div className="flex-1 overflow-auto bg-gray-950 rounded-b-xl">
          {isLoading && (
            <div className="flex items-center gap-2 p-4 text-xs text-gray-400">
              <Loader2 className="w-3.5 h-3.5 animate-spin" /> Lade Logs...
            </div>
          )}
          {error && (
            <p className="p-4 text-xs text-red-400">{(error as Error).message}</p>
          )}
          {data && (
            <pre className="p-4 text-xs font-mono text-gray-300 leading-relaxed whitespace-pre-wrap break-words">
              {data.logs || "(keine Logs)"}
            </pre>
          )}
        </div>
      </div>
    </div>
  );
}

export function ComponentCard({ component: c }: { component: Component }) {
  const qc = useQueryClient();
  const [expanded, setExpanded] = useState(false);
  const [logsFor, setLogsFor] = useState<string | null>(null);
  const [confirmRestart, setConfirmRestart] = useState<string | null>(null);

  const healthy = c.ready === c.desired && c.desired > 0;
  const degraded = c.ready > 0 && c.ready < c.desired;

  const { data: pods, isLoading: podsLoading } = useQuery({
    queryKey: ["pods", c.name],
    queryFn: () => api.get<PodInfo[]>(`/api/v1/status/pods/${c.name}`),
    enabled: expanded,
    refetchInterval: expanded ? 10_000 : false,
  });

  const restart = useMutation({
    mutationFn: (podName: string) => api.delete(`/api/v1/status/pods/${podName}`),
    onSuccess: () => {
      setConfirmRestart(null);
      setTimeout(() => qc.invalidateQueries({ queryKey: ["pods", c.name] }), 1500);
    },
  });

  return (
    <>
      <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-xl overflow-hidden">
        {/* Header row */}
        <div
          className="flex items-start p-4 cursor-pointer hover:bg-gray-50 dark:hover:bg-gray-750 select-none"
          onClick={() => setExpanded((v) => !v)}
        >
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2 mb-1">
              <span className="text-sm font-medium text-gray-900 dark:text-gray-100 truncate" title={c.name}>
                {shortName(c.name)}
              </span>
              {healthy ? (
                <CheckCircle className="w-4 h-4 text-green-500 shrink-0" />
              ) : degraded ? (
                <AlertTriangle className="w-4 h-4 text-yellow-500 shrink-0" />
              ) : c.desired === 0 ? (
                <Clock className="w-4 h-4 text-gray-400 shrink-0" />
              ) : (
                <XCircle className="w-4 h-4 text-red-500 shrink-0" />
              )}
            </div>
            <div className="text-xs text-gray-500 dark:text-gray-400">
              {c.ready}/{c.desired} Ready
              {c.restarts > 0 && (
                <span className={`ml-2 font-medium ${restartClass(c.restarts)}`}>
                  {c.restarts} {c.restarts === 1 ? "Restart" : "Restarts"}
                </span>
              )}
            </div>
          </div>
          <div className="shrink-0 ml-2 text-gray-300 dark:text-gray-600">
            {expanded ? <ChevronDown className="w-4 h-4" /> : <ChevronRight className="w-4 h-4" />}
          </div>
        </div>

        {/* Pod list */}
        {expanded && (
          <div className="border-t border-gray-100 dark:border-gray-700 bg-gray-50 dark:bg-gray-900/50">
            {podsLoading && (
              <div className="flex items-center gap-2 px-4 py-3 text-xs text-gray-400">
                <Loader2 className="w-3.5 h-3.5 animate-spin" /> Lade Pods...
              </div>
            )}
            {pods?.length === 0 && (
              <p className="px-4 py-3 text-xs text-gray-400">Keine Pods gefunden.</p>
            )}
            {pods?.map((pod) => (
              <div key={pod.name} className="px-4 py-2.5 border-b border-gray-100 dark:border-gray-700/50 last:border-0">
                <div className="flex items-center gap-2">
                  <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${pod.phase === "Running" && pod.ready ? "bg-green-500" : pod.phase === "Running" ? "bg-yellow-500" : pod.phase === "Pending" ? "bg-blue-500" : "bg-red-500"}`} />
                  <span className="flex-1 text-xs font-mono text-gray-700 dark:text-gray-300 truncate" title={pod.name}>
                    {pod.name}
                  </span>
                  <span className={`text-xs shrink-0 ${phaseClass(pod.phase, pod.ready)}`}>
                    {pod.phase}
                  </span>
                  {pod.restarts > 0 && (
                    <span className={`text-xs shrink-0 ${restartClass(pod.restarts)}`}>
                      {pod.restarts}×
                    </span>
                  )}
                  <button
                    onClick={(e) => { e.stopPropagation(); setLogsFor(pod.name); }}
                    className="p-1 text-gray-400 hover:text-blue-500 dark:hover:text-blue-400 rounded transition-colors"
                    title="Logs anzeigen"
                  >
                    <ScrollText className="w-3.5 h-3.5" />
                  </button>
                  <button
                    onClick={(e) => { e.stopPropagation(); setConfirmRestart(pod.name); }}
                    className="p-1 text-gray-400 hover:text-red-500 dark:hover:text-red-400 rounded transition-colors"
                    title="Pod neustarten (löschen)"
                  >
                    <RotateCcw className="w-3.5 h-3.5" />
                  </button>
                </div>
                {pod.started_at && (
                  <p className="text-xs text-gray-400 dark:text-gray-500 mt-0.5 pl-3.5">
                    {new Date(pod.started_at).toLocaleString("de-DE", { dateStyle: "short", timeStyle: "short" })}
                    {pod.node && ` · ${pod.node}`}
                  </p>
                )}
              </div>
            ))}

            {/* Restart confirmation */}
            {confirmRestart && (
              <div className="px-4 py-3 bg-red-50 dark:bg-red-950/40 border-t border-red-200 dark:border-red-800 flex items-center gap-3">
                <span className="text-xs text-red-700 dark:text-red-300 flex-1">
                  Pod <code className="font-mono">{confirmRestart}</code> löschen und neu starten?
                </span>
                <button
                  onClick={() => restart.mutate(confirmRestart)}
                  disabled={restart.isPending}
                  className="flex items-center gap-1 px-2.5 py-1 bg-red-600 hover:bg-red-700 disabled:opacity-50 text-white text-xs rounded-lg"
                >
                  {restart.isPending ? <Loader2 className="w-3 h-3 animate-spin" /> : <RotateCcw className="w-3 h-3" />}
                  Ja, neustarten
                </button>
                <button
                  onClick={() => setConfirmRestart(null)}
                  className="px-2.5 py-1 text-xs text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200"
                >
                  Abbrechen
                </button>
                {restart.isError && (
                  <span className="text-xs text-red-500">{(restart.error as Error).message}</span>
                )}
              </div>
            )}
          </div>
        )}
      </div>

      {logsFor && (
        <LogsModal namespace="ess" podName={logsFor} onClose={() => setLogsFor(null)} />
      )}
    </>
  );
}
