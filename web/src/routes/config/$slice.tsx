import { createFileRoute, Link } from "@tanstack/react-router";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, useEffect, useRef, useCallback } from "react";
import Editor from "@monaco-editor/react";
import type { OnMount } from "@monaco-editor/react";
import * as monaco from "monaco-editor";
import * as jsYaml from "js-yaml";
import { api } from "@/lib/api";
import { useUpgradeStream } from "@/lib/ws";
import { ArrowLeft, Save, GitCommit, AlertCircle, CheckCircle2, Loader2, ChevronDown, ChevronRight, Rocket, AlertTriangle, XCircle, Zap, X } from "lucide-react";
import { useTheme } from "@/lib/theme";
import { Link as LinkTo } from "@tanstack/react-router";

export const Route = createFileRoute("/config/$slice")({
  component: SliceEditor,
});

interface Slice {
  name: string;
  file: string;
  description?: string;
  content: string;
}

interface ValidationResult {
  valid: boolean;
  errors: Array<{ field: string; message: string }> | null;
}

interface DeployResponse {
  upgrade_id: string;
  history_id: string;
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
  const [showDiff, setShowDiff] = useState(false);

  // Deploy state
  const [showDeploy, setShowDeploy] = useState(false);
  const [deployMsg, setDeployMsg] = useState("");
  const [deployId, setDeployId] = useState<string | null>(null);
  const [deployLogs, setDeployLogs] = useState<string[]>([]);
  const [deployDone, setDeployDone] = useState(false);
  const [deployStatus, setDeployStatus] = useState<string | null>(null);
  const [schemaErrors, setSchemaErrors] = useState<Array<{ field: string; message: string }>>([]);
  const [validating, setValidating] = useState(false);
  const logRef = useRef<HTMLDivElement>(null);

  // Refs so Monaco command closure always sees current values
  const saveRef = useRef<() => void>(() => {});
  const editorRef = useRef<Parameters<OnMount>[0] | null>(null);
  const monacoRef = useRef<typeof monaco | null>(null);

  const { data: slice, isLoading } = useQuery({
    queryKey: ["config", "slice", sliceName],
    queryFn: () => api.get<Slice>(`/api/v1/config/slices/${sliceName}`),
  });

  const { data: diffData } = useQuery({
    queryKey: ["config", "diff"],
    queryFn: () => api.get<{ diff: string }>("/api/v1/config/diff"),
    enabled: showCommit,
    refetchOnWindowFocus: false,
  });

  useEffect(() => {
    if (slice && !isDirty) {
      setContent(slice.content);
    }
  }, [slice]);

  useEffect(() => {
    const handler = (e: BeforeUnloadEvent) => {
      if (isDirty) {
        e.preventDefault();
        e.returnValue = "";
      }
    };
    window.addEventListener("beforeunload", handler);
    return () => window.removeEventListener("beforeunload", handler);
  }, [isDirty]);

  // YAML syntax validation via js-yaml → Monaco markers
  const validateYamlSyntax = useCallback((val: string) => {
    if (!editorRef.current || !monacoRef.current) return;
    const model = editorRef.current.getModel();
    if (!model) return;
    const markers: monaco.editor.IMarkerData[] = [];
    try {
      jsYaml.load(val);
    } catch (e: unknown) {
      if (e && typeof e === "object" && "mark" in e) {
        const mark = (e as { mark?: { line?: number; column?: number } }).mark;
        const msg = (e as { message?: string }).message ?? "YAML syntax error";
        const line = (mark?.line ?? 0) + 1;
        const col = (mark?.column ?? 0) + 1;
        markers.push({
          severity: monacoRef.current.MarkerSeverity.Error,
          message: msg,
          startLineNumber: line,
          endLineNumber: line,
          startColumn: col,
          endColumn: col + 1,
        });
      }
    }
    monacoRef.current.editor.setModelMarkers(model, "yaml-syntax", markers);
  }, []);

  const save = useMutation({
    mutationFn: (c: string) =>
      api.put(`/api/v1/config/slices/${sliceName}`, { content: c }),
    onSuccess: () => {
      setIsDirty(false);
      setSaved(true);
      setTimeout(() => setSaved(false), 2500);
      qc.invalidateQueries({ queryKey: ["config", "slices"] });
      qc.invalidateQueries({ queryKey: ["config", "diff"] });
    },
  });

  useEffect(() => {
    saveRef.current = () => {
      if (isDirty && !save.isPending) {
        save.mutate(content);
      }
    };
  }, [isDirty, content, save]);

  const commit = useMutation({
    mutationFn: (msg: string) =>
      api.post("/api/v1/config/apply", { message: msg }),
    onSuccess: () => {
      setShowCommit(false);
      setShowDiff(false);
      setCommitMsg("");
      qc.invalidateQueries({ queryKey: ["config", "history"] });
      qc.invalidateQueries({ queryKey: ["config", "diff"] });
    },
  });

  const deploy = useMutation({
    mutationFn: (msg: string) =>
      api.post<DeployResponse>("/api/v1/helm/releases/ess/apply-config", { message: msg }),
    onSuccess: (res) => {
      setDeployId(res.upgrade_id);
      setDeployLogs([]);
      setDeployDone(false);
      setDeployStatus(null);
    },
  });

  useUpgradeStream(deployId, {
    onLog: (line) => {
      setDeployLogs((prev) => [...prev, line]);
      setTimeout(() => {
        logRef.current?.scrollTo({ top: logRef.current.scrollHeight, behavior: "smooth" });
      }, 30);
    },
    onDone: (status) => {
      setDeployDone(true);
      setDeployStatus(status);
      if (status === "success") {
        qc.invalidateQueries({ queryKey: ["helm"] });
        qc.invalidateQueries({ queryKey: ["config", "history"] });
      }
    },
  });

  const handleEditorMount: OnMount = (editor, monacoInstance) => {
    editorRef.current = editor;
    monacoRef.current = monacoInstance as unknown as typeof monaco;
    editor.addCommand(monacoInstance.KeyMod.CtrlCmd | monacoInstance.KeyCode.KeyS, () => {
      saveRef.current();
    });
    // Initial syntax check
    if (content) validateYamlSyntax(content);
  };

  function handleEditorChange(val: string | undefined) {
    const v = val ?? "";
    setContent(v);
    setIsDirty(true);
    setSaved(false);
    validateYamlSyntax(v);
  }

  async function handleDeploy() {
    // Validate merged config against JSON Schema first (non-blocking — user can override)
    setValidating(true);
    setSchemaErrors([]);
    try {
      const result = await api.post<ValidationResult>("/api/v1/config/validate-merged", {});
      if (!result.valid && result.errors && result.errors.length > 0) {
        setSchemaErrors(result.errors);
        setValidating(false);
        return; // Show errors, user can override with "Trotzdem deployen"
      }
    } catch {
      // If validate-merged fails, proceed anyway
    }
    setValidating(false);
    deploy.mutate(deployMsg || `config: apply ${sliceName}.yaml`);
  }

  function resetDeploy() {
    setShowDeploy(false);
    setDeployId(null);
    setDeployLogs([]);
    setDeployDone(false);
    setDeployStatus(null);
    setDeployMsg("");
    setSchemaErrors([]);
    deploy.reset();
  }

  if (isLoading) {
    return <div className="text-sm text-gray-500 dark:text-gray-400">Lade...</div>;
  }

  const diffLines = diffData?.diff?.split("\n") ?? [];
  const hasDiff = diffLines.some((l) => l.startsWith("+") || l.startsWith("-"));

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
            title="Speichern (Ctrl+S)"
            className="flex items-center gap-1.5 px-3 py-1.5 bg-gray-100 dark:bg-gray-800 hover:bg-gray-200 dark:hover:bg-gray-700 disabled:opacity-40 text-gray-700 dark:text-gray-300 text-xs font-medium rounded-lg transition-colors"
          >
            {save.isPending ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Save className="w-3.5 h-3.5" />}
            Speichern
          </button>

          <button
            onClick={() => { setShowCommit(true); setShowDiff(false); setShowDeploy(false); }}
            disabled={isDirty}
            title={isDirty ? "Erst speichern, dann committen" : "Nur git commit, kein Deploy"}
            className="flex items-center gap-1.5 px-3 py-1.5 bg-gray-100 dark:bg-gray-800 hover:bg-gray-200 dark:hover:bg-gray-700 disabled:opacity-40 text-gray-700 dark:text-gray-300 text-xs font-medium rounded-lg transition-colors"
          >
            <GitCommit className="w-3.5 h-3.5" />
            Committen
          </button>

          <button
            onClick={() => { setShowDeploy(true); setShowCommit(false); setSchemaErrors([]); deploy.reset(); setDeployId(null); }}
            disabled={isDirty}
            title={isDirty ? "Erst speichern, dann deployen" : "Config committen und auf Cluster anwenden"}
            className="flex items-center gap-1.5 px-3 py-1.5 bg-blue-600 hover:bg-blue-700 disabled:opacity-40 text-white text-xs font-medium rounded-lg transition-colors"
          >
            <Rocket className="w-3.5 h-3.5" />
            Deployen
          </button>
        </div>
      </div>

      {/* Commit panel */}
      {showCommit && !showDeploy && (
        <div className="border-b border-blue-200 dark:border-blue-800 bg-blue-50 dark:bg-blue-950/40 shrink-0">
          <div className="px-6 py-3 flex items-center gap-3">
            <input
              autoFocus
              value={commitMsg}
              onChange={(e) => setCommitMsg(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && commit.mutate(commitMsg || `config: update ${sliceName}.yaml`)}
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
            <button onClick={() => setShowCommit(false)} className="px-3 py-1.5 text-xs text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200">
              Abbrechen
            </button>
            {commit.isError && <span className="text-xs text-red-600 dark:text-red-400">{(commit.error as Error).message}</span>}
            {commit.isSuccess && <span className="text-xs text-green-600 dark:text-green-400 flex items-center gap-1"><CheckCircle2 className="w-3.5 h-3.5" /> Committed!</span>}
          </div>

          {diffData !== undefined && (
            <div className="px-6 pb-2">
              <button onClick={() => setShowDiff((v) => !v)} className="flex items-center gap-1 text-xs text-blue-600 dark:text-blue-400 hover:underline">
                {showDiff ? <ChevronDown className="w-3.5 h-3.5" /> : <ChevronRight className="w-3.5 h-3.5" />}
                {hasDiff ? "Diff anzeigen" : "Keine ungespeicherten Änderungen"}
              </button>
              {showDiff && hasDiff && (
                <pre className="mt-2 text-xs font-mono bg-gray-900 dark:bg-black rounded-lg px-4 py-3 overflow-x-auto max-h-64 overflow-y-auto leading-relaxed">
                  {diffLines.map((line, i) => (
                    <span key={i} className={
                      line.startsWith("+") && !line.startsWith("+++") ? "text-green-400 block" :
                      line.startsWith("-") && !line.startsWith("---") ? "text-red-400 block" :
                      line.startsWith("@@") ? "text-blue-400 block" :
                      "text-gray-400 block"
                    }>{line || " "}</span>
                  ))}
                </pre>
              )}
            </div>
          )}
        </div>
      )}

      {/* Deploy panel */}
      {showDeploy && (
        <div className="border-b border-blue-200 dark:border-blue-800 bg-blue-50 dark:bg-blue-950/40 shrink-0">
          {!deployId ? (
            <div className="px-6 py-3 space-y-3">
              <div className="flex items-center gap-3">
                <Rocket className="w-4 h-4 text-blue-600 dark:text-blue-400 shrink-0" />
                <span className="text-sm font-medium text-blue-900 dark:text-blue-100">Config committen und auf Cluster anwenden</span>
                <button onClick={resetDeploy} className="ml-auto text-gray-400 hover:text-gray-600 dark:hover:text-gray-200">
                  <X className="w-4 h-4" />
                </button>
              </div>

              <div className="flex items-center gap-3">
                <input
                  autoFocus
                  value={deployMsg}
                  onChange={(e) => setDeployMsg(e.target.value)}
                  placeholder={`config: apply ${sliceName}.yaml`}
                  className="flex-1 px-3 py-1.5 bg-white dark:bg-gray-900 border border-blue-300 dark:border-blue-700 text-sm text-gray-900 dark:text-gray-100 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500"
                />
                <button
                  onClick={handleDeploy}
                  disabled={deploy.isPending || validating}
                  className="flex items-center gap-1.5 px-3 py-1.5 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-xs font-medium rounded-lg"
                >
                  {validating ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Rocket className="w-3.5 h-3.5" />}
                  {validating ? "Validiere..." : "Jetzt deployen"}
                </button>
              </div>

              {/* Schema validation errors */}
              {schemaErrors.length > 0 && (
                <div className="bg-red-50 dark:bg-red-950/40 border border-red-200 dark:border-red-800 rounded-lg px-3 py-2 space-y-1">
                  <div className="flex items-center gap-1.5 text-xs font-medium text-red-700 dark:text-red-400">
                    <AlertCircle className="w-3.5 h-3.5" />
                    Schema-Fehler ({schemaErrors.length}) — Deploy trotzdem möglich:
                  </div>
                  {schemaErrors.slice(0, 5).map((e, i) => (
                    <div key={i} className="text-xs text-red-600 dark:text-red-400 font-mono">
                      <span className="text-red-400 dark:text-red-500">{e.field}: </span>{e.message}
                    </div>
                  ))}
                  {schemaErrors.length > 5 && (
                    <div className="text-xs text-red-500 dark:text-red-400">+{schemaErrors.length - 5} weitere Fehler</div>
                  )}
                  <button
                    onClick={() => { setSchemaErrors([]); deploy.mutate(deployMsg || `config: apply ${sliceName}.yaml`); }}
                    className="mt-1 text-xs text-red-600 dark:text-red-400 underline hover:no-underline"
                  >
                    Trotzdem deployen
                  </button>
                </div>
              )}

              {deploy.isError && (
                <div className="flex items-center gap-1.5 text-xs text-red-600 dark:text-red-400">
                  <AlertCircle className="w-3.5 h-3.5" />
                  {(deploy.error as Error).message}
                </div>
              )}
            </div>
          ) : (
            <div className="px-6 py-3 space-y-3">
              <div className="flex items-center gap-3">
                <span className="text-sm font-medium text-blue-900 dark:text-blue-100">Deploy läuft...</span>
                {deployDone && deployStatus === "success" && (
                  <span className="flex items-center gap-1 text-xs text-green-600 dark:text-green-400 font-medium">
                    <CheckCircle2 className="w-3.5 h-3.5" /> Erfolgreich
                  </span>
                )}
                {deployDone && deployStatus === "hooks-failed" && (
                  <span className="flex items-center gap-1 text-xs text-yellow-600 dark:text-yellow-400 font-medium">
                    <AlertTriangle className="w-3.5 h-3.5" /> Hooks fehlgeschlagen
                  </span>
                )}
                {deployDone && deployStatus === "failed" && (
                  <span className="flex items-center gap-1 text-xs text-red-600 dark:text-red-400 font-medium">
                    <XCircle className="w-3.5 h-3.5" /> Fehlgeschlagen
                  </span>
                )}
                {deployDone && (
                  <button onClick={resetDeploy} className="ml-auto text-xs text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200">
                    Schließen
                  </button>
                )}
              </div>

              {/* Log terminal */}
              <div
                ref={logRef}
                className="bg-gray-950 rounded-lg p-3 font-mono text-xs text-green-400 max-h-48 overflow-y-auto"
              >
                {deployLogs.map((line, i) => (
                  <div key={i} className={`leading-relaxed ${line.startsWith("ERROR") ? "text-red-400" : line.startsWith("WARNING") ? "text-yellow-400" : ""}`}>
                    {line}
                  </div>
                ))}
                {!deployDone && <div className="animate-pulse mt-1">▋</div>}
              </div>

              {deployDone && deployStatus === "hooks-failed" && (
                <div className="flex items-center gap-2 text-xs">
                  <AlertTriangle className="w-3.5 h-3.5 text-yellow-500 shrink-0" />
                  <span className="text-yellow-700 dark:text-yellow-400">
                    Helm-Apply erfolgreich, aber Post-Upgrade-Hooks fehlgeschlagen (SFU-Patches).
                  </span>
                  <LinkTo to="/hooks" className="flex items-center gap-1 text-yellow-600 dark:text-yellow-400 underline">
                    <Zap className="w-3 h-3" /> Hooks
                  </LinkTo>
                </div>
              )}
            </div>
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
          onMount={handleEditorMount}
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
          <span className="text-yellow-500 dark:text-yellow-400">● Ungespeicherte Änderungen — Ctrl+S zum Speichern</span>
        )}
        {!isDirty && !saved && (
          <span className="text-green-500">✓ Synchron mit Disk</span>
        )}
      </div>
    </div>
  );
}
