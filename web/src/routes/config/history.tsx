import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api } from "@/lib/api";
import { Card, Icon, Badge, Button, Spinner, EmptyState , ConfirmDialog } from "@/components/mc";
import { DiffView } from "@/components/config/DiffView";

export const Route = createFileRoute("/config/history")({
  component: ConfigHistory,
});

interface CommitInfo { sha: string; message: string; author: string; time: string }

function CopyableSha({ sha }: { sha: string }) {
  const [copied, setCopied] = useState(false);
  function copy(e: React.MouseEvent) {
    e.stopPropagation();
    navigator.clipboard.writeText(sha).then(() => { setCopied(true); setTimeout(() => setCopied(false), 1500); });
  }
  return (
    <button onClick={copy} title="SHA kopieren" style={{ display: "inline-flex", alignItems: "center", gap: 4, fontFamily: "var(--mono)", color: "var(--accent)", background: "transparent", border: "none", cursor: "pointer", padding: 0, fontSize: "inherit" }}>
      {sha.slice(0, 10)}
      <Icon name={copied ? "check" : "copy"} size={11} style={{ color: copied ? "var(--status-ok)" : "var(--text-faint)" }} />
    </button>
  );
}

function CommitDiffPanel({ sha }: { sha: string }) {
  const { data, isLoading, error } = useQuery({
    queryKey: ["config", "history", sha, "diff"],
    queryFn: () => api.get<{ diff: string }>(`/api/v1/config/history/${sha}/diff`),
    staleTime: Infinity,
  });
  if (isLoading) return <div style={{ display: "flex", alignItems: "center", gap: 8, padding: "12px 16px", fontSize: 12, color: "var(--text-faint)" }}><Spinner size={13} /> Lade Diff…</div>;
  if (error) return <p style={{ padding: "12px 16px", fontSize: 12, color: "var(--status-err)" }}>{(error as Error).message}</p>;
  return <DiffView raw={data?.diff ?? ""} />;
}

function ConfigHistory() {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [expandedSha, setExpandedSha] = useState<string | null>(null);
  const [rollingBack, setRollingBack] = useState<string | null>(null);
  const [confirmSha, setConfirmSha] = useState<string | null>(null);

  const { data: commits, isLoading } = useQuery({
    queryKey: ["config", "history"],
    queryFn: () => api.get<CommitInfo[]>("/api/v1/config/history"),
  });

  const rollback = useMutation({
    mutationFn: (sha: string) => { setRollingBack(sha); return api.post(`/api/v1/config/history/${sha}/rollback`, {}); },
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["config"] }); setConfirmSha(null); setRollingBack(null); },
    onError: () => setRollingBack(null),
  });

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 18, maxWidth: 900 }}>
      <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
        <Button variant="ghost" size="sm" icon="chevLeft" onClick={() => navigate({ to: "/config" })}>Einstellungen</Button>
      </div>

      {isLoading && <div style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 13, color: "var(--text-faint)" }}><Spinner size={14} /> Lade…</div>}
      {commits?.length === 0 && <Card pad={false}><EmptyState icon="git" title="Noch keine Commits" sub="Jede gespeicherte Config-Änderung wird hier als Git-Commit festgehalten." /></Card>}

      <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
        {commits?.map((c, idx) => {
          const expanded = expandedSha === c.sha;
          const isFirst = idx === 0;
          return (
            <Card key={c.sha} pad={false} style={{ overflow: "hidden" }}>
              <div onClick={() => setExpandedSha(expanded ? null : c.sha)} className="mc-row"
                style={{ display: "flex", alignItems: "center", gap: 12, padding: "13px 16px", cursor: "pointer", userSelect: "none" }}>
                <div style={{ display: "grid", placeItems: "center", width: 32, height: 32, borderRadius: "var(--radius-sm)", background: "var(--surface-2)", color: "var(--text-faint)", flexShrink: 0 }}><Icon name="git" size={15} /></div>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <p style={{ margin: 0, fontSize: 13.5, fontWeight: 500, color: "var(--text)", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>{c.message.split("\n")[0]}</p>
                  <p style={{ margin: "2px 0 0", fontSize: 11.5, color: "var(--text-faint)", display: "flex", alignItems: "center", gap: 6, flexWrap: "wrap" }}>
                    <span onClick={(e) => e.stopPropagation()}><CopyableSha sha={c.sha} /></span>
                    <span>·</span><span>{c.author}</span>
                    <span>·</span><span>{new Date(c.time).toLocaleString("de-DE", { dateStyle: "short", timeStyle: "short" })}</span>
                  </p>
                </div>
                <div style={{ display: "flex", alignItems: "center", gap: 8, flexShrink: 0 }} onClick={(e) => e.stopPropagation()}>
                  {isFirst ? <Badge tone="ok" size="sm">aktuell</Badge>
                    : <Button variant="ghost" size="sm" icon="rotate" onClick={() => setConfirmSha(c.sha)} title="Auf diesen Stand zurücksetzen">Rollback</Button>}
                </div>
                <Icon name={expanded ? "chevDown" : "chevRight"} size={16} style={{ color: "var(--text-faint)", flexShrink: 0 }} />
              </div>
              {expanded && <div style={{ borderTop: "1px solid var(--border-soft)" }}><CommitDiffPanel sha={c.sha} /></div>}
            </Card>
          );
        })}
      </div>

      <ConfirmDialog
        open={confirmSha !== null}
        title="Rollback bestätigen"
        confirmLabel="Jetzt zurücksetzen"
        confirmIcon="rotate"
        busy={rollingBack === confirmSha}
        error={rollback.isError ? (rollback.error as Error).message : null}
        onCancel={() => setConfirmSha(null)}
        onConfirm={() => confirmSha && rollback.mutate(confirmSha)}
      >
        Der Working-Tree wird auf Commit <code style={{ fontFamily: "var(--mono)", color: "var(--accent)" }}>{confirmSha?.slice(0, 10)}</code> zurückgesetzt.
        Alle ungespeicherten Änderungen gehen verloren.
      </ConfirmDialog>
    </div>
  );
}
