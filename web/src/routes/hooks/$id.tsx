import { createFileRoute, Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { CheckCircle, XCircle, Clock, ArrowLeft } from "lucide-react";

export const Route = createFileRoute("/hooks/$id")({
  component: HookDetail,
});

interface HookAction {
  type: string;
  description?: string;
}

interface HookDetail {
  id: string;
  name: string;
  builtin: boolean;
  actions: HookAction[];
}

interface HookRun {
  id: string;
  status: string;
  ts_start: string;
  trigger_type: string;
}

function HookDetail() {
  const { id } = Route.useParams();
  const { data: hook } = useQuery({
    queryKey: ["hooks", id],
    queryFn: () => api.get<HookDetail>(`/api/v1/hooks/${id}`),
  });
  const { data: runs } = useQuery({
    queryKey: ["hooks", id, "runs"],
    queryFn: () => api.get<HookRun[]>(`/api/v1/hooks/${id}/runs`),
    refetchInterval: 5_000,
  });

  if (!hook) return <div className="text-sm text-gray-500">Lade...</div>;

  return (
    <div className="space-y-6">
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
      </div>

      <div className="bg-white border border-gray-200 rounded-xl p-4 space-y-3">
        <h2 className="font-medium text-sm text-gray-900">Aktionen</h2>
        {hook.actions?.map((action, i) => (
          <div key={i} className="flex items-start gap-3 text-sm">
            <span className="text-gray-400 text-xs mt-0.5">{i + 1}.</span>
            <div>
              <code className="bg-gray-100 px-1.5 py-0.5 rounded text-xs">
                {action.type}
              </code>
              {action.description && (
                <p className="text-gray-600 mt-0.5 text-xs">{action.description}</p>
              )}
            </div>
          </div>
        ))}
      </div>

      <div className="space-y-3">
        <h2 className="font-medium text-sm text-gray-900">Letzte Ausführungen</h2>
        {runs?.map((run) => (
          <div
            key={run.id}
            className="bg-white border border-gray-200 rounded-xl p-3 flex items-center gap-3"
          >
            <RunStatusIcon status={run.status} />
            <div className="flex-1 text-sm">
              <span className="font-medium capitalize">{run.status}</span>
              <span className="text-gray-500 ml-2 text-xs">
                {new Date(run.ts_start).toLocaleString("de-DE")}
              </span>
            </div>
            <span className="text-xs text-gray-400">{run.trigger_type}</span>
          </div>
        ))}
        {runs?.length === 0 && (
          <p className="text-sm text-gray-500">Noch keine Ausführungen.</p>
        )}
      </div>
    </div>
  );
}

function RunStatusIcon({ status }: { status: string }) {
  if (status === "success") return <CheckCircle className="w-4 h-4 text-green-500" />;
  if (status === "failed") return <XCircle className="w-4 h-4 text-red-500" />;
  if (status === "running") return <Clock className="w-4 h-4 text-blue-500 animate-spin" />;
  return <Clock className="w-4 h-4 text-gray-400" />;
}
