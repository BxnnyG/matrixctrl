import { createFileRoute, Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { CheckCircle, XCircle, AlertTriangle, Clock, ArrowLeft } from "lucide-react";

export const Route = createFileRoute("/helm/history")({
  component: HelmHistory,
});

interface UpgradeEntry {
  id: string;
  from_version: string;
  to_version: string;
  status: string;
  ts_initiated: string;
  helm_revision?: number;
}

function HelmHistory() {
  const { data: history } = useQuery({
    queryKey: ["helm", "history"],
    queryFn: () => api.get<UpgradeEntry[]>("/api/v1/helm/releases/ess/history"),
  });

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-3">
        <Link to="/helm" className="text-gray-400 hover:text-gray-600">
          <ArrowLeft className="w-5 h-5" />
        </Link>
        <h1 className="text-2xl font-semibold">Upgrade-History</h1>
      </div>

      <div className="space-y-3">
        {history?.map((entry) => (
          <div
            key={entry.id}
            className="bg-white border border-gray-200 rounded-xl p-4 flex items-center gap-4"
          >
            <StatusIcon status={entry.status} />
            <div className="flex-1">
              <div className="text-sm font-medium">
                {entry.from_version} → {entry.to_version}
              </div>
              <div className="text-xs text-gray-500 mt-0.5">
                {new Date(entry.ts_initiated).toLocaleString("de-DE")}
                {entry.helm_revision && ` · Revision #${entry.helm_revision}`}
              </div>
            </div>
            <StatusBadge status={entry.status} />
          </div>
        ))}
        {history?.length === 0 && (
          <p className="text-sm text-gray-500">Noch keine Upgrades über MatrixCtrl.</p>
        )}
      </div>
    </div>
  );
}

function StatusIcon({ status }: { status: string }) {
  if (status === "success") return <CheckCircle className="w-4 h-4 text-green-500" />;
  if (status === "failed") return <XCircle className="w-4 h-4 text-red-500" />;
  if (status === "hooks-failed") return <AlertTriangle className="w-4 h-4 text-yellow-500" />;
  return <Clock className="w-4 h-4 text-blue-500" />;
}

function StatusBadge({ status }: { status: string }) {
  const cls =
    status === "success" ? "bg-green-100 text-green-700" :
    status === "failed" ? "bg-red-100 text-red-700" :
    status === "hooks-failed" ? "bg-yellow-100 text-yellow-700" :
    "bg-gray-100 text-gray-600";
  return (
    <span className={`text-xs px-2 py-1 rounded-full font-medium ${cls}`}>
      {status}
    </span>
  );
}
