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
        <h1 className="text-2xl font-semibold">Helm Release</h1>
        <Link
          to="/helm/upgrade"
          className="px-3 py-1.5 bg-blue-600 text-white text-sm rounded-lg hover:bg-blue-700"
        >
          Upgrade
        </Link>
      </div>

      {release && (
        <div className="bg-white border border-gray-200 rounded-xl p-4">
          <div className="flex items-center gap-3 mb-4">
            <Package className="w-5 h-5 text-blue-500" />
            <div>
              <h2 className="font-medium">{release.name}</h2>
              <p className="text-xs text-gray-500">Namespace: {release.namespace}</p>
            </div>
            <div className="ml-auto">
              <ReleaseStatusBadge status={release.status} />
            </div>
          </div>
          <div className="grid grid-cols-2 gap-4 text-sm">
            <div>
              <span className="text-gray-500">Version</span>
              <p className="font-mono font-medium">{release.chart_version}</p>
            </div>
            <div>
              <span className="text-gray-500">Revision</span>
              <p className="font-mono font-medium">#{release.revision}</p>
            </div>
            <div>
              <span className="text-gray-500">Zuletzt deployed</span>
              <p className="font-medium">
                {release.deployed_at
                  ? new Date(release.deployed_at).toLocaleString("de-DE")
                  : "—"}
              </p>
            </div>
          </div>
        </div>
      )}

      {versions && versions.length > 0 && (
        <div>
          <h2 className="text-lg font-medium mb-3">Verfügbare Versionen</h2>
          <div className="space-y-2">
            {versions.slice(0, 5).map((v) => (
              <div
                key={v.version}
                className="flex items-center justify-between bg-white border border-gray-200 rounded-lg px-4 py-2 text-sm"
              >
                <span className="font-mono">{v.version}</span>
                {v.published_at && (
                  <span className="text-gray-500 text-xs">
                    {new Date(v.published_at).toLocaleDateString("de-DE")}
                  </span>
                )}
              </div>
            ))}
          </div>
        </div>
      )}

      <div className="flex justify-end">
        <Link
          to="/helm/history"
          className="text-sm text-blue-600 hover:text-blue-700"
        >
          Upgrade-History →
        </Link>
      </div>
    </div>
  );
}

function ReleaseStatusBadge({ status }: { status: string }) {
  const map: Record<string, { icon: React.ReactNode; cls: string }> = {
    deployed: { icon: <CheckCircle className="w-3.5 h-3.5" />, cls: "bg-green-100 text-green-700" },
    failed: { icon: <XCircle className="w-3.5 h-3.5" />, cls: "bg-red-100 text-red-700" },
    "hooks-failed": { icon: <AlertTriangle className="w-3.5 h-3.5" />, cls: "bg-yellow-100 text-yellow-700" },
    pending: { icon: <Clock className="w-3.5 h-3.5" />, cls: "bg-blue-100 text-blue-700" },
  };
  const s = map[status] ?? map["pending"];
  return (
    <span className={`flex items-center gap-1 text-xs px-2 py-1 rounded-full font-medium ${s.cls}`}>
      {s.icon} {status}
    </span>
  );
}
