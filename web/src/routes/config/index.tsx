import { createFileRoute, Link } from "@tanstack/react-router";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, useMemo, useRef, type ReactNode } from "react";
import Editor from "@monaco-editor/react";
import { api } from "@/lib/api";
import { useUpgradeStream } from "@/lib/ws";
import { useTheme } from "@/lib/theme";
import { type JSONSchema, fieldKind, humanize, getByPath, countLeaves } from "@/lib/schema";
import { groupNav, orderKeys } from "@/lib/sections";
import {
  Save, Rocket, CheckCircle2, AlertTriangle, XCircle, Loader2,
  Search, ChevronRight, ChevronDown, Settings2, FileCode, SlidersHorizontal, History,
  Server, ShieldCheck, Globe, Video, UserCog, Database, Link2, Box, type LucideIcon,
} from "lucide-react";

// Icon per section file for a more scannable, less flat UI.
const SECTION_ICONS: Record<string, LucideIcon> = {
  "general.yaml": Settings2,
  "synapse.yaml": Server,
  "matrixAuthenticationService.yaml": ShieldCheck,
  "elementWeb.yaml": Globe,
  "elementAdmin.yaml": UserCog,
  "matrixRTC.yaml": Video,
  "wellKnownDelegation.yaml": Link2,
  "postgres.yaml": Database,
  "redis.yaml": Database,
};
function iconFor(file: string): LucideIcon {
  return SECTION_ICONS[file] ?? Box;
}

export const Route = createFileRoute("/config/")({
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
  const [fileSel, setFileSel] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [changes, setChanges] = useState<Record<string, unknown>>({});
  const [saved, setSaved] = useState(false);

  const [deployId, setDeployId] = useState<string | null>(null);
  const [logs, setLogs] = useState<string[]>([]);
  const [done, setDone] = useState(false);
  const [status, setStatus] = useState<string | null>(null);
  const logRef = useRef<HTMLDivElement>(null);

  const dirty = Object.keys(changes).length > 0;

  const fileGroups = useMemo(() => {
    const g: Record<string, string[]> = {};
    for (const [topKey, file] of Object.entries(data?.files ?? {})) {
      (g[file] ??= []).push(topKey);
    }
    for (const k of Object.keys(g)) g[k] = orderKeys(g[k]);
    return g;
  }, [data?.files]);

  const navGroups = useMemo(() => groupNav(Object.keys(fileGroups)), [fileGroups]);
  const fileList = useMemo(() => navGroups.flatMap((g) => g.files), [navGroups]);
  const [collapsedGroups, setCollapsedGroups] = useState<Record<string, boolean>>({});

  const activeFile = fileSel ?? fileList[0] ?? null;

  const saveStd = useMutation({
    mutationFn: () => api.post("/api/v1/config/settings", { changes, removals: [] }),
    onSuccess: () => {
      setChanges({}); setSaved(true); setTimeout(() => setSaved(false), 2500);
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
    return <div className="text-sm text-gray-500 dark:text-gray-400">Kein Schema verfügbar.</div>;
  }

  const searchHits = query.trim()
    ? Object.values(fileGroups).flat().flatMap((top) => collectLeaves(schema.properties?.[top], top))
        .filter((l) => l.path.toLowerCase().includes(query.toLowerCase()) || (data?.comments[l.path] ?? "").toLowerCase().includes(query.toLowerCase()))
        .slice(0, 120)
    : null;

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="flex items-center gap-3 px-6 py-3 bg-white dark:bg-gray-900 border-b border-gray-200 dark:border-gray-800 shrink-0">
        <Settings2 className="w-4 h-4 text-blue-600 dark:text-blue-400" />
        <h1 className="text-sm font-semibold text-gray-900 dark:text-gray-100">Einstellungen</h1>

        <div className="flex items-center bg-gray-100 dark:bg-gray-800 rounded-lg p-0.5 ml-2">
          <button onClick={() => setMode("standard")} className={`flex items-center gap-1.5 px-3 py-1 rounded-md text-xs font-medium transition-colors ${mode === "standard" ? "bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 shadow-sm" : "text-gray-500 dark:text-gray-400"}`}>
            <SlidersHorizontal className="w-3.5 h-3.5" /> Standard
          </button>
          <button onClick={() => setMode("yaml")} className={`flex items-center gap-1.5 px-3 py-1 rounded-md text-xs font-medium transition-colors ${mode === "yaml" ? "bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 shadow-sm" : "text-gray-500 dark:text-gray-400"}`}>
            <FileCode className="w-3.5 h-3.5" /> YAML
          </button>
        </div>

        <div className="flex-1" />
        <Link to="/config/history" className="flex items-center gap-1.5 px-3 py-1.5 text-xs text-gray-500 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-800 rounded-lg transition-colors">
          <History className="w-3.5 h-3.5" /> Verlauf
        </Link>
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
        {/* Category nav (second sidebar) */}
        <aside className="w-56 shrink-0 border-r border-gray-200 dark:border-gray-800 bg-gray-50 dark:bg-gray-900/50 overflow-y-auto">
          <div className="p-3">
            <div className="relative mb-2">
              <Search className="absolute left-2.5 top-2.5 w-3.5 h-3.5 text-gray-400" />
              <input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Alle Optionen suchen…" className="w-full pl-8 pr-2 py-1.5 text-sm border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500" />
            </div>
            {navGroups.map((grp) => {
              const collapsed = collapsedGroups[grp.label] ?? !grp.defaultOpen;
              return (
                <div key={grp.label} className="mb-1">
                  <button onClick={() => setCollapsedGroups((p) => ({ ...p, [grp.label]: !collapsed }))} className="w-full flex items-center gap-1 px-2 py-1.5 text-[10px] font-semibold uppercase tracking-wider text-gray-400 dark:text-gray-500 hover:text-gray-600 dark:hover:text-gray-300">
                    <ChevronDown className={`w-3 h-3 transition-transform ${collapsed ? "-rotate-90" : ""}`} />
                    {grp.label}
                  </button>
                  {!collapsed && grp.files.map((f) => {
                    const active = activeFile === f && !query;
                    const Icon = iconFor(f);
                    const leaves = (fileGroups[f] ?? []).reduce((n, top) => n + countLeaves(schema.properties?.[top]), 0);
                    return (
                      <button key={f} onClick={() => { setFileSel(f); setQuery(""); }} className={`w-full flex items-center gap-2.5 px-2.5 py-1.5 rounded-lg text-left transition-colors ${active ? "bg-blue-50 dark:bg-blue-950/60" : "hover:bg-gray-100 dark:hover:bg-gray-800"}`}>
                        <Icon className={`w-4 h-4 shrink-0 ${active ? "text-blue-600 dark:text-blue-400" : "text-gray-400 dark:text-gray-500"}`} />
                        <div className="flex-1 min-w-0">
                          <div className={`text-sm truncate ${active ? "text-blue-700 dark:text-blue-300 font-medium" : "text-gray-700 dark:text-gray-300"}`}>{humanize(f.replace(/\.yaml$/, ""))}</div>
                          <div className="text-[10px] text-gray-400 dark:text-gray-600 font-mono truncate">{f}</div>
                        </div>
                        <span className="text-[10px] text-gray-400 dark:text-gray-600 tabular-nums shrink-0">{leaves}</span>
                      </button>
                    );
                  })}
                </div>
              );
            })}
          </div>
        </aside>

        {/* Full-width panel */}
        <main className="flex-1 overflow-y-auto bg-gray-50/50 dark:bg-gray-950/30">
          {searchHits ? (
            <div className="px-8 py-6 space-y-2">
              <p className="text-xs text-gray-500 dark:text-gray-400 mb-3">{searchHits.length} Treffer für „{query}"</p>
              <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-xl divide-y divide-gray-100 dark:divide-gray-700/60">
                {searchHits.map((l) => (
                  <Field key={l.path} node={l.node} path={l.path} comment={data?.comments[l.path]} value={effectiveValue(l.path)} onChange={(v) => setValue(l.path, v)} />
                ))}
              </div>
            </div>
          ) : mode === "yaml" && activeFile ? (
            <YamlPane sliceName={activeFile.replace(/\.yaml$/, "")} theme={theme} qc={qc} />
          ) : activeFile ? (
            <div className="px-8 py-6 space-y-8">
              {(fileGroups[activeFile] ?? []).map((top) => {
                const node = schema.properties?.[top];
                if (!node) return null;
                const SecIcon = iconFor(activeFile);
                return (
                  <section key={top}>
                    <div className="flex items-start gap-3 mb-4">
                      <div className="flex items-center justify-center w-9 h-9 rounded-lg bg-gradient-to-br from-blue-500/90 to-[#0DBD8B]/90 text-white shrink-0 shadow-sm">
                        <SecIcon className="w-[18px] h-[18px]" />
                      </div>
                      <div className="min-w-0">
                        <h2 className="text-lg font-semibold text-gray-900 dark:text-gray-100 leading-tight">{humanize(top)}</h2>
                        <div className="flex items-center gap-2 mt-0.5">
                          <code className="text-[11px] text-gray-400 dark:text-gray-500 font-mono">{activeFile}</code>
                          {data?.comments[top] && <span className="text-sm text-gray-500 dark:text-gray-400 truncate">· {data.comments[top]}</span>}
                        </div>
                      </div>
                    </div>
                    {fieldKind(node) === "object"
                      ? <SchemaSection node={node} path={top} comments={data?.comments ?? {}} effectiveValue={effectiveValue} setValue={setValue} />
                      : <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-xl"><Field node={node} path={top} comment={data?.comments[top]} value={effectiveValue(top)} onChange={(v) => setValue(top, v)} /></div>}
                  </section>
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

interface GroupProps {
  node: JSONSchema; path: string; comments: Record<string, string>;
  effectiveValue: (p: string) => unknown; setValue: (p: string, v: unknown) => void; depth?: number;
}

// Uniform renderer: at every level the direct scalar/enum/bool fields go into ONE
// card, and each nested object becomes its own collapsible card below. This makes
// the hierarchy consistent and obvious — never a mix of loose rows and boxes.
function SchemaSection(props: GroupProps) {
  const { node, path, comments, depth = 0 } = props;
  if (!node.properties) return null;
  const ordered = orderKeys(Object.keys(node.properties)).map((k) => [k, node.properties![k]] as const);
  const leaves = ordered.filter(([, c]) => fieldKind(c) !== "object");
  const groups = ordered.filter(([, c]) => fieldKind(c) === "object");

  return (
    <div className="space-y-3">
      {leaves.length > 0 && (
        <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-xl shadow-sm divide-y divide-gray-100 dark:divide-gray-700/60">
          {leaves.map(([key, child]) => {
            const childPath = `${path}.${key}`;
            return <Field key={childPath} node={child} path={childPath} comment={comments[childPath]} value={props.effectiveValue(childPath)} onChange={(v) => props.setValue(childPath, v)} />;
          })}
        </div>
      )}
      {groups.map(([key, child]) => {
        const childPath = `${path}.${key}`;
        const childLeaves = Object.values(child.properties ?? {}).filter((c) => fieldKind(c) !== "object").length;
        const childGroups = Object.values(child.properties ?? {}).filter((c) => fieldKind(c) === "object").length;
        return (
          <CollapsibleCard key={childPath} title={humanize(key)} comment={comments[childPath]} count={`${childLeaves}${childGroups ? `+${childGroups}` : ""}`} depth={depth}>
            <SchemaSection {...props} node={child} path={childPath} depth={depth + 1} />
          </CollapsibleCard>
        );
      })}
    </div>
  );
}

function CollapsibleCard({ title, comment, count, depth, children }: { title: string; comment?: string; count: string; depth: number; children: ReactNode }) {
  const [open, setOpen] = useState(depth < 1);
  return (
    <div className={`bg-white dark:bg-gray-800 border rounded-xl shadow-sm overflow-hidden transition-colors ${open ? "border-blue-200 dark:border-blue-900/60" : "border-gray-200 dark:border-gray-700"}`}>
      <button onClick={() => setOpen((o) => !o)} className="w-full flex items-center gap-2.5 px-4 py-3 hover:bg-gray-50 dark:hover:bg-gray-700/40 transition-colors text-left">
        <div className={`flex items-center justify-center w-5 h-5 rounded-md shrink-0 transition-colors ${open ? "bg-blue-100 dark:bg-blue-900/50 text-blue-600 dark:text-blue-400" : "bg-gray-100 dark:bg-gray-700 text-gray-400 dark:text-gray-500"}`}>
          <ChevronRight className={`w-3.5 h-3.5 transition-transform ${open ? "rotate-90" : ""}`} />
        </div>
        <div className="flex-1 min-w-0">
          <div className="text-sm font-semibold text-gray-800 dark:text-gray-200">{title}</div>
          {comment && <div className="text-xs text-gray-500 dark:text-gray-400 truncate">{comment}</div>}
        </div>
        <span className="text-[10px] font-mono text-gray-400 dark:text-gray-600 shrink-0 bg-gray-50 dark:bg-gray-900/60 px-1.5 py-0.5 rounded">{count}</span>
      </button>
      {open && <div className="px-3 pb-3 pt-3 pl-7 space-y-3 border-t border-gray-100 dark:border-gray-700/60 bg-gray-50/40 dark:bg-gray-900/20">{children}</div>}
    </div>
  );
}

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
          <input type="number" value={value === undefined || value === null ? "" : String(value)} onChange={(e) => onChange(e.target.value === "" ? undefined : Number(e.target.value))} className={`${inputCls} w-40`} />
        ) : kind === "string" ? (
          <input type="text" value={value === undefined || value === null ? "" : String(value)} onChange={(e) => onChange(e.target.value === "" ? undefined : e.target.value)} className={`${inputCls} w-72`} />
        ) : (
          <span className="text-[10px] text-gray-400 dark:text-gray-600 italic">nur via YAML</span>
        )}
      </div>
    </div>
  );
}
