import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { useRef, useState } from "react";
import { useQuery, useMutation } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { useUpgradeStream } from "@/lib/ws";
import { AlertTriangle, ArrowLeft, CheckCircle, XCircle, Zap } from "lucide-react";

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
  const logRef = useRef<HTMLDivElement>(null);
  const [selectedVersion, setSelectedVersion] = useState("");
  const [upgradeId, setUpgradeId] = useState<string | null>(null);
  const [logs, setLogs] = useState<string[]>([]);
  const [done, setDone] = useState(false);
  const [finalStatus, setFinalStatus] = useState<string | null>(null);

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
      setLogs([]);
      setDone(false);
      setFinalStatus(null);
    },
  });

  useUpgradeStream(upgradeId, {
    onLog: (line) => {
      setLogs((prev) => [...prev, line]);
      // Auto-scroll
      setTimeout(() => {
        logRef.current?.scrollTo({ top: logRef.current.scrollHeight, behavior: "smooth" });
      }, 30);
    },
    onDone: (status) => {
      setDone(true);
      setFinalStatus(status);
      if (status === "success") {
        setTimeout(() => navigate({ to: "/helm" }), 3000);
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
        <div className="bg-blue-50 dark:bg-blue-950/40 border border-blue-200 dark:border-blue-800 rounded-xl p-4 text-sm text-gray-800 dark:text-gray-200">
          Aktuelle Version:{" "}
          <code className="font-mono font-medium">{current.chart_version}</code>
          {" · "}Revision #{current.revision}
        </div>
      )}

      <div className="bg-yellow-50 dark:bg-yellow-950/40 border border-yellow-200 dark:border-yellow-800 rounded-xl p-4 flex gap-3 text-sm">
        <AlertTriangle className="w-4 h-4 text-yellow-600 dark:text-yellow-400 shrink-0 mt-0.5" />
        <div>
          <strong className="text-yellow-800 dark:text-yellow-300">Post-Upgrade Hooks werden automatisch ausgeführt.</strong>
          <br />
          <span className="text-yellow-700 dark:text-yellow-400">SFU hostNetwork und Service externalTrafficPolicy werden nach dem Upgrade gepatcht.</span>
        </div>
      </div>

      {!upgradeId ? (
        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              Zielversion
            </label>
            <select
              value={selectedVersion}
              onChange={(e) => setSelectedVersion(e.target.value)}
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
            >
              <option value="">Version wählen...</option>
              {versions && versions.length > 0 && (
                <option value={versions[0].version}>
                  Latest — {versions[0].version}
                </option>
              )}
              {versions?.map((v) => (
                <option key={v.version} value={v.version}>
                  {v.version}
                  {v.version === current?.chart_version ? " (aktuell)" : ""}
                </option>
              ))}
            </select>
          </div>

          <button
            onClick={() => upgrade.mutate(selectedVersion)}
            disabled={!selectedVersion || upgrade.isPending}
            className="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-medium rounded-lg transition-colors"
          >
            {upgrade.isPending ? "Starte..." : "Upgrade starten"}
          </button>

          {upgrade.isError && (
            <p className="text-sm text-red-600">{(upgrade.error as Error).message}</p>
          )}
        </div>
      ) : (
        <div className="space-y-4">
          {/* Log terminal */}
          <div
            ref={logRef}
            className="bg-gray-950 rounded-xl p-4 font-mono text-xs text-green-400 min-h-48 max-h-96 overflow-y-auto"
          >
            {logs.map((line, i) => (
              <div key={i} className="leading-relaxed">{line}</div>
            ))}
            {!done && <div className="animate-pulse mt-1">▋</div>}
          </div>

          {/* Final status banner */}
          {done && finalStatus === "success" && (
            <div className="flex items-center gap-3 bg-green-50 border border-green-200 rounded-xl p-4 text-sm">
              <CheckCircle className="w-5 h-5 text-green-600 shrink-0" />
              <div>
                <strong className="text-green-800">Upgrade erfolgreich.</strong>
                <span className="text-green-600 ml-2">Weiterleitung...</span>
              </div>
            </div>
          )}

          {done && finalStatus === "hooks-failed" && (
            <div className="flex items-start gap-3 bg-yellow-50 border border-yellow-200 rounded-xl p-4 text-sm">
              <AlertTriangle className="w-5 h-5 text-yellow-600 shrink-0 mt-0.5" />
              <div className="space-y-2">
                <div>
                  <strong className="text-yellow-800">Helm-Upgrade erfolgreich, aber Hooks fehlgeschlagen.</strong>
                  <p className="text-yellow-700 mt-0.5">
                    Der ESS-Release ist auf dem neuen Stand. Die Post-Upgrade-Patches (SFU hostNetwork etc.) wurden jedoch nicht vollständig angewendet — WebRTC-Calling könnte beeinträchtigt sein.
                  </p>
                </div>
                <div className="flex gap-2">
                  <Link
                    to="/hooks"
                    className="flex items-center gap-1.5 px-3 py-1.5 bg-yellow-600 hover:bg-yellow-700 text-white rounded-lg text-xs font-medium"
                  >
                    <Zap className="w-3.5 h-3.5" />
                    Hooks manuell ausführen
                  </Link>
                  <Link
                    to="/helm"
                    className="px-3 py-1.5 bg-white border border-yellow-300 text-yellow-700 hover:bg-yellow-50 rounded-lg text-xs"
                  >
                    Zur Helm-Übersicht
                  </Link>
                </div>
              </div>
            </div>
          )}

          {done && finalStatus === "failed" && (
            <div className="flex items-start gap-3 bg-red-50 border border-red-200 rounded-xl p-4 text-sm">
              <XCircle className="w-5 h-5 text-red-600 shrink-0 mt-0.5" />
              <div className="space-y-2">
                <div>
                  <strong className="text-red-800">Upgrade fehlgeschlagen.</strong>
                  <p className="text-red-700 mt-0.5">
                    Helm hat die vorherige Revision automatisch wiederhergestellt. Sieh die Logs oben für Details.
                  </p>
                </div>
                <Link
                  to="/helm"
                  className="inline-block px-3 py-1.5 bg-white border border-red-300 text-red-700 hover:bg-red-50 rounded-lg text-xs"
                >
                  Zur Helm-Übersicht
                </Link>
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
