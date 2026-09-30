import { useEffect, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Button, Icon, Spinner } from "@/components/mc";

// Updating MatrixCtrl from its own panel (etappe 116).
//
// The update runs in a Job beside the panel; the panel itself goes away for a minute
// (its Deployment is Recreate) and comes back as the new version. So this dialog has to
// survive its own server disappearing: it asks until the new version answers, then
// reloads — the JavaScript in this tab belongs to the old one.

interface Job { name: string; version: string; state: "running" | "succeeded" | "failed"; log?: string }
interface Status { current: string; ready: boolean; missing?: string[]; job?: Job }

const norm = (v?: string) => (v ?? "").replace(/^v/, "").replace(/-dirty$/, "");

export function SelfUpdateDialog({ latest, command, onClose }: { latest: string; command: string; onClose: () => void }) {
  const status = useQuery({
    queryKey: ["self-update"],
    queryFn: () => api.get<Status>("/api/v1/self-update"),
  });
  const [phase, setPhase] = useState<"ask" | "running" | "done" | "failed">("ask");
  const [log, setLog] = useState<string>("");
  const [elapsed, setElapsed] = useState(0);

  const start = useMutation({
    mutationFn: () => api.post<Job>("/api/v1/self-update", { version: norm(latest) }),
    onSuccess: () => setPhase("running"),
  });

  // While running: the panel is expected to vanish and return. Errors are the normal
  // state for a while, not a failure.
  useEffect(() => {
    if (phase !== "running") return;
    const t = setInterval(async () => {
      setElapsed((s) => s + 3);
      try {
        const v = await api.get<{ version: string }>("/api/v1/version");
        if (norm(v.version) === norm(latest)) {
          setPhase("done");
          setTimeout(() => window.location.reload(), 2500);
          return;
        }
      } catch { /* the old panel is gone, the new one not yet here */ }
      try {
        const s = await api.get<Status>("/api/v1/self-update");
        if (s.job?.log) setLog(s.job.log);
        if (s.job?.state === "failed" && norm(s.job.version) === norm(latest)) setPhase("failed");
      } catch { /* same */ }
    }, 3000);
    return () => clearInterval(t);
  }, [phase, latest]);

  const s = status.data;
  const busy = phase === "running" || start.isPending;

  return (
    <div style={{ position: "fixed", inset: 0, background: "oklch(0 0 0 / 0.6)", display: "flex", alignItems: "center", justifyContent: "center", zIndex: 70, padding: 16 }}
      onClick={() => { if (!busy) onClose(); }}>
      <div role="dialog" aria-modal="true" onClick={(e) => e.stopPropagation()}
        style={{ background: "var(--surface)", border: "1px solid var(--border)", borderRadius: "var(--radius-lg)", padding: 24, maxWidth: 560, width: "100%", display: "flex", flexDirection: "column", gap: 14, boxShadow: "0 24px 60px -12px oklch(0 0 0 / 0.6)" }}>
        <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
          <Icon name={phase === "failed" ? "alert" : phase === "done" ? "check" : "upload"} size={20}
            style={{ color: phase === "failed" ? "var(--status-err)" : phase === "done" ? "var(--status-ok)" : "var(--accent)" }} />
          <h2 style={{ margin: 0, fontSize: 15, fontWeight: 650, color: "var(--text)" }}>
            {phase === "done" ? `MatrixCtrl ${norm(latest)} läuft` : phase === "failed" ? "Update fehlgeschlagen" : `MatrixCtrl ${norm(latest)} ist verfügbar`}
          </h2>
        </div>

        {phase === "ask" && (
          <>
            <p style={{ margin: 0, fontSize: 13, color: "var(--text-dim)", lineHeight: 1.6 }}>
              Installiert ist <strong>{s?.current ?? "…"}</strong>. Das Update läuft neben dem Panel;
              MatrixCtrl ist dabei etwa eine Minute nicht erreichbar. Wird die neue Version nicht
              innerhalb von fünf Minuten bereit, springt sie von selbst auf die alte zurück.
              Matrix selbst läuft die ganze Zeit weiter.
            </p>
            {status.isLoading && <span style={{ fontSize: 12.5, color: "var(--text-faint)" }}><Spinner size={12} /> Prüfe die Rechte…</span>}
            {s && !s.ready && (
              <div style={{ fontSize: 12.5, color: "var(--status-warn)", lineHeight: 1.55 }}>
                <Icon name="alert" size={13} /> Diese Installation hat die Rechte für ein Update aus dem Panel noch nicht.
                Einmalig per Befehl aktualisieren — die Rechte kommen mit dieser Version, danach geht es von hier.
              </div>
            )}
            {start.isError && <div style={{ fontSize: 12.5, color: "var(--status-err)" }}>{(start.error as Error).message}</div>}
            <div style={{ display: "flex", gap: 8, justifyContent: "flex-end" }}>
              <Button variant="ghost" size="sm" onClick={onClose}>Abbrechen</Button>
              {s?.ready && (
                <Button variant="primary" size="sm" icon={start.isPending ? undefined : "upload"} disabled={start.isPending} onClick={() => start.mutate()}>
                  {start.isPending ? <Spinner size={13} /> : "Jetzt aktualisieren"}
                </Button>
              )}
            </div>
            <details style={{ fontSize: 12, color: "var(--text-faint)" }} open={s ? !s.ready : false}>
              <summary style={{ cursor: "pointer" }}>Per Befehl auf dem Server</summary>
              <pre style={{ margin: "8px 0 0", padding: 11, fontSize: 11.5, fontFamily: "var(--mono)", lineHeight: 1.6, color: "var(--text)", background: "var(--bg)", border: "1px solid var(--border)", borderRadius: "var(--radius-sm)", whiteSpace: "pre-wrap", wordBreak: "break-all", userSelect: "all" }}>{command}</pre>
            </details>
          </>
        )}

        {phase === "running" && (
          <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
            <span style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 13, color: "var(--text-dim)" }}>
              <Spinner size={14} /> Aktualisiere… ({elapsed}s) — das Panel startet dabei neu, die Seite lädt danach von selbst.
            </span>
            {log && <LogBox text={log} />}
          </div>
        )}

        {phase === "done" && (
          <span style={{ fontSize: 13, color: "var(--status-ok)" }}>Fertig. Die Seite lädt gleich neu.</span>
        )}

        {phase === "failed" && (
          <>
            <span style={{ fontSize: 13, color: "var(--text)" }}>Die vorige Version läuft weiter — das Update wurde zurückgerollt.</span>
            {log && <LogBox text={log} />}
            <div style={{ display: "flex", justifyContent: "flex-end" }}>
              <Button variant="ghost" size="sm" onClick={onClose}>Schließen</Button>
            </div>
          </>
        )}
      </div>
    </div>
  );
}

function LogBox({ text }: { text: string }) {
  return (
    <pre className="mc-scroll" style={{ margin: 0, maxHeight: 180, overflowY: "auto", padding: 10, fontSize: 11.5, fontFamily: "var(--mono)", lineHeight: 1.55, background: "oklch(0.13 0.005 256)", color: "oklch(0.82 0.13 150)", borderRadius: "var(--radius-sm)", border: "1px solid var(--border)", whiteSpace: "pre-wrap" }}>{text}</pre>
  );
}
