import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Card, Icon, Badge, Button, StatusDot, Toggle, Spinner, EmptyState } from "@/components/mc";
import { HookEditor, type Hook } from "@/components/hooks/HookEditor";

export const Route = createFileRoute("/hooks/")({
  component: HooksList,
});

const RUN_TONE: Record<string, "ok" | "err" | "warn" | "info"> = {
  success: "ok", failed: "err", partial: "warn", running: "info",
};
const TRIGGER_LABEL: Record<string, string> = {
  "post-upgrade": "Nach Upgrade", "post-rollback": "Nach Rollback", manual: "Nur manuell",
};

function HooksList() {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [triggeringId, setTriggeringId] = useState<string | null>(null);
  const [editing, setEditing] = useState<Hook | null>(null);
  const [creating, setCreating] = useState(false);

  const { data: hooks, isLoading } = useQuery({
    queryKey: ["hooks"],
    queryFn: () => api.get<Hook[]>("/api/v1/hooks"),
  });

  const trigger = useMutation({
    mutationFn: (id: string) => { setTriggeringId(id); return api.post(`/api/v1/hooks/${id}/trigger`, {}); },
    onSettled: () => { setTriggeringId(null); qc.invalidateQueries({ queryKey: ["hooks"] }); },
  });

  // Works for built-ins too — deciding what runs on the next deployment is an
  // operator call even for hooks whose actions are locked.
  const setEnabled = useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) => api.post(`/api/v1/hooks/${id}/enabled`, { enabled }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["hooks"] }),
  });

  const autoHooks = (hooks ?? []).filter((h) => h.trigger !== "manual");
  const activeCount = autoHooks.filter((h) => h.enabled).length;

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 18 }}>
      {/* Deployment summary — answers "what runs next time I deploy?" */}
      <Card style={{ display: "flex", gap: 14, alignItems: "center", flexWrap: "wrap", padding: "16px 18px" }}>
        <div style={{ display: "grid", placeItems: "center", width: 40, height: 40, borderRadius: "var(--radius-sm)", background: "var(--accent-soft)", color: "var(--accent)", flexShrink: 0 }}><Icon name="rocket" size={19} /></div>
        <div style={{ flex: 1, minWidth: 240 }}>
          <div style={{ fontSize: 13.5, fontWeight: 600, color: "var(--text)" }}>
            {activeCount} von {autoHooks.length} Hooks laufen beim nächsten Deployment
          </div>
          <div style={{ fontSize: 12, color: "var(--text-faint)", marginTop: 2 }}>
            Built-in-Hooks patchen die SFU-Services, damit WebRTC-Calling nach dem Upgrade funktioniert.
          </div>
        </div>
        <Button variant="primary" icon="plus" onClick={() => setCreating(true)}>Hook erstellen</Button>
      </Card>

      {isLoading && <div style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 13, color: "var(--text-faint)", padding: "8px 2px" }}><Spinner size={14} /> Lade Hooks…</div>}

      <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
        {hooks?.map((hook) => {
          const runTone = hook.lastRunStatus ? RUN_TONE[hook.lastRunStatus] : undefined;
          return (
            <Card key={hook.id} style={{ opacity: hook.enabled ? 1 : 0.66 }}>
              <div style={{ display: "flex", alignItems: "flex-start", gap: 16, flexWrap: "wrap" }}>
                <div style={{ display: "grid", placeItems: "center", width: 40, height: 40, borderRadius: "var(--radius-sm)", background: hook.enabled ? "var(--accent-soft)" : "var(--surface-2)", color: hook.enabled ? "var(--accent)" : "var(--text-faint)", flexShrink: 0 }}><Icon name="hook" size={18} /></div>

                <div style={{ flex: 1, minWidth: 200 }}>
                  <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap", marginBottom: 4 }}>
                    <span style={{ fontSize: 14, fontWeight: 600, color: "var(--text)" }}>{hook.name}</span>
                    {hook.builtin && <Badge tone="accent" size="sm">Built-in</Badge>}
                    {!hook.enabled && <Badge tone="neutral" size="sm">Deaktiviert</Badge>}
                    {runTone && hook.lastRunStatus && (
                      <span style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
                        <StatusDot status={runTone === "ok" ? "ok" : runTone === "info" ? "info" : runTone === "warn" ? "warn" : "err"} pulse={hook.lastRunStatus === "running"} size={7} />
                        <span style={{ fontSize: 11.5, color: `var(--status-${runTone})`, fontWeight: 500 }}>{hook.lastRunStatus === "success" ? "OK" : hook.lastRunStatus}</span>
                      </span>
                    )}
                  </div>
                  {hook.description && <p style={{ margin: 0, fontSize: 12.5, color: "var(--text-faint)", lineHeight: 1.5 }}>{hook.description}</p>}
                  <div style={{ display: "flex", gap: 12, marginTop: 7, fontSize: 11.5, color: "var(--text-faint)", flexWrap: "wrap" }}>
                    <span style={{ display: "inline-flex", alignItems: "center", gap: 4 }}><Icon name="clock" size={11} />{TRIGGER_LABEL[hook.trigger] ?? hook.trigger}</span>
                    <span>Priorität {hook.priority}</span>
                    <span>{hook.actions.length} {hook.actions.length === 1 ? "Aktion" : "Aktionen"}</span>
                  </div>
                </div>

                <div style={{ display: "flex", alignItems: "center", gap: 10, flexShrink: 0 }}>
                  <div style={{ display: "flex", flexDirection: "column", alignItems: "center", gap: 3 }}>
                    <Toggle checked={hook.enabled} onChange={(v) => setEnabled.mutate({ id: hook.id, enabled: v })} />
                    <span style={{ fontSize: 9.5, color: "var(--text-faint)", whiteSpace: "nowrap" }}>Deployment</span>
                  </div>
                  <Button variant="soft" size="sm" icon={triggeringId === hook.id ? undefined : "play"} disabled={!hook.enabled || triggeringId !== null} onClick={() => trigger.mutate(hook.id)} title="Jetzt ausführen">
                    {triggeringId === hook.id ? <Spinner size={12} /> : "Ausführen"}
                  </Button>
                  <Button variant="outline" size="sm" icon="edit" onClick={() => setEditing(hook)}>{hook.builtin ? "Ansehen" : "Bearbeiten"}</Button>
                  <Button variant="ghost" size="sm" iconRight="chevRight" onClick={() => navigate({ to: "/hooks/$id", params: { id: hook.id } })}>Verlauf</Button>
                </div>
              </div>
            </Card>
          );
        })}

        {hooks?.length === 0 && (
          <Card pad={false}>
            <EmptyState icon="hook" title="Keine Hooks konfiguriert" sub="Lege einen eigenen Hook an, um nach jedem Deployment automatisch zu patchen."
              action={<Button variant="primary" icon="plus" onClick={() => setCreating(true)}>Hook erstellen</Button>} />
          </Card>
        )}
      </div>

      {(editing || creating) && <HookEditor hook={editing} onClose={() => { setEditing(null); setCreating(false); }} />}
    </div>
  );
}
