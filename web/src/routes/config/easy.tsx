import { createFileRoute, Link } from "@tanstack/react-router";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, useEffect, useRef } from "react";
import { api } from "@/lib/api";
import { useUpgradeStream } from "@/lib/ws";
import { ArrowLeft, Code2, Save, Rocket, CheckCircle2, AlertTriangle, XCircle, Loader2, Zap } from "lucide-react";

export const Route = createFileRoute("/config/easy")({
  component: EasyMode,
});

interface EasyField {
  path: string;
  label: string;
  help: string;
  type: "bool" | "string" | "select";
  group: string;
  options?: string[];
}

interface EasyResponse {
  fields: EasyField[];
  values: Record<string, unknown>;
}

interface DeployResponse {
  upgrade_id: string;
}

function EasyMode() {
  const qc = useQueryClient();
  const { data, isLoading } = useQuery({
    queryKey: ["config", "easy"],
    queryFn: () => api.get<EasyResponse>("/api/v1/config/easy"),
  });

  const [values, setValues] = useState<Record<string, unknown>>({});
  const [dirty, setDirty] = useState(false);
  const [saved, setSaved] = useState(false);

  // Deploy stream state
  const [deployId, setDeployId] = useState<string | null>(null);
  const [logs, setLogs] = useState<string[]>([]);
  const [done, setDone] = useState(false);
  const [status, setStatus] = useState<string | null>(null);
  const logRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (data?.values && !dirty) setValues(data.values);
  }, [data]);

  const save = useMutation({
    mutationFn: (v: Record<string, unknown>) => api.post("/api/v1/config/easy", { values: v }),
    onSuccess: () => {
      setDirty(false);
      setSaved(true);
      setTimeout(() => setSaved(false), 2500);
      qc.invalidateQueries({ queryKey: ["config", "diff"] });
    },
  });

  const deploy = useMutation({
    mutationFn: (msg: string) =>
      api.post<DeployResponse>("/api/v1/helm/releases/ess/apply-config", { message: msg }),
    onSuccess: (res) => {
      setDeployId(res.upgrade_id);
      setLogs([]);
      setDone(false);
      setStatus(null);
    },
  });

  useUpgradeStream(deployId, {
    onLog: (line) => {
      setLogs((p) => [...p, line]);
      setTimeout(() => logRef.current?.scrollTo({ top: logRef.current.scrollHeight, behavior: "smooth" }), 30);
    },
    onDone: (s) => {
      setDone(true);
      setStatus(s);
      if (s === "success") qc.invalidateQueries({ queryKey: ["helm"] });
    },
  });

  function set(path: string, v: unknown) {
    setValues((prev) => ({ ...prev, [path]: v }));
    setDirty(true);
    setSaved(false);
  }

  async function saveAndDeploy() {
    await save.mutateAsync(values);
    deploy.mutate("config: Easy-Mode-Änderungen anwenden");
  }

  if (isLoading) {
    return <div className="text-sm text-gray-500 dark:text-gray-400">Lade…</div>;
  }

  const fields = data?.fields ?? [];
  const groups = [...new Set(fields.map((f) => f.group))];

  return (
    <div className="space-y-6 max-w-2xl">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <Link to="/config" className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300">
            <ArrowLeft className="w-5 h-5" />
          </Link>
          <div>
            <h1 className="text-2xl font-semibold text-gray-900 dark:text-gray-100">Easy Mode</h1>
            <p className="text-sm text-gray-500 dark:text-gray-400 mt-0.5">
              Häufige Einstellungen ohne YAML — schreibt in die Overlay-Slice <code className="bg-gray-100 dark:bg-gray-800 px-1 rounded text-xs">easy.yaml</code>
            </p>
          </div>
        </div>
        <Link
          to="/config/$slice"
          params={{ slice: "easy" }}
          className="flex items-center gap-1.5 px-3 py-1.5 text-xs text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-800 rounded-lg transition-colors"
        >
          <Code2 className="w-3.5 h-3.5" />
          YAML ansehen
        </Link>
      </div>

      {/* RTC info banner */}
      <div className="flex gap-3 bg-blue-50 dark:bg-blue-950/40 border border-blue-200 dark:border-blue-800 rounded-xl p-4 text-sm">
        <Zap className="w-4 h-4 text-blue-600 dark:text-blue-400 shrink-0 mt-0.5" />
        <p className="text-blue-800 dark:text-blue-300">
          Wenn du die WebRTC-Felder hier setzt und deployst, sind die manuellen SFU-Patches nach jedem
          Upgrade nicht mehr nötig — die Post-Upgrade-Hooks werden überflüssig.
        </p>
      </div>

      {/* Field groups */}
      {groups.map((group) => (
        <div key={group} className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-xl overflow-hidden">
          <div className="px-4 py-2.5 bg-gray-50 dark:bg-gray-900/50 border-b border-gray-200 dark:border-gray-700">
            <h2 className="text-sm font-medium text-gray-700 dark:text-gray-300">{group}</h2>
          </div>
          <div className="divide-y divide-gray-100 dark:divide-gray-700/50">
            {fields.filter((f) => f.group === group).map((f) => (
              <div key={f.path} className="flex items-start gap-4 px-4 py-3">
                <div className="flex-1 min-w-0">
                  <div className="text-sm font-medium text-gray-900 dark:text-gray-100">{f.label}</div>
                  <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">{f.help}</p>
                  <code className="text-[10px] text-gray-400 dark:text-gray-600 font-mono">{f.path}</code>
                </div>
                <div className="shrink-0 pt-0.5">
                  {f.type === "bool" && (
                    <button
                      onClick={() => set(f.path, !values[f.path])}
                      className={`relative w-10 h-6 rounded-full transition-colors ${
                        values[f.path] ? "bg-[#0DBD8B]" : "bg-gray-300 dark:bg-gray-600"
                      }`}
                      role="switch"
                      aria-checked={!!values[f.path]}
                    >
                      <span
                        className={`absolute top-0.5 left-0.5 w-5 h-5 bg-white rounded-full shadow transition-transform ${
                          values[f.path] ? "translate-x-4" : ""
                        }`}
                      />
                    </button>
                  )}
                  {f.type === "select" && (
                    <select
                      value={(values[f.path] as string) ?? ""}
                      onChange={(e) => set(f.path, e.target.value)}
                      className="px-2.5 py-1.5 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                    >
                      <option value="">— nicht gesetzt —</option>
                      {f.options?.map((o) => (
                        <option key={o} value={o}>{o}</option>
                      ))}
                    </select>
                  )}
                  {f.type === "string" && (
                    <input
                      type="text"
                      value={(values[f.path] as string) ?? ""}
                      onChange={(e) => set(f.path, e.target.value)}
                      className="px-2.5 py-1.5 w-48 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                    />
                  )}
                </div>
              </div>
            ))}
          </div>
        </div>
      ))}

      {/* Actions */}
      <div className="flex items-center gap-3">
        <button
          onClick={() => save.mutate(values)}
          disabled={!dirty || save.isPending}
          className="flex items-center gap-1.5 px-4 py-2 bg-gray-100 dark:bg-gray-800 hover:bg-gray-200 dark:hover:bg-gray-700 disabled:opacity-40 text-gray-700 dark:text-gray-300 text-sm font-medium rounded-lg transition-colors"
        >
          {save.isPending ? <Loader2 className="w-4 h-4 animate-spin" /> : <Save className="w-4 h-4" />}
          Speichern
        </button>
        <button
          onClick={saveAndDeploy}
          disabled={deploy.isPending || !!deployId}
          className="flex items-center gap-1.5 px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-sm font-medium rounded-lg transition-colors"
        >
          <Rocket className="w-4 h-4" />
          Speichern & Deployen
        </button>
        {saved && (
          <span className="flex items-center gap-1 text-xs text-green-600 dark:text-green-400">
            <CheckCircle2 className="w-3.5 h-3.5" /> Gespeichert
          </span>
        )}
        {(save.isError || deploy.isError) && (
          <span className="text-xs text-red-600 dark:text-red-400">
            {((save.error || deploy.error) as Error)?.message}
          </span>
        )}
      </div>

      {/* Deploy log */}
      {deployId && (
        <div className="space-y-3">
          <div className="flex items-center gap-3">
            <span className="text-sm font-medium text-gray-700 dark:text-gray-300">Deploy</span>
            {done && status === "success" && (
              <span className="flex items-center gap-1 text-xs text-green-600 dark:text-green-400 font-medium"><CheckCircle2 className="w-3.5 h-3.5" /> Erfolgreich</span>
            )}
            {done && status === "hooks-failed" && (
              <span className="flex items-center gap-1 text-xs text-yellow-600 dark:text-yellow-400 font-medium"><AlertTriangle className="w-3.5 h-3.5" /> Hooks fehlgeschlagen</span>
            )}
            {done && status === "failed" && (
              <span className="flex items-center gap-1 text-xs text-red-600 dark:text-red-400 font-medium"><XCircle className="w-3.5 h-3.5" /> Fehlgeschlagen</span>
            )}
          </div>
          <div ref={logRef} className="bg-gray-950 rounded-xl p-4 font-mono text-xs text-green-400 max-h-72 overflow-y-auto">
            {logs.map((line, i) => (
              <div key={i} className={`leading-relaxed ${line.startsWith("ERROR") ? "text-red-400" : line.startsWith("WARNING") ? "text-yellow-400" : ""}`}>{line}</div>
            ))}
            {!done && <div className="animate-pulse mt-1">▋</div>}
          </div>
        </div>
      )}
    </div>
  );
}
