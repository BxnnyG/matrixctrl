import { createFileRoute, Link } from "@tanstack/react-router";
import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Play, CheckCircle, XCircle, Clock, Loader2 } from "lucide-react";

export const Route = createFileRoute("/hooks/")({
  component: HooksList,
});

interface HookAction {
  type: string;
}

interface Hook {
  id: string;
  name: string;
  description?: string;
  trigger: string;
  enabled: boolean;
  priority: number;
  builtin: boolean;
  actions: HookAction[];
  lastRunStatus?: string;
}

function HooksList() {
  const qc = useQueryClient();
  const [triggeringId, setTriggeringId] = useState<string | null>(null);

  const { data: hooks, isLoading } = useQuery({
    queryKey: ["hooks"],
    queryFn: () => api.get<Hook[]>("/api/v1/hooks"),
  });

  const trigger = useMutation({
    mutationFn: (id: string) => {
      setTriggeringId(id);
      return api.post(`/api/v1/hooks/${id}/trigger`, {});
    },
    onSettled: () => {
      setTriggeringId(null);
      qc.invalidateQueries({ queryKey: ["hooks"] });
    },
  });

  const toggle = useMutation({
    mutationFn: (hook: Hook) =>
      api.put(`/api/v1/hooks/${hook.id}`, {
        name: hook.name,
        description: hook.description ?? "",
        enabled: !hook.enabled,
        priority: hook.priority,
        actions: hook.actions,
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["hooks"] }),
  });

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold">Post-Upgrade Hooks</h1>

      {isLoading && <div className="text-sm text-gray-500">Lade Hooks...</div>}

      <div className="space-y-3">
        {hooks?.map((hook) => (
          <div
            key={hook.id}
            className={`bg-white border rounded-xl p-4 ${
              hook.enabled ? "border-gray-200" : "border-gray-100 opacity-60"
            }`}
          >
            <div className="flex items-start gap-4">
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2 flex-wrap">
                  <span className="font-medium text-sm">{hook.name}</span>
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
                  {hook.lastRunStatus && (
                    <LastRunBadge status={hook.lastRunStatus} />
                  )}
                </div>
                {hook.description && (
                  <p className="text-xs text-gray-500 mt-1 leading-relaxed">
                    {hook.description}
                  </p>
                )}
                <div className="text-xs text-gray-400 mt-1">
                  <code className="bg-gray-100 px-1 rounded">{hook.trigger}</code>
                  {" · "}Priorität {hook.priority}
                  {" · "}{hook.actions.length} {hook.actions.length === 1 ? "Aktion" : "Aktionen"}
                </div>
              </div>

              <div className="flex items-center gap-2 shrink-0">
                <button
                  onClick={() => trigger.mutate(hook.id)}
                  disabled={!hook.enabled || triggeringId !== null}
                  className="flex items-center gap-1.5 px-3 py-1.5 text-xs bg-gray-100 hover:bg-gray-200 disabled:opacity-40 rounded-lg transition-colors"
                  title="Jetzt ausführen"
                >
                  {triggeringId === hook.id ? (
                    <Loader2 className="w-3 h-3 animate-spin" />
                  ) : (
                    <Play className="w-3 h-3" />
                  )}
                  Ausführen
                </button>

                {!hook.builtin && (
                  <button
                    onClick={() => toggle.mutate(hook)}
                    disabled={toggle.isPending}
                    className="px-3 py-1.5 text-xs text-gray-600 bg-gray-100 hover:bg-gray-200 disabled:opacity-40 rounded-lg transition-colors"
                  >
                    {hook.enabled ? "Deaktivieren" : "Aktivieren"}
                  </button>
                )}

                <Link
                  to="/hooks/$id"
                  params={{ id: hook.id }}
                  className="px-3 py-1.5 text-xs text-blue-600 hover:bg-blue-50 rounded-lg transition-colors"
                >
                  Details
                </Link>
              </div>
            </div>
          </div>
        ))}

        {hooks?.length === 0 && (
          <p className="text-sm text-gray-500">Keine Hooks konfiguriert.</p>
        )}
      </div>
    </div>
  );
}

function LastRunBadge({ status }: { status: string }) {
  if (status === "success")
    return (
      <span className="flex items-center gap-1 text-xs text-green-700 bg-green-50 px-1.5 py-0.5 rounded">
        <CheckCircle className="w-3 h-3" /> OK
      </span>
    );
  if (status === "failed" || status === "partial")
    return (
      <span className="flex items-center gap-1 text-xs text-red-700 bg-red-50 px-1.5 py-0.5 rounded">
        <XCircle className="w-3 h-3" /> {status}
      </span>
    );
  if (status === "running")
    return (
      <span className="flex items-center gap-1 text-xs text-blue-700 bg-blue-50 px-1.5 py-0.5 rounded">
        <Clock className="w-3 h-3 animate-spin" /> läuft
      </span>
    );
  return null;
}
