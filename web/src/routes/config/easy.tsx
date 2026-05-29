import { createFileRoute, Link } from "@tanstack/react-router";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, useMemo, useRef, type ReactNode } from "react";
import { api } from "@/lib/api";
import { useUpgradeStream } from "@/lib/ws";
import {
  type JSONSchema, fieldKind, humanize, getByPath, sections, countLeaves,
} from "@/lib/schema";
import {
  ArrowLeft, Code2, Save, Rocket, CheckCircle2, AlertTriangle, XCircle, Loader2,
  Search, RotateCcw, ChevronRight, Settings2,
} from "lucide-react";

export const Route = createFileRoute("/config/easy")({
  component: EasyMode,
});

interface SettingsResponse {
  schema?: JSONSchema;
  values: Record<string, unknown>;
  comments: Record<string, string>;
  overlay: Record<string, unknown>;
}

interface DeployResponse {
  upgrade_id: string;
}

function EasyMode() {
  const qc = useQueryClient();
  const { data, isLoading } = useQuery({
    queryKey: ["config", "settings"],
    queryFn: () => api.get<SettingsResponse>("/api/v1/config/settings"),
    staleTime: 60_000,
  });

  // Pending edits (path → value) and resets (paths back to base).
  const [changes, setChanges] = useState<Record<string, unknown>>({});
  const [removals, setRemovals] = useState<Set<string>>(new Set());
  const [section, setSection] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [saved, setSaved] = useState(false);

  // Deploy stream
  const [deployId, setDeployId] = useState<string | null>(null);
  const [logs, setLogs] = useState<string[]>([]);
  const [done, setDone] = useState(false);
  const [status, setStatus] = useState<string | null>(null);
  const logRef = useRef<HTMLDivElement>(null);

  const dirty = Object.keys(changes).length > 0 || removals.size > 0;

  const save = useMutation({
    mutationFn: () =>
      api.post("/api/v1/config/overlay", {
        changes,
        removals: [...removals],
      }),
    onSuccess: () => {
      setChanges({});
      setRemovals(new Set());
      setSaved(true);
      setTimeout(() => setSaved(false), 2500);
      qc.invalidateQueries({ queryKey: ["config", "settings"] });
      qc.invalidateQueries({ queryKey: ["config", "diff"] });
    },
  });

  const deploy = useMutation({
    mutationFn: (msg: string) =>
      api.post<DeployResponse>("/api/v1/helm/releases/ess/apply-config", { message: msg }),
    onSuccess: (res) => {
      setDeployId(res.upgrade_id);
      setLogs([]); setDone(false); setStatus(null);
    },
  });

  useUpgradeStream(deployId, {
    onLog: (line) => {
      setLogs((p) => [...p, line]);
      setTimeout(() => logRef.current?.scrollTo({ top: logRef.current.scrollHeight, behavior: "smooth" }), 30);
    },
    onDone: (s) => {
      setDone(true); setStatus(s);
      if (s === "success") qc.invalidateQueries({ queryKey: ["helm"] });
    },
  });

  const schema = data?.schema;
  const secs = useMemo(() => sections(schema), [schema]);

  // Active section: explicit selection, else the first available. No setState in render.
  const activeSection = section ?? secs[0] ?? null;

  // ── value resolution ────────────────────────────────────────────────
  function effectiveValue(path: string): unknown {
    if (path in changes) return changes[path];
    if (removals.has(path)) return getByPath(data?.values, path); // base after reset
    const ov = getByPath(data?.overlay, path);
    if (ov !== undefined) return ov;
    return getByPath(data?.values, path);
  }
  function isOverridden(path: string): boolean {
    if (path in changes) return true;
    if (removals.has(path)) return false;
    return getByPath(data?.overlay, path) !== undefined;
  }

  function setValue(path: string, v: unknown) {
    setChanges((p) => ({ ...p, [path]: v }));
    setRemovals((p) => { const n = new Set(p); n.delete(path); return n; });
    setSaved(false);
  }
  function resetValue(path: string) {
    setChanges((p) => { const n = { ...p }; delete n[path]; return n; });
    setRemovals((p) => new Set(p).add(path));
    setSaved(false);
  }

  async function saveAndDeploy() {
    if (dirty) await save.mutateAsync();
    deploy.mutate("config: Settings via Easy Mode angewendet");
  }

  if (isLoading) {
    return (
      <div className="flex items-center gap-2 text-sm text-gray-500 dark:text-gray-400">
        <Loader2 className="w-4 h-4 animate-spin" /> Lade Schema…
      </div>
    );
  }

  if (!schema) {
    return (
      <div className="text-sm text-gray-500 dark:text-gray-400">
        Kein Schema für diese ESS-Version verfügbar.{" "}
        <Link to="/config" className="text-blue-600 dark:text-blue-400 underline">Zurück</Link>
      </div>
    );
  }

  // ── search across all paths ─────────────────────────────────────────
  const searchHits = query.trim()
    ? collectLeaves(schema, "", data?.comments ?? {}).filter(
        (l) =>
          l.path.toLowerCase().includes(query.toLowerCase()) ||
          (data?.comments[l.path] ?? "").toLowerCase().includes(query.toLowerCase()),
      ).slice(0, 80)
    : null;

  return (
    <div className="flex flex-col h-[calc(100vh-4rem)] -mt-8 -mx-6 -mb-8">
      {/* Header */}
      <div className="flex items-center gap-3 px-6 py-3 bg-white dark:bg-gray-900 border-b border-gray-200 dark:border-gray-800 shrink-0">
        <Link to="/config" className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300">
          <ArrowLeft className="w-5 h-5" />
        </Link>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <Settings2 className="w-4 h-4 text-blue-600 dark:text-blue-400" />
            <h1 className="text-sm font-semibold text-gray-900 dark:text-gray-100">Einstellungen</h1>
            <span className="text-xs text-gray-400 dark:text-gray-500">alle ESS-Optionen · {secs.length} Bereiche</span>
          </div>
        </div>
        {dirty && (
          <span className="text-xs text-yellow-600 dark:text-yellow-400 bg-yellow-50 dark:bg-yellow-950/40 px-2 py-0.5 rounded">
            {Object.keys(changes).length + removals.size} ungespeichert
          </span>
        )}
        {saved && (
          <span className="flex items-center gap-1 text-xs text-green-600 dark:text-green-400">
            <CheckCircle2 className="w-3.5 h-3.5" /> Gespeichert
          </span>
        )}
        <button
          onClick={() => save.mutate()}
          disabled={!dirty || save.isPending}
          className="flex items-center gap-1.5 px-3 py-1.5 bg-gray-100 dark:bg-gray-800 hover:bg-gray-200 dark:hover:bg-gray-700 disabled:opacity-40 text-gray-700 dark:text-gray-300 text-xs font-medium rounded-lg transition-colors"
        >
          {save.isPending ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Save className="w-3.5 h-3.5" />}
          Speichern
        </button>
        <button
          onClick={saveAndDeploy}
          disabled={deploy.isPending || !!deployId}
          className="flex items-center gap-1.5 px-3 py-1.5 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white text-xs font-medium rounded-lg transition-colors"
        >
          <Rocket className="w-3.5 h-3.5" /> Speichern & Deployen
        </button>
      </div>

      <div className="flex flex-1 overflow-hidden">
        {/* Section nav */}
        <aside className="w-60 shrink-0 border-r border-gray-200 dark:border-gray-800 bg-gray-50 dark:bg-gray-900/50 overflow-y-auto">
          <div className="p-3">
            <div className="relative mb-2">
              <Search className="absolute left-2.5 top-2.5 w-3.5 h-3.5 text-gray-400" />
              <input
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="Suchen…"
                className="w-full pl-8 pr-2 py-1.5 text-sm border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500"
              />
            </div>
            {secs.map((s) => {
              const node = schema.properties?.[s];
              const active = activeSection === s && !query;
              return (
                <button
                  key={s}
                  onClick={() => { setSection(s); setQuery(""); }}
                  className={`w-full flex items-center justify-between gap-2 px-2.5 py-1.5 rounded-lg text-sm text-left transition-colors ${
                    active
                      ? "bg-blue-50 dark:bg-blue-950/60 text-blue-700 dark:text-blue-300 font-medium"
                      : "text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-800"
                  }`}
                >
                  <span className="truncate">{humanize(s)}</span>
                  <span className="text-[10px] text-gray-400 dark:text-gray-600 tabular-nums">{countLeaves(node)}</span>
                </button>
              );
            })}
          </div>
        </aside>

        {/* Field panel */}
        <main className="flex-1 overflow-y-auto px-6 py-5">
          {searchHits ? (
            <div className="space-y-1 max-w-3xl">
              <p className="text-xs text-gray-500 dark:text-gray-400 mb-3">{searchHits.length} Treffer für „{query}"</p>
              {searchHits.map((l) => (
                <Field
                  key={l.path} node={l.node} path={l.path}
                  comment={data?.comments[l.path]}
                  value={effectiveValue(l.path)} overridden={isOverridden(l.path)}
                  onChange={(v) => setValue(l.path, v)} onReset={() => resetValue(l.path)}
                />
              ))}
            </div>
          ) : activeSection && schema.properties?.[activeSection] ? (
            <div className="max-w-3xl">
              <h2 className="text-lg font-semibold text-gray-900 dark:text-gray-100 mb-1">{humanize(activeSection)}</h2>
              {data?.comments[activeSection] && (
                <p className="text-sm text-gray-500 dark:text-gray-400 mb-4">{data.comments[activeSection]}</p>
              )}
              <SchemaGroup
                node={schema.properties[activeSection]} path={activeSection}
                comments={data?.comments ?? {}}
                effectiveValue={effectiveValue} isOverridden={isOverridden}
                setValue={setValue} resetValue={resetValue}
                slice={topSlice(activeSection)}
              />
            </div>
          ) : null}

          {/* Deploy log */}
          {deployId && (
            <div className="mt-6 max-w-3xl space-y-2">
              <div className="flex items-center gap-3">
                <span className="text-sm font-medium text-gray-700 dark:text-gray-300">Deploy</span>
                {done && status === "success" && <span className="flex items-center gap-1 text-xs text-green-600 dark:text-green-400"><CheckCircle2 className="w-3.5 h-3.5" /> Erfolgreich</span>}
                {done && status === "hooks-failed" && <span className="flex items-center gap-1 text-xs text-yellow-600 dark:text-yellow-400"><AlertTriangle className="w-3.5 h-3.5" /> Hooks fehlgeschlagen</span>}
                {done && status === "failed" && <span className="flex items-center gap-1 text-xs text-red-600 dark:text-red-400"><XCircle className="w-3.5 h-3.5" /> Fehlgeschlagen</span>}
              </div>
              <div ref={logRef} className="bg-gray-950 rounded-xl p-4 font-mono text-xs text-green-400 max-h-64 overflow-y-auto">
                {logs.map((line, i) => (
                  <div key={i} className={`leading-relaxed ${line.startsWith("ERROR") ? "text-red-400" : line.startsWith("WARNING") ? "text-yellow-400" : ""}`}>{line}</div>
                ))}
                {!done && <div className="animate-pulse mt-1">▋</div>}
              </div>
            </div>
          )}
        </main>
      </div>
    </div>
  );
}

// Map a top-level schema section to the config slice that most likely owns it,
// for the "edit in YAML" fallback link.
function topSlice(section: string): string {
  if (section === "matrixRTC") return "rtc";
  return "values";
}

interface LeafEntry { path: string; node: JSONSchema }

function collectLeaves(node: JSONSchema, path: string, _comments: Record<string, string>, acc: LeafEntry[] = []): LeafEntry[] {
  const kind = fieldKind(node);
  if (kind === "object" && node.properties) {
    for (const [k, child] of Object.entries(node.properties)) {
      collectLeaves(child, path ? `${path}.${k}` : k, _comments, acc);
    }
  } else if (path) {
    acc.push({ path, node });
  }
  return acc;
}

// ── Recursive group renderer ──────────────────────────────────────────
interface GroupProps {
  node: JSONSchema;
  path: string;
  comments: Record<string, string>;
  effectiveValue: (p: string) => unknown;
  isOverridden: (p: string) => boolean;
  setValue: (p: string, v: unknown) => void;
  resetValue: (p: string) => void;
  slice: string;
  depth?: number;
}

function SchemaGroup(props: GroupProps) {
  const { node, path, comments, slice, depth = 0 } = props;
  if (!node.properties) return null;

  return (
    <div className="space-y-1">
      {Object.entries(node.properties).map(([key, child]) => {
        const childPath = path ? `${path}.${key}` : key;
        const kind = fieldKind(child);

        if (kind === "object") {
          return (
            <NestedGroup key={childPath} title={key} comment={comments[childPath]} depth={depth}>
              <SchemaGroup {...props} node={child} path={childPath} depth={depth + 1} />
            </NestedGroup>
          );
        }
        return (
          <Field
            key={childPath} node={child} path={childPath}
            comment={comments[childPath]}
            value={props.effectiveValue(childPath)} overridden={props.isOverridden(childPath)}
            onChange={(v) => props.setValue(childPath, v)} onReset={() => props.resetValue(childPath)}
            slice={slice}
          />
        );
      })}
    </div>
  );
}

function NestedGroup({ title, comment, depth, children }: { title: string; comment?: string; depth: number; children: ReactNode }) {
  const [open, setOpen] = useState(depth < 1);
  return (
    <div className="border border-gray-200 dark:border-gray-700 rounded-xl overflow-hidden">
      <button
        onClick={() => setOpen((o) => !o)}
        className="w-full flex items-center gap-2 px-3 py-2 bg-gray-50 dark:bg-gray-900/50 hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors text-left"
      >
        <ChevronRight className={`w-3.5 h-3.5 text-gray-400 transition-transform ${open ? "rotate-90" : ""}`} />
        <span className="text-sm font-medium text-gray-800 dark:text-gray-200">{humanize(title)}</span>
        {comment && <span className="text-xs text-gray-400 dark:text-gray-500 truncate">— {comment}</span>}
      </button>
      {open && <div className="p-3 space-y-1">{children}</div>}
    </div>
  );
}

// ── Leaf field ────────────────────────────────────────────────────────
interface FieldProps {
  node: JSONSchema;
  path: string;
  comment?: string;
  value: unknown;
  overridden: boolean;
  onChange: (v: unknown) => void;
  onReset: () => void;
  slice?: string;
}

function Field({ node, path, comment, value, overridden, onChange, onReset, slice = "values" }: FieldProps) {
  const kind = fieldKind(node);
  const key = path.split(".").pop() ?? path;

  return (
    <div className="flex items-start gap-4 px-3 py-2.5 rounded-lg hover:bg-gray-50 dark:hover:bg-gray-800/40 transition-colors">
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium text-gray-900 dark:text-gray-100">{humanize(key)}</span>
          {overridden && (
            <span className="text-[10px] text-blue-600 dark:text-blue-400 bg-blue-50 dark:bg-blue-950/60 px-1.5 py-0.5 rounded">überschrieben</span>
          )}
        </div>
        {comment && <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5 leading-relaxed">{comment}</p>}
        <code className="text-[10px] text-gray-400 dark:text-gray-600 font-mono">{path}</code>
      </div>
      <div className="shrink-0 flex items-center gap-2 pt-0.5">
        <Widget kind={kind} node={node} value={value} onChange={onChange} slice={slice} path={path} />
        {overridden && (
          <button onClick={onReset} title="Auf Basiswert zurücksetzen" className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300">
            <RotateCcw className="w-3.5 h-3.5" />
          </button>
        )}
      </div>
    </div>
  );
}

function Widget({ kind, node, value, onChange, slice, path }: {
  kind: ReturnType<typeof fieldKind>; node: JSONSchema; value: unknown;
  onChange: (v: unknown) => void; slice: string; path: string;
}) {
  const inputCls = "px-2.5 py-1.5 border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-blue-500";

  switch (kind) {
    case "boolean":
      return (
        <button
          onClick={() => onChange(!value)}
          role="switch" aria-checked={!!value}
          className={`relative w-10 h-6 rounded-full transition-colors ${value ? "bg-[#0DBD8B]" : "bg-gray-300 dark:bg-gray-600"}`}
        >
          <span className={`absolute top-0.5 left-0.5 w-5 h-5 bg-white rounded-full shadow transition-transform ${value ? "translate-x-4" : ""}`} />
        </button>
      );
    case "enum":
      return (
        <select value={(value as string) ?? ""} onChange={(e) => onChange(e.target.value || undefined)} className={inputCls}>
          <option value="">— nicht gesetzt —</option>
          {node.enum?.map((o) => <option key={String(o)} value={String(o)}>{String(o)}</option>)}
        </select>
      );
    case "number":
    case "integer":
      return (
        <input
          type="number" value={value === undefined || value === null ? "" : String(value)}
          onChange={(e) => onChange(e.target.value === "" ? undefined : Number(e.target.value))}
          className={`${inputCls} w-32`}
        />
      );
    case "string":
      return (
        <input
          type="text" value={value === undefined || value === null ? "" : String(value)}
          onChange={(e) => onChange(e.target.value === "" ? undefined : e.target.value)}
          className={`${inputCls} w-56`}
        />
      );
    default:
      // array / freeform / unknown → not safely form-editable; link to YAML editor.
      return (
        <Link
          to="/config/$slice" params={{ slice }}
          className="flex items-center gap-1 text-xs text-gray-500 dark:text-gray-400 hover:text-blue-600 dark:hover:text-blue-400"
          title={`${path} ist komplex (Liste/Objekt) — im YAML-Editor bearbeiten`}
        >
          <Code2 className="w-3.5 h-3.5" /> YAML
        </Link>
      );
  }
}
