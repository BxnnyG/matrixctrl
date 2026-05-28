import { createFileRoute, Link } from "@tanstack/react-router";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api } from "@/lib/api";
import { ArrowLeft, GitCommit, ChevronDown, ChevronRight, RotateCcw, Loader2, AlertTriangle, Copy, Check } from "lucide-react";
import { DiffView } from "@/components/config/DiffView";

export const Route = createFileRoute("/config/history")({
  component: ConfigHistory,
});

interface CommitInfo {
  sha: string;
  message: string;
  author: string;
  time: string;
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

function CommitDiffPanel({ sha }: { sha: string }) {
  const { data, isLoading, error } = useQuery({
    queryKey: ["config", "history", sha, "diff"],
    queryFn: () => api.get<{ diff: string }>(`/api/v1/config/history/${sha}/diff`),
    staleTime: Infinity,
  });

  if (isLoading) return (
    <div className="flex items-center gap-2 px-4 py-3 text-xs text-gray-400 dark:text-gray-500">
      <Loader2 className="w-3.5 h-3.5 animate-spin" /> Lade Diff...
    </div>
  );
  if (error) return <p className="px-4 py-3 text-xs text-red-500">{(error as Error).message}</p>;

  return <DiffView raw={data?.diff ?? ""} />;
}

function ConfigHistory() {
  const qc = useQueryClient();
  const [expandedSha, setExpandedSha] = useState<string | null>(null);
  const [rollingBack, setRollingBack] = useState<string | null>(null);
  const [confirmSha, setConfirmSha] = useState<string | null>(null);

  const { data: commits, isLoading } = useQuery({
    queryKey: ["config", "history"],
    queryFn: () => api.get<CommitInfo[]>("/api/v1/config/history"),
  });

  const rollback = useMutation({
    mutationFn: (sha: string) => {
      setRollingBack(sha);
      return api.post(`/api/v1/config/history/${sha}/rollback`, {});
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["config"] });
      setConfirmSha(null);
      setRollingBack(null);
    },
    onError: () => setRollingBack(null),
  });

  return (
    <div className="space-y-6 max-w-4xl">
      <div className="flex items-center gap-3">
        <Link to="/config" className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300">
          <ArrowLeft className="w-5 h-5" />
        </Link>
        <h1 className="text-2xl font-semibold text-gray-900 dark:text-gray-100">Config-History</h1>
      </div>

      {isLoading && <p className="text-sm text-gray-500 dark:text-gray-400">Lade...</p>}
      {commits?.length === 0 && <p className="text-sm text-gray-500 dark:text-gray-400">Noch keine Commits.</p>}

      <div className="space-y-2">
        {commits?.map((c, idx) => {
          const expanded = expandedSha === c.sha;
          const isFirst = idx === 0;
          return (
            <div key={c.sha} className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-xl overflow-hidden">
              {/* Clickable header row */}
              <div
                className="flex items-center gap-3 px-4 py-3 cursor-pointer hover:bg-gray-50 dark:hover:bg-gray-700/40 select-none"
                onClick={() => setExpandedSha(expanded ? null : c.sha)}
              >
                <GitCommit className="w-4 h-4 text-gray-400 dark:text-gray-500 shrink-0" />
                <div className="flex-1 min-w-0">
                  <p className="text-sm text-gray-900 dark:text-gray-100 truncate">{c.message.split("\n")[0]}</p>
                  <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5 flex items-center gap-1.5 flex-wrap">
                    <span onClick={(e) => e.stopPropagation()}><CopyableSha sha={c.sha} /></span>
                    <span>·</span>
                    <span>{c.author}</span>
                    <span>·</span>
                    <span>{new Date(c.time).toLocaleString("de-DE", { dateStyle: "short", timeStyle: "short" })}</span>
                  </p>
                </div>
                <div className="flex items-center gap-1 shrink-0" onClick={(e) => e.stopPropagation()}>
                  {isFirst && (
                    <span className="text-xs text-green-600 dark:text-green-400 bg-green-50 dark:bg-green-950 px-2 py-1 rounded-lg">aktuell</span>
                  )}
                  {!isFirst && (
                    <button
                      onClick={() => setConfirmSha(c.sha)}
                      className="flex items-center gap-1 px-2 py-1 text-xs text-gray-500 dark:text-gray-400 hover:text-red-600 dark:hover:text-red-400 hover:bg-red-50 dark:hover:bg-red-950 rounded-lg transition-colors"
                      title="Auf diesen Stand zurücksetzen"
                    >
                      <RotateCcw className="w-3.5 h-3.5" />
                      Rollback
                    </button>
                  )}
                </div>
                <div className="shrink-0 text-gray-300 dark:text-gray-600">
                  {expanded ? <ChevronDown className="w-4 h-4" /> : <ChevronRight className="w-4 h-4" />}
                </div>
              </div>

              {expanded && (
                <div className="border-t border-gray-100 dark:border-gray-700">
                  <CommitDiffPanel sha={c.sha} />
                </div>
              )}
            </div>
          );
        })}
      </div>

      {/* Rollback confirmation modal */}
      {confirmSha && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
          <div className="bg-white dark:bg-gray-800 rounded-xl p-6 max-w-md w-full mx-4 shadow-xl">
            <div className="flex items-start gap-3 mb-4">
              <AlertTriangle className="w-5 h-5 text-yellow-500 shrink-0 mt-0.5" />
              <div>
                <h2 className="font-semibold text-gray-900 dark:text-gray-100">Rollback bestätigen</h2>
                <p className="text-sm text-gray-600 dark:text-gray-400 mt-1">
                  Der Working-Tree wird auf Commit{" "}
                  <code className="font-mono text-blue-600 dark:text-blue-400">{confirmSha}</code>{" "}
                  zurückgesetzt. Alle ungespeicherten Änderungen gehen verloren.
                </p>
              </div>
            </div>
            <div className="flex gap-2 justify-end">
              <button onClick={() => setConfirmSha(null)} className="px-3 py-1.5 text-sm text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-lg">
                Abbrechen
              </button>
              <button
                onClick={() => rollback.mutate(confirmSha)}
                disabled={rollingBack === confirmSha}
                className="flex items-center gap-1.5 px-3 py-1.5 bg-red-600 hover:bg-red-700 disabled:opacity-50 text-white text-sm font-medium rounded-lg"
              >
                {rollingBack === confirmSha ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <RotateCcw className="w-3.5 h-3.5" />}
                Jetzt zurücksetzen
              </button>
            </div>
            {rollback.isError && (
              <p className="text-xs text-red-600 dark:text-red-400 mt-2">{(rollback.error as Error).message}</p>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
