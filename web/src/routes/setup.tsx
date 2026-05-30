import { createFileRoute } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
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
  const { data, isLoading } = useQuery({
    queryKey: ["setup", "status"],
    queryFn: () => api.get<SetupStatus>("/api/v1/setup/status"),
    refetchInterval: 30_000,
  });

  if (isLoading) {
    return <div className="flex items-center gap-2 text-sm text-gray-500 dark:text-gray-400"><Loader2 className="w-4 h-4 animate-spin" /> Lade…</div>;
  }
  if (!data) return null;

  const allGreen = data.ess_installed && data.config_sections > 0 && (data.oidc_configured || true);

  return (
    <div className="space-y-6 max-w-3xl">
      <div>
        <h1 className="text-2xl font-semibold text-gray-900 dark:text-gray-100">Setup & Onboarding</h1>
        <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
          Integrationsstatus von MatrixCtrl ↔ ESS ↔ Matrix-Login.
        </p>
      </div>

      {/* Overall banner */}
      {allGreen ? (
        <div className="flex items-center gap-3 bg-green-50 dark:bg-green-950/40 border border-green-200 dark:border-green-800 rounded-xl px-4 py-3 text-sm">
          <CheckCircle2 className="w-5 h-5 text-green-600 dark:text-green-400 shrink-0" />
          <span className="text-green-800 dark:text-green-300">Alles verbunden — MatrixCtrl verwaltet dein ESS-Deployment.</span>
        </div>
      ) : (
        <div className="flex items-center gap-3 bg-blue-50 dark:bg-blue-950/40 border border-blue-200 dark:border-blue-800 rounded-xl px-4 py-3 text-sm">
          <Rocket className="w-5 h-5 text-blue-600 dark:text-blue-400 shrink-0" />
          <span className="text-blue-800 dark:text-blue-300">Greenfield erkannt — ESS ist noch nicht deployed. Der Deploy-Assistent kommt in Phase 1.5.</span>
        </div>
      )}

      {/* Checklist */}
      <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-xl shadow-sm divide-y divide-gray-100 dark:divide-gray-700/60">
        <Row
          ok={data.ess_installed} icon={Server}
          title="ESS deployed"
          detail={data.ess_installed
            ? `Release „${data.ess_release}" v${data.ess_version ?? "?"} (${data.ess_status ?? "?"}) in Namespace ${data.ess_namespace}`
            : `Kein Release „${data.ess_release}" in Namespace ${data.ess_namespace} gefunden`}
        />
        <Row
          ok={data.config_sections > 0} icon={SlidersHorizontal}
          title="Config-Sektionen"
          detail={`${data.config_sections} Sektions-Dateien im versionierten Config-Repo`}
        />
        <Row
          ok={data.oidc_configured} warn={!data.oidc_configured} icon={ShieldCheck}
          title="Matrix-Login (OIDC)"
          detail={data.oidc_configured
            ? "Admin-only Login via MAS ist aktiv"
            : "Bootstrap-Modus aktiv (lokaler Admin) — für Greenfield ok; nach ESS-Deploy auf OIDC umschalten"}
        />
      </div>

      {/* Roadmap hint */}
      <div className="flex items-start gap-3 bg-gray-50 dark:bg-gray-900/50 border border-gray-200 dark:border-gray-800 rounded-xl px-4 py-3 text-sm">
        <Info className="w-4 h-4 text-gray-400 shrink-0 mt-0.5" />
        <div className="text-gray-600 dark:text-gray-400">
          <strong className="text-gray-800 dark:text-gray-200">Phase 1.5 (in Arbeit):</strong> Greenfield-Deploy von ESS im
          Bootstrap-Modus, Config-Seed aus den Chart-Defaults, und automatische OIDC-Client-Registrierung via MAS Admin API.
          Siehe <code className="bg-gray-100 dark:bg-gray-800 px-1 rounded text-xs">docs/SETUP.md</code>.
        </div>
      </div>
    </div>
  );
}
