import { Package, CheckCircle, AlertTriangle, XCircle } from "lucide-react";

interface Release {
  name: string;
  chart_version: string;
  revision: number;
  status: string;
  deployed_at?: string;
}

export function ReleaseCard({ release }: { release?: Release }) {
  if (!release) return null;

  return (
    <div className="bg-white border border-gray-200 rounded-xl p-4 flex items-center gap-4">
      <Package className="w-8 h-8 text-blue-500" />
      <div>
        <div className="flex items-center gap-2">
          <h2 className="font-semibold text-gray-900">{release.name}</h2>
          <ReleaseStatus status={release.status} />
        </div>
        <p className="text-xs text-gray-500 mt-0.5">
          {release.chart_version} · Revision #{release.revision}
          {release.deployed_at &&
            ` · ${new Date(release.deployed_at).toLocaleString("de-DE")}`}
        </p>
      </div>
    </div>
  );
}

function ReleaseStatus({ status }: { status: string }) {
  if (status === "deployed")
    return (
      <span className="flex items-center gap-1 text-xs text-green-600">
        <CheckCircle className="w-3 h-3" /> deployed
      </span>
    );
  if (status === "hooks-failed")
    return (
      <span className="flex items-center gap-1 text-xs text-yellow-600">
        <AlertTriangle className="w-3 h-3" /> hooks-failed
      </span>
    );
  if (status === "failed")
    return (
      <span className="flex items-center gap-1 text-xs text-red-600">
        <XCircle className="w-3 h-3" /> failed
      </span>
    );
  return <span className="text-xs text-gray-500">{status}</span>;
}
