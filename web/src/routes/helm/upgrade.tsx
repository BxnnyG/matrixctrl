import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { useUpgradeStream, type UpgradeProgress } from "@/lib/ws";
import { ProgressPanel, type Outcome } from "@/components/RolloutProgress";
import { Card, Icon, Badge, Button, SectionTitle, Spinner, ConfirmDialog } from "@/components/mc";
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

/** Mirrors imagepin.Finding. */
interface PinnedImage { component: string; config: string; chart?: string; kind: "older" | "orphan" }

/** Images the config pins that the target chart does not match — shown before the
 *  upgrade starts, with the fix one click away (etappe 109). On 2026-09-28 both
 *  failures of the 26.9.3 upgrade were in this list, as a log line nobody could act on
 *  while the upgrade was already failing. */
function PinnedImages({ pinned, note, busy, onFollow, error }: {
  pinned: PinnedImage[]; note?: string; busy: boolean; onFollow: () => void; error?: string;
}) {
  if (note) {
    return <div style={{ fontSize: 12.5, color: "var(--text-faint)" }}><Icon name="info" size={13} /> {note}</div>;
  }
  if (pinned.length === 0) return null;
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 10, padding: 14, borderRadius: "var(--radius)", border: "1px solid var(--status-warn)", background: "color-mix(in oklch, var(--status-warn) 8%, var(--surface))" }}>
      <div style={{ display: "flex", gap: 10, alignItems: "flex-start", fontSize: 13, lineHeight: 1.55, color: "var(--text)" }}>
        <Icon name="alert" size={16} style={{ color: "var(--status-warn)", flexShrink: 0, marginTop: 2 }} />
        <div>
          <strong>Die Konfiguration schreibt Images fest, die nicht zu dieser Version passen.</strong>
          <div style={{ color: "var(--text-dim)" }}>Das Chart ist auf seine eigenen Images abgestimmt — mit älteren scheitert das Upgrade oft erst mittendrin. Das Upgrade ist gesperrt, bis diese Komponenten dem Chart folgen.</div>
        </div>
      </div>
      <div style={{ display: "flex", flexDirection: "column", gap: 4, paddingLeft: 26 }}>
        {pinned.map((p) => (
          <div key={p.component} style={{ fontSize: 12.5, color: "var(--text-dim)" }}>
            <code style={{ fontFamily: "var(--mono)", color: "var(--text)" }}>{p.component}</code>{" "}
            {p.kind === "orphan"
              ? <>— festgeschrieben <code style={{ fontFamily: "var(--mono)" }}>{p.config}</code>, das Chart hat dafür kein Image mehr</>
              : <>— festgeschrieben <code style={{ fontFamily: "var(--mono)" }}>{p.config}</code>, das Chart bringt <code style={{ fontFamily: "var(--mono)" }}>{p.chart}</code></>}
          </div>
        ))}
      </div>
      <div style={{ paddingLeft: 26, display: "flex", flexDirection: "column", gap: 6 }}>
        <div>
          <Button variant="primary" size="sm" icon={busy ? undefined : "check"} disabled={busy} onClick={onFollow}>
            {busy ? <><Spinner size={13} /> Trage ein…</> : "Dem Chart folgen lassen"}
          </Button>
        </div>
        <span style={{ fontSize: 12, color: "var(--text-faint)" }}>Die Zeilen werden in den Einstellungen auskommentiert, nicht gelöscht — der alte Wert bleibt lesbar. Das Upgrade übernimmt sie mit.</span>
        {error && <span style={{ fontSize: 12, color: "var(--status-err)" }}>{error}</span>}
      </div>
    </div>
  );
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
  const [overrideOpen, setOverrideOpen] = useState(false);
  const qc = useQueryClient();
  const hooksQuery = useQuery({
    queryKey: ["hooks"],
    queryFn: () => api.get<{ name: string; trigger: string; enabled: boolean; covered?: string }[]>("/api/v1/hooks"),
  });
  const pendingHooks = (hooksQuery.data ?? []).filter((h) => h.enabled && h.trigger !== "manual" && h.trigger !== "post-rollback" && !h.covered);

  const check = useQuery({
    queryKey: ["helm", "upgrade-check", selectedVersion],
    queryFn: () => api.get<{ pinned: PinnedImage[]; note?: string }>(`/api/v1/helm/upgrade-check?version=${encodeURIComponent(selectedVersion)}`),
    enabled: !!selectedVersion,
    refetchOnWindowFocus: false,
  });
  const pinned = check.data?.pinned ?? [];
  const follow = useMutation({
    mutationFn: () => api.post("/api/v1/config/follow-chart", { items: pinned.map((p) => ({ component: p.component, kind: p.kind })) }),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: ["helm", "upgrade-check"] });
      qc.invalidateQueries({ queryKey: ["config"] });
    },
  });

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
    mutationFn: ({ version, override }: { version: string; override?: boolean }) =>
      api.post<UpgradeResponse>("/api/v1/helm/releases/ess/upgrade", { to_version: version, override_pinned_images: !!override }),
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

      {/* Only hooks that will actually do something. This was a fixed sentence about
          the two call-server patches — still shown after ESS had taken both over and
          MatrixCtrl skipped them (etappe 119c). */}
      {pendingHooks.length > 0 && (
        <Card style={{ display: "flex", gap: 12, alignItems: "flex-start" }}>
          <Icon name="hook" size={18} style={{ color: "var(--accent)", flexShrink: 0, marginTop: 1 }} />
          <div style={{ fontSize: 13, lineHeight: 1.55 }}>
            <strong style={{ color: "var(--text)" }}>Nach dem Upgrade {pendingHooks.length === 1 ? "läuft ein Hook" : `laufen ${pendingHooks.length} Hooks`}:</strong>
            <div style={{ color: "var(--text-dim)", marginTop: 2 }}>{pendingHooks.map((h) => h.name).join(", ")}</div>
          </div>
        </Card>
      )}

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

          {selectedVersion && (
            <PinnedImages pinned={pinned} note={check.data?.note} busy={follow.isPending} onFollow={() => follow.mutate()}
              error={follow.isError ? (follow.error as Error).message : undefined} />
          )}

          <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
            <Button variant="primary" icon="upload" disabled={!selectedVersion || upgrade.isPending || check.isFetching || pinned.length > 0}
              onClick={() => upgrade.mutate({ version: selectedVersion })}>
              {upgrade.isPending ? <><Spinner size={14} /> Starte…</> : check.isFetching ? <><Spinner size={14} /> Prüfe…</> : "Upgrade starten"}
            </Button>
            {pinned.length > 0 && (
              <Button variant="dangerGhost" size="sm" disabled={upgrade.isPending} onClick={() => setOverrideOpen(true)}>Trotzdem starten…</Button>
            )}
          </div>
          <ConfirmDialog open={overrideOpen} title="Mit festgeschriebenen Images upgraden?" confirmLabel="Trotzdem starten" confirmIcon="alert"
            busy={upgrade.isPending} onConfirm={() => { setOverrideOpen(false); upgrade.mutate({ version: selectedVersion, override: true }); }}
            onCancel={() => setOverrideOpen(false)}>
            Die genannten Komponenten laufen dann weiter mit ihren alten Images unter dem neuen Chart. Beim letzten Upgrade ist genau das zweimal gescheitert. Nur fortfahren, wenn du eine bestimmte Version bewusst behalten willst.
          </ConfirmDialog>
          {upgrade.isError && <div style={{ fontSize: 13, color: "var(--status-err)" }}>{(upgrade.error as Error).message}</div>}
        </Card>
      ) : (
        <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
          {progress ? (
            <ProgressPanel progress={progress} elapsed={elapsed} outcome={done ? (finalStatus as Outcome) : undefined} />
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
                  {/* Said as it is. This claimed Helm had restored the previous revision —
                      ESS upgrades do not run atomic, and on 2026-10-10 three failed
                      attempts left most pods on the new version and the release on
                      "failed" (etappe 119c). */}
                  <div style={{ color: "var(--text-dim)", marginTop: 3 }}>
                    Es wurde nichts zurückgerollt: ESS steht jetzt auf „failed“, ein Teil läuft womöglich schon in der neuen Version.
                    Ein neuer Versuch setzt dort an; auf der Übersicht kannst du stattdessen zur vorigen Revision zurückrollen. Der Grund steht im Log oben.
                  </div>
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
