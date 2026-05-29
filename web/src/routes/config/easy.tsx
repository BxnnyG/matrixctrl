import { createFileRoute, Link } from "@tanstack/react-router";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, useMemo, useRef, type ReactNode } from "react";
import Editor from "@monaco-editor/react";
import { api } from "@/lib/api";
import { useUpgradeStream } from "@/lib/ws";
import { useTheme } from "@/lib/theme";
import { type JSONSchema, fieldKind, humanize, getByPath, countLeaves } from "@/lib/schema";
import {
  ArrowLeft, Save, Rocket, CheckCircle2, AlertTriangle, XCircle, Loader2,
  Search, ChevronRight, Settings2, FileCode, SlidersHorizontal,
} from "lucide-react";

export const Route = createFileRoute("/config/easy")({
  component: Settings,
});

interface SettingsResponse {
  schema?: JSONSchema;
  values: Record<string, unknown>;
  comments: Record<string, string>;
  files: Record<string, string>; // top-level key → "section.yaml"
}

interface Slice { name: string; file: string; content: string }
interface DeployResponse { upgrade_id: string }

type Mode = "standard" | "yaml";

function Settings() {
  const qc = useQueryClient();
  const { theme } = useTheme();
  const { data, isLoading } = useQuery({
    queryKey: ["config", "settings"],
    queryFn: () => api.get<SettingsResponse>("/api/v1/config/settings"),
    staleTime: 60_000,
  });

  const [mode, setMode] = useState<Mode>("standard");
  const [fileSel, setFileSel] = useState<string | null>(null); // selected section file, e.g. "synapse.yaml"
  const [query, setQuery] = useState("");
  const [changes, setChanges] = useState<Record<string, unknown>>({});
  const [saved, setSaved] = useState(false);

  // Deploy stream
  const [deployId, setDeployId] = useState<string | null>(null);
  const [logs, setLogs] = useState<string[]>([]);
  const [done, setDone] = useState(false);
  const [status, setStatus] = useState<string | null>(null);
  const logRef = useRef<HTMLDivElement>(null);

  const dirty = Object.keys(changes).length > 0;

  // Invert files map → { "synapse.yaml": ["synapse"], "general.yaml": ["serverName", ...] }
  const fileGroups = useMemo(() => {
    const g: Record<string, string[]> = {};
    for (const [topKey, file] of Object.entries(data?.files ?? {})) {
      (g[file] ??= []).push(topKey);
    }
    for (const k of Object.keys(g)) g[k].sort();
    return g;
  }, [data?.files]);

  const fileList = useMemo(() => {
    const fs = Object.keys(fileGroups);
    fs.sort((a, b) => (a === "general.yaml" ? -1 : b === "general.yaml" ? 1 : a.localeCompare(b)));
    return fs;
  }, [fileGroups]);

  const activeFile = fileSel ?? fileList[0] ?? null;

  const saveStd = useMutation({
    mutationFn: () => api.post("/api/v1/config/settings", { changes, removals: [] }),
    onSuccess: () => {
      setChanges({});
      setSaved(true);
      setTimeout(() => setSaved(false), 2500);
      qc.invalidateQueries({ queryKey: ["config", "settings"] });
      qc.invalidateQueries({ queryKey: ["config", "diff"] });
    },
  });

  const deploy = useMutation({
    mutationFn: (msg: string) => api.post<DeployResponse>("/api/v1/helm/releases/ess/apply-config", { message: msg }),
    onSuccess: (res) => { setDeployId(res.upgrade_id); setLogs([]); setDone(false); setStatus(null); },
  });

  useUpgradeStream(deployId, {
    onLog: (line) => { setLogs((p) => [...p, line]); setTimeout(() => logRef.current?.scrollTo({ top: logRef.current.scrollHeight, behavior: "smooth" }), 30); },
    onDone: (s) => { setDone(true); setStatus(s); if (s === "success") qc.invalidateQueries({ queryKey: ["helm"] }); },
  });

  function effectiveValue(path: string): unknown {
    if (path in changes) return changes[path];
    return getByPath(data?.values, path);
  }
  function setValue(path: string, v: unknown) { setChanges((p) => ({ ...p, [path]: v })); setSaved(false); }

  async function saveAndDeploy() {
    if (dirty) await saveStd.mutateAsync();
    deploy.mutate("config: Einstellungen angewendet");
  }

  if (isLoading) {
    return <div className="flex items-center gap-2 text-sm text-gray-500 dark:text-gray-400"><Loader2 className="w-4 h-4 animate-spin" /> Lade…</div>;
  }
  const schema = data?.schema;
  if (!schema) {
    return <div className="text-sm text-gray-500 dark:text-gray-400">Kein Schema verfügbar. <Link to="/config" className="text-blue-600 dark:text-blue-400 underline">Zurück</Link></div>;
  }

  const searchHits = query.trim()
    ? Object.values(fileGroups).flat().flatMap((top) =>
        collectLeaves(schema.properties?.[top], top)
      ).filter((l) =>
        l.path.toLowerCase().includes(query.toLowerCase()) ||
        (data?.comments[l.path] ?? "").toLowerCase().includes(query.toLowerCase())
      ).slice(0, 80)
    : null;

  return (
    <div className="flex flex-col h-[calc(100vh-4rem)] -mt-8 -mx-6 -mb-8">
      {/* Header */}
      <div className="flex items-center gap-3 px-6 py-3 bg-white dark:bg-gray-900 border-b border-gray-200 dark:border-gray-800 shrink-0">
        <Link to="/config" className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"><ArrowLeft className="w-5 h-5" /></Link>
        <Settings2 className="w-4 h-4 text-blue-600 dark:text-blue-400" />
        <h1 className="text-sm font-semibold text-gray-900 dark:text-gray-100">Einstellungen</h1>

        {/* Standard / YAML switch */}
        <div className="flex items-center bg-gray-100 dark:bg-gray-800 rounded-lg p-0.5 ml-2">
          <button onClick={() => setMode("standard")} className={`flex items-center gap-1.5 px-3 py-1 rounded-md text-xs font-medium transition-colors ${mode === "standard" ? "bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 shadow-sm" : "text-gray-500 dark:text-gray-400"}`}>
            <SlidersHorizontal className="w-3.5 h-3.5" /> Standard
          </button>
          <button onClick={() => setMode("yaml")} className={`flex items-center gap-1.5 px-3 py-1 rounded-md text-xs font-medium transition-colors ${mode === "yaml" ? "bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 shadow-sm" : "text-gray-500 dark:text-gray-400"}`}>
            <FileCode className="w-3.5 h-3.5" /> YAML
          </button>
        </div>

        <div className="flex-1" />
        {mode === "standard" && dirty && <span className="text-xs text-yellow-600 dark:text-yellow-400 bg-yellow-50 dark:bg-yellow-950/40 px-2 py-0.5 rounded">{Object.keys(changes).length} ungespeichert</span>}
        {saved && <span className="flex items-center gap-1 text-xs text-green-600 dark:text-green-400"><CheckCircle2 className="w-3.5 h-3.5" /> Gespeichert</span>}
        {mode === "standard" && (
          <>
            <button onClick={() => saveStd.mutate()} disabled={!dirty || saveStd.isPending} className="flex items-center gap-1.5 px-3 py-1.5 bg-gray-100 dark:bg-gray-800 hover:bg-gray-200 dark:hover:bg-gray-700 disabled:opacity-40 text-gray-700 dark:text-gray-300 text-xs font-medium rounded-lg">
              {saveStd.isPending ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Save className="w-3.5 h-3.5" />} Speichern
            </button>
            <button onClick={saveAndDeploy} disabled={deploy.isPending || !!deployId} className="flex items-center gap-1.5 px-3 py-1.5 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-xs font-medium rounded-lg">
              <Rocket className="w-3.5 h-3.5" /> Speichern & Deployen
            </button>
          </>
        )}
      </div>

      <div className="flex flex-1 overflow-hidden">
        {/* File nav */}
        <aside className="w-56 shrink-0 border-r border-gray-200 dark:border-gray-800 bg-gray-50 dark:bg-gray-900/50 overflow-y-auto">
          <div className="p-3">
            {mode === "standard" && (
              <div className="relative mb-2">
                <Search className="absolute left-2.5 top-2.5 w-3.5 h-3.5 text-gray-400" />
                <input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Suchen…" className="w-full pl-8 pr-2 py-1.5 text-sm border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500" />
              </div>
            )}
            {fileList.map((f) => {
              const active = activeFile === f && !query;
              const leaves = (fileGroups[f] ?? []).reduce((n, top) => n + countLeaves(schema.properties?.[top]), 0);
              return (
                <button key={f} onClick={() => { setFileSel(f); setQuery(""); }} className={`w-full flex items-center justify-between gap-2 px-2.5 py-1.5 rounded-lg text-sm text-left transition-colors ${active ? "bg-blue-50 dark:bg-blue-950/60 text-blue-700 dark:text-blue-300 font-medium" : "text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-800"}`}>
                  <span className="truncate">{humanize(f.replace(/\.yaml$/, ""))}</span>
                  {mode === "standard" && <span className="text-[10px] text-gray-400 dark:text-gray-600 tabular-nums">{leaves}</span>}
                  {mode === "yaml" && <FileCode className="w-3 h-3 text-gray-400 dark:text-gray-600" />}
                </button>
              );
            })}
          </div>
        </aside>

        {/* Panel */}
        <main className="flex-1 overflow-y-auto">
          {mode === "yaml" && activeFile ? (
            <YamlPane sliceName={activeFile.replace(/\.yaml$/, "")} theme={theme} qc={qc} />
          ) : searchHits ? (
            <div className="px-6 py-5 space-y-1 max-w-3xl">
              <p className="text-xs text-gray-500 dark:text-gray-400 mb-3">{searchHits.length} Treffer für „{query}"</p>
              {searchHits.map((l) => (
                <Field key={l.path} node={l.node} path={l.path} comment={data?.comments[l.path]} value={effectiveValue(l.path)} onChange={(v) => setValue(l.path, v)} />
              ))}
            </div>
          ) : activeFile ? (
            <div className="px-6 py-5 max-w-3xl space-y-5">
              {(fileGroups[activeFile] ?? []).map((top) => {
                const node = schema.properties?.[top];
                if (!node) return null;
                return (
                  <div key={top}>
                    <h2 className="text-lg font-semibold text-gray-900 dark:text-gray-100 mb-1">{humanize(top)}</h2>
                    {data?.comments[top] && <p className="text-sm text-gray-500 dark:text-gray-400 mb-3">{data.comments[top]}</p>}
                    {fieldKind(node) === "object"
                      ? <SchemaGroup node={node} path={top} comments={data?.comments ?? {}} effectiveValue={effectiveValue} setValue={setValue} />
                      : <Field node={node} path={top} comment={data?.comments[top]} value={effectiveValue(top)} onChange={(v) => setValue(top, v)} />}
                  </div>
                );
              })}

              {deployId && (
                <div className="space-y-2">
                  <div className="flex items-center gap-3">
                    <span className="text-sm font-medium text-gray-700 dark:text-gray-300">Deploy</span>
                    {done && status === "success" && <span className="flex items-center gap-1 text-xs text-green-600 dark:text-green-400"><CheckCircle2 className="w-3.5 h-3.5" /> Erfolgreich</span>}
                    {done && status === "hooks-failed" && <span className="flex items-center gap-1 text-xs text-yellow-600 dark:text-yellow-400"><AlertTriangle className="w-3.5 h-3.5" /> Hooks fehlgeschlagen</span>}
                    {done && status === "failed" && <span className="flex items-center gap-1 text-xs text-red-600 dark:text-red-400"><XCircle className="w-3.5 h-3.5" /> Fehlgeschlagen</span>}
                  </div>
                  <div ref={logRef} className="bg-gray-950 rounded-xl p-4 font-mono text-xs text-green-400 max-h-64 overflow-y-auto">
                    {logs.map((line, i) => <div key={i} className={`leading-relaxed ${line.startsWith("ERROR") ? "text-red-400" : line.startsWith("WARNING") ? "text-yellow-400" : ""}`}>{line}</div>)}
                    {!done && <div className="animate-pulse mt-1">▋</div>}
                  </div>
                </div>
              )}
            </div>
          ) : null}
        </main>
      </div>
    </div>
  );
}

// ── YAML mode: Monaco editor for one section file ─────────────────────
function YamlPane({ sliceName, theme, qc }: { sliceName: string; theme: string; qc: ReturnType<typeof useQueryClient> }) {
  const [content, setContent] = useState<string | null>(null);
  const [dirty, setDirty] = useState(false);
  const { data: slice, isLoading } = useQuery({
    queryKey: ["config", "slice", sliceName],
    queryFn: () => api.get<Slice>(`/api/v1/config/slices/${sliceName}`),
  });
  const save = useMutation({
    mutationFn: (c: string) => api.put(`/api/v1/config/slices/${sliceName}`, { content: c }),
    onSuccess: () => { setDirty(false); qc.invalidateQueries({ queryKey: ["config"] }); },
  });
  const value = content ?? slice?.content ?? "";

  return (
    <div className="flex flex-col h-full">
      <div className="flex items-center gap-2 px-4 py-2 border-b border-gray-200 dark:border-gray-800 shrink-0">
        <FileCode className="w-3.5 h-3.5 text-gray-400" />
        <span className="font-mono text-xs text-gray-600 dark:text-gray-400">{slice?.file ?? `${sliceName}.yaml`}</span>
        {dirty && <span className="text-[10px] text-yellow-600 dark:text-yellow-400">ungespeichert</span>}
        <div className="flex-1" />
        <button onClick={() => save.mutate(value)} disabled={!dirty || save.isPending} className="flex items-center gap-1.5 px-3 py-1 bg-blue-600 hover:bg-blue-700 disabled:opacity-40 text-white text-xs font-medium rounded-lg">
          {save.isPending ? <Loader2 className="w-3 h-3 animate-spin" /> : <Save className="w-3 h-3" />} Speichern
        </button>
      </div>
      <div className="flex-1">
        {isLoading ? (
          <div className="flex items-center gap-2 p-4 text-sm text-gray-400"><Loader2 className="w-4 h-4 animate-spin" /> Lade…</div>
        ) : (
          <Editor height="100%" defaultLanguage="yaml" value={value} theme={theme === "dark" ? "vs-dark" : "vs"}
            onChange={(v) => { setContent(v ?? ""); setDirty(true); }}
            options={{ fontSize: 13, minimap: { enabled: false }, scrollBeyondLastLine: false, wordWrap: "on", tabSize: 2, automaticLayout: true }} />
        )}
      </div>
    </div>
  );
}

interface LeafEntry { path: string; node: JSONSchema }
function collectLeaves(node: JSONSchema | undefined, path: string, acc: LeafEntry[] = []): LeafEntry[] {
  if (!node) return acc;
  if (fieldKind(node) === "object" && node.properties) {
    for (const [k, child] of Object.entries(node.properties)) collectLeaves(child, `${path}.${k}`, acc);
  } else if (path) acc.push({ path, node });
  return acc;
}

// ── Recursive group renderer ──────────────────────────────────────────
interface GroupProps {
  node: JSONSchema; path: string; comments: Record<string, string>;
  effectiveValue: (p: string) => unknown; setValue: (p: string, v: unknown) => void;
  depth?: number;
}
function SchemaGroup(props: GroupProps) {
  const { node, path, comments, depth = 0 } = props;
  if (!node.properties) return null;
  return (
    <div className="space-y-1">
      {Object.entries(node.properties).map(([key, child]) => {
        const childPath = `${path}.${key}`;
        if (fieldKind(child) === "object") {
          return (
            <NestedGroup key={childPath} title={key} comment={comments[childPath]} depth={depth}>
              <SchemaGroup {...props} node={child} path={childPath} depth={depth + 1} />
            </NestedGroup>
          );
        }
        return <Field key={childPath} node={child} path={childPath} comment={comments[childPath]} value={props.effectiveValue(childPath)} onChange={(v) => props.setValue(childPath, v)} />;
      })}
    </div>
  );
}

function NestedGroup({ title, comment, depth, children }: { title: string; comment?: string; depth: number; children: ReactNode }) {
  const [open, setOpen] = useState(depth < 1);
  return (
    <div className="border border-gray-200 dark:border-gray-700 rounded-xl overflow-hidden">
      <button onClick={() => setOpen((o) => !o)} className="w-full flex items-center gap-2 px-3 py-2 bg-gray-50 dark:bg-gray-900/50 hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors text-left">
        <ChevronRight className={`w-3.5 h-3.5 text-gray-400 transition-transform ${open ? "rotate-90" : ""}`} />
        <span className="text-sm font-medium text-gray-800 dark:text-gray-200">{humanize(title)}</span>
        {comment && <span className="text-xs text-gray-400 dark:text-gray-500 truncate">— {comment}</span>}
      </button>
      {open && <div className="p-3 space-y-1">{children}</div>}
    </div>
  );
}

// ── Leaf field ────────────────────────────────────────────────────────
interface FieldProps { node: JSONSchema; path: string; comment?: string; value: unknown; onChange: (v: unknown) => void }
function Field({ node, path, comment, value, onChange }: FieldProps) {
  const kind = fieldKind(node);
  const key = path.split(".").pop() ?? path;
  const inputCls = "px-2.5 py-1.5 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-blue-500";

  return (
    <div className="flex items-start gap-4 px-3 py-2.5 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-800/40 transition-colors">
      <div className="flex-1 min-w-0">
        <span className="text-sm font-medium text-gray-900 dark:text-gray-100">{humanize(key)}</span>
        {comment && <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5 leading-relaxed">{comment}</p>}
        <code className="text-[10px] text-gray-400 dark:text-gray-600 font-mono">{path}</code>
      </div>
      <div className="shrink-0 pt-0.5">
        {kind === "boolean" ? (
          <button onClick={() => onChange(!value)} role="switch" aria-checked={!!value} className={`relative w-10 h-6 rounded-full transition-colors ${value ? "bg-[#0DBD8B]" : "bg-gray-300 dark:bg-gray-600"}`}>
            <span className={`absolute top-0.5 left-0.5 w-5 h-5 bg-white rounded-full shadow transition-transform ${value ? "translate-x-4" : ""}`} />
          </button>
        ) : kind === "enum" ? (
          <select value={(value as string) ?? ""} onChange={(e) => onChange(e.target.value || undefined)} className={inputCls}>
            <option value="">— nicht gesetzt —</option>
            {node.enum?.map((o) => <option key={String(o)} value={String(o)}>{String(o)}</option>)}
          </select>
        ) : kind === "number" || kind === "integer" ? (
          <input type="number" value={value === undefined || value === null ? "" : String(value)} onChange={(e) => onChange(e.target.value === "" ? undefined : Number(e.target.value))} className={`${inputCls} w-32`} />
        ) : kind === "string" ? (
          <input type="text" value={value === undefined || value === null ? "" : String(value)} onChange={(e) => onChange(e.target.value === "" ? undefined : e.target.value)} className={`${inputCls} w-56`} />
        ) : (
          <span className="text-[10px] text-gray-400 dark:text-gray-600 italic">nur via YAML</span>
        )}
      </div>
    </div>
  );
}
