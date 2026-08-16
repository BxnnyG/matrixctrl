import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { useQuery, useMutation } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { useUpgradeStream, type UpgradeProgress, type ProgressComponent } from "@/lib/ws";
import { Card, Icon, Badge, Button, SectionTitle, Spinner } from "@/components/mc";
import { Markdown } from "@/components/Markdown";

export const Route = createFileRoute("/helm/upgrade")({
  component: UpgradeWizard,
  // The version travels from the list page so "Upgrade auf 26.8.0" arrives with
  // 26.8.0 selected, instead of asking the operator to pick it a second time (E32).
  validateSearch: (search: Record<string, unknown>): { version?: string } => ({
    version: typeof search.version === "string" ? search.version : undefined,
  }),
});

interface ReleaseNotes {
  version: string;
  available: boolean;
  title?: string;
  published_at?: string;
  body?: string;
  url?: string;
  /** Why it is unavailable. "could not be fetched" and "none published" lead to
   *  different conclusions, so they are not collapsed into one empty state. */
  reason?: string;
}

interface HelmRelease { chart_version: string; revision: number }
interface ESSVersion { version: string }
interface UpgradeResponse { upgrade_id: string }

const essVersion = (v: string) => v.replace(/^matrix-stack-/, "");

/** The release notes for the version about to be installed.
 *
 *  Not decoration: 26.8.0's notes say "Upgrade Element Web to v1.12.25" and
 *  "Upgrade Synapse to v1.158.0" — exactly the upgrades the operator's pinned image
 *  tags were silently preventing. This screen now carries both halves: what the
 *  version brings, and (from the upgrade log) what a pin will stop it bringing. */
function NotesPanel({ notes, loading, version }: { notes?: ReleaseNotes; loading: boolean; version: string }) {
  if (loading && !notes) {
    return <div style={{ fontSize: 12.5, color: "var(--text-faint)" }}><Spinner size={13} /> Lade Release Notes…</div>;
  }
  if (!notes?.available) {
    return (
      <div style={{ fontSize: 12.5, color: "var(--text-faint)", padding: "10px 12px", borderRadius: "var(--radius-sm)", background: "var(--surface-2)" }}>
        {notes?.reason ?? "Keine Release Notes verfügbar."}
      </div>
    );
  }
  return (
    <div style={{ borderRadius: "var(--radius-sm)", background: "var(--surface-2)", border: "1px solid var(--border)" }}>
      <div style={{ display: "flex", alignItems: "center", gap: 10, padding: "10px 14px", borderBottom: "1px solid var(--border)", flexWrap: "wrap" }}>
        <Icon name="file" size={15} style={{ color: "var(--text-dim)" }} />
        <span style={{ fontSize: 13, fontWeight: 650, color: "var(--text)" }}>{notes.title || version}</span>
        {notes.published_at && (
          <span style={{ fontSize: 11.5, color: "var(--text-faint)" }}>
            {new Date(notes.published_at).toLocaleDateString("de-DE", { day: "2-digit", month: "2-digit", year: "numeric" })}
          </span>
        )}
        {notes.url && (
          <a href={notes.url} target="_blank" rel="noreferrer noopener"
            style={{ marginLeft: "auto", fontSize: 11.5, color: "var(--accent)", textDecoration: "none" }}>
            auf GitHub ↗
          </a>
        )}
      </div>
      <div className="mc-scroll" style={{ maxHeight: 320, overflowY: "auto", padding: "12px 14px" }}>
        <Markdown text={notes.body ?? ""} />
      </div>
    </div>
  );
}

const PHASES: { key: UpgradeProgress["phase"]; label: string }[] = [
  { key: "config", label: "Konfiguration" },
  { key: "apply", label: "Anwenden" },
  { key: "rollout", label: "Rollout" },
  { key: "hooks", label: "Hooks" },
  { key: "done", label: "Fertig" },
];

/** Phases in which the cluster numbers mean something.
 *
 *  Before Helm has written anything, every workload still matches its old spec and
 *  reads as ready — so a bar shown during `config` or `apply` would open at 100 %,
 *  fall as pods roll, and climb back. The backend promotes `apply` to `rollout` the
 *  moment a workload stops being settled, which is exactly when these become true. */
const SHOWS_NUMBERS: UpgradeProgress["phase"][] = ["rollout", "hooks", "done"];

const STATE_LABEL: Record<ProgressComponent["state"], string> = {
  ready: "bereit",
  pulling: "lädt Image",
  starting: "startet",
  failing: "Fehler",
  waiting: "wartet",
};

const STATE_COLOR: Record<ProgressComponent["state"], string> = {
  ready: "var(--status-ok)",
  pulling: "var(--accent)",
  starting: "var(--accent)",
  failing: "var(--status-err)",
  waiting: "var(--text-faint)",
};

/** The stepper. Answers "what is it doing" without reading the log upward. */
function PhaseSteps({ phase }: { phase: UpgradeProgress["phase"] }) {
  const at = Math.max(0, PHASES.findIndex((p) => p.key === phase));
  return (
    <div style={{ display: "flex", alignItems: "center", gap: 0, flexWrap: "wrap" }}>
      {PHASES.map((p, i) => {
        const done = i < at;
        const active = i === at;
        return (
          <div key={p.key} style={{ display: "flex", alignItems: "center", gap: 0 }}>
            <div style={{ display: "flex", alignItems: "center", gap: 7, padding: "5px 10px", borderRadius: 999, background: active ? "var(--accent-soft)" : "transparent" }}>
              <div style={{
                display: "grid", placeItems: "center", width: 16, height: 16, borderRadius: 999, flexShrink: 0,
                background: done ? "var(--status-ok)" : active ? "var(--accent)" : "var(--surface-2)",
                color: done || active ? "var(--surface)" : "var(--text-faint)",
                border: done || active ? "none" : "1px solid var(--border)",
              }}>
                {done ? <Icon name="check" size={10} /> : active ? <Spinner size={9} /> : null}
              </div>
              <span style={{ fontSize: 12, fontWeight: active ? 650 : 500, color: done ? "var(--text-dim)" : active ? "var(--accent)" : "var(--text-faint)", whiteSpace: "nowrap" }}>
                {p.label}
              </span>
            </div>
            {i < PHASES.length - 1 && (
              <div style={{ width: 16, height: 1, background: done ? "var(--status-ok)" : "var(--border)", flexShrink: 0 }} />
            )}
          </div>
        );
      })}
    </div>
  );
}

/** Per-component state, live.
 *
 *  The denominator is workloads, not pods. A pod count churns — old pods terminate
 *  while new ones start, so "4 of 9" can fall while everything is going right —
 *  whereas the workload set is fixed for the operation and is what `helm --wait` is
 *  itself waiting on. */
function ProgressPanel({ progress, elapsed }: { progress: UpgradeProgress; elapsed: number }) {
  const pct = progress.total > 0 ? Math.round((progress.ready / progress.total) * 100) : 0;
  const showNumbers = progress.total > 0 && SHOWS_NUMBERS.includes(progress.phase);

  return (
    <Card style={{ display: "flex", flexDirection: "column", gap: 14 }}>
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 16, flexWrap: "wrap" }}>
        <PhaseSteps phase={progress.phase} />
        {/* Ticks every second in the client. The backend's 30 s log line was the
            only sign of life on a healthy upgrade, which is precisely when the
            panel was quietest. */}
        <span style={{ fontFamily: "var(--mono)", fontSize: 12, color: "var(--text-faint)", flexShrink: 0 }}>
          {formatElapsed(elapsed)}
        </span>
      </div>

      {showNumbers && (
        <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
          <div style={{ display: "flex", justifyContent: "space-between", fontSize: 12, color: "var(--text-dim)" }}>
            <span>{progress.ready} von {progress.total} Komponenten bereit</span>
            <span style={{ fontFamily: "var(--mono)" }}>{pct}%</span>
          </div>
          <div style={{ height: 6, borderRadius: 999, background: "var(--surface-2)", overflow: "hidden" }}>
            <div style={{ width: `${pct}%`, height: "100%", borderRadius: 999, background: "var(--accent)", transition: "width 400ms ease" }} />
          </div>
        </div>
      )}

      {showNumbers && progress.components && progress.components.length > 0 && (
        <div style={{ display: "flex", flexDirection: "column" }}>
          {progress.components.map((c, i) => (
            <div key={c.name} style={{ display: "flex", alignItems: "flex-start", gap: 10, padding: "8px 0", borderTop: i === 0 ? "none" : "1px solid var(--border-soft)" }}>
              <div style={{ width: 7, height: 7, borderRadius: 999, background: STATE_COLOR[c.state], flexShrink: 0, marginTop: 5 }} />
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ display: "flex", alignItems: "baseline", gap: 8, flexWrap: "wrap" }}>
                  <span style={{ fontFamily: "var(--mono)", fontSize: 12.5, color: "var(--text)", overflowWrap: "anywhere" }}>{c.name}</span>
                  <span style={{ fontSize: 11.5, color: STATE_COLOR[c.state] }}>{STATE_LABEL[c.state]}</span>
                </div>
                {c.detail && (
                  <div style={{ fontSize: 11.5, color: "var(--status-err)", marginTop: 3, overflowWrap: "anywhere" }}>{c.detail}</div>
                )}
              </div>
              <span style={{ fontFamily: "var(--mono)", fontSize: 11.5, color: "var(--text-faint)", flexShrink: 0 }}>
                {c.ready}/{c.desired}
              </span>
            </div>
          ))}
        </div>
      )}
    </Card>
  );
}

function formatElapsed(seconds: number): string {
  if (seconds < 60) return `${seconds}s`;
  return `${Math.floor(seconds / 60)}m ${String(seconds % 60).padStart(2, "0")}s`;
}

function UpgradeWizard() {
  const navigate = useNavigate();
  const logRef = useRef<HTMLDivElement>(null);
  const preselected = Route.useSearch().version;
  const [selectedVersion, setSelectedVersion] = useState(preselected ?? "");
  const [upgradeId, setUpgradeId] = useState<string | null>(null);
  const [logs, setLogs] = useState<string[]>([]);
  const [done, setDone] = useState(false);
  const [finalStatus, setFinalStatus] = useState<string | null>(null);
  const [progress, setProgress] = useState<UpgradeProgress | null>(null);
  const [elapsed, setElapsed] = useState(0);
  const [showLog, setShowLog] = useState(false);

  // The elapsed clock runs in the client. It used to arrive as a log line every
  // 30 s, which meant the only evidence that a healthy upgrade was alive appeared
  // twice a minute — and the probe's diagnosis is deduped, so a *smooth* rollout
  // produced the least output of all (E43).
  useEffect(() => {
    if (!upgradeId || done) return;
    const t = setInterval(() => setElapsed((s) => s + 1), 1000);
    return () => clearInterval(t);
  }, [upgradeId, done]);

  const { data: current } = useQuery({
    queryKey: ["helm", "release"],
    queryFn: () => api.get<HelmRelease>("/api/v1/helm/releases/ess"),
  });
  const { data: versions } = useQuery({
    queryKey: ["helm", "versions"],
    queryFn: () => api.get<ESSVersion[]>("/api/v1/helm/versions"),
  });

  const { data: notes, isFetching: notesLoading } = useQuery({
    queryKey: ["helm", "notes", selectedVersion],
    queryFn: () => api.get<ReleaseNotes>(`/api/v1/helm/versions/${encodeURIComponent(selectedVersion)}/notes`),
    enabled: selectedVersion !== "",
    // Published notes do not change, and GitHub's unauthenticated limit is 60
    // requests an hour — refetching on every render would exhaust it.
    staleTime: Infinity,
  });

  const upgrade = useMutation({
    mutationFn: (toVersion: string) =>
      api.post<UpgradeResponse>("/api/v1/helm/releases/ess/upgrade", { to_version: toVersion }),
    onSuccess: (res) => {
      setUpgradeId(res.upgrade_id);
      setLogs([]);
      setDone(false);
      setFinalStatus(null);
      setProgress(null);
      setElapsed(0);
    },
  });

  useUpgradeStream(upgradeId, {
    onLog: (line) => {
      setLogs((prev) => [...prev, line]);
      setTimeout(() => logRef.current?.scrollTo({ top: logRef.current.scrollHeight, behavior: "smooth" }), 30);
    },
    onProgress: setProgress,
    onDone: (status) => {
      setDone(true);
      setFinalStatus(status);
      // Collapsed by default, opened on failure. The structured panel is the thing
      // to read while an upgrade is going well; the moment it is not, the log is,
      // and making the operator hunt for it at that point would be the wrong
      // trade in the one situation that matters.
      if (status !== "success") setShowLog(true);
      // Longer than the old three seconds. The point of this screen is now the
      // finished component table, and redirecting off it before it can be read
      // undoes the work.
      if (status === "success") setTimeout(() => navigate({ to: "/helm" }), 6000);
    },
  });

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 20, maxWidth: 720 }}>
      <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
        <Button variant="ghost" size="sm" icon="chevLeft" onClick={() => navigate({ to: "/helm" })}>Release</Button>
      </div>

      {current && (
        <Card style={{ display: "flex", alignItems: "center", gap: 12, padding: "14px 18px" }}>
          <Icon name="helm" size={18} style={{ color: "var(--accent)" }} />
          <span style={{ fontSize: 13, color: "var(--text-dim)" }}>Aktuell:</span>
          <code style={{ fontFamily: "var(--mono)", fontSize: 13, fontWeight: 600, color: "var(--text)" }}>{essVersion(current.chart_version)}</code>
          <Badge tone="neutral" size="sm">Revision #{current.revision}</Badge>
        </Card>
      )}

      <Card style={{ display: "flex", gap: 12, alignItems: "flex-start", background: "color-mix(in oklch, var(--status-warn) 9%, var(--surface))", borderColor: "color-mix(in oklch, var(--status-warn) 30%, var(--border))" }}>
        <Icon name="alert" size={18} style={{ color: "var(--status-warn)", flexShrink: 0, marginTop: 1 }} />
        <div style={{ fontSize: 13, lineHeight: 1.55 }}>
          <strong style={{ color: "var(--text)" }}>Post-Upgrade-Hooks laufen automatisch.</strong>
          <div style={{ color: "var(--text-dim)", marginTop: 2 }}>SFU <code style={{ fontFamily: "var(--mono)" }}>hostNetwork</code> und Service <code style={{ fontFamily: "var(--mono)" }}>externalTrafficPolicy</code> werden nach dem Upgrade gepatcht.</div>
        </div>
      </Card>

      {!upgradeId ? (
        <Card style={{ display: "flex", flexDirection: "column", gap: 16 }}>
          <SectionTitle sub="Zielversion für das ESS-Helm-Release wählen">Upgrade konfigurieren</SectionTitle>
          <div>
            <label style={{ display: "block", fontSize: 12.5, fontWeight: 600, color: "var(--text-dim)", marginBottom: 8 }}>Zielversion</label>
            <select value={selectedVersion} onChange={(e) => setSelectedVersion(e.target.value)}
              className="mc-input" style={{ width: "100%", padding: "9px 12px", borderRadius: "var(--radius-sm)", background: "var(--surface-2)", border: "1px solid var(--border)", color: "var(--text)", fontSize: 13.5, fontFamily: "var(--font)" }}>
              <option value="">Version wählen…</option>
              {versions?.map((v, i) => (
                <option key={v.version} value={v.version}>{essVersion(v.version)}{i === 0 ? " — latest" : ""}{v.version === current?.chart_version ? " (aktuell)" : ""}</option>
              ))}
            </select>
          </div>
          {selectedVersion && (
            <NotesPanel notes={notes} loading={notesLoading} version={essVersion(selectedVersion)} />
          )}

          <div>
            <Button variant="primary" icon="upload" disabled={!selectedVersion || upgrade.isPending} onClick={() => upgrade.mutate(selectedVersion)}>
              {upgrade.isPending ? <><Spinner size={14} /> Starte…</> : "Upgrade starten"}
            </Button>
          </div>
          {upgrade.isError && <div style={{ fontSize: 13, color: "var(--status-err)" }}>{(upgrade.error as Error).message}</div>}
        </Card>
      ) : (
        <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
          {progress ? (
            <ProgressPanel progress={progress} elapsed={elapsed} />
          ) : (
            <Card style={{ display: "flex", alignItems: "center", gap: 10, fontSize: 13, color: "var(--text-dim)" }}>
              <Spinner size={14} /> Upgrade wird gestartet…
            </Card>
          )}

          <div>
            <Button variant="ghost" size="sm" icon={showLog ? "chevDown" : "chevRight"} onClick={() => setShowLog((v) => !v)}>
              {showLog ? "Log ausblenden" : `Log anzeigen (${logs.length} Zeilen)`}
            </Button>
          </div>

          {/* whiteSpace/overflowWrap are the fix, not styling. `overflowY: auto`
              computes overflow-x to `auto` as well, so the 200-character pinned-tag
              warning made the box scroll sideways — and the auto-scroll only ever
              set `top`, leaving the view parked to the right while the cursor sat at
              x=0. That is the "cursor verschwindet nach links ausm frame" the
              operator reported. Wrapping removes the horizontal axis entirely. */}
          {showLog && (
            <div ref={logRef} className="mc-scroll" style={{ background: "oklch(0.13 0.005 256)", borderRadius: "var(--radius)", border: "1px solid var(--border)", padding: 16, fontFamily: "var(--mono)", fontSize: 12, color: "oklch(0.82 0.13 150)", minHeight: 160, maxHeight: 400, overflowY: "auto", overflowX: "hidden", whiteSpace: "pre-wrap", overflowWrap: "anywhere", lineHeight: 1.6 }}>
              {logs.map((line, i) => <div key={i}>{line}</div>)}
              {!done && <span style={{ animation: "mc-ping 1.2s ease infinite" }}>▋</span>}
            </div>
          )}

          {done && finalStatus === "success" && (
            <Card style={{ display: "flex", alignItems: "center", gap: 12, background: "color-mix(in oklch, var(--status-ok) 10%, var(--surface))", borderColor: "color-mix(in oklch, var(--status-ok) 30%, var(--border))" }}>
              <Icon name="check" size={20} style={{ color: "var(--status-ok)" }} />
              <div style={{ fontSize: 13 }}><strong style={{ color: "var(--text)" }}>Upgrade erfolgreich.</strong> <span style={{ color: "var(--text-faint)" }}>Weiterleitung…</span></div>
            </Card>
          )}

          {done && finalStatus === "hooks-failed" && (
            <Card style={{ display: "flex", gap: 12, alignItems: "flex-start", background: "color-mix(in oklch, var(--status-warn) 9%, var(--surface))", borderColor: "color-mix(in oklch, var(--status-warn) 30%, var(--border))" }}>
              <Icon name="alert" size={20} style={{ color: "var(--status-warn)", flexShrink: 0, marginTop: 1 }} />
              <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
                <div style={{ fontSize: 13, lineHeight: 1.55 }}>
                  <strong style={{ color: "var(--text)" }}>Helm-Upgrade erfolgreich, aber Hooks fehlgeschlagen.</strong>
                  <div style={{ color: "var(--text-dim)", marginTop: 3 }}>Der ESS-Release ist auf dem neuen Stand. Die Post-Upgrade-Patches (SFU hostNetwork etc.) wurden jedoch nicht vollständig angewendet — WebRTC-Calling könnte beeinträchtigt sein.</div>
                </div>
                <div style={{ display: "flex", gap: 8 }}>
                  <Button variant="soft" size="sm" icon="hook" onClick={() => navigate({ to: "/hooks" })}>Hooks manuell ausführen</Button>
                  <Button variant="outline" size="sm" onClick={() => navigate({ to: "/helm" })}>Zur Übersicht</Button>
                </div>
              </div>
            </Card>
          )}

          {done && finalStatus === "failed" && (
            <Card style={{ display: "flex", gap: 12, alignItems: "flex-start", background: "color-mix(in oklch, var(--status-err) 9%, var(--surface))", borderColor: "color-mix(in oklch, var(--status-err) 30%, var(--border))" }}>
              <Icon name="x" size={20} style={{ color: "var(--status-err)", flexShrink: 0, marginTop: 1 }} />
              <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
                <div style={{ fontSize: 13, lineHeight: 1.55 }}>
                  <strong style={{ color: "var(--text)" }}>Upgrade fehlgeschlagen.</strong>
                  <div style={{ color: "var(--text-dim)", marginTop: 3 }}>Helm hat die vorherige Revision automatisch wiederhergestellt. Sieh die Logs oben für Details.</div>
                </div>
                <div><Button variant="outline" size="sm" onClick={() => navigate({ to: "/helm" })}>Zur Übersicht</Button></div>
              </div>
            </Card>
          )}
        </div>
      )}
    </div>
  );
}
