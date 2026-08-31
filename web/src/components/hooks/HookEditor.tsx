// Create / edit a hook, including its action list.
//
// Built-in hooks are protected server-side (their actions and name can't change,
// they can't be deleted) — the editor reflects that by rendering read-only for
// them while still allowing the enable toggle, which is the operator's real
// decision: does this hook run on the next deployment?
import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Icon, Badge, Button, Toggle, Spinner } from "@/components/mc";

export interface HookAction {
  type: string;
  description?: string;
  resource?: string;
  name?: string;
  namespace?: string;
  patch_type?: string;
  patch?: string;
  timeout_secs?: number;
  url?: string;
  method?: string;
  body?: string;
}
export interface Hook {
  id: string;
  name: string;
  description?: string;
  trigger: string;
  enabled: boolean;
  priority: number;
  builtin: boolean;
  actions: HookAction[];
  lastRunStatus?: string;
}

const ACTION_TYPES = [
  { id: "kubectl_patch", label: "Kubernetes-Patch", icon: "sliders" },
  { id: "wait_rollout", label: "Auf Rollout warten", icon: "clock" },
  { id: "http_request", label: "HTTP-Request", icon: "globe" },
] as const;

const TRIGGERS = [
  { id: "post-upgrade", label: "Nach Upgrade und Rollback" },
  { id: "post-rollback", label: "Nur nach Rollback" },
  { id: "manual", label: "Nur manuell" },
] as const;

const input: React.CSSProperties = {
  width: "100%", padding: "8px 11px", border: "1px solid var(--border)", background: "var(--surface-2)",
  color: "var(--text)", borderRadius: "var(--radius-sm)", fontSize: 13, fontFamily: "var(--font)",
};
const mono: React.CSSProperties = { ...input, fontFamily: "var(--mono)", fontSize: 12 };
const label: React.CSSProperties = { display: "block", fontSize: 11.5, fontWeight: 600, color: "var(--text-dim)", marginBottom: 5 };

function emptyHook(): Hook {
  return { id: "", name: "", description: "", trigger: "post-upgrade", enabled: true, priority: 100, builtin: false, actions: [] };
}

function ActionCard({ action, index, readOnly, onChange, onRemove }: {
  action: HookAction; index: number; readOnly: boolean;
  onChange: (a: HookAction) => void; onRemove: () => void;
}) {
  const set = (patch: Partial<HookAction>) => onChange({ ...action, ...patch });
  return (
    <div style={{ border: "1px solid var(--border)", borderRadius: "var(--radius-sm)", background: "var(--surface-2)", padding: 12 }}>
      <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 10 }}>
        <span style={{ display: "grid", placeItems: "center", width: 20, height: 20, borderRadius: 5, background: "var(--accent-soft)", color: "var(--accent)", fontSize: 10.5, fontWeight: 700, fontFamily: "var(--mono)" }}>{index + 1}</span>
        {readOnly ? (
          <code style={{ fontFamily: "var(--mono)", fontSize: 12, color: "var(--accent)" }}>{action.type}</code>
        ) : (
          <select value={action.type} onChange={(e) => set({ type: e.target.value })} style={{ ...input, width: "auto", padding: "5px 9px", fontSize: 12 }}>
            {ACTION_TYPES.map((t) => <option key={t.id} value={t.id}>{t.label}</option>)}
          </select>
        )}
        <div style={{ flex: 1 }} />
        {!readOnly && <Button variant="ghost" size="sm" icon="trash" onClick={onRemove} title="Aktion entfernen" />}
      </div>

      {readOnly ? (
        <div style={{ display: "flex", flexDirection: "column", gap: 4, fontSize: 11.5, color: "var(--text-dim)" }}>
          {action.description && <span>{action.description}</span>}
          {action.resource && <code style={{ fontFamily: "var(--mono)", color: "var(--text-faint)" }}>{action.resource}/{action.namespace}/{action.name}{action.patch_type ? ` (${action.patch_type})` : ""}</code>}
          {action.patch && <pre className="mc-scroll" style={{ margin: 0, padding: 8, background: "var(--panel)", borderRadius: 6, fontFamily: "var(--mono)", fontSize: 11, color: "var(--text-faint)", maxHeight: 100, overflow: "auto" }}>{action.patch}</pre>}
          {action.timeout_secs ? <span>Timeout: {action.timeout_secs}s</span> : null}
        </div>
      ) : (
        <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          <div><label style={label}>Beschreibung</label><input value={action.description ?? ""} onChange={(e) => set({ description: e.target.value })} style={input} placeholder="Was macht dieser Schritt?" /></div>

          {(action.type === "kubectl_patch" || action.type === "wait_rollout") && (
            <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr 1fr", gap: 8 }}>
              <div><label style={label}>Resource</label><input value={action.resource ?? ""} onChange={(e) => set({ resource: e.target.value })} style={mono} placeholder="deployment | service" /></div>
              <div><label style={label}>Namespace</label><input value={action.namespace ?? ""} onChange={(e) => set({ namespace: e.target.value })} style={mono} placeholder="ess" /></div>
              <div><label style={label}>Name</label><input value={action.name ?? ""} onChange={(e) => set({ name: e.target.value })} style={mono} placeholder="ess-matrix-rtc-sfu" /></div>
            </div>
          )}

          {action.type === "kubectl_patch" && (
            <>
              <div>
                <label style={label}>Patch-Typ</label>
                <select value={action.patch_type ?? "merge"} onChange={(e) => set({ patch_type: e.target.value })} style={{ ...input, width: "auto" }}>
                  <option value="merge">merge</option><option value="json">json</option><option value="strategic">strategic</option>
                </select>
              </div>
              <div>
                <label style={label}>Patch (JSON)</label>
                <textarea value={action.patch ?? ""} onChange={(e) => set({ patch: e.target.value })} rows={3} style={{ ...mono, resize: "vertical" }} placeholder='{"spec":{"externalTrafficPolicy":"Local"}}' />
              </div>
            </>
          )}

          {action.type === "wait_rollout" && (
            <div><label style={label}>Timeout (Sekunden)</label><input type="number" value={action.timeout_secs ?? 120} onChange={(e) => set({ timeout_secs: Number(e.target.value) })} style={{ ...mono, width: 140 }} /></div>
          )}

          {action.type === "http_request" && (
            <>
              <div style={{ display: "grid", gridTemplateColumns: "120px 1fr", gap: 8 }}>
                <div>
                  <label style={label}>Methode</label>
                  <select value={action.method ?? "POST"} onChange={(e) => set({ method: e.target.value })} style={input}>
                    <option>GET</option><option>POST</option><option>PUT</option><option>DELETE</option>
                  </select>
                </div>
                <div><label style={label}>URL</label><input value={action.url ?? ""} onChange={(e) => set({ url: e.target.value })} style={mono} placeholder="https://…" /></div>
              </div>
              <div><label style={label}>Body</label><textarea value={action.body ?? ""} onChange={(e) => set({ body: e.target.value })} rows={2} style={{ ...mono, resize: "vertical" }} /></div>
            </>
          )}
        </div>
      )}
    </div>
  );
}

export function HookEditor({ hook, onClose }: { hook: Hook | null; onClose: () => void }) {
  const qc = useQueryClient();
  const isNew = !hook;
  const [draft, setDraft] = useState<Hook>(hook ? { ...hook, actions: hook.actions.map((a) => ({ ...a })) } : emptyHook());
  const readOnly = draft.builtin;

  const invalidate = () => { qc.invalidateQueries({ queryKey: ["hooks"] }); };

  const save = useMutation({
    mutationFn: () => {
      const body = {
        name: draft.name, description: draft.description ?? "", trigger: draft.trigger,
        enabled: draft.enabled, priority: draft.priority, actions: draft.actions,
      };
      return isNew ? api.post("/api/v1/hooks", body) : api.put(`/api/v1/hooks/${draft.id}`, body);
    },
    onSuccess: () => { invalidate(); onClose(); },
  });

  const remove = useMutation({
    mutationFn: () => api.delete(`/api/v1/hooks/${draft.id}`),
    onSuccess: () => { invalidate(); onClose(); },
  });

  // Built-ins can't be edited but their enabled flag is still the operator's call.
  const toggleEnabled = useMutation({
    mutationFn: (enabled: boolean) => api.post(`/api/v1/hooks/${draft.id}/enabled`, { enabled }),
    onSuccess: () => invalidate(),
  });

  const setEnabled = (v: boolean) => {
    setDraft((d) => ({ ...d, enabled: v }));
    if (readOnly && draft.id) toggleEnabled.mutate(v);
  };

  const valid = draft.name.trim().length > 0 && (readOnly || draft.actions.length > 0);

  return (
    <>
      <div onClick={onClose} style={{ position: "fixed", inset: 0, background: "oklch(0 0 0 / 0.5)", zIndex: 70, backdropFilter: "blur(2px)" }} />
      <aside className="mc-scroll" style={{ position: "fixed", top: 0, right: 0, height: "100vh", width: "min(640px, 100vw)", background: "var(--panel)", borderLeft: "1px solid var(--border)", zIndex: 71, overflowY: "auto", boxShadow: "-16px 0 50px -16px oklch(0 0 0 / 0.6)" }}>
        <div style={{ position: "sticky", top: 0, zIndex: 2, display: "flex", alignItems: "center", gap: 12, padding: "16px 20px", background: "var(--panel)", borderBottom: "1px solid var(--border)" }}>
          <div style={{ display: "grid", placeItems: "center", width: 36, height: 36, borderRadius: "var(--radius-sm)", background: "var(--accent-soft)", color: "var(--accent)" }}><Icon name="hook" size={17} /></div>
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ fontSize: 15, fontWeight: 650, color: "var(--text)" }}>{isNew ? "Neuer Hook" : draft.name}</div>
            <div style={{ fontSize: 11.5, color: "var(--text-faint)" }}>{readOnly ? "Built-in — nur Aktivierung änderbar" : isNew ? "Eigenen Hook anlegen" : "Hook bearbeiten"}</div>
          </div>
          <button onClick={onClose} style={{ background: "transparent", border: "none", color: "var(--text-faint)", cursor: "pointer", padding: 4 }}><Icon name="x" size={18} /></button>
        </div>

        <div style={{ padding: 20, display: "flex", flexDirection: "column", gap: 16 }}>
          {/* Runs on next deployment */}
          <div style={{ display: "flex", alignItems: "center", gap: 12, padding: "12px 14px", borderRadius: "var(--radius-sm)", background: draft.enabled ? "var(--accent-soft)" : "var(--surface-2)", border: `1px solid ${draft.enabled ? "color-mix(in oklch, var(--accent) 30%, var(--border))" : "var(--border)"}` }}>
            <Icon name={draft.enabled ? "rocket" : "power"} size={17} style={{ color: draft.enabled ? "var(--accent)" : "var(--text-faint)" }} />
            <div style={{ flex: 1 }}>
              <div style={{ fontSize: 13, fontWeight: 600, color: "var(--text)" }}>Beim nächsten Deployment ausführen</div>
              <div style={{ fontSize: 11.5, color: "var(--text-faint)" }}>{draft.enabled ? "Läuft automatisch nach dem Upgrade." : "Wird übersprungen."}</div>
            </div>
            <Toggle checked={draft.enabled} onChange={setEnabled} />
          </div>

          <div><label style={label}>Name</label>
            <input value={draft.name} onChange={(e) => setDraft({ ...draft, name: e.target.value })} disabled={readOnly} style={{ ...input, opacity: readOnly ? 0.6 : 1 }} placeholder="SFU hostNetwork patchen" />
          </div>
          <div><label style={label}>Beschreibung</label>
            <textarea value={draft.description ?? ""} onChange={(e) => setDraft({ ...draft, description: e.target.value })} disabled={readOnly} rows={2} style={{ ...input, resize: "vertical", opacity: readOnly ? 0.6 : 1 }} />
          </div>
          <div style={{ display: "grid", gridTemplateColumns: "1fr 140px", gap: 12 }}>
            <div><label style={label}>Trigger</label>
              <select value={draft.trigger} onChange={(e) => setDraft({ ...draft, trigger: e.target.value })} disabled={readOnly} style={{ ...input, opacity: readOnly ? 0.6 : 1 }}>
                {TRIGGERS.map((t) => <option key={t.id} value={t.id}>{t.label}</option>)}
              </select>
            </div>
            <div><label style={label}>Priorität</label>
              <input type="number" value={draft.priority} onChange={(e) => setDraft({ ...draft, priority: Number(e.target.value) })} disabled={readOnly} style={{ ...mono, opacity: readOnly ? 0.6 : 1 }} />
            </div>
          </div>
          <p style={{ margin: "-6px 0 0", fontSize: 11, color: "var(--text-faint)" }}>Niedrigere Priorität läuft zuerst.</p>

          <div>
            <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 8 }}>
              <span style={{ fontSize: 12.5, fontWeight: 600, color: "var(--text)" }}>Aktionen</span>
              <Badge tone="neutral" size="sm">{draft.actions.length}</Badge>
              <div style={{ flex: 1 }} />
              {!readOnly && (
                <Button variant="soft" size="sm" icon="plus" onClick={() => setDraft({ ...draft, actions: [...draft.actions, { type: "kubectl_patch", namespace: "ess", patch_type: "merge" }] })}>Aktion</Button>
              )}
            </div>
            <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
              {draft.actions.map((a, i) => (
                <ActionCard key={i} action={a} index={i} readOnly={readOnly}
                  onChange={(next) => setDraft({ ...draft, actions: draft.actions.map((x, j) => (j === i ? next : x)) })}
                  onRemove={() => setDraft({ ...draft, actions: draft.actions.filter((_, j) => j !== i) })} />
              ))}
              {draft.actions.length === 0 && (
                <div style={{ padding: "18px 14px", textAlign: "center", fontSize: 12.5, color: "var(--text-faint)", border: "1px dashed var(--border)", borderRadius: "var(--radius-sm)" }}>
                  Noch keine Aktionen — füge mindestens eine hinzu.
                </div>
              )}
            </div>
          </div>

          {(save.isError || remove.isError) && (
            <div style={{ fontSize: 12, color: "var(--status-err)" }}>{((save.error || remove.error) as Error).message}</div>
          )}

          {!readOnly && (
            <div style={{ display: "flex", gap: 10, paddingTop: 4 }}>
              <Button variant="primary" icon="check" disabled={!valid || save.isPending} onClick={() => save.mutate()}>
                {save.isPending ? <Spinner size={14} /> : isNew ? "Hook anlegen" : "Speichern"}
              </Button>
              <Button variant="ghost" onClick={onClose}>Abbrechen</Button>
              <div style={{ flex: 1 }} />
              {!isNew && <Button variant="dangerGhost" icon="trash" disabled={remove.isPending} onClick={() => remove.mutate()}>Löschen</Button>}
            </div>
          )}
        </div>
      </aside>
    </>
  );
}
