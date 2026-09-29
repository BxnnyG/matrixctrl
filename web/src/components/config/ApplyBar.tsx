import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "@/lib/api";
import { useUpgradeStream, type UpgradeProgress } from "@/lib/ws";
import { type ConfigVerdict, refusal, restartSummary, serviceName } from "@/lib/apply";
import { ProgressPanel, type Outcome } from "@/components/RolloutProgress";
import { Button, ConfirmDialog, Icon, Spinner } from "@/components/mc";

// "Änderungen ausstehend" at the foot of the settings page (etappe 108).
//
// The page had one button that committed, rendered, applied and waited, and it was only
// while that ran that a log line said which services were restarting — and whether they
// still fitted, as a warning, after the commit, while the deploy went ahead anyway. The
// bar asks first: what is pending, which services it restarts, and whether it fits.
// When it does not fit, the reason is there instead of a deploy.

interface Props {
  /** Section files with uncommitted edits. */
  files: string[];
  /** Form edits not yet written to the files. "Übernehmen" saves them first. */
  unsaved: number;
  /** The diff text, so the preview is asked again whenever it changes. */
  diffKey: string;
  onSave: () => Promise<unknown>;
  onView: () => void;
  /** Bumped by the header's "Deployen" to apply the current state without edits. */
  redeploy: number;
}

type Finished = { status: string } | null;

export function ApplyBar({ files, unsaved, diffKey, onSave, onView, redeploy }: Props) {
  const qc = useQueryClient();
  const pending = files.length > 0 || unsaved > 0;

  const preview = useQuery({
    queryKey: ["config", "preview", diffKey],
    queryFn: () => api.post<ConfigVerdict>("/api/v1/config/preview", {}),
    enabled: files.length > 0,
    staleTime: 60_000,
    refetchOnWindowFocus: false,
  });
  const verdict = preview.data;
  const refused = refusal(verdict);

  const [upgradeId, setUpgradeId] = useState<string | null>(null);
  const [progress, setProgress] = useState<UpgradeProgress | null>(null);
  const [logs, setLogs] = useState<string[]>([]);
  const [showLog, setShowLog] = useState(false);
  const [outcome, setOutcome] = useState<Finished>(null);
  const [elapsed, setElapsed] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const [overrideOpen, setOverrideOpen] = useState(false);
  const [confirmDiscard, setConfirmDiscard] = useState(false);
  const [reverted, setReverted] = useState<string | null>(null);
  const [starting, setStarting] = useState(false);
  const revisionBefore = useRef<number | null>(null);
  // Read once after a failure; `rev: null` means it could not be read, and then the
  // rollback is offered as before — unknown is not "nothing changed".
  const [afterFailure, setAfterFailure] = useState<{ rev: number | null } | null>(null);
  const logRef = useRef<HTMLDivElement>(null);
  const running = !!upgradeId && !outcome;

  useEffect(() => {
    if (!running) return;
    const t = setInterval(() => setElapsed((s) => s + 1), 1000);
    return () => clearInterval(t);
  }, [running]);

  useUpgradeStream(upgradeId, {
    onLog: (line) => {
      setLogs((p) => [...p, line]);
      setTimeout(() => logRef.current?.scrollTo({ top: logRef.current.scrollHeight }), 30);
    },
    onProgress: setProgress,
    onDone: (status) => {
      if (status === "failed") {
        releaseRevision().then((rev) => setAfterFailure({ rev }));
      }
      setOutcome({ status });
      qc.invalidateQueries({ queryKey: ["config"] });
      qc.invalidateQueries({ queryKey: ["helm"] });
    },
  });

  const apply = useMutation({
    mutationFn: (override: boolean) => api.post<{ upgrade_id: string }>("/api/v1/helm/releases/ess/apply-config", {
      message: "config: Einstellungen übernommen",
      override_capacity: override,
    }),
    onSuccess: (res) => {
      setUpgradeId(res.upgrade_id); setProgress(null); setLogs([]); setOutcome(null); setElapsed(0); setReverted(null);
    },
    onError: (e) => {
      setError((e as Error).message);
      // A 409 carries the reason; the preview shows it in full.
      if (e instanceof ApiError && e.status === 409) preview.refetch();
    },
  });

  const discard = useMutation({
    mutationFn: () => api.post("/api/v1/config/discard", {}),
    onSuccess: () => { setConfirmDiscard(false); qc.invalidateQueries({ queryKey: ["config"] }); },
  });

  const revert = useMutation({
    mutationFn: () => api.post<{ cluster: string; config: string; hooks_failed?: boolean; config_error?: string }>(
      "/api/v1/config/revert-apply", { upgrade_id: upgradeId }),
    onSuccess: (r) => {
      const parts = [
        r.cluster === "unchanged" ? "Cluster war unverändert" : `Cluster auf ${r.cluster.replace("revision", "Revision")}`,
        r.config === "unchanged" ? "Einstellungen waren unverändert" : "Einstellungen auf den Stand davor",
      ];
      if (r.hooks_failed) parts.push("mindestens ein Hook danach fehlgeschlagen");
      if (r.config_error) parts.push(`Einstellungen nicht zurückgesetzt: ${r.config_error}`);
      setReverted(parts.join(" · "));
      qc.invalidateQueries({ queryKey: ["config"] });
      qc.invalidateQueries({ queryKey: ["helm"] });
    },
  });

  // "Übernehmen": save the form if needed, ask the preview, and only then deploy.
  async function start(override = false) {
    setError(null);
    setStarting(true);
    try {
      if (unsaved > 0) await onSave();
      const v = (await preview.refetch()).data;
      if (!override && refusal(v)) return; // the reason is on screen instead
      revisionBefore.current = await releaseRevision();
      setAfterFailure(null);
      apply.mutate(override);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setStarting(false);
    }
  }

  const lastRedeploy = useRef(redeploy);
  useEffect(() => {
    if (redeploy !== lastRedeploy.current) { lastRedeploy.current = redeploy; start(); }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [redeploy]);

  if (!pending && !upgradeId && !error && !starting && !apply.isPending) return null;

  const failed = outcome?.status === "failed";
  // Failed before Helm wrote a revision — a refused pre-flight, a schema error: the
  // cluster is as it was, so there is nothing to go back from. Offering it anyway would
  // revert the *settings*, which is how a correct fix nearly got undone in production
  // (etappe 109). Decided by the release revision, not the phase: the pre-flight that
  // refused runs inside "Anwenden".
  const untouched = failed && afterFailure?.rev != null && revisionBefore.current !== null && afterFailure.rev === revisionBefore.current;
  const busy = apply.isPending || preview.isFetching;

  return (
    <div style={{ flexShrink: 0, borderTop: "1px solid var(--border)", background: "var(--panel)", boxShadow: "0 -8px 24px -16px oklch(0 0 0 / 0.5)" }}>
      {!pending && !upgradeId && (starting || apply.isPending) && (
        <div style={{ display: "flex", alignItems: "center", gap: 8, padding: "12px 22px", fontSize: 13, color: "var(--text-dim)" }}><Spinner size={14} /> Prüfe und wende an…</div>
      )}
      {upgradeId && (
        <div className="mc-scroll" style={{ maxHeight: "48vh", overflowY: "auto", padding: "14px 22px 0", display: "flex", flexDirection: "column", gap: 10 }}>
          {progress ? <ProgressPanel progress={progress} elapsed={elapsed} outcome={outcome?.status as Outcome} /> : !outcome && (
            <div style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 13, color: "var(--text-dim)" }}><Spinner size={14} /> Wird übernommen…</div>
          )}

          {outcome?.status === "success" && <Line tone="ok" icon="check">Übernommen — alle Dienste laufen mit der neuen Konfiguration.</Line>}
          {outcome?.status === "hooks-failed" && (
            <Line tone="warn" icon="alert">Übernommen, aber mindestens ein Hook danach ist fehlgeschlagen — manuelle Patches fehlen womöglich. Details auf der Hooks-Seite.</Line>
          )}
          {/* Decision 3: ask, with going back preselected. */}
          {untouched && (
            <Line tone="err" icon="x">
              Übernehmen wurde abgebrochen, bevor etwas angewendet wurde — auf dem Cluster hat sich nichts geändert. Der Grund steht im Log; die Einstellungen sind gespeichert und können nach einer Korrektur erneut übernommen werden.
            </Line>
          )}
          {failed && !untouched && afterFailure !== null && !reverted && (
            <div style={{ display: "flex", flexDirection: "column", gap: 10, padding: 14, borderRadius: "var(--radius)", border: "1px solid var(--status-err)", background: "color-mix(in oklch, var(--status-err) 8%, var(--surface))" }}>
              <div style={{ display: "flex", gap: 10, alignItems: "flex-start" }}>
                <Icon name="x" size={16} style={{ color: "var(--status-err)", marginTop: 1 }} />
                <div style={{ fontSize: 13, color: "var(--text)", lineHeight: 1.55 }}>
                  <strong>Übernehmen ist fehlgeschlagen.</strong> Der Rücksprung setzt den Cluster auf den Stand vor diesem Versuch und die Einstellungen auf den Inhalt von damals — als neuer Eintrag im Verlauf, der Versuch bleibt nachvollziehbar.
                </div>
              </div>
              <div style={{ display: "flex", gap: 8 }}>
                <AutoFocusButton disabled={revert.isPending} onClick={() => revert.mutate()}>
                  {revert.isPending ? <><Spinner size={13} /> Springe zurück…</> : "Auf den letzten guten Stand zurück"}
                </AutoFocusButton>
                <Button variant="ghost" size="sm" disabled={revert.isPending} onClick={() => setUpgradeId(null)}>So lassen</Button>
              </div>
              {revert.isError && <span style={{ fontSize: 12, color: "var(--status-err)" }}>{(revert.error as Error).message}</span>}
            </div>
          )}
          {reverted && <Line tone="ok" icon="rotate">Zurückgesprungen: {reverted}.</Line>}

          <div>
            <Button variant="ghost" size="sm" icon={showLog ? "chevDown" : "chevRight"} onClick={() => setShowLog((v) => !v)}>
              {showLog ? "Log ausblenden" : `Log anzeigen (${logs.length} Zeilen)`}
            </Button>
            {showLog && (
              <div ref={logRef} className="mc-scroll" style={{ marginTop: 6, background: "oklch(0.13 0.005 256)", borderRadius: "var(--radius)", border: "1px solid var(--border)", padding: 12, fontFamily: "var(--mono)", fontSize: 12, color: "oklch(0.82 0.13 150)", maxHeight: 200, overflowY: "auto", lineHeight: 1.6 }}>
                {logs.map((line, i) => <div key={i} style={{ color: line.startsWith("ERROR") ? "var(--status-err)" : line.startsWith("WARNING") ? "var(--status-warn)" : undefined }}>{line}</div>)}
              </div>
            )}
          </div>
        </div>
      )}

      {/* The refusal, in full: which service, what it asks for, what there is. */}
      {!running && pending && refused && (
        <div style={{ padding: "12px 22px 0", display: "flex", flexDirection: "column", gap: 6 }}>
          <Line tone="err" icon="alert">{refused} Es wurde nichts gespeichert und nichts angewendet.</Line>
          {verdict?.findings.filter((f) => f.level === "blocked").map((f, i) => (
            <div key={i} style={{ fontSize: 12.5, color: "var(--text-dim)", paddingLeft: 26 }}>
              {f.workload && <code style={{ fontFamily: "var(--mono)", color: "var(--text)" }}>{serviceName(f.workload)}</code>} {f.message}
            </div>
          ))}
          {verdict?.stuck && <div style={{ fontSize: 12.5, color: "var(--text-dim)", paddingLeft: 26 }}>Zurücksetzen geht über den Rollback auf der Update-Seite.</div>}
        </div>
      )}
      {/* Warnings, not refusals: an alias may point somewhere on purpose. The move to a
          new server left one on the old cluster's Traefik, and Element Call failed until
          it was found by hand (etappe 109). */}
      {!running && pending && (verdict?.stale_aliases?.length ?? 0) > 0 && (
        <div style={{ padding: "12px 22px 0", display: "flex", flexDirection: "column", gap: 6 }}>
          {verdict!.stale_aliases!.map((a, i) => (
            <Line key={i} tone="warn" icon="alert">
              {a.message}
              {a.suggest && <> In den Einstellungen unter <code style={{ fontFamily: "var(--mono)" }}>hostAliases</code> die IP <code style={{ fontFamily: "var(--mono)" }}>{a.ip}</code> durch <code style={{ fontFamily: "var(--mono)" }}>{a.suggest}</code> ersetzen.</>}
            </Line>
          ))}
        </div>
      )}
      {error && !refused && <div style={{ padding: "12px 22px 0" }}><Line tone="err" icon="alert">{error}</Line></div>}

      {pending && !running && (
        <div style={{ display: "flex", alignItems: "center", gap: 12, padding: "10px 22px", flexWrap: "wrap" }}>
          <span style={{ width: 8, height: 8, borderRadius: 999, background: "var(--status-warn)", flexShrink: 0 }} />
          <div style={{ minWidth: 0, flex: 1, display: "flex", flexDirection: "column", gap: 2 }}>
            <span style={{ fontSize: 13, fontWeight: 600, color: "var(--text)" }}>
              Änderungen ausstehend
              <span style={{ fontWeight: 400, color: "var(--text-faint)" }}>
                {files.length > 0 && ` · ${files.map((f) => f.replace(/\.yaml$/, "")).join(", ")}`}
                {unsaved > 0 && ` · ${unsaved} noch nicht gespeichert`}
              </span>
            </span>
            <span style={{ fontSize: 12, color: "var(--text-faint)", display: "inline-flex", alignItems: "center", gap: 6 }}>
              {preview.isFetching ? <><Spinner size={11} /> prüfe Auswirkung…</>
                : unsaved > 0 && files.length === 0 ? "Auswirkung wird beim Übernehmen geprüft"
                : restartSummary(verdict)}
              {verdict && !verdict.rendered && verdict.note && <span title={verdict.note}><Icon name="info" size={12} /></span>}
            </span>
          </div>
          <Button variant="ghost" size="sm" icon="diff" onClick={onView}>Ansehen</Button>
          <Button variant="outline" size="sm" icon="rotate" disabled={busy || files.length === 0} onClick={() => setConfirmDiscard(true)}>Verwerfen</Button>
          {refused && verdict?.blocking && !verdict.stuck ? (
            <Button variant="dangerGhost" size="sm" onClick={() => setOverrideOpen(true)}>Trotzdem übernehmen…</Button>
          ) : (
            <Button variant="primary" size="sm" icon={busy ? undefined : "check"} disabled={busy || !!refused} onClick={() => start()}>
              {busy ? <><Spinner size={13} /> Prüfe…</> : "Übernehmen"}
            </Button>
          )}
        </div>
      )}
      {!pending && upgradeId && !running && (
        <div style={{ display: "flex", justifyContent: "flex-end", padding: "10px 22px" }}>
          <Button variant="ghost" size="sm" onClick={() => { setUpgradeId(null); setError(null); }}>Schließen</Button>
        </div>
      )}
      {!pending && !upgradeId && error && (
        <div style={{ display: "flex", justifyContent: "flex-end", padding: "10px 22px" }}>
          <Button variant="ghost" size="sm" onClick={() => setError(null)}>Schließen</Button>
        </div>
      )}

      <ConfirmDialog open={confirmDiscard} title="Änderungen verwerfen?" confirmLabel="Verwerfen" confirmIcon="rotate"
        busy={discard.isPending} error={discard.isError ? (discard.error as Error).message : undefined}
        onConfirm={() => discard.mutate()} onCancel={() => setConfirmDiscard(false)}>
        Alle ausstehenden Änderungen in {files.join(", ")} gehen verloren; die Einstellungen stehen danach wieder auf dem letzten übernommenen Stand. Auf dem Cluster ändert sich nichts.
      </ConfirmDialog>

      {/* The way past the check, for when the check is wrong. Deliberately a second,
          explicit step: the check was right both times it was ignored in production. */}
      <ConfirmDialog open={overrideOpen} title="Kapazitätsprüfung übergehen?" confirmLabel="Trotzdem übernehmen" confirmIcon="alert"
        busy={apply.isPending} onConfirm={() => { setOverrideOpen(false); start(true); }} onCancel={() => setOverrideOpen(false)}>
        Die Prüfung sagt, dass mindestens ein Dienst auf keinen Node passt. Übernommen bleibt er vermutlich auf <em>Pending</em> stehen — bei Postgres heißt das: der ganze Server ist nicht erreichbar. Nur fortfahren, wenn du sicher bist, dass die Prüfung sich irrt.
      </ConfirmDialog>
    </div>
  );
}

/** The newest revision of the managed release, or null when it cannot be read. */
async function releaseRevision(): Promise<number | null> {
  try {
    return (await api.get<{ revision: number }>("/api/v1/helm/releases/ess")).revision ?? null;
  } catch {
    return null;
  }
}

function Line({ tone, icon, children }: { tone: "ok" | "warn" | "err"; icon: "check" | "alert" | "x" | "rotate"; children: React.ReactNode }) {
  const color = tone === "ok" ? "var(--status-ok)" : tone === "warn" ? "var(--status-warn)" : "var(--status-err)";
  return (
    <div style={{ display: "flex", gap: 8, alignItems: "flex-start", fontSize: 13, color: "var(--text)", lineHeight: 1.5 }}>
      <Icon name={icon} size={16} style={{ color, flexShrink: 0, marginTop: 1 }} />
      <span>{children}</span>
    </div>
  );
}

/** The preselected answer: focused, so Enter takes it. */
function AutoFocusButton({ children, onClick, disabled }: { children: React.ReactNode; onClick: () => void; disabled?: boolean }) {
  const ref = useRef<HTMLSpanElement>(null);
  useEffect(() => { ref.current?.querySelector("button")?.focus(); }, []);
  return <span ref={ref}><Button variant="primary" size="sm" icon="rotate" disabled={disabled} onClick={onClick}>{children}</Button></span>;
}
