import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Badge, Button, Card, Icon, Spinner } from "@/components/mc";

// Updating MatrixCtrl from its own panel (etappe 116).
//
// The update runs in a Job beside the panel; the panel itself goes away for a minute
// (its Deployment is Recreate) and comes back as the new version. So this dialog has to
// survive its own server disappearing: it asks until the new version answers, then
// reloads — the JavaScript in this tab belongs to the old one.

interface Job { name: string; version: string; state: "running" | "succeeded" | "failed"; log?: string }
interface Status { current: string; ready: boolean; missing?: string[]; job?: Job }
/** Mirrors internal/updatecheck.Result. */
interface UpdateResult { current: string; latest?: string; available: boolean; checked_at?: string; error?: string }
export interface VersionInfo { version: string; commit: string; update?: UpdateResult; may_write: boolean }

/** The documented upgrade path, verbatim. A command shown to be run must be one that
 *  can be pasted — a README line with a "…" in it cost an operator an evening (§4.76). */
export const UPDATE_COMMAND = "bash <(curl -fsSL https://raw.githubusercontent.com/bxnnyg/matrixctrl/master/scripts/install.sh)";

const norm = (v?: string) => (v ?? "").replace(/^v/, "").replace(/-dirty$/, "");

export function SelfUpdateDialog({ latest, command, onClose }: { latest: string; command: string; onClose: () => void }) {
  const status = useQuery({
    queryKey: ["self-update"],
    queryFn: () => api.get<Status>("/api/v1/self-update"),
  });
  const [chosen, setPhase] = useState<"ask" | "running" | "done" | "failed">("ask");
  // Opened while an update is already under way — from another tab, or this one before
  // a reload: follow it rather than offer to start a second one the server refuses.
  const phase = chosen === "ask" && status.data?.job?.state === "running" ? "running" : chosen;
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

function since(iso?: string): string {
  if (!iso) return "noch nie";
  const secs = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 1000));
  if (!Number.isFinite(secs)) return "unbekannt";
  if (secs < 60) return "gerade eben";
  const mins = Math.round(secs / 60);
  if (mins < 60) return `vor ${mins} Min.`;
  return `vor ${Math.round(mins / 60)} Std.`;
}

/** MatrixCtrl's own version, always on the updates page — not only when there is news.
 *
 *  Etappe 116 built the one-click update and put it behind a pill in the sidebar
 *  footer that appears only once an update is known, which the check learned at most
 *  every six hours. An operator who had just been told "updates go from the panel now"
 *  found nothing that said so, and no way to ask (etappe 116c). */
export function MatrixCtrlUpdateCard() {
  const qc = useQueryClient();
  const version = useQuery({
    queryKey: ["version"],
    queryFn: () => api.get<VersionInfo>("/api/v1/version"),
    staleTime: 10 * 60_000,
  });
  const self = useQuery({
    queryKey: ["self-update"],
    queryFn: () => api.get<Status>("/api/v1/self-update"),
  });
  // Writes into the same cache entry the sidebar reads, so its pill agrees at once.
  const recheck = useMutation({
    mutationFn: () => api.get<VersionInfo>("/api/v1/version?refresh=1"),
    onSuccess: (v) => qc.setQueryData(["version"], v),
  });
  const [open, setOpen] = useState(false);

  const v = version.data;
  const u = v?.update;
  const s = self.data;
  const latest = u?.latest ? norm(u.latest) : "";
  const running = s?.job?.state === "running";
  const mayWrite = v?.may_write !== false;

  let line: React.ReactNode;
  if (running) {
    line = <>Ein Update auf {norm(s!.job!.version)} läuft gerade.</>;
  } else if (u?.available && s?.ready) {
    line = <>Version <strong>{latest}</strong> ist da. Ein Klick installiert sie: Das Update läuft neben dem Panel,
      MatrixCtrl ist dabei etwa eine Minute weg, und startet die neue Version nicht, geht es von selbst zurück.
      Matrix läuft die ganze Zeit weiter.</>;
  } else if (u?.available) {
    line = <>Version <strong>{latest}</strong> ist da. Diese Installation darf sich noch nicht selbst aktualisieren —
      einmalig per Befehl auf dem Server, danach geht es hier mit einem Klick.</>;
  } else if (u && !u.error) {
    line = s && !s.ready
      ? <>Aktuell. Damit künftige Updates hier mit einem Klick gehen, einmalig per Befehl aktualisieren — die Rechte dafür kommen mit dem Update.</>
      : <>Aktuell. Kommt eine neue Version, installierst du sie hier mit einem Klick.</>;
  } else if (u?.error) {
    line = <>Ob es eine neuere Version gibt, ließ sich nicht prüfen: {u.error}</>;
  } else {
    line = <>Prüfe…</>;
  }

  return (
    <Card style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 14, flexWrap: "wrap" }}>
        <div style={{ display: "flex", alignItems: "center", gap: 14, minWidth: 0 }}>
          <div style={{ display: "grid", placeItems: "center", width: 40, height: 40, borderRadius: "var(--radius-sm)", background: "var(--accent-soft)", color: "var(--accent)", flexShrink: 0 }}><Icon name="upload" size={19} /></div>
          <div style={{ minWidth: 0 }}>
            <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
              <span style={{ fontSize: 15, fontWeight: 650 }}>MatrixCtrl</span>
              {running ? <Badge tone="accent" icon="clock">Update läuft</Badge>
                : u?.available ? <Badge tone="warn" icon="upload">Update verfügbar</Badge>
                : u && !u.error ? <Badge tone="ok" icon="check">Aktuell</Badge> : null}
            </div>
            <div style={{ display: "flex", gap: 10, flexWrap: "wrap", fontSize: 12.5, color: "var(--text-faint)", marginTop: 2 }}>
              <span style={{ fontFamily: "var(--mono)" }}>installiert {v?.version ?? "…"}</span>
              {latest && <><span>·</span><span style={{ fontFamily: "var(--mono)" }}>neueste {latest}</span></>}
              <span>·</span><span>geprüft {since(u?.checked_at)}</span>
            </div>
          </div>
        </div>
        <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
          <Button variant="ghost" size="sm" icon={recheck.isPending ? undefined : "refresh"} disabled={recheck.isPending} onClick={() => recheck.mutate()}>
            {recheck.isPending ? <><Spinner size={13} /> Prüfe…</> : "Jetzt prüfen"}
          </Button>
          {mayWrite && (running || (u?.available && s?.ready)) && (
            <Button variant="primary" size="sm" icon="upload" onClick={() => setOpen(true)}>
              {running ? "Fortschritt" : `Auf ${latest} aktualisieren`}
            </Button>
          )}
        </div>
      </div>
      <p style={{ margin: 0, fontSize: 13, color: "var(--text-dim)", lineHeight: 1.6 }}>{line}</p>
      {recheck.isError && <span style={{ fontSize: 12.5, color: "var(--status-err)" }}>{(recheck.error as Error).message}</span>}
      {latest && u?.available && (
        <a href={`https://github.com/bxnnyg/matrixctrl/releases/tag/v${latest}`} target="_blank" rel="noreferrer"
          style={{ fontSize: 12.5, color: "var(--accent)", alignSelf: "flex-start" }}>Was ist neu in {latest}?</a>
      )}
      {s && !s.ready && mayWrite && (
        <pre style={{ margin: 0, padding: 11, fontSize: 11.5, fontFamily: "var(--mono)", lineHeight: 1.6, color: "var(--text)", background: "var(--bg)", border: "1px solid var(--border)", borderRadius: "var(--radius-sm)", whiteSpace: "pre-wrap", wordBreak: "break-all", userSelect: "all" }}>{UPDATE_COMMAND}</pre>
      )}
      {open && (latest || running) && (
        <SelfUpdateDialog latest={running ? s!.job!.version : latest} command={UPDATE_COMMAND} onClose={() => setOpen(false)} />
      )}
    </Card>
  );
}

