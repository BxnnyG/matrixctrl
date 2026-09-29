import { Card, Icon, Spinner } from "@/components/mc";
import type { UpgradeProgress, ProgressComponent } from "@/lib/ws";

// The live view of a Helm operation: phase, and every workload with its state. Shared by
// the update page and "Übernehmen" on the settings page (etappe 108), which until then
// showed nothing but the raw log — the stream carried the per-service progress all along.

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

/** How an operation ended, once it has. */
export type Outcome = "success" | "hooks-failed" | "failed" | null | undefined;

export type StepState = "done" | "running" | "failed" | "pending";

/** One step of the stepper.
 *
 *  "Fertig" is a step you arrive at, not one you work on: reaching it is the end, so it
 *  is ticked — it used to spin forever after a finished apply. And a step on which the
 *  operation failed shows that, instead of a spinner that suggests it is still trying. */
export function stepState(i: number, phase: UpgradeProgress["phase"], outcome: Outcome): StepState {
  const at = Math.max(0, PHASES.findIndex((p) => p.key === phase));
  if (i < at) return "done";
  if (i > at) return "pending";
  if (outcome === "failed") return "failed";
  if (phase === "done" || outcome === "success" || outcome === "hooks-failed") return "done";
  return "running";
}

/** The stepper. Answers "what is it doing" without reading the log upward. */
function PhaseSteps({ phase, outcome }: { phase: UpgradeProgress["phase"]; outcome?: Outcome }) {
  return (
    <div style={{ display: "flex", alignItems: "center", gap: 0, flexWrap: "wrap" }}>
      {PHASES.map((p, i) => {
        const state = stepState(i, phase, outcome);
        const done = state === "done";
        const failed = state === "failed";
        const active = state === "running" || failed;
        return (
          <div key={p.key} style={{ display: "flex", alignItems: "center", gap: 0 }}>
            <div style={{ display: "flex", alignItems: "center", gap: 7, padding: "5px 10px", borderRadius: 999, background: state === "running" ? "var(--accent-soft)" : "transparent" }}>
              <div style={{
                display: "grid", placeItems: "center", width: 16, height: 16, borderRadius: 999, flexShrink: 0,
                background: done ? "var(--status-ok)" : failed ? "var(--status-err)" : active ? "var(--accent)" : "var(--surface-2)",
                color: done || active ? "var(--surface)" : "var(--text-faint)",
                border: done || active ? "none" : "1px solid var(--border)",
              }}>
                {done ? <Icon name="check" size={10} /> : failed ? <Icon name="x" size={10} /> : active ? <Spinner size={9} /> : null}
              </div>
              <span style={{ fontSize: 12, fontWeight: active ? 650 : 500, color: done ? "var(--text-dim)" : failed ? "var(--status-err)" : active ? "var(--accent)" : "var(--text-faint)", whiteSpace: "nowrap" }}>
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
export function ProgressPanel({ progress, elapsed, outcome }: { progress: UpgradeProgress; elapsed: number; outcome?: Outcome }) {
  const pct = progress.total > 0 ? Math.round((progress.ready / progress.total) * 100) : 0;
  const showNumbers = progress.total > 0 && SHOWS_NUMBERS.includes(progress.phase);

  return (
    <Card style={{ display: "flex", flexDirection: "column", gap: 14 }}>
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 16, flexWrap: "wrap" }}>
        <PhaseSteps phase={progress.phase} outcome={outcome} />
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

export function formatElapsed(seconds: number): string {
  if (seconds < 60) return `${seconds}s`;
  return `${Math.floor(seconds / 60)}m ${String(seconds % 60).padStart(2, "0")}s`;
}

