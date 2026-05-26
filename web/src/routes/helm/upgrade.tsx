import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { useQuery, useMutation } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { useUpgradeStream } from "@/lib/ws";
import { AlertTriangle, ArrowLeft } from "lucide-react";

export const Route = createFileRoute("/helm/upgrade")({
  component: UpgradeWizard,
});

interface HelmRelease {
  chart_version: string;
  revision: number;
}

interface ESSVersion {
  version: string;
}

interface UpgradeResponse {
  upgrade_id: string;
}

function UpgradeWizard() {
  const navigate = useNavigate();
  const [selectedVersion, setSelectedVersion] = useState("");
  const [upgradeId, setUpgradeId] = useState<string | null>(null);
  const [logs, setLogs] = useState<string[]>([]);

  const { data: current } = useQuery({
    queryKey: ["helm", "release"],
    queryFn: () => api.get<HelmRelease>("/api/v1/helm/releases/ess"),
  });
  const { data: versions } = useQuery({
    queryKey: ["helm", "versions"],
    queryFn: () => api.get<ESSVersion[]>("/api/v1/helm/versions"),
  });

  const upgrade = useMutation({
    mutationFn: (toVersion: string) =>
      api.post<UpgradeResponse>("/api/v1/helm/releases/ess/upgrade", { to_version: toVersion }),
    onSuccess: (res) => {
      setUpgradeId(res.upgrade_id);
      setLogs([`Upgrade gestartet: ${res.upgrade_id}`]);
    },
  });

  useUpgradeStream(upgradeId, {
    onLog: (line) => setLogs((prev) => [...prev, line]),
    onDone: (status) => {
      if (status === "success") {
        setTimeout(() => navigate({ to: "/helm" }), 2000);
      }
    },
  });

  return (
    <div className="space-y-6 max-w-2xl">
      <div className="flex items-center gap-3">
        <Link to="/helm" className="text-gray-400 hover:text-gray-600">
          <ArrowLeft className="w-5 h-5" />
        </Link>
        <h1 className="text-2xl font-semibold">ESS Upgrade</h1>
      </div>

      {current && (
        <div className="bg-blue-50 border border-blue-200 rounded-xl p-4 text-sm">
          Aktuelle Version:{" "}
          <code className="font-mono font-medium">{current.chart_version}</code>
          {" · "}Revision #{current.revision}
        </div>
      )}

      <div className="bg-yellow-50 border border-yellow-200 rounded-xl p-4 flex gap-3 text-sm">
        <AlertTriangle className="w-4 h-4 text-yellow-600 shrink-0 mt-0.5" />
        <div>
          <strong>Post-Upgrade Hooks werden automatisch ausgeführt.</strong>
          <br />
          SFU hostNetwork und Service externalTrafficPolicy werden nach dem Upgrade gepatcht.
        </div>
      </div>

      {!upgradeId ? (
        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-2">
              Zielversion
            </label>
            <select
              value={selectedVersion}
              onChange={(e) => setSelectedVersion(e.target.value)}
              className="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
            >
              <option value="">Version wählen...</option>
              {versions?.map((v) => (
                <option key={v.version} value={v.version}>
                  {v.version}
                </option>
              ))}
            </select>
          </div>

          <button
            onClick={() => upgrade.mutate(selectedVersion)}
            disabled={!selectedVersion || upgrade.isPending}
            className="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-medium rounded-lg transition-colors"
          >
            {upgrade.isPending ? "Starte Upgrade..." : "Upgrade starten"}
          </button>

          {upgrade.isError && (
            <p className="text-sm text-red-600">
              {(upgrade.error as Error).message}
            </p>
          )}
        </div>
      ) : (
        <div className="bg-gray-900 rounded-xl p-4 font-mono text-xs text-green-400 min-h-48 max-h-96 overflow-y-auto">
          {logs.map((line, i) => (
            <div key={i}>{line}</div>
          ))}
          <div className="animate-pulse">▋</div>
        </div>
      )}
    </div>
  );
}
