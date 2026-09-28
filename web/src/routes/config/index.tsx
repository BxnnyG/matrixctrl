import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, useMemo, useRef, useEffect, type ReactNode } from "react";
import * as jsYaml from "js-yaml";
import type { OnMount } from "@monaco-editor/react";
import { YamlEditor } from "@/components/config/YamlEditor";
import { api } from "@/lib/api";
import { ApplyBar } from "@/components/config/ApplyBar";
import { parseDiff } from "@/components/config/DiffView";

interface ConfigLocation {
  path: string;
  seed?: string;
  bytes: number;
  files: number;
  commits: number;
  versioned: boolean;
}
import { type JSONSchema, fieldKind, humanize, getByPath, countLeaves, collectLeaves, mapEntries } from "@/lib/schema";
import { groupNav, orderKeys } from "@/lib/sections";
import { Icon, Badge, Button, Toggle, Spinner, EmptyState, type IconName } from "@/components/mc";
import { DiffView } from "@/components/config/DiffView";

// Icon (mc set) per section file for a scannable, less flat UI.
const SECTION_ICONS: Record<string, IconName> = {
  "general.yaml": "settings",
  "synapse.yaml": "server",
  "matrixAuthenticationService.yaml": "key",
  "elementWeb.yaml": "globe",
  "elementAdmin.yaml": "users",
  "matrixRTC.yaml": "phone",
  "wellKnownDelegation.yaml": "ext",
  "postgres.yaml": "database",
  "redis.yaml": "database",
};
const iconFor = (file: string): IconName => SECTION_ICONS[file] ?? "file";

// Every view of this page is a URL — `?section=synapse.yaml&mode=yaml` — so a setting
// can be linked to, and the old per-section editor can simply forward here.
export const Route = createFileRoute("/config/")({
  component: Settings,
  validateSearch: (s: Record<string, unknown>): { section?: string; mode?: Mode } => ({
    section: typeof s.section === "string" ? s.section : undefined,
    mode: s.mode === "yaml" || s.mode === "diff" || s.mode === "standard" ? s.mode : undefined,
  }),
});

interface SchemaSource { version: string; from: "deployed" | "embedded"; fallback: boolean; reason?: string }
interface SettingsResponse {
  schema?: JSONSchema;
  schema_source?: SchemaSource;
  values: Record<string, unknown>;
  comments: Record<string, string>;
  files: Record<string, string>; // top-level key → "section.yaml"
}
interface Slice { name: string; file: string; content: string }

type Mode = "standard" | "yaml" | "diff";

function Settings() {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const { data, isLoading } = useQuery({
    queryKey: ["config", "settings"],
    queryFn: () => api.get<SettingsResponse>("/api/v1/config/settings"),
    staleTime: 60_000,
  });

  const search = Route.useSearch();
  const [mode, setMode] = useState<Mode>(search.mode ?? "standard");
  const [fileSel, setFileSel] = useState<string | null>(search.section ?? null);
  // Cheap and static; asked once so the header can state where the configuration is.
  //
  // It used to sit further down, next to the header that reads it — which put it after
  // two early returns. On a cold cache the first render returns at `isLoading` and this
  // hook never runs; the second render reaches it, and React counts one hook more than
  // last time: minified error #310, "Rendered more hooks than during the previous
  // render". The whole page became "Something went wrong!" whenever it was opened
  // without a warm query cache.
  //
  // Hooks go where the rules allow, not where the narrative reads best.
  const { data: location } = useQuery({
    queryKey: ["config", "location"],
    queryFn: () => api.get<ConfigLocation>("/api/v1/config/location"),
    staleTime: 5 * 60_000,
  });

  const [query, setQuery] = useState("");
  const [changes, setChanges] = useState<Record<string, unknown>>({});
  const [saved, setSaved] = useState(false);

  const [redeploy, setRedeploy] = useState(0);

  const dirty = Object.keys(changes).length > 0;

  const fileGroups = useMemo(() => {
    const g: Record<string, string[]> = {};
    for (const [topKey, file] of Object.entries(data?.files ?? {})) (g[file] ??= []).push(topKey);
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

  // Uncommitted working-tree changes — what "Übernehmen" would ship. Always loaded
  // now: the pending-changes bar is on every view of the page (etappe 108).
  const { data: diffData, isFetching: diffLoading } = useQuery({
    queryKey: ["config", "diff"],
    queryFn: () => api.get<{ diff: string }>("/api/v1/config/diff"),
    refetchOnWindowFocus: false,
  });
  const hasDiff = !!diffData?.diff && !diffData.diff.startsWith("(") && /^[+-]/m.test(diffData.diff);
  const pendingFiles = useMemo(() => hasDiff ? parseDiff(diffData!.diff).map((f) => f.displayName) : [], [hasDiff, diffData]);

  function effectiveValue(path: string): unknown {
    if (path in changes) return changes[path];
    return getByPath(data?.values, path);
  }
  function setValue(path: string, v: unknown) { setChanges((p) => ({ ...p, [path]: v })); setSaved(false); }

  if (isLoading) return <div style={{ display: "flex", alignItems: "center", gap: 8, padding: 24, fontSize: 13, color: "var(--text-faint)" }}><Spinner size={14} /> Lade…</div>;
  const schema = data?.schema;
  if (!schema) return <div style={{ padding: 24, fontSize: 13, color: "var(--text-faint)" }}>Kein Schema verfügbar.</div>;

  const searchHits = query.trim()
    // In navigation order — Synapse and Postgres before the one-shot helper jobs.
    ? fileList.flatMap((f) => fileGroups[f] ?? []).flatMap((top) => collectLeaves(schema.properties?.[top], top, data?.values))
        .filter((l) => l.path.toLowerCase().includes(query.toLowerCase()) || (data?.comments[l.path] ?? "").toLowerCase().includes(query.toLowerCase()))
        .slice(0, 120)
    : null;

  const segBtn = (on: boolean): React.CSSProperties => ({
    display: "inline-flex", alignItems: "center", gap: 6, padding: "5px 11px", fontSize: 12.5, fontWeight: 550, fontFamily: "var(--font)",
    border: "none", cursor: "pointer", borderRadius: "calc(var(--radius-sm) - 2px)",
    background: on ? "var(--surface)" : "transparent", color: on ? "var(--text)" : "var(--text-faint)", boxShadow: on ? "0 1px 2px rgba(0,0,0,.25)" : "none",
  });

  return (
    <div style={{ display: "flex", flexDirection: "column", height: "100%", background: "var(--bg)" }}>
      {/* Header */}
      <div style={{ display: "flex", alignItems: "center", gap: 12, padding: "10px 22px", background: "var(--panel)", borderBottom: "1px solid var(--border)", flexShrink: 0 }}>
        <Icon name="sliders" size={16} style={{ color: "var(--accent)" }} />
        <h1 style={{ margin: 0, fontSize: 14, fontWeight: 600, color: "var(--text)" }}>Einstellungen</h1>

        <div style={{ display: "flex", gap: 2, padding: 3, marginLeft: 6, background: "var(--surface-2)", borderRadius: "var(--radius-sm)", border: "1px solid var(--border-soft)" }}>
          <button onClick={() => setMode("standard")} style={segBtn(mode === "standard")}><Icon name="sliders" size={13} /> Standard</button>
          <button onClick={() => setMode("yaml")} style={segBtn(mode === "yaml")}><Icon name="file" size={13} /> YAML</button>
          <button onClick={() => setMode("diff")} style={segBtn(mode === "diff")}><Icon name="diff" size={13} /> Änderungen</button>
        </div>

        <div style={{ flex: 1 }} />

        {/* Where this configuration actually lives. Added because an operator who had
            been editing it for months still assumed it landed in the folder they once
            seeded it from — ten screens about the configuration, and none of them said
            (etappe 71). Compact by default; the detail is in the tooltip. */}
        {location && (
          <span
            title={`Gespeichert in einem Git-Repository auf eigenem Volume: ${location.path}\n` +
              `${location.files} Dateien · ${(location.bytes / 1024).toFixed(0)} KB · ${location.commits} Versionen` +
              (location.seed ? `\n\nDer Ordner ${location.seed} war nur die Saat beim ersten Start und wird nicht mehr gelesen.` : "")}
            aria-label="Wo die Einstellungen gespeichert sind"
            style={{ display: "inline-grid", placeItems: "center", width: 28, height: 28, borderRadius: "var(--radius-sm)",
              background: "var(--surface-2)", border: "1px solid var(--border-soft)", color: "var(--text-faint)", cursor: "default" }}>
            <Icon name="database" size={13} />
          </span>
        )}

        {/* Which chart the form is built from. It used to be a schema compiled into the
            binary, silently 26.5.x whatever was running (etappe 107) — so it is named,
            and a fallback is shown as one rather than passed off as the truth. */}
        {data?.schema_source && (
          <span title={data.schema_source.fallback
              ? `Das Schema des laufenden Charts war nicht lesbar (${data.schema_source.reason ?? "unbekannt"}). Formular und Prüfung nutzen das eingebaute Schema ${data.schema_source.version} — Felder neuerer Versionen fehlen.`
              : "Formular und Prüfung nutzen das Schema aus dem Chart, das gerade läuft."}
            style={{ display: "inline-flex", alignItems: "center", gap: 6, padding: "4px 9px", borderRadius: "var(--radius-sm)", cursor: "default",
              background: data.schema_source.fallback ? "color-mix(in oklch, var(--status-warn) 14%, transparent)" : "var(--surface-2)",
              border: `1px solid ${data.schema_source.fallback ? "var(--status-warn)" : "var(--border-soft)"}`,
              color: data.schema_source.fallback ? "var(--status-warn)" : "var(--text-faint)", fontSize: 11.5 }}>
            <Icon name={data.schema_source.fallback ? "alert" : "check"} size={13} />
            ESS {data.schema_source.version}{data.schema_source.fallback ? " (eingebaut)" : ""}
          </span>
        )}

        <Button variant="ghost" size="sm" icon="clock" onClick={() => navigate({ to: "/config/history" })}>Verlauf</Button>
        {mode === "standard" && dirty && <Badge tone="warn" size="sm">{Object.keys(changes).length} ungespeichert</Badge>}
        {saved && <span style={{ display: "inline-flex", alignItems: "center", gap: 4, fontSize: 12, color: "var(--status-ok)" }}><Icon name="check" size={14} stroke={2.2} /> Gespeichert</span>}
        {mode === "standard" && (
          <Button variant="outline" size="sm" icon={saveStd.isPending ? undefined : "download"} disabled={!dirty || saveStd.isPending} onClick={() => saveStd.mutate()}>
            {saveStd.isPending ? <Spinner size={13} /> : "Speichern"}
          </Button>
        )}
        {/* With nothing pending, applying the current state again is still possible —
            to re-run the hooks, say. With edits pending, the bar at the foot is the way
            (etappe 108): it says what they would do before doing it. */}
        {!dirty && !hasDiff && (
          <Button variant="outline" size="sm" icon="rocket" onClick={() => setRedeploy((n) => n + 1)}>Erneut anwenden</Button>
        )}
      </div>

      <div style={{ display: "flex", flex: 1, overflow: "hidden" }}>
        {/* Category nav */}
        <aside className="mc-scroll" style={{ width: 232, flexShrink: 0, borderRight: "1px solid var(--border)", background: "var(--panel)", overflowY: "auto" }}>
          <div style={{ padding: 12 }}>
            <div style={{ position: "relative", marginBottom: 10 }}>
              <Icon name="search" size={14} style={{ position: "absolute", left: 10, top: 9, color: "var(--text-faint)" }} />
              <input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Optionen suchen…"
                style={{ width: "100%", padding: "8px 10px 8px 30px", fontSize: 13, background: "var(--surface-2)", border: "1px solid var(--border)", color: "var(--text)", borderRadius: "var(--radius-sm)", fontFamily: "var(--font)" }} />
            </div>
            {navGroups.map((grp) => {
              const collapsed = collapsedGroups[grp.label] ?? !grp.defaultOpen;
              return (
                <div key={grp.label} style={{ marginBottom: 4 }}>
                  <button onClick={() => setCollapsedGroups((p) => ({ ...p, [grp.label]: !collapsed }))}
                    style={{ width: "100%", display: "flex", alignItems: "center", gap: 4, padding: "6px 8px", fontSize: 10, fontWeight: 600, letterSpacing: "0.06em", textTransform: "uppercase", color: "var(--text-faint)", background: "transparent", border: "none", cursor: "pointer" }}>
                    <Icon name="chevDown" size={12} style={{ transform: collapsed ? "rotate(-90deg)" : "none", transition: "transform .15s" }} />
                    {grp.label}
                  </button>
                  {!collapsed && grp.files.map((f) => {
                    const active = activeFile === f && !query;
                    const leaves = (fileGroups[f] ?? []).reduce((n, top) => n + countLeaves(schema.properties?.[top]), 0);
                    return (
                      <button key={f} onClick={() => { setFileSel(f); setQuery(""); }}
                        style={{ width: "100%", display: "flex", alignItems: "center", gap: 10, padding: "8px 10px", borderRadius: "var(--radius-sm)", textAlign: "left", cursor: "pointer", border: "none", background: active ? "var(--accent-soft)" : "transparent", color: active ? "var(--accent)" : "var(--text-dim)" }}>
                        <Icon name={iconFor(f)} size={16} style={{ flexShrink: 0, color: active ? "var(--accent)" : "var(--text-faint)" }} />
                        <div style={{ flex: 1, minWidth: 0 }}>
                          <div style={{ fontSize: 13, fontWeight: active ? 600 : 500, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis", color: active ? "var(--accent)" : "var(--text)" }}>{humanize(f.replace(/\.yaml$/, ""))}</div>
                          <div style={{ fontSize: 10, fontFamily: "var(--mono)", color: "var(--text-faint)", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>{f}</div>
                        </div>
                        <span style={{ fontSize: 10, fontFamily: "var(--mono)", color: "var(--text-faint)", flexShrink: 0 }}>{leaves}</span>
                      </button>
                    );
                  })}
                </div>
              );
            })}
          </div>
        </aside>

        {/* Main panel */}
        <main className="mc-scroll" style={{ flex: 1, overflowY: "auto", background: "var(--bg)" }}>
          {searchHits ? (
            <div style={{ padding: "24px 32px", display: "flex", flexDirection: "column", gap: 8 }}>
              <p style={{ margin: "0 0 8px", fontSize: 12, color: "var(--text-faint)" }}>{searchHits.length} Treffer für „{query}"</p>
              <div style={{ background: "var(--surface)", border: "1px solid var(--border)", borderRadius: "var(--radius)" }}>
                {searchHits.map((l, i) => <Field key={l.path} node={l.node} path={l.path} comment={data?.comments[l.path]} value={effectiveValue(l.path)} onChange={(v) => setValue(l.path, v)} divider={i > 0} onYaml={() => { setQuery(""); setMode("yaml"); }} trail />)}
              </div>
            </div>
          ) : mode === "diff" ? (
            <div style={{ padding: "24px 32px" }}>
              <div style={{ display: "flex", alignItems: "flex-start", gap: 12, marginBottom: 16 }}>
                <div style={{ display: "grid", placeItems: "center", width: 38, height: 38, borderRadius: "var(--radius-sm)", background: "var(--accent-soft)", color: "var(--accent)", flexShrink: 0 }}><Icon name="diff" size={18} /></div>
                <div>
                  <h2 style={{ margin: 0, fontSize: 17, fontWeight: 650, letterSpacing: "-0.01em", color: "var(--text)" }}>Ausstehende Änderungen</h2>
                  <p style={{ margin: "2px 0 0", fontSize: 12.5, color: "var(--text-faint)" }}>Was „Übernehmen" jetzt auf den Cluster bringen würde — gegen den zuletzt übernommenen Stand.</p>
                </div>
              </div>
              <div style={{ background: "var(--surface)", border: "1px solid var(--border)", borderRadius: "var(--radius)", overflow: "hidden" }}>
                {diffLoading && !diffData ? (
                  <div style={{ display: "flex", alignItems: "center", gap: 8, padding: 18, fontSize: 13, color: "var(--text-faint)" }}><Spinner size={14} /> Lade Diff…</div>
                ) : hasDiff ? (
                  <DiffView raw={diffData!.diff} />
                ) : (
                  <EmptyState icon="check" title="Keine ausstehenden Änderungen" sub="Alles ist übernommen — es gibt nichts anzuwenden." />
                )}
              </div>
            </div>
          ) : mode === "yaml" && activeFile ? (
            <YamlPane sliceName={activeFile.replace(/\.yaml$/, "")} qc={qc} />
          ) : activeFile ? (
            <div style={{ padding: "24px 32px", display: "flex", flexDirection: "column", gap: 30 }}>
              {(fileGroups[activeFile] ?? []).map((top) => {
                const node = schema.properties?.[top];
                if (!node) return null;
                return (
                  <section key={top}>
                    <div style={{ display: "flex", alignItems: "flex-start", gap: 12, marginBottom: 16 }}>
                      <div style={{ display: "grid", placeItems: "center", width: 38, height: 38, borderRadius: "var(--radius-sm)", background: "var(--accent-soft)", color: "var(--accent)", flexShrink: 0 }}><Icon name={iconFor(activeFile)} size={18} /></div>
                      <div style={{ minWidth: 0 }}>
                        <h2 style={{ margin: 0, fontSize: 17, fontWeight: 650, letterSpacing: "-0.01em", color: "var(--text)" }}>{humanize(top)}</h2>
                        <div style={{ display: "flex", alignItems: "center", gap: 8, marginTop: 2 }}>
                          <code style={{ fontSize: 11, fontFamily: "var(--mono)", color: "var(--text-faint)" }}>{activeFile}</code>
                          {data?.comments[top] && <span style={{ fontSize: 12.5, color: "var(--text-faint)", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>· {data.comments[top]}</span>}
                        </div>
                      </div>
                    </div>
                    {fieldKind(node) === "object"
                      ? <SchemaSection node={node} path={top} comments={data?.comments ?? {}} effectiveValue={effectiveValue} setValue={setValue} onYaml={() => setMode("yaml")} />
                      : <div style={{ background: "var(--surface)", border: "1px solid var(--border)", borderRadius: "var(--radius)" }}><Field node={node} path={top} comment={data?.comments[top]} value={effectiveValue(top)} onChange={(v) => setValue(top, v)} onYaml={() => setMode("yaml")} /></div>}
                  </section>
                );
              })}

            </div>
          ) : null}
        </main>
      </div>

      <ApplyBar files={pendingFiles} unsaved={Object.keys(changes).length} diffKey={diffData?.diff ?? ""}
        onSave={() => saveStd.mutateAsync()} onView={() => setMode("diff")} redeploy={redeploy} />
    </div>
  );
}

interface SchemaCheck { valid: boolean; errors: Array<{ field: string; message: string }> | null }

function YamlPane({ sliceName, qc }: { sliceName: string; qc: ReturnType<typeof useQueryClient> }) {
  const [content, setContent] = useState<string | null>(null);
  const [dirty, setDirty] = useState(false);
  const [syntax, setSyntax] = useState<{ line: number; message: string } | null>(null);
  const [check, setCheck] = useState<SchemaCheck | null>(null);
  const editorRef = useRef<Parameters<OnMount>[0] | null>(null);
  const monacoRef = useRef<Parameters<OnMount>[1] | null>(null);

  const { data: slice, isLoading } = useQuery({
    queryKey: ["config", "slice", sliceName],
    queryFn: () => api.get<Slice>(`/api/v1/config/slices/${sliceName}`),
  });
  const value = content ?? slice?.content ?? "";

  // Syntax is checked as you type, and the error is marked on its line — taken from the
  // editor at /config/$slice, which had this all along and was linked from nowhere.
  useEffect(() => {
    const t = window.setTimeout(() => {
      let err: { line: number; message: string } | null = null;
      try { jsYaml.load(value); } catch (e: unknown) {
        const mark = (e as { mark?: { line?: number } }).mark;
        const reason = (e as { reason?: string }).reason ?? "ungültiges YAML";
        err = { line: (mark?.line ?? 0) + 1, message: reason };
      }
      setSyntax(err);
      const model = editorRef.current?.getModel();
      const m = monacoRef.current;
      if (model && m) {
        m.editor.setModelMarkers(model, "yaml-syntax", err ? [{
          severity: m.MarkerSeverity.Error, message: err.message,
          startLineNumber: err.line, endLineNumber: err.line, startColumn: 1, endColumn: 200,
        }] : []);
      }
    }, 300);
    return () => window.clearTimeout(t);
  }, [value]);

  const save = useMutation({
    mutationFn: (c: string) => api.put(`/api/v1/config/slices/${sliceName}`, { content: c }),
    onSuccess: async () => {
      setDirty(false);
      qc.invalidateQueries({ queryKey: ["config"] });
      // Then against the schema of the chart that is running — which, since etappe
      // 107, is the one this check actually uses.
      try { setCheck(await api.post<SchemaCheck>("/api/v1/config/validate-merged", {})); } catch { setCheck(null); }
    },
  });

  return (
    <div style={{ display: "flex", flexDirection: "column", height: "100%" }}>
      <div style={{ display: "flex", alignItems: "center", gap: 8, padding: "8px 16px", borderBottom: "1px solid var(--border)", flexShrink: 0, background: "var(--panel)" }}>
        <Icon name="file" size={14} style={{ color: "var(--text-faint)" }} />
        <span style={{ fontFamily: "var(--mono)", fontSize: 12, color: "var(--text-dim)" }}>{slice?.file ?? `${sliceName}.yaml`}</span>
        {dirty && <span style={{ fontSize: 10.5, color: "var(--status-warn)" }}>ungespeichert</span>}
        {syntax
          ? <span style={{ fontSize: 11.5, color: "var(--status-err)" }}>Zeile {syntax.line}: {syntax.message}</span>
          : dirty && <span style={{ fontSize: 11.5, color: "var(--status-ok)" }}>gültiges YAML</span>}
        <div style={{ flex: 1 }} />
        <Button variant="primary" size="sm" icon={save.isPending ? undefined : "download"} disabled={!dirty || save.isPending || !!syntax} onClick={() => save.mutate(value)}>
          {save.isPending ? <Spinner size={12} /> : "Speichern"}
        </Button>
      </div>
      {check && !dirty && (
        <div style={{ padding: "8px 16px", borderBottom: "1px solid var(--border)", fontSize: 12, background: "var(--panel)",
          color: check.valid ? "var(--status-ok)" : "var(--status-err)" }}>
          {check.valid
            ? "Gespeichert und gegen das Schema des laufenden Charts geprüft — passt."
            : <>Gespeichert, aber das Schema widerspricht:{" "}
                {(check.errors ?? []).slice(0, 5).map((e, i) => <div key={i} style={{ fontFamily: "var(--mono)", marginTop: 2 }}>{e.field}: {e.message}</div>)}</>}
        </div>
      )}
      <div style={{ flex: 1 }}>
        {isLoading ? (
          <div style={{ display: "flex", alignItems: "center", gap: 8, padding: 16, fontSize: 13, color: "var(--text-faint)" }}><Spinner size={14} /> Lade…</div>
        ) : (
          <YamlEditor height="100%" defaultLanguage="yaml" value={value} theme="vs-dark"
            onMount={(editor, monaco) => { editorRef.current = editor; monacoRef.current = monaco; }}
            onChange={(v) => { setContent(v ?? ""); setDirty(true); setCheck(null); }}
            options={{ fontSize: 13, minimap: { enabled: false }, scrollBeyondLastLine: false, wordWrap: "on", tabSize: 2, automaticLayout: true }} />
        )}
      </div>
    </div>
  );
}

interface GroupProps {
  node: JSONSchema; path: string; comments: Record<string, string>;
  effectiveValue: (p: string) => unknown; setValue: (p: string, v: unknown) => void; depth?: number;
  onYaml?: () => void;
}

function SchemaSection(props: GroupProps) {
  const { node, path, comments, depth = 0 } = props;
  if (!node.properties) return null;
  const ordered = orderKeys(Object.keys(node.properties)).map((k) => [k, node.properties![k]] as const);
  // An open map whose entries the form can render (memory and CPU under resources, or
  // keys that already have a value) is a group of fields, not a "nur via YAML" dead end.
  const entriesOf = (key: string, child: JSONSchema) => mapEntries(child, `${path}.${key}`, props.effectiveValue(`${path}.${key}`));
  const isMap = (key: string, child: JSONSchema) => fieldKind(child) === "freeform" && entriesOf(key, child).length > 0;
  const leaves = ordered.filter(([k, c]) => fieldKind(c) !== "object" && !isMap(k, c));
  const maps = ordered.filter(([k, c]) => isMap(k, c));
  const groups = ordered.filter(([, c]) => fieldKind(c) === "object");

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      {leaves.length > 0 && (
        <div style={{ background: "var(--surface)", border: "1px solid var(--border)", borderRadius: "var(--radius)", boxShadow: "var(--shadow)" }}>
          {leaves.map(([key, child], i) => {
            const childPath = `${path}.${key}`;
            return <Field key={childPath} node={child} path={childPath} comment={comments[childPath]} value={props.effectiveValue(childPath)} onChange={(v) => props.setValue(childPath, v)} divider={i > 0} onYaml={props.onYaml} />;
          })}
        </div>
      )}
      {maps.map(([key, child]) => {
        const childPath = `${path}.${key}`;
        const entries = entriesOf(key, child);
        return (
          <CollapsibleCard key={childPath} title={humanize(key)} comment={comments[childPath]} count={String(entries.length)} depth={depth}>
            <div style={{ background: "var(--surface)", border: "1px solid var(--border)", borderRadius: "var(--radius)" }}>
              {entries.map((e, i) => (
                <Field key={e.path} node={e.node} path={e.path} comment={comments[e.path]} value={props.effectiveValue(e.path)}
                  onChange={(v) => props.setValue(e.path, v)} divider={i > 0} onYaml={props.onYaml} />
              ))}
            </div>
          </CollapsibleCard>
        );
      })}
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
    <div style={{ background: "var(--surface)", border: `1px solid ${open ? "color-mix(in oklch, var(--accent) 30%, var(--border))" : "var(--border)"}`, borderRadius: "var(--radius)", boxShadow: "var(--shadow)", overflow: "hidden" }}>
      <button onClick={() => setOpen((o) => !o)} style={{ width: "100%", display: "flex", alignItems: "center", gap: 10, padding: "12px 16px", background: "transparent", border: "none", cursor: "pointer", textAlign: "left" }}>
        <div style={{ display: "grid", placeItems: "center", width: 20, height: 20, borderRadius: 6, flexShrink: 0, background: open ? "var(--accent-soft)" : "var(--surface-2)", color: open ? "var(--accent)" : "var(--text-faint)" }}>
          <Icon name="chevRight" size={13} style={{ transform: open ? "rotate(90deg)" : "none", transition: "transform .15s" }} />
        </div>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ fontSize: 13.5, fontWeight: 600, color: "var(--text)" }}>{title}</div>
          {comment && <div style={{ fontSize: 12, color: "var(--text-faint)", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>{comment}</div>}
        </div>
        <span style={{ fontSize: 10, fontFamily: "var(--mono)", color: "var(--text-faint)", flexShrink: 0, background: "var(--surface-2)", padding: "2px 6px", borderRadius: 5 }}>{count}</span>
      </button>
      {open && <div style={{ padding: "14px 14px 14px 28px", display: "flex", flexDirection: "column", gap: 12, borderTop: "1px solid var(--border-soft)", background: "var(--panel)" }}>{children}</div>}
    </div>
  );
}

interface FieldProps { node: JSONSchema; path: string; comment?: string; value: unknown; onChange: (v: unknown) => void; divider?: boolean; onYaml?: () => void; trail?: boolean }
function Field({ node, path, comment, value, onChange, divider, onYaml, trail }: FieldProps) {
  const kind = fieldKind(node);
  const key = path.split(".").pop() ?? path;
  const inputStyle: React.CSSProperties = { padding: "7px 10px", border: "1px solid var(--border)", background: "var(--surface-2)", color: "var(--text)", borderRadius: "var(--radius-sm)", fontSize: 13, fontFamily: "var(--font)" };

  return (
    <div className="mc-row" style={{ display: "flex", alignItems: "flex-start", gap: 16, padding: "11px 14px", borderTop: divider ? "1px solid var(--border-soft)" : "none" }}>
      <div style={{ flex: 1, minWidth: 0 }}>
        {/* In search results 78 rows were all called "Memory"; the trail says whose. */}
        {trail && path.includes(".") && (
          <div style={{ fontSize: 11, color: "var(--text-faint)", marginBottom: 1 }}>
            {path.split(".").slice(0, -1).map(humanize).join(" › ")}
          </div>
        )}
        <span style={{ fontSize: 13, fontWeight: 600, color: "var(--text)" }}>{humanize(key)}</span>
        {comment && <HelpText text={comment} />}
        <code style={{ fontSize: 10, fontFamily: "var(--mono)", color: "var(--text-faint)", opacity: 0.7 }}>{path}</code>
      </div>
      <div style={{ flexShrink: 0, paddingTop: 2 }}>
        {kind === "boolean" ? (
          <Toggle checked={!!value} onChange={(v) => onChange(v)} />
        ) : kind === "enum" ? (
          <select value={(value as string) ?? ""} onChange={(e) => onChange(e.target.value || undefined)} style={inputStyle}>
            <option value="">— nicht gesetzt —</option>
            {node.enum?.map((o) => <option key={String(o)} value={String(o)}>{String(o)}</option>)}
          </select>
        ) : kind === "number" || kind === "integer" ? (
          <input type="number" value={value === undefined || value === null ? "" : String(value)} onChange={(e) => onChange(e.target.value === "" ? undefined : Number(e.target.value))} style={{ ...inputStyle, width: 160 }} />
        ) : kind === "string" ? (
          <input type="text" value={value === undefined || value === null ? "" : String(value)} onChange={(e) => onChange(e.target.value === "" ? undefined : e.target.value)} style={{ ...inputStyle, width: 280 }} />
        ) : (
          // Not a dead end any more: one click to the place where it can be edited.
          onYaml
            ? <Button variant="ghost" size="sm" icon="file" onClick={onYaml}>Im YAML bearbeiten</Button>
            : <span style={{ fontSize: 10, color: "var(--text-faint)", fontStyle: "italic" }}>nur via YAML</span>
        )}
      </div>
    </div>
  );
}

/** Help text with its examples shown as examples. The backend keeps YAML samples on their
 *  own indented lines (etappe 107); consecutive lines of that shape are set in the code
 *  font, the rest reads as prose. */
function HelpText({ text }: { text: string }) {
  const isCode = (l: string) => /^\s/.test(l) || /^[\w.\-"'<>]+:( \S+)?$/.test(l) || /^- /.test(l);
  const blocks: { code: boolean; lines: string[] }[] = [];
  for (const line of text.split("\n")) {
    const code = isCode(line);
    const last = blocks[blocks.length - 1];
    if (last && last.code === code) last.lines.push(line);
    else blocks.push({ code, lines: [line] });
  }
  return (
    <div style={{ margin: "2px 0 0", fontSize: 12, color: "var(--text-faint)", lineHeight: 1.5 }}>
      {blocks.map((b, i) => b.code
        ? <pre key={i} style={{ margin: "4px 0", padding: "6px 9px", fontFamily: "var(--mono)", fontSize: 11.5, background: "var(--surface-2)",
            border: "1px solid var(--border-soft)", borderRadius: "var(--radius-sm)", whiteSpace: "pre", overflowX: "auto" }}>{b.lines.join("\n")}</pre>
        : b.lines.map((l, j) => <p key={`${i}-${j}`} style={{ margin: 0 }}>{l}</p>))}
    </div>
  );
}
