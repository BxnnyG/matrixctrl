import { Package, CheckCircle, AlertTriangle, XCircle } from "lucide-react";

interface Release {
  name: string;
  chart_version: string;
  revision: number;
  status: string;
  deployed_at?: string;
}

// "matrix-stack-26.5.1" → "26.5.1"
function essVersion(chartVersion: string) {
  return chartVersion.replace(/^matrix-stack-/, "");
}

export function ReleaseCard({ release }: { release?: Release }) {
  if (!release) return null;

  return (
    <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-xl p-4 flex items-center gap-4">
      <Package className="w-8 h-8 text-blue-500 shrink-0" />
      <div className="min-w-0">
        <div className="flex items-center gap-2 flex-wrap">
          <h2 className="font-semibold text-gray-900 dark:text-gray-100">{release.name}</h2>
          <span className="text-sm font-mono font-medium text-blue-600 dark:text-blue-400">
            ESS {essVersion(release.chart_version)}
          </span>
          <ReleaseStatus status={release.status} />
        </div>
        <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
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
      <span className="flex items-center gap-1 text-xs text-green-600 dark:text-green-400">
        <CheckCircle className="w-3 h-3" /> deployed
      </span>
    );
  if (status === "hooks-failed")
    return (
      <span className="flex items-center gap-1 text-xs text-yellow-600 dark:text-yellow-400">
        <AlertTriangle className="w-3 h-3" /> hooks-failed
      </span>
    );
  if (status === "failed")
    return (
      <span className="flex items-center gap-1 text-xs text-red-600 dark:text-red-400">
        <XCircle className="w-3 h-3" /> failed
      </span>
    );
  return <span className="text-xs text-gray-500 dark:text-gray-400">{status}</span>;
}
