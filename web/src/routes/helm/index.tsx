import { createFileRoute, Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Package, Clock, CheckCircle, XCircle, AlertTriangle } from "lucide-react";

export const Route = createFileRoute("/helm/")({
  component: HelmPage,
});

interface HelmRelease {
  name: string;
  namespace: string;
  chart_version: string;
  revision: number;
  status: string;
  deployed_at?: string;
}

interface ESSVersion {
  version: string;
  published_at?: string;
}

function essVersion(v: string) {
  return v.replace(/^matrix-stack-/, "");
}

function HelmPage() {
  const { data: release } = useQuery({
    queryKey: ["helm", "release"],
    queryFn: () => api.get<HelmRelease>("/api/v1/helm/releases/ess"),
    refetchInterval: 15_000,
  });
  const { data: versions } = useQuery({
    queryKey: ["helm", "versions"],
    queryFn: () => api.get<ESSVersion[]>("/api/v1/helm/versions"),
  });

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold text-gray-900 dark:text-gray-100">Helm Release</h1>
        <Link to="/helm/upgrade" className="px-3 py-1.5 bg-blue-600 text-white text-sm rounded-lg hover:bg-blue-700">
          Upgrade
        </Link>
      </div>

      {release && (
        <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-xl p-4">
          <div className="flex items-center gap-3 mb-4">
            <Package className="w-5 h-5 text-blue-500" />
            <div>
              <h2 className="font-medium text-gray-900 dark:text-gray-100">{release.name}</h2>
              <p className="text-xs text-gray-500 dark:text-gray-400">Namespace: {release.namespace}</p>
            </div>
            <div className="ml-auto">
              <ReleaseStatusBadge status={release.status} />
            </div>
          </div>
          <div className="grid grid-cols-2 gap-4 text-sm">
            <div>
              <span className="text-gray-500 dark:text-gray-400 text-xs">ESS Version</span>
              <p className="font-mono font-medium text-gray-900 dark:text-gray-100">
                {essVersion(release.chart_version)}
              </p>
            </div>
            <div>
              <span className="text-gray-500 dark:text-gray-400 text-xs">Revision</span>
              <p className="font-mono font-medium text-gray-900 dark:text-gray-100">#{release.revision}</p>
            </div>
            <div>
              <span className="text-gray-500 dark:text-gray-400 text-xs">Chart</span>
              <p className="font-mono text-xs text-gray-600 dark:text-gray-400">{release.chart_version}</p>
            </div>
            <div>
              <span className="text-gray-500 dark:text-gray-400 text-xs">Zuletzt deployed</span>
              <p className="text-sm text-gray-900 dark:text-gray-100">
                {release.deployed_at ? new Date(release.deployed_at).toLocaleString("de-DE") : "—"}
              </p>
            </div>
          </div>
        </div>
      )}

      {versions && versions.length > 0 && (
        <div>
          <h2 className="text-lg font-medium text-gray-900 dark:text-gray-100 mb-3">Verfügbare Versionen</h2>
          <div className="space-y-2">
            {versions.slice(0, 8).map((v, i) => (
              <div key={v.version} className="flex items-center justify-between bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg px-4 py-2 text-sm">
                <div className="flex items-center gap-2">
                  <span className="font-mono text-gray-900 dark:text-gray-100">{essVersion(v.version)}</span>
                  {i === 0 && (
                    <span className="text-xs bg-blue-100 dark:bg-blue-900 text-blue-700 dark:text-blue-300 px-1.5 py-0.5 rounded">latest</span>
                  )}
                  {v.version === release?.chart_version && (
                    <span className="text-xs bg-green-100 dark:bg-green-900 text-green-700 dark:text-green-300 px-1.5 py-0.5 rounded">aktuell</span>
                  )}
                </div>
                {v.published_at && (
                  <span className="text-gray-500 dark:text-gray-400 text-xs">
                    {new Date(v.published_at).toLocaleDateString("de-DE")}
                  </span>
                )}
              </div>
            ))}
          </div>
        </div>
      )}

      <div className="flex justify-end">
        <Link to="/helm/history" className="text-sm text-blue-600 dark:text-blue-400 hover:text-blue-700">
          Upgrade-History →
        </Link>
      </div>
    </div>
  );
}

function ReleaseStatusBadge({ status }: { status: string }) {
  const map: Record<string, { icon: React.ReactNode; cls: string }> = {
    deployed: { icon: <CheckCircle className="w-3.5 h-3.5" />, cls: "bg-green-100 dark:bg-green-900 text-green-700 dark:text-green-300" },
    failed: { icon: <XCircle className="w-3.5 h-3.5" />, cls: "bg-red-100 dark:bg-red-900 text-red-700 dark:text-red-300" },
    "hooks-failed": { icon: <AlertTriangle className="w-3.5 h-3.5" />, cls: "bg-yellow-100 dark:bg-yellow-900 text-yellow-700 dark:text-yellow-300" },
    pending: { icon: <Clock className="w-3.5 h-3.5" />, cls: "bg-blue-100 dark:bg-blue-900 text-blue-700 dark:text-blue-300" },
  };
  const s = map[status] ?? map["pending"];
  return (
    <span className={`flex items-center gap-1 text-xs px-2 py-1 rounded-full font-medium ${s.cls}`}>
      {s.icon} {status}
    </span>
  );
}
