import { useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { ConfigVerdict } from "@/lib/apply";
import { Badge, Button, Card, Icon, Spinner, Toggle, type IconName } from "@/components/mc";

// The task layer: what someone running a homeserver actually looks for, in German, with
// what applying it does (etappe 113). Cards and fields come from the API
// (internal/tasks); this renders them. Every change goes into the working tree like any
// other edit — the pending-changes bar at the foot applies it.

interface Field {
  id: string; label: string; help?: string;
  kind: "bool" | "text" | "quantity" | "size" | "duration" | "port" | "choice" | "allowlist" | "label";
  default?: unknown; restarts?: string; warn?: string; locked?: string; group?: string;
  options?: { value: string; label: string }[];
}
interface CardDef { id: string; title: string; sub: string; icon: IconName; fields: Field[] }
interface Value { value: unknown; is_default: boolean; set_elsewhere?: string }
interface TasksResponse { cards: CardDef[]; values: Record<string, Value> }

export function TaskView({ diffKey }: { diffKey: string }) {
  const qc = useQueryClient();
  const { data, isLoading, error } = useQuery({
    queryKey: ["config", "tasks"],
    queryFn: () => api.get<TasksResponse>("/api/v1/config/tasks"),
  });
  // Same key as the pending-changes bar: one render, two readers.
  const { data: verdict } = useQuery({
    queryKey: ["config", "preview", diffKey],
    queryFn: () => api.post<ConfigVerdict>("/api/v1/config/preview", {}),
    staleTime: 60_000,
    refetchOnWindowFocus: false,
  });
  const save = useMutation({
    mutationFn: (changes: Record<string, unknown>) => api.post("/api/v1/config/tasks", { changes }),
    onSettled: () => qc.invalidateQueries({ queryKey: ["config"] }),
  });

  if (isLoading) return <div style={{ display: "flex", gap: 8, padding: 24, fontSize: 13, color: "var(--text-faint)" }}><Spinner size={14} /> Lade…</div>;
  if (error || !data) return <div style={{ padding: 24, fontSize: 13, color: "var(--status-err)" }}>{(error as Error)?.message ?? "Keine Daten"}</div>;

  const set = (id: string, v: unknown) => save.mutate({ [id]: v });

  return (
    <div style={{ padding: "24px 32px", display: "flex", flexDirection: "column", gap: 20, maxWidth: 980 }}>
      <p style={{ margin: 0, fontSize: 13, color: "var(--text-dim)", lineHeight: 1.6 }}>
        Die wichtigsten Einstellungen, in Worten. Änderungen werden erst mit <strong>Übernehmen</strong> unten wirksam — dort steht vorher, was dabei neu startet. Alles andere findest du unter <em>Alle Einstellungen</em>.
      </p>
      {save.isError && <div style={{ fontSize: 12.5, color: "var(--status-err)" }}>{(save.error as Error).message}</div>}
      {data.cards.map((c) => (
        <TaskCard key={c.id} card={c} values={data.values} onSet={set} busy={save.isPending} verdict={verdict} />
      ))}
    </div>
  );
}

function TaskCard({ card, values, onSet, busy, verdict }: {
  card: CardDef; values: Record<string, Value>; onSet: (id: string, v: unknown) => void; busy: boolean; verdict?: ConfigVerdict;
}) {
  const navigate = useNavigate();
  const plain = card.fields.filter((f) => f.kind !== "quantity");
  const groups = [...new Set(card.fields.filter((f) => f.kind === "quantity").map((f) => f.group ?? ""))];
  return (
    <Card style={{ display: "flex", flexDirection: "column", gap: 4, padding: 0, overflow: "hidden" }}>
      <div style={{ display: "flex", alignItems: "center", gap: 12, padding: "16px 18px 10px" }}>
        <div style={{ display: "grid", placeItems: "center", width: 34, height: 34, borderRadius: "var(--radius-sm)", background: "var(--accent-soft)", color: "var(--accent)" }}><Icon name={card.icon} size={17} /></div>
        <div style={{ flex: 1 }}>
          <div style={{ fontSize: 15, fontWeight: 650, color: "var(--text)" }}>{card.title}</div>
          <div style={{ fontSize: 12.5, color: "var(--text-faint)" }}>{card.sub}</div>
        </div>
        {card.id === "registration" && (
          <Button variant="outline" size="sm" icon="key" onClick={() => navigate({ to: "/login-providers" })}>Google, GitHub, Zitadel …</Button>
        )}
      </div>
      {plain.map((f) => <FieldRow key={f.id} f={f} v={values[f.id]} onSet={onSet} busy={busy} />)}
      {groups.length > 0 && <Resources fields={card.fields} groups={groups} values={values} onSet={onSet} busy={busy} verdict={verdict} />}
    </Card>
  );
}

function FieldRow({ f, v, onSet, busy }: { f: Field; v?: Value; onSet: (id: string, v: unknown) => void; busy: boolean }) {
  const value = v?.value;
  const readOnly = !!f.locked || !!v?.set_elsewhere;
  // Only fields with a default can be "changed" or reset. An address has none: taking it
  // away would leave the service unreachable, so it offers neither the warning nor the
  // reset — it is simply edited.
  // An allow-list's default is "everyone" — no value at all.
  const hasDefault = f.default !== undefined || f.kind === "allowlist";
  const changed = hasDefault && !v?.is_default && value !== f.default;
  return (
    <div style={{ display: "flex", gap: 16, alignItems: "flex-start", padding: "12px 18px", borderTop: "1px solid var(--border-soft)" }}>
      <div style={{ flex: 1, minWidth: 0, display: "flex", flexDirection: "column", gap: 3 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
          <span style={{ fontSize: 13.5, fontWeight: 600, color: "var(--text)" }}>{f.label}</span>
          {v?.is_default && !readOnly && <Badge tone="neutral" size="sm">Standard</Badge>}
          {f.locked && <Badge tone="neutral" size="sm" icon="lock">fest</Badge>}
        </div>
        {f.help && <span style={{ fontSize: 12.5, color: "var(--text-dim)", lineHeight: 1.5 }}>{f.help}</span>}
        {f.locked && <span style={{ fontSize: 12, color: "var(--text-faint)" }}>{f.locked}</span>}
        {v?.set_elsewhere && (
          <span style={{ fontSize: 12, color: "var(--status-warn)" }}>
            Wird im eigenen Block <code style={{ fontFamily: "var(--mono)" }}>{v.set_elsewhere}</code> gesetzt — dort ändern (Alle Einstellungen → YAML).
          </span>
        )}
        {changed && f.warn && <span style={{ fontSize: 12, color: "var(--status-warn)" }}><Icon name="alert" size={12} /> {f.warn}</span>}
        {!readOnly && f.restarts && <span style={{ fontSize: 11.5, color: "var(--text-faint)" }}>Beim Übernehmen: {f.restarts}</span>}
        {hasDefault && !v?.is_default && !readOnly && (
          <button type="button" disabled={busy} onClick={() => onSet(f.id, null)}
            style={{ alignSelf: "flex-start", padding: 0, border: "none", background: "none", color: "var(--accent)", fontSize: 12, cursor: "pointer" }}>
            Zurück auf Standard
          </button>
        )}
      </div>
      <div style={{ flexShrink: 0, paddingTop: 2 }}>
        <FieldControl f={f} value={value} readOnly={readOnly} busy={busy} onSet={onSet} />
      </div>
    </div>
  );
}

function FieldControl({ f, value, readOnly, busy, onSet }: {
  f: Field; value: unknown; readOnly: boolean; busy: boolean; onSet: (id: string, v: unknown) => void;
}) {
  const clear = (nv: string) => onSet(f.id, nv === "" ? null : nv);
  switch (f.kind) {
    case "bool":
      return (
        <span style={{ opacity: readOnly ? 0.5 : 1, pointerEvents: readOnly || busy ? "none" : "auto" }}>
          <Toggle checked={value === true} onChange={(nv) => onSet(f.id, nv)} />
        </span>
      );
    case "choice":
      return (
        <select value={String(value ?? "")} disabled={readOnly || busy} onChange={(e) => onSet(f.id, e.target.value)}
          style={{ width: 180, padding: "7px 10px", fontSize: 13, background: "var(--surface-2)", border: "1px solid var(--border)", color: "var(--text)", borderRadius: "var(--radius-sm)", fontFamily: "var(--font)" }}>
          {f.options?.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
        </select>
      );
    case "port":
      return <TextInput value={value == null ? "" : String(value)} readOnly={readOnly} busy={busy} width={110}
        onCommit={(nv) => onSet(f.id, nv === "" ? null : Number(nv))} />;
    case "size":
      return <TextInput value={(value as string) ?? ""} readOnly={readOnly} busy={busy} width={110} placeholder="z. B. 100M" onCommit={clear} />;
    case "duration":
      return <TextInput value={(value as string) ?? ""} readOnly={readOnly} busy={busy} width={110} placeholder="unbegrenzt" onCommit={clear} />;
    case "allowlist":
      return <AllowList value={value as string[] | null | undefined} readOnly={readOnly} busy={busy} onSet={(v) => onSet(f.id, v)} />;
    default:
      return <TextInput value={(value as string) ?? ""} readOnly={readOnly} busy={busy} width={260} mono={f.kind !== "label"} onCommit={clear} />;
  }
}

/** Everyone (no value) · no one (empty list) · only these servers. */
function AllowList({ value, readOnly, busy, onSet }: {
  value: string[] | null | undefined; readOnly: boolean; busy: boolean; onSet: (v: string[] | null) => void;
}) {
  const mode = value == null ? "all" : value.length === 0 ? "none" : "some";
  const [draft, setDraft] = useState<string | null>(null);
  const text = draft ?? (value ?? []).join("\n");
  const parse = (t: string) => t.split(/[\s,]+/).map((x) => x.trim().toLowerCase()).filter(Boolean);
  const radio = (m: string, label: string, v: string[] | null) => (
    <label style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 13, color: "var(--text)", cursor: readOnly ? "default" : "pointer" }}>
      <input type="radio" checked={mode === m} disabled={readOnly || busy} onChange={() => onSet(v)} /> {label}
    </label>
  );
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 6, width: 260 }}>
      {radio("all", "Alle Server", null)}
      {radio("none", "Keine — nur dieser Server", [])}
      {radio("some", "Nur diese Server", mode === "some" ? value! : ["matrix.org"])}
      {mode === "some" && (
        <textarea value={text} disabled={readOnly || busy} rows={3} onChange={(e) => setDraft(e.target.value)}
          onBlur={() => { if (draft !== null) { const l = parse(draft); setDraft(null); if (l.length) onSet(l); } }}
          style={{ padding: "7px 10px", fontSize: 12.5, fontFamily: "var(--mono)", background: "var(--surface-2)", border: "1px solid var(--border)", color: "var(--text)", borderRadius: "var(--radius-sm)" }} />
      )}
    </div>
  );
}

/** Saved on Enter or leaving the field — not on every keystroke, which would write a
 *  half-typed address into the settings. */
function TextInput({ value, readOnly, busy, width, placeholder, mono = true, onCommit }: {
  value: string; readOnly?: boolean; busy?: boolean; width: number; placeholder?: string; mono?: boolean; onCommit: (v: string) => void;
}) {
  const [draft, setDraft] = useState<string | null>(null);
  const shown = draft ?? value;
  const commit = () => {
    if (draft !== null && draft.trim() !== value) onCommit(draft.trim());
    setDraft(null);
  };
  return (
    <input value={shown} readOnly={readOnly} disabled={busy} placeholder={placeholder}
      onChange={(e) => setDraft(e.target.value)} onBlur={commit}
      onKeyDown={(e) => { if (e.key === "Enter") (e.target as HTMLInputElement).blur(); if (e.key === "Escape") setDraft(null); }}
      style={{ width, padding: "7px 10px", fontSize: 13, fontFamily: mono ? "var(--mono)" : "var(--font)", background: readOnly ? "transparent" : "var(--surface-2)",
        border: `1px solid ${readOnly ? "var(--border-soft)" : "var(--border)"}`, color: "var(--text)", borderRadius: "var(--radius-sm)" }} />
  );
}

/** Memory and CPU per service, with what everything reserves after the pending changes
 *  against the node — the same arithmetic as the capacity refusal (capacity.Requests). */
function Resources({ fields, groups, values, onSet, busy, verdict }: {
  fields: Field[]; groups: string[]; values: Record<string, Value>; onSet: (id: string, v: unknown) => void; busy: boolean; verdict?: ConfigVerdict;
}) {
  const cols = ["Speicher reserviert", "Speicher höchstens", "CPU reserviert", "CPU höchstens"];
  const reqs = verdict?.requests ?? [];
  const node = verdict?.node;
  const memSum = reqs.reduce((n, r) => n + r.mem_mi, 0);
  const cpuSum = reqs.reduce((n, r) => n + r.cpu_millis, 0);
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 14, padding: "12px 18px 18px", borderTop: "1px solid var(--border-soft)" }}>
      {node ? (
        <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(260px, 1fr))", gap: 14 }}>
          <Bar label="Speicher, alle Dienste zusammen" used={memSum} total={node.mem_mi} fmt={(n) => `${(n / 1024).toFixed(1)} Gi`} />
          <Bar label="CPU, alle Dienste zusammen" used={cpuSum} total={node.cpu_millis} fmt={(n) => `${(n / 1000).toFixed(2)} Kerne`} />
        </div>
      ) : (
        <span style={{ fontSize: 12, color: "var(--text-faint)" }}>Die Kapazität lässt sich gerade nicht berechnen{verdict?.note ? ` — ${verdict.note}` : ""}.</span>
      )}
      <span style={{ fontSize: 11.5, color: "var(--text-faint)" }}>
        „Reserviert" ist, was der Dienst sicher bekommt — die Summe muss auf den Server passen. „Höchstens" ist die Obergrenze; wer sie beim Speicher überschreitet, wird neu gestartet. Leer = Standard des Charts.
      </span>
      <div style={{ display: "grid", gridTemplateColumns: `minmax(170px, 1.2fr) repeat(4, minmax(110px, 1fr))`, gap: "8px 10px", alignItems: "center" }}>
        <span />
        {cols.map((c) => <span key={c} style={{ fontSize: 11.5, color: "var(--text-faint)", fontWeight: 600 }}>{c}</span>)}
        {groups.map((g) => {
          const row = fields.filter((f) => f.group === g);
          return [
            <span key={g} style={{ fontSize: 13, color: "var(--text)", fontWeight: 550 }}>{g}</span>,
            ...row.map((f) => (
              <TextInput key={f.id} value={(values[f.id]?.value as string) ?? ""} busy={busy} width={120} placeholder="Standard"
                onCommit={(nv) => onSet(f.id, nv === "" ? null : nv)} />
            )),
          ];
        })}
      </div>
    </div>
  );
}

function Bar({ label, used, total, fmt }: { label: string; used: number; total: number; fmt: (n: number) => string }) {
  const pct = total > 0 ? Math.min(100, Math.round((used / total) * 100)) : 0;
  const tone = pct > 95 ? "var(--status-err)" : pct > 80 ? "var(--status-warn)" : "var(--status-ok)";
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
      <div style={{ display: "flex", justifyContent: "space-between", fontSize: 12, color: "var(--text-dim)" }}>
        <span>{label}</span>
        <span style={{ fontFamily: "var(--mono)" }}>{fmt(used)} / {fmt(total)}</span>
      </div>
      <div style={{ height: 7, borderRadius: 999, background: "var(--surface-2)", overflow: "hidden" }}>
        <div style={{ width: `${pct}%`, height: "100%", background: tone, borderRadius: 999 }} />
      </div>
      <span style={{ fontSize: 11, color: "var(--text-faint)" }}>nach den ausstehenden Änderungen · {pct} % des Servers reserviert</span>
    </div>
  );
}
