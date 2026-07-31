import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, useEffect, useRef, useCallback } from "react";
import { YamlEditor } from "@/components/config/YamlEditor";
import type { OnMount } from "@monaco-editor/react";
import type * as monaco from "monaco-editor";
import * as jsYaml from "js-yaml";
import { api } from "@/lib/api";
import { useUpgradeStream } from "@/lib/ws";
import { Icon, Badge, Button, Spinner } from "@/components/mc";

export const Route = createFileRoute("/config/$slice")({
  component: SliceEditor,
});

interface Slice { name: string; file: string; description?: string; content: string }
interface ValidationResult { valid: boolean; errors: Array<{ field: string; message: string }> | null }
interface DeployResponse { upgrade_id: string; history_id: string }

function SliceEditor() {
  const { slice: sliceName } = Route.useParams();
  const qc = useQueryClient();
  const navigate = useNavigate();

  const [content, setContent] = useState<string>("");
  const [isDirty, setIsDirty] = useState(false);
  const [saved, setSaved] = useState(false);
  const [commitMsg, setCommitMsg] = useState("");
  const [showCommit, setShowCommit] = useState(false);
  const [showDiff, setShowDiff] = useState(false);

  const [showDeploy, setShowDeploy] = useState(false);
  const [deployMsg, setDeployMsg] = useState("");
  const [deployId, setDeployId] = useState<string | null>(null);
  const [deployLogs, setDeployLogs] = useState<string[]>([]);
  const [deployDone, setDeployDone] = useState(false);
  const [deployStatus, setDeployStatus] = useState<string | null>(null);
  const [schemaErrors, setSchemaErrors] = useState<Array<{ field: string; message: string }>>([]);
  const [validating, setValidating] = useState(false);
  const logRef = useRef<HTMLDivElement>(null);

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

  useEffect(() => { if (slice && !isDirty) setContent(slice.content); }, [slice]);
  useEffect(() => {
    const handler = (e: BeforeUnloadEvent) => { if (isDirty) { e.preventDefault(); e.returnValue = ""; } };
    window.addEventListener("beforeunload", handler);
    return () => window.removeEventListener("beforeunload", handler);
  }, [isDirty]);

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
        markers.push({ severity: monacoRef.current.MarkerSeverity.Error, message: msg, startLineNumber: line, endLineNumber: line, startColumn: col, endColumn: col + 1 });
      }
    }
    monacoRef.current.editor.setModelMarkers(model, "yaml-syntax", markers);
  }, []);

  const save = useMutation({
    mutationFn: (c: string) => api.put(`/api/v1/config/slices/${sliceName}`, { content: c }),
    onSuccess: () => {
      setIsDirty(false); setSaved(true); setTimeout(() => setSaved(false), 2500);
      qc.invalidateQueries({ queryKey: ["config", "slices"] });
      qc.invalidateQueries({ queryKey: ["config", "diff"] });
    },
  });
  useEffect(() => { saveRef.current = () => { if (isDirty && !save.isPending) save.mutate(content); }; }, [isDirty, content, save]);

  const commit = useMutation({
    mutationFn: (msg: string) => api.post("/api/v1/config/apply", { message: msg }),
    onSuccess: () => {
      setShowCommit(false); setShowDiff(false); setCommitMsg("");
      qc.invalidateQueries({ queryKey: ["config", "history"] });
      qc.invalidateQueries({ queryKey: ["config", "diff"] });
    },
  });

  const deploy = useMutation({
    mutationFn: (msg: string) => api.post<DeployResponse>("/api/v1/helm/releases/ess/apply-config", { message: msg }),
    onSuccess: (res) => { setDeployId(res.upgrade_id); setDeployLogs([]); setDeployDone(false); setDeployStatus(null); },
  });

  useUpgradeStream(deployId, {
    onLog: (line) => { setDeployLogs((prev) => [...prev, line]); setTimeout(() => logRef.current?.scrollTo({ top: logRef.current.scrollHeight, behavior: "smooth" }), 30); },
    onDone: (status) => {
      setDeployDone(true); setDeployStatus(status);
      if (status === "success") { qc.invalidateQueries({ queryKey: ["helm"] }); qc.invalidateQueries({ queryKey: ["config", "history"] }); }
    },
  });

  const handleEditorMount: OnMount = (editor, monacoInstance) => {
    editorRef.current = editor;
    monacoRef.current = monacoInstance as unknown as typeof monaco;
    editor.addCommand(monacoInstance.KeyMod.CtrlCmd | monacoInstance.KeyCode.KeyS, () => saveRef.current());
    if (content) validateYamlSyntax(content);
  };
  function handleEditorChange(val: string | undefined) {
    const v = val ?? "";
    setContent(v); setIsDirty(true); setSaved(false); validateYamlSyntax(v);
  }

  async function handleDeploy() {
    setValidating(true); setSchemaErrors([]);
    try {
      const result = await api.post<ValidationResult>("/api/v1/config/validate-merged", {});
      if (!result.valid && result.errors && result.errors.length > 0) { setSchemaErrors(result.errors); setValidating(false); return; }
    } catch { /* proceed */ }
    setValidating(false);
    deploy.mutate(deployMsg || `config: apply ${sliceName}.yaml`);
  }
  function resetDeploy() {
    setShowDeploy(false); setDeployId(null); setDeployLogs([]); setDeployDone(false); setDeployStatus(null); setDeployMsg(""); setSchemaErrors([]); deploy.reset();
  }

  if (isLoading) return <div style={{ display: "flex", alignItems: "center", gap: 8, padding: 24, fontSize: 13, color: "var(--text-faint)" }}><Spinner size={14} /> Lade…</div>;

  const diffLines = diffData?.diff?.split("\n") ?? [];
  const hasDiff = diffLines.some((l) => l.startsWith("+") || l.startsWith("-"));
  const panelStyle: React.CSSProperties = { borderBottom: "1px solid var(--border)", background: "var(--panel)", flexShrink: 0 };
  const inputStyle: React.CSSProperties = { flex: 1, padding: "8px 11px", background: "var(--surface-2)", border: "1px solid var(--border)", color: "var(--text)", borderRadius: "var(--radius-sm)", fontSize: 13, fontFamily: "var(--font)" };

  return (
    <div style={{ display: "flex", flexDirection: "column", height: "100%", background: "var(--bg)" }}>
      {/* Header */}
      <div style={{ display: "flex", alignItems: "center", gap: 12, padding: "10px 22px", background: "var(--panel)", borderBottom: "1px solid var(--border)", flexShrink: 0 }}>
        <Button variant="ghost" size="sm" icon="chevLeft" onClick={() => navigate({ to: "/config" })} />
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
            <span style={{ fontFamily: "var(--mono)", fontSize: 13.5, fontWeight: 600, color: "var(--text)" }}>{slice?.file}</span>
            <Badge tone="neutral" size="sm">{sliceName}</Badge>
            {isDirty && <Badge tone="warn" size="sm">ungespeichert</Badge>}
          </div>
          {slice?.description && <p style={{ margin: "2px 0 0", fontSize: 12, color: "var(--text-faint)" }}>{slice.description}</p>}
        </div>
        <div style={{ display: "flex", alignItems: "center", gap: 10, flexShrink: 0 }}>
          {saved && <span style={{ display: "inline-flex", alignItems: "center", gap: 4, fontSize: 12, color: "var(--status-ok)" }}><Icon name="check" size={14} stroke={2.2} /> Gespeichert</span>}
          {save.isError && <span style={{ display: "inline-flex", alignItems: "center", gap: 4, fontSize: 12, color: "var(--status-err)" }}><Icon name="alert" size={14} /> {(save.error as Error).message}</span>}
          <Button variant="outline" size="sm" icon={save.isPending ? undefined : "download"} disabled={!isDirty || save.isPending} onClick={() => save.mutate(content)} title="Speichern (Ctrl+S)">
            {save.isPending ? <Spinner size={13} /> : "Speichern"}
          </Button>
          <Button variant="outline" size="sm" icon="git" disabled={isDirty} onClick={() => { setShowCommit(true); setShowDiff(false); setShowDeploy(false); }} title={isDirty ? "Erst speichern" : "Nur git commit"}>Committen</Button>
          <Button variant="primary" size="sm" icon="rocket" disabled={isDirty} onClick={() => { setShowDeploy(true); setShowCommit(false); setSchemaErrors([]); deploy.reset(); setDeployId(null); }} title={isDirty ? "Erst speichern" : "Committen und anwenden"}>Deployen</Button>
        </div>
      </div>

      {/* Commit panel */}
      {showCommit && !showDeploy && (
        <div style={panelStyle}>
          <div style={{ padding: "12px 22px", display: "flex", alignItems: "center", gap: 12 }}>
            <input autoFocus value={commitMsg} onChange={(e) => setCommitMsg(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && commit.mutate(commitMsg || `config: update ${sliceName}.yaml`)}
              placeholder={`config: update ${sliceName}.yaml`} style={inputStyle} />
            <Button variant="primary" size="sm" disabled={commit.isPending} onClick={() => commit.mutate(commitMsg || `config: update ${sliceName}.yaml`)}>
              {commit.isPending ? <Spinner size={13} /> : "Commit"}
            </Button>
            <Button variant="ghost" size="sm" onClick={() => setShowCommit(false)}>Abbrechen</Button>
            {commit.isError && <span style={{ fontSize: 12, color: "var(--status-err)" }}>{(commit.error as Error).message}</span>}
            {commit.isSuccess && <span style={{ display: "inline-flex", alignItems: "center", gap: 4, fontSize: 12, color: "var(--status-ok)" }}><Icon name="check" size={14} stroke={2.2} /> Committed!</span>}
          </div>
          {diffData !== undefined && (
            <div style={{ padding: "0 22px 10px" }}>
              <button onClick={() => setShowDiff((v) => !v)} style={{ display: "inline-flex", alignItems: "center", gap: 4, fontSize: 12, color: "var(--accent)", background: "transparent", border: "none", cursor: "pointer" }}>
                <Icon name={showDiff ? "chevDown" : "chevRight"} size={13} />
                {hasDiff ? "Diff anzeigen" : "Keine ungespeicherten Änderungen"}
              </button>
              {showDiff && hasDiff && (
                <pre className="mc-scroll" style={{ margin: "8px 0 0", fontFamily: "var(--mono)", fontSize: 12, background: "oklch(0.13 0.005 256)", borderRadius: "var(--radius-sm)", padding: "12px 16px", overflowX: "auto", maxHeight: 256, overflowY: "auto", lineHeight: 1.6 }}>
                  {diffLines.map((line, i) => (
                    <span key={i} style={{ display: "block", color:
                      line.startsWith("+") && !line.startsWith("+++") ? "oklch(0.82 0.13 150)" :
                      line.startsWith("-") && !line.startsWith("---") ? "oklch(0.7 0.18 25)" :
                      line.startsWith("@@") ? "var(--accent)" : "var(--text-faint)" }}>{line || " "}</span>
                  ))}
                </pre>
              )}
            </div>
          )}
        </div>
      )}

      {/* Deploy panel */}
      {showDeploy && (
        <div style={panelStyle}>
          {!deployId ? (
            <div style={{ padding: "12px 22px", display: "flex", flexDirection: "column", gap: 12 }}>
              <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
                <Icon name="rocket" size={16} style={{ color: "var(--accent)" }} />
                <span style={{ fontSize: 13, fontWeight: 600, color: "var(--text)" }}>Config committen und auf Cluster anwenden</span>
                <button onClick={resetDeploy} style={{ marginLeft: "auto", background: "transparent", border: "none", color: "var(--text-faint)", cursor: "pointer", padding: 2 }}><Icon name="x" size={16} /></button>
              </div>
              <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
                <input autoFocus value={deployMsg} onChange={(e) => setDeployMsg(e.target.value)} placeholder={`config: apply ${sliceName}.yaml`} style={inputStyle} />
                <Button variant="primary" size="sm" icon={validating ? undefined : "rocket"} disabled={deploy.isPending || validating} onClick={handleDeploy}>
                  {validating ? <><Spinner size={13} /> Validiere…</> : "Jetzt deployen"}
                </Button>
              </div>
              {schemaErrors.length > 0 && (
                <div style={{ background: "color-mix(in oklch, var(--status-err) 10%, var(--surface))", border: "1px solid color-mix(in oklch, var(--status-err) 30%, var(--border))", borderRadius: "var(--radius-sm)", padding: "10px 12px", display: "flex", flexDirection: "column", gap: 4 }}>
                  <div style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 12, fontWeight: 600, color: "var(--status-err)" }}><Icon name="alert" size={14} /> Schema-Fehler ({schemaErrors.length}) — Deploy trotzdem möglich:</div>
                  {schemaErrors.slice(0, 5).map((e, i) => (
                    <div key={i} style={{ fontSize: 11.5, fontFamily: "var(--mono)", color: "var(--status-err)" }}><span style={{ opacity: 0.7 }}>{e.field}: </span>{e.message}</div>
                  ))}
                  {schemaErrors.length > 5 && <div style={{ fontSize: 11.5, color: "var(--status-err)", opacity: 0.8 }}>+{schemaErrors.length - 5} weitere Fehler</div>}
                  <button onClick={() => { setSchemaErrors([]); deploy.mutate(deployMsg || `config: apply ${sliceName}.yaml`); }} style={{ marginTop: 4, fontSize: 12, color: "var(--status-err)", textDecoration: "underline", background: "transparent", border: "none", cursor: "pointer", textAlign: "left", width: "fit-content" }}>Trotzdem deployen</button>
                </div>
              )}
              {deploy.isError && <div style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 12, color: "var(--status-err)" }}><Icon name="alert" size={14} /> {(deploy.error as Error).message}</div>}
            </div>
          ) : (
            <div style={{ padding: "12px 22px", display: "flex", flexDirection: "column", gap: 12 }}>
              <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
                <span style={{ fontSize: 13, fontWeight: 600, color: "var(--text)" }}>Deploy läuft…</span>
                {deployDone && deployStatus === "success" && <span style={{ display: "inline-flex", alignItems: "center", gap: 4, fontSize: 12, color: "var(--status-ok)", fontWeight: 600 }}><Icon name="check" size={14} stroke={2.2} /> Erfolgreich</span>}
                {deployDone && deployStatus === "hooks-failed" && <span style={{ display: "inline-flex", alignItems: "center", gap: 4, fontSize: 12, color: "var(--status-warn)", fontWeight: 600 }}><Icon name="alert" size={14} /> Hooks fehlgeschlagen</span>}
                {deployDone && deployStatus === "failed" && <span style={{ display: "inline-flex", alignItems: "center", gap: 4, fontSize: 12, color: "var(--status-err)", fontWeight: 600 }}><Icon name="x" size={14} stroke={2.2} /> Fehlgeschlagen</span>}
                {deployDone && <Button variant="ghost" size="sm" onClick={resetDeploy} style={{ marginLeft: "auto" }}>Schließen</Button>}
              </div>
              <div ref={logRef} className="mc-scroll" style={{ background: "oklch(0.13 0.005 256)", borderRadius: "var(--radius-sm)", padding: 12, fontFamily: "var(--mono)", fontSize: 12, color: "oklch(0.82 0.13 150)", maxHeight: 200, overflowY: "auto", lineHeight: 1.6 }}>
                {deployLogs.map((line, i) => <div key={i} style={{ color: line.startsWith("ERROR") ? "var(--status-err)" : line.startsWith("WARNING") ? "var(--status-warn)" : undefined }}>{line}</div>)}
                {!deployDone && <div style={{ animation: "mc-ping 1.2s ease infinite", marginTop: 2 }}>▋</div>}
              </div>
              {deployDone && deployStatus === "hooks-failed" && (
                <div style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 12 }}>
                  <Icon name="alert" size={14} style={{ color: "var(--status-warn)", flexShrink: 0 }} />
                  <span style={{ color: "var(--status-warn)" }}>Helm-Apply erfolgreich, aber Post-Upgrade-Hooks fehlgeschlagen (SFU-Patches).</span>
                  <Button variant="ghost" size="sm" icon="hook" onClick={() => navigate({ to: "/hooks" })}>Hooks</Button>
                </div>
              )}
            </div>
          )}
        </div>
      )}

      {/* Editor */}
      <div style={{ flex: 1, overflow: "hidden" }}>
        <YamlEditor height="100%" defaultLanguage="yaml" value={content} onChange={handleEditorChange} onMount={handleEditorMount} theme="vs-dark"
          options={{ fontSize: 13, minimap: { enabled: false }, scrollBeyondLastLine: false, wordWrap: "on", lineNumbers: "on", renderLineHighlight: "line", tabSize: 2, insertSpaces: true, automaticLayout: true }} />
      </div>

      {/* Status bar */}
      <div style={{ padding: "5px 22px", background: "var(--panel)", borderTop: "1px solid var(--border)", display: "flex", alignItems: "center", gap: 16, fontSize: 11.5, color: "var(--text-faint)", flexShrink: 0, fontFamily: "var(--mono)" }}>
        <span>YAML</span>
        <span>{content.split("\n").length} Zeilen</span>
        <span>{new Blob([content]).size} Bytes</span>
        {isDirty && <span style={{ color: "var(--status-warn)" }}>● Ungespeichert — Ctrl+S zum Speichern</span>}
        {!isDirty && !saved && <span style={{ color: "var(--status-ok)" }}>✓ Synchron mit Disk</span>}
      </div>
    </div>
  );
}
