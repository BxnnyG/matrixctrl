import { createFileRoute } from "@tanstack/react-router";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, useRef } from "react";
import { api } from "@/lib/api";
import { useUpgradeStream } from "@/lib/ws";
import { CheckCircle2, XCircle, AlertTriangle, Loader2, Rocket, ShieldCheck, Server, SlidersHorizontal, Info } from "lucide-react";

export const Route = createFileRoute("/setup")({
  component: Setup,
});

interface SetupStatus {
  ess_namespace: string;
  ess_release: string;
  ess_installed: boolean;
  ess_version?: string;
  ess_status?: string;
  oidc_configured: boolean;
  bootstrap_active: boolean;
  config_sections: number;
}
interface ESSVersion { version: string }
interface DeployResponse { upgrade_id: string }

function Row({ ok, warn, icon: Icon, title, detail }: { ok: boolean; warn?: boolean; icon: React.ElementType; title: string; detail: string }) {
  const Badge = ok ? CheckCircle2 : warn ? AlertTriangle : XCircle;
  const color = ok ? "text-green-500" : warn ? "text-yellow-500" : "text-red-500";
  return (
    <div className="flex items-start gap-3 px-4 py-3.5">
      <div className="flex items-center justify-center w-9 h-9 rounded-lg bg-gray-100 dark:bg-gray-700/60 text-gray-500 dark:text-gray-400 shrink-0">
        <Icon className="w-[18px] h-[18px]" />
      </div>
      <div className="flex-1 min-w-0">
        <div className="text-sm font-medium text-gray-900 dark:text-gray-100">{title}</div>
        <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">{detail}</p>
      </div>
      <Badge className={`w-5 h-5 shrink-0 ${color}`} />
    </div>
  );
}

function Setup() {
  const qc = useQueryClient();
  const { data, isLoading } = useQuery({
    queryKey: ["setup", "status"],
    queryFn: () => api.get<SetupStatus>("/api/v1/setup/status"),
    refetchInterval: 30_000,
  });

  if (isLoading) {
    return <div className="flex items-center gap-2 text-sm text-gray-500 dark:text-gray-400"><Loader2 className="w-4 h-4 animate-spin" /> Lade…</div>;
  }
  if (!data) return null;

  const allGreen = data.ess_installed && data.config_sections > 0;

  return (
    <div className="space-y-6 max-w-3xl">
      <div>
        <h1 className="text-2xl font-semibold text-gray-900 dark:text-gray-100">Setup & Onboarding</h1>
        <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">Integrationsstatus von MatrixCtrl ↔ ESS ↔ Matrix-Login.</p>
      </div>

      {allGreen ? (
        <div className="flex items-center gap-3 bg-green-50 dark:bg-green-950/40 border border-green-200 dark:border-green-800 rounded-xl px-4 py-3 text-sm">
          <CheckCircle2 className="w-5 h-5 text-green-600 dark:text-green-400 shrink-0" />
          <span className="text-green-800 dark:text-green-300">Alles verbunden — MatrixCtrl verwaltet dein ESS-Deployment.</span>
        </div>
      ) : (
        <DeployWizard release={data.ess_release} onDone={() => qc.invalidateQueries({ queryKey: ["setup", "status"] })} />
      )}

      <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-xl shadow-sm divide-y divide-gray-100 dark:divide-gray-700/60">
        <Row ok={data.ess_installed} icon={Server} title="ESS deployed"
          detail={data.ess_installed ? `Release „${data.ess_release}" v${data.ess_version ?? "?"} (${data.ess_status ?? "?"}) in ${data.ess_namespace}` : `Kein Release „${data.ess_release}" in ${data.ess_namespace}`} />
        <Row ok={data.config_sections > 0} icon={SlidersHorizontal} title="Config-Sektionen"
          detail={`${data.config_sections} Sektions-Dateien im versionierten Config-Repo`} />
        <Row ok={data.oidc_configured} warn={!data.oidc_configured} icon={ShieldCheck} title="Matrix-Login (OIDC)"
          detail={data.oidc_configured ? "Admin-only Login via MAS ist aktiv" : "Bootstrap-Modus (lokaler Admin) — nach ESS-Deploy auf OIDC umschalten"} />
      </div>

      <div className="flex items-start gap-3 bg-gray-50 dark:bg-gray-900/50 border border-gray-200 dark:border-gray-800 rounded-xl px-4 py-3 text-sm">
        <Info className="w-4 h-4 text-gray-400 shrink-0 mt-0.5" />
        <div className="text-gray-600 dark:text-gray-400">
          <strong className="text-gray-800 dark:text-gray-200">Phase 1.5:</strong> Greenfield-Deploy (oben) seedet die Config aus den
          Chart-Defaults und installiert ESS. Noch offen: automatische OIDC-Client-Registrierung via MAS Admin API. Siehe{" "}
          <code className="bg-gray-100 dark:bg-gray-800 px-1 rounded text-xs">docs/SETUP.md</code>.
        </div>
      </div>
    </div>
  );
}

function DeployWizard({ release, onDone }: { release: string; onDone: () => void }) {
  const [serverName, setServerName] = useState("");
  const [version, setVersion] = useState("");
  const [deployId, setDeployId] = useState<string | null>(null);
  const [logs, setLogs] = useState<string[]>([]);
  const [done, setDone] = useState(false);
  const [status, setStatus] = useState<string | null>(null);
  const logRef = useRef<HTMLDivElement>(null);

  const { data: versions } = useQuery({
    queryKey: ["helm", "versions"],
    queryFn: () => api.get<ESSVersion[]>("/api/v1/helm/versions"),
  });

  const deploy = useMutation({
    mutationFn: () => api.post<DeployResponse>("/api/v1/setup/deploy-ess", { version, server_name: serverName }),
    onSuccess: (res) => { setDeployId(res.upgrade_id); setLogs([]); setDone(false); setStatus(null); },
  });

  useUpgradeStream(deployId, {
    onLog: (line) => { setLogs((p) => [...p, line]); setTimeout(() => logRef.current?.scrollTo({ top: logRef.current.scrollHeight, behavior: "smooth" }), 30); },
    onDone: (s) => { setDone(true); setStatus(s); if (s === "success") onDone(); },
  });

  const validDomain = /^[a-z0-9.-]+\.[a-z]{2,}$/i.test(serverName);

  return (
    <div className="bg-white dark:bg-gray-800 border border-blue-200 dark:border-blue-900/60 rounded-xl shadow-sm overflow-hidden">
      <div className="flex items-center gap-3 px-4 py-3 bg-blue-50/60 dark:bg-blue-950/30 border-b border-blue-100 dark:border-blue-900/40">
        <div className="flex items-center justify-center w-9 h-9 rounded-lg bg-gradient-to-br from-blue-500 to-[#0DBD8B] text-white shrink-0"><Rocket className="w-[18px] h-[18px]" /></div>
        <div>
          <div className="text-sm font-semibold text-gray-900 dark:text-gray-100">ESS deployen</div>
          <div className="text-xs text-gray-500 dark:text-gray-400">Greenfield — Release „{release}" ist noch nicht installiert</div>
        </div>
      </div>

      {!deployId ? (
        <div className="p-4 space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1.5">Server Name</label>
              <input value={serverName} onChange={(e) => setServerName(e.target.value)} placeholder="example.com"
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-gray-100 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-blue-500" />
              <p className="text-[11px] text-gray-400 dark:text-gray-500 mt-1">Hostnames werden abgeleitet: matrix., mas., element., admin., mrtc.</p>
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1.5">ESS-Version</label>
              <select value={version} onChange={(e) => setVersion(e.target.value)}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-900 dark:text-gray-100 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-blue-500">
                <option value="">Version wählen…</option>
                {versions?.map((v) => <option key={v.version} value={v.version}>{v.version}</option>)}
              </select>
            </div>
          </div>
          <div className="flex items-center gap-3">
            <button onClick={() => deploy.mutate()} disabled={!validDomain || !version || deploy.isPending}
              className="flex items-center gap-1.5 px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-medium rounded-lg">
              {deploy.isPending ? <Loader2 className="w-4 h-4 animate-spin" /> : <Rocket className="w-4 h-4" />} ESS deployen
            </button>
            {serverName && !validDomain && <span className="text-xs text-yellow-600 dark:text-yellow-400">Bitte eine gültige Domain eingeben</span>}
            {deploy.isError && <span className="text-xs text-red-600 dark:text-red-400">{(deploy.error as Error).message}</span>}
          </div>
        </div>
      ) : (
        <div className="p-4 space-y-3">
          <div className="flex items-center gap-3 text-sm">
            <span className="font-medium text-gray-700 dark:text-gray-300">Deploy</span>
            {done && status === "success" && <span className="flex items-center gap-1 text-xs text-green-600 dark:text-green-400"><CheckCircle2 className="w-3.5 h-3.5" /> Erfolgreich</span>}
            {done && status === "hooks-failed" && <span className="flex items-center gap-1 text-xs text-yellow-600 dark:text-yellow-400"><AlertTriangle className="w-3.5 h-3.5" /> Hooks fehlgeschlagen</span>}
            {done && status === "failed" && <span className="flex items-center gap-1 text-xs text-red-600 dark:text-red-400"><XCircle className="w-3.5 h-3.5" /> Fehlgeschlagen</span>}
          </div>
          <div ref={logRef} className="bg-gray-950 rounded-lg p-3 font-mono text-xs text-green-400 max-h-72 overflow-y-auto">
            {logs.map((line, i) => <div key={i} className={`leading-relaxed ${line.startsWith("ERROR") ? "text-red-400" : line.startsWith("WARNING") ? "text-yellow-400" : ""}`}>{line}</div>)}
            {!done && <div className="animate-pulse mt-1">▋</div>}
          </div>
        </div>
      )}
    </div>
  );
}
