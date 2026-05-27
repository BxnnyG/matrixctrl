import { createFileRoute, Link } from "@tanstack/react-router";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, useEffect } from "react";
import Editor from "@monaco-editor/react";
import { api } from "@/lib/api";
import { ArrowLeft, Save, GitCommit, AlertCircle, CheckCircle2, Loader2 } from "lucide-react";
import { useTheme } from "@/lib/theme";

export const Route = createFileRoute("/config/$slice")({
  component: SliceEditor,
});

interface Slice {
  name: string;
  file: string;
  description?: string;
  content: string;
}

function SliceEditor() {
  const { slice: sliceName } = Route.useParams();
  const qc = useQueryClient();
  const { theme } = useTheme();

  const [content, setContent] = useState<string>("");
  const [isDirty, setIsDirty] = useState(false);
  const [saved, setSaved] = useState(false);
  const [commitMsg, setCommitMsg] = useState("");
  const [showCommit, setShowCommit] = useState(false);

  const { data: slice, isLoading } = useQuery({
    queryKey: ["config", "slice", sliceName],
    queryFn: () => api.get<Slice>(`/api/v1/config/slices/${sliceName}`),
  });

  useEffect(() => {
    if (slice && !isDirty) {
      setContent(slice.content);
    }
  }, [slice]);

  const save = useMutation({
    mutationFn: (c: string) =>
      api.put(`/api/v1/config/slices/${sliceName}`, { content: c }),
    onSuccess: () => {
      setIsDirty(false);
      setSaved(true);
      setTimeout(() => setSaved(false), 2500);
      qc.invalidateQueries({ queryKey: ["config", "slices"] });
    },
  });

  const commit = useMutation({
    mutationFn: (msg: string) =>
      api.post("/api/v1/config/apply", { message: msg }),
    onSuccess: () => {
      setShowCommit(false);
      setCommitMsg("");
      qc.invalidateQueries({ queryKey: ["config", "history"] });
    },
  });

  function handleEditorChange(val: string | undefined) {
    setContent(val ?? "");
    setIsDirty(true);
    setSaved(false);
  }

  if (isLoading) {
    return <div className="text-sm text-gray-500 dark:text-gray-400">Lade...</div>;
  }

  return (
    <div className="flex flex-col h-[calc(100vh-4rem)] -mt-8 -mx-6 -mb-8">
      {/* Header */}
      <div className="flex items-center gap-3 px-6 py-3 bg-white dark:bg-gray-900 border-b border-gray-200 dark:border-gray-800 shrink-0">
        <Link to="/config" className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300">
          <ArrowLeft className="w-5 h-5" />
        </Link>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <span className="font-mono text-sm font-medium text-gray-900 dark:text-gray-100">{slice?.file}</span>
            <span className="text-xs text-gray-400 dark:text-gray-500 bg-gray-100 dark:bg-gray-700 px-1.5 py-0.5 rounded">{sliceName}</span>
            {isDirty && (
              <span className="text-xs text-yellow-600 dark:text-yellow-400 bg-yellow-50 dark:bg-yellow-950 px-1.5 py-0.5 rounded">
                ungespeichert
              </span>
            )}
          </div>
          {slice?.description && (
            <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">{slice.description}</p>
          )}
        </div>

        <div className="flex items-center gap-2 shrink-0">
          {saved && (
            <span className="flex items-center gap-1 text-xs text-green-600 dark:text-green-400">
              <CheckCircle2 className="w-3.5 h-3.5" /> Gespeichert
            </span>
          )}
          {save.isError && (
            <span className="flex items-center gap-1 text-xs text-red-600 dark:text-red-400">
              <AlertCircle className="w-3.5 h-3.5" /> {(save.error as Error).message}
            </span>
          )}

          <button
            onClick={() => save.mutate(content)}
            disabled={!isDirty || save.isPending}
            className="flex items-center gap-1.5 px-3 py-1.5 bg-gray-100 dark:bg-gray-800 hover:bg-gray-200 dark:hover:bg-gray-700 disabled:opacity-40 text-gray-700 dark:text-gray-300 text-xs font-medium rounded-lg transition-colors"
          >
            {save.isPending ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Save className="w-3.5 h-3.5" />}
            Speichern
          </button>

          <button
            onClick={() => setShowCommit(true)}
            disabled={isDirty}
            className="flex items-center gap-1.5 px-3 py-1.5 bg-blue-600 hover:bg-blue-700 disabled:opacity-40 text-white text-xs font-medium rounded-lg transition-colors"
          >
            <GitCommit className="w-3.5 h-3.5" />
            Committen
          </button>
        </div>
      </div>

      {/* Commit dialog */}
      {showCommit && (
        <div className="px-6 py-3 bg-blue-50 dark:bg-blue-950/40 border-b border-blue-200 dark:border-blue-800 flex items-center gap-3 shrink-0">
          <input
            autoFocus
            value={commitMsg}
            onChange={(e) => setCommitMsg(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && commitMsg && commit.mutate(commitMsg)}
            placeholder={`config: update ${sliceName}.yaml`}
            className="flex-1 px-3 py-1.5 bg-white dark:bg-gray-900 border border-blue-300 dark:border-blue-700 text-sm text-gray-900 dark:text-gray-100 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500"
          />
          <button
            onClick={() => commit.mutate(commitMsg || `config: update ${sliceName}.yaml`)}
            disabled={commit.isPending}
            className="px-3 py-1.5 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-xs font-medium rounded-lg"
          >
            {commit.isPending ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : "Commit"}
          </button>
          <button
            onClick={() => setShowCommit(false)}
            className="px-3 py-1.5 text-xs text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200"
          >
            Abbrechen
          </button>
          {commit.isError && (
            <span className="text-xs text-red-600 dark:text-red-400">{(commit.error as Error).message}</span>
          )}
          {commit.isSuccess && (
            <span className="text-xs text-green-600 dark:text-green-400 flex items-center gap-1">
              <CheckCircle2 className="w-3.5 h-3.5" /> Committed!
            </span>
          )}
        </div>
      )}

      {/* Editor */}
      <div className="flex-1 overflow-hidden">
        <Editor
          height="100%"
          defaultLanguage="yaml"
          value={content}
          onChange={handleEditorChange}
          theme={theme === "dark" ? "vs-dark" : "vs"}
          options={{
            fontSize: 13,
            minimap: { enabled: false },
            scrollBeyondLastLine: false,
            wordWrap: "on",
            lineNumbers: "on",
            renderLineHighlight: "line",
            tabSize: 2,
            insertSpaces: true,
            automaticLayout: true,
          }}
        />
      </div>

      {/* Status bar */}
      <div className="px-6 py-1.5 bg-gray-100 dark:bg-gray-900 border-t border-gray-200 dark:border-gray-800 flex items-center gap-4 text-xs text-gray-400 dark:text-gray-500 shrink-0">
        <span>YAML</span>
        <span>{content.split("\n").length} Zeilen</span>
        <span>{new Blob([content]).size} Bytes</span>
        {isDirty && (
          <span className="text-yellow-500 dark:text-yellow-400">● Ungespeicherte Änderungen — Speichern vor Commit</span>
        )}
        {!isDirty && !saved && (
          <span className="text-green-500">✓ Synchron mit Disk</span>
        )}
      </div>
    </div>
  );
}
