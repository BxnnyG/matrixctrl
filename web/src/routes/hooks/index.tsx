import { createFileRoute, Link } from "@tanstack/react-router";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Play, CheckCircle, XCircle, Clock, Zap } from "lucide-react";

export const Route = createFileRoute("/hooks/")({
  component: HooksList,
});

interface Hook {
  id: string;
  name: string;
  trigger: string;
  enabled: boolean;
  priority: number;
  builtin: boolean;
  actions: unknown[];
  lastRunStatus?: string;
}

function HooksList() {
  const qc = useQueryClient();
  const { data: hooks, isLoading } = useQuery({
    queryKey: ["hooks"],
    queryFn: () => api.get<Hook[]>("/api/v1/hooks"),
  });

  const trigger = useMutation({
    mutationFn: (id: string) => api.post(`/api/v1/hooks/${id}/trigger`, {}),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["hooks"] }),
  });

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Post-Upgrade Hooks</h1>
      </div>

      {isLoading && <div className="text-sm text-gray-500">Lade Hooks...</div>}

      <div className="space-y-3">
        {hooks?.map((hook) => (
          <div
            key={hook.id}
            className="bg-white border border-gray-200 rounded-xl p-4 flex items-center gap-4"
          >
            <div className="flex-1">
              <div className="flex items-center gap-2">
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
              </div>
              <div className="text-xs text-gray-500 mt-0.5">
                Trigger: <code className="bg-gray-100 px-1 rounded">{hook.trigger}</code>
                {" · "}Priority: {hook.priority}
                {" · "}
                {hook.actions?.length ?? 0} Aktionen
              </div>
            </div>

            <div className="flex items-center gap-2">
              {hook.lastRunStatus && (
                <StatusIcon status={hook.lastRunStatus} />
              )}
              <button
                onClick={() => trigger.mutate(hook.id)}
                disabled={trigger.isPending || !hook.enabled}
                className="flex items-center gap-1.5 px-3 py-1.5 text-xs bg-gray-100 hover:bg-gray-200 disabled:opacity-50 rounded-lg transition-colors"
              >
                <Play className="w-3 h-3" />
                Ausführen
              </button>
              <Link
                to="/hooks/$id"
                params={{ id: hook.id }}
                className="px-3 py-1.5 text-xs text-gray-600 hover:bg-gray-100 rounded-lg"
              >
                Details
              </Link>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

function StatusIcon({ status }: { status: string }) {
  if (status === "success") return <CheckCircle className="w-4 h-4 text-green-500" />;
  if (status === "failed" || status === "partial") return <XCircle className="w-4 h-4 text-red-500" />;
  if (status === "running") return <Clock className="w-4 h-4 text-blue-500 animate-spin" />;
  return <Zap className="w-4 h-4 text-gray-400" />;
}
