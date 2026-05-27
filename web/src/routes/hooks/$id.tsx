import { createFileRoute, Link } from "@tanstack/react-router";
import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import {
  CheckCircle, XCircle, Clock, ArrowLeft, Play, Loader2, ChevronDown, ChevronRight,
} from "lucide-react";

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

interface HookDetail {
  id: string;
  name: string;
  description?: string;
  trigger: string;
  enabled: boolean;
  priority: number;
  builtin: boolean;
  actions: HookAction[];
}

interface ActionResult {
  action_index: number;
  type: string;
  status: string;
  error?: string;
  duration_ms: number;
}

interface HookRun {
  id: string;
  status: string;
  ts_start: string;
  ts_end?: string;
  trigger_type: string;
  action_results: ActionResult[];
  triggered_by: string;
}

function HookDetail() {
  const { id } = Route.useParams();
  const qc = useQueryClient();
  const [triggering, setTriggering] = useState(false);
  const [expandedRun, setExpandedRun] = useState<string | null>(null);

  const { data: hook } = useQuery({
    queryKey: ["hooks", id],
    queryFn: () => api.get<HookDetail>(`/api/v1/hooks/${id}`),
  });

  const { data: runs } = useQuery({
    queryKey: ["hooks", id, "runs"],
    queryFn: () => api.get<HookRun[]>(`/api/v1/hooks/${id}/runs`),
    refetchInterval: 5_000,
  });

  const trigger = useMutation({
    mutationFn: () => {
      setTriggering(true);
      return api.post(`/api/v1/hooks/${id}/trigger`, {});
    },
    onSettled: () => {
      setTriggering(false);
      qc.invalidateQueries({ queryKey: ["hooks", id, "runs"] });
    },
  });

  if (!hook) return <div className="text-sm text-gray-500">Lade...</div>;

  return (
    <div className="space-y-6 max-w-3xl">
      <div className="flex items-center gap-3">
        <Link to="/hooks" className="text-gray-400 hover:text-gray-600">
          <ArrowLeft className="w-5 h-5" />
        </Link>
        <h1 className="text-2xl font-semibold">{hook.name}</h1>
        {hook.builtin && (
          <span className="text-xs bg-blue-100 text-blue-700 px-1.5 py-0.5 rounded">
            Built-in
          </span>
        )}
        {!hook.enabled && (
          <span className="text-xs bg-gray-100 text-gray-500 px-1.5 py-0.5 rounded">
            Deaktiviert
          </span>
        )}
      </div>

      {/* Meta */}
      <div className="bg-white border border-gray-200 rounded-xl p-4 grid grid-cols-3 gap-4 text-sm">
        <div>
          <span className="text-xs text-gray-500 block mb-0.5">Trigger</span>
          <code className="bg-gray-100 px-1.5 py-0.5 rounded text-xs">{hook.trigger}</code>
        </div>
        <div>
          <span className="text-xs text-gray-500 block mb-0.5">Priorität</span>
          <span className="font-medium">{hook.priority}</span>
        </div>
        <div>
          <span className="text-xs text-gray-500 block mb-0.5">Status</span>
          <span className={`font-medium ${hook.enabled ? "text-green-600" : "text-gray-400"}`}>
            {hook.enabled ? "Aktiv" : "Deaktiviert"}
          </span>
        </div>
        {hook.description && (
          <div className="col-span-3">
            <span className="text-xs text-gray-500 block mb-0.5">Beschreibung</span>
            <p className="text-xs text-gray-600 leading-relaxed">{hook.description}</p>
          </div>
        )}
      </div>

      {/* Actions */}
      <div className="bg-white border border-gray-200 rounded-xl p-4 space-y-3">
        <h2 className="font-medium text-sm">Aktionen</h2>
        {hook.actions.map((action, i) => (
          <div key={i} className="flex items-start gap-3 text-sm border-t border-gray-50 pt-3 first:border-0 first:pt-0">
            <span className="text-gray-400 text-xs mt-1 w-4 shrink-0">{i + 1}.</span>
            <div className="space-y-1 min-w-0">
              <code className="bg-gray-100 px-1.5 py-0.5 rounded text-xs">{action.type}</code>
              {action.description && (
                <p className="text-gray-600 text-xs">{action.description}</p>
              )}
              {action.resource && (
                <p className="text-gray-400 text-xs font-mono">
                  {action.resource}/{action.namespace}/{action.name}
                  {action.patch_type && ` (${action.patch_type})`}
                </p>
              )}
              {action.timeout_secs && (
                <p className="text-gray-400 text-xs">Timeout: {action.timeout_secs}s</p>
              )}
            </div>
          </div>
        ))}
      </div>

      {/* Trigger button */}
      <div className="flex items-center gap-3">
        <button
          onClick={() => trigger.mutate()}
          disabled={!hook.enabled || triggering}
          className="flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-40 text-white text-sm font-medium rounded-lg transition-colors"
        >
          {triggering ? <Loader2 className="w-4 h-4 animate-spin" /> : <Play className="w-4 h-4" />}
          Jetzt ausführen
        </button>
        {trigger.isError && (
          <p className="text-sm text-red-600">{(trigger.error as Error).message}</p>
        )}
        {trigger.isSuccess && !triggering && (
          <p className="text-sm text-green-600">Gestartet — sieh Ausführungen unten.</p>
        )}
      </div>

      {/* Run history */}
      <div className="space-y-2">
        <h2 className="font-medium text-sm">Letzte Ausführungen</h2>

        {runs?.length === 0 && (
          <p className="text-sm text-gray-500">Noch keine Ausführungen.</p>
        )}

        {runs?.map((run) => {
          const expanded = expandedRun === run.id;
          const durationSec = run.ts_end
            ? ((new Date(run.ts_end).getTime() - new Date(run.ts_start).getTime()) / 1000).toFixed(1)
            : null;

          return (
            <div key={run.id} className="bg-white border border-gray-200 rounded-xl overflow-hidden">
              <button
                className="w-full flex items-center gap-3 p-3 text-left hover:bg-gray-50 transition-colors"
                onClick={() => setExpandedRun(expanded ? null : run.id)}
              >
                <RunStatusIcon status={run.status} />
                <div className="flex-1 text-sm">
                  <span className="font-medium capitalize">{run.status}</span>
                  <span className="text-gray-500 ml-2 text-xs">
                    {new Date(run.ts_start).toLocaleString("de-DE")}
                  </span>
                  {durationSec && (
                    <span className="text-gray-400 ml-2 text-xs">{durationSec}s</span>
                  )}
                </div>
                <span className="text-xs text-gray-400">{run.trigger_type}</span>
                {expanded ? (
                  <ChevronDown className="w-4 h-4 text-gray-400" />
                ) : (
                  <ChevronRight className="w-4 h-4 text-gray-400" />
                )}
              </button>

              {expanded && run.action_results.length > 0 && (
                <div className="border-t border-gray-100 px-3 py-2 space-y-1.5">
                  {run.action_results.map((r) => (
                    <div key={r.action_index} className="flex items-start gap-2 text-xs">
                      {r.status === "success" ? (
                        <CheckCircle className="w-3.5 h-3.5 text-green-500 shrink-0 mt-0.5" />
                      ) : (
                        <XCircle className="w-3.5 h-3.5 text-red-500 shrink-0 mt-0.5" />
                      )}
                      <div className="min-w-0">
                        <span className="text-gray-700">
                          Aktion {r.action_index + 1}
                        </span>
                        <code className="ml-1.5 bg-gray-100 px-1 rounded">{r.type}</code>
                        <span className="text-gray-400 ml-1.5">{r.duration_ms}ms</span>
                        {r.error && (
                          <p className="text-red-600 mt-0.5 leading-relaxed">{r.error}</p>
                        )}
                      </div>
                    </div>
                  ))}
                </div>
              )}

              {expanded && run.action_results.length === 0 && (
                <div className="border-t border-gray-100 px-3 py-2 text-xs text-gray-400">
                  Keine Aktions-Details verfügbar.
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}

function RunStatusIcon({ status }: { status: string }) {
  if (status === "success") return <CheckCircle className="w-4 h-4 text-green-500 shrink-0" />;
  if (status === "failed") return <XCircle className="w-4 h-4 text-red-500 shrink-0" />;
  if (status === "partial") return <XCircle className="w-4 h-4 text-yellow-500 shrink-0" />;
  if (status === "running") return <Loader2 className="w-4 h-4 text-blue-500 animate-spin shrink-0" />;
  return <Clock className="w-4 h-4 text-gray-400 shrink-0" />;
}
