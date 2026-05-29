import { createFileRoute, Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { api } from "@/lib/api";
import { FileText, ChevronRight, GitBranch, Clock, History, Copy, Check, ChevronDown, Loader2, Wand2 } from "lucide-react";
import { DiffView } from "@/components/config/DiffView";

export const Route = createFileRoute("/config/")({
  component: ConfigPage,
});

interface SliceMeta {
  name: string;
  file: string;
  description?: string;
  lines: number;
}

interface CommitInfo {
  sha: string;
  message: string;
  author: string;
  time: string;
}

function InlineDiff({ sha }: { sha: string }) {
  const { data, isLoading, error } = useQuery({
    queryKey: ["config", "history", sha, "diff"],
    queryFn: () => api.get<{ diff: string }>(`/api/v1/config/history/${sha}/diff`),
    staleTime: Infinity,
  });

  if (isLoading) return (
    <div className="flex items-center gap-2 px-3 py-2 text-xs text-gray-400 dark:text-gray-500">
      <Loader2 className="w-3.5 h-3.5 animate-spin" /> Lade Diff...
    </div>
  );
  if (error) return <p className="px-3 py-2 text-xs text-red-500">{(error as Error).message}</p>;

  return (
    <div className="border border-gray-200 dark:border-gray-700 rounded-lg overflow-hidden mt-1 ml-5">
      <DiffView raw={data?.diff ?? ""} maxHeight="max-h-72" />
    </div>
  );
}

function CopyableSha({ sha }: { sha: string }) {
  const [copied, setCopied] = useState(false);
  function copy(e: React.MouseEvent) {
    e.stopPropagation();
    navigator.clipboard.writeText(sha).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    });
  }
  return (
    <button onClick={copy} className="inline-flex items-center gap-1 font-mono text-blue-600 dark:text-blue-400 hover:underline group" title="SHA kopieren">
      {sha}
      {copied ? <Check className="w-3 h-3 text-green-500" /> : <Copy className="w-3 h-3 opacity-0 group-hover:opacity-60 transition-opacity" />}
    </button>
  );
}

function ConfigPage() {
  const [expandedSha, setExpandedSha] = useState<string | null>(null);

  const { data: slices } = useQuery({
    queryKey: ["config", "slices"],
    queryFn: () => api.get<SliceMeta[]>("/api/v1/config/slices"),
  });
  const { data: history } = useQuery({
    queryKey: ["config", "history"],
    queryFn: () => api.get<CommitInfo[]>("/api/v1/config/history"),
  });

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900 dark:text-gray-100">Config Slices</h1>
          <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
            YAML-Dateien die bei Helm-Upgrades als{" "}
            <code className="bg-gray-100 dark:bg-gray-800 px-1 rounded text-xs">--values</code> übergeben werden
          </p>
        </div>
      </div>

      {/* Easy Mode entry */}
      <Link
        to="/config/easy"
        className="flex items-center gap-4 bg-gradient-to-r from-blue-50 to-[#0DBD8B]/10 dark:from-blue-950/40 dark:to-[#0DBD8B]/10 border border-blue-200 dark:border-blue-800 rounded-xl px-4 py-3.5 hover:border-blue-300 dark:hover:border-blue-700 transition-colors group"
      >
        <div className="w-9 h-9 rounded-lg bg-gradient-to-br from-blue-600 to-[#0DBD8B] flex items-center justify-center shrink-0">
          <Wand2 className="w-4.5 h-4.5 text-white" />
        </div>
        <div className="flex-1 min-w-0">
          <div className="text-sm font-semibold text-gray-900 dark:text-gray-100">Easy Mode</div>
          <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
            WebRTC & häufige Einstellungen per Toggle — ohne YAML
          </p>
        </div>
        <ChevronRight className="w-4 h-4 text-gray-300 dark:text-gray-600 group-hover:text-blue-500 shrink-0" />
      </Link>

      <div className="space-y-2">
        {slices?.filter((s) => s.name !== "easy").map((s) => (
          <Link
            key={s.name}
            to="/config/$slice"
            params={{ slice: s.name }}
            className="flex items-center gap-4 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-xl px-4 py-3 hover:border-blue-300 dark:hover:border-blue-700 transition-colors group"
          >
            <FileText className="w-4 h-4 text-gray-400 dark:text-gray-500 shrink-0" />
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-2">
                <span className="font-mono text-sm font-medium text-gray-900 dark:text-gray-100">{s.file}</span>
                <span className="text-xs text-gray-400 dark:text-gray-500 bg-gray-100 dark:bg-gray-700 px-1.5 py-0.5 rounded">{s.name}</span>
              </div>
              {s.description && (
                <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5 truncate">{s.description}</p>
              )}
            </div>
            <span className="text-xs text-gray-400 dark:text-gray-500 tabular-nums shrink-0">{s.lines} Zeilen</span>
            <ChevronRight className="w-4 h-4 text-gray-300 dark:text-gray-600 group-hover:text-gray-500 dark:group-hover:text-gray-400 shrink-0" />
          </Link>
        ))}
      </div>

      {history && history.length > 0 && (
        <div>
          <div className="flex items-center justify-between mb-2">
            <div className="flex items-center gap-2">
              <GitBranch className="w-4 h-4 text-gray-400 dark:text-gray-500" />
              <h2 className="text-sm font-medium text-gray-700 dark:text-gray-300">Letzte Änderungen</h2>
            </div>
            <Link to="/config/history" className="flex items-center gap-1 text-xs text-blue-600 dark:text-blue-400 hover:underline">
              <History className="w-3.5 h-3.5" />
              Vollständige History
            </Link>
          </div>
          <div className="space-y-0.5">
            {history.slice(0, 5).map((c) => {
              const isExpanded = expandedSha === c.sha;
              return (
                <div key={c.sha}>
                  <div
                    className="flex items-center gap-3 text-sm py-1.5 px-2 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-800/50 cursor-pointer group"
                    onClick={() => setExpandedSha(isExpanded ? null : c.sha)}
                  >
                    {isExpanded
                      ? <ChevronDown className="w-3.5 h-3.5 text-gray-400 shrink-0" />
                      : <ChevronRight className="w-3.5 h-3.5 text-gray-300 dark:text-gray-600 group-hover:text-gray-400 shrink-0" />
                    }
                    <span onClick={(e) => e.stopPropagation()}>
                      <CopyableSha sha={c.sha} />
                    </span>
                    <span className="flex-1 text-gray-700 dark:text-gray-300 truncate">{c.message.split("\n")[0]}</span>
                    <span className="flex items-center gap-1 text-xs text-gray-400 dark:text-gray-500 shrink-0">
                      <Clock className="w-3 h-3" />
                      {new Date(c.time).toLocaleString("de-DE", { dateStyle: "short", timeStyle: "short" })}
                    </span>
                  </div>
                  {isExpanded && <InlineDiff sha={c.sha} />}
                </div>
              );
            })}
          </div>
        </div>
      )}
    </div>
  );
}
