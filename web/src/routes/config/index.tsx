import { createFileRoute, Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { FileText, ChevronRight, GitBranch, Clock } from "lucide-react";

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

function ConfigPage() {
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
            YAML-Dateien die bei Helm-Upgrades als <code className="bg-gray-100 dark:bg-gray-800 px-1 rounded text-xs">--values</code> übergeben werden
          </p>
        </div>
      </div>

      <div className="space-y-2">
        {slices?.map((s) => (
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
          <div className="flex items-center gap-2 mb-3">
            <GitBranch className="w-4 h-4 text-gray-400 dark:text-gray-500" />
            <h2 className="text-sm font-medium text-gray-700 dark:text-gray-300">Letzte Änderungen</h2>
          </div>
          <div className="space-y-1">
            {history.slice(0, 5).map((c) => (
              <div key={c.sha} className="flex items-center gap-3 text-sm py-1.5 px-2 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-800/50">
                <code className="text-xs font-mono text-blue-600 dark:text-blue-400 w-16 shrink-0">{c.sha}</code>
                <span className="flex-1 text-gray-700 dark:text-gray-300 truncate">{c.message.split("\n")[0]}</span>
                <span className="flex items-center gap-1 text-xs text-gray-400 dark:text-gray-500 shrink-0">
                  <Clock className="w-3 h-3" />
                  {new Date(c.time).toLocaleString("de-DE", { dateStyle: "short", timeStyle: "short" })}
                </span>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
