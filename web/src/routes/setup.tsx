import { createFileRoute } from "@tanstack/react-router";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, useRef, type ReactNode, type RefObject } from "react";
import { api } from "@/lib/api";
import { essVersion, type ArchiveManifest } from "@/lib/archive";
import { useUpgradeStream } from "@/lib/ws";
import { Card, Icon, Button, Spinner, StatusDot, type IconName } from "@/components/mc";

export const Route = createFileRoute("/setup")({
  component: Setup,
});

interface SetupStatus {
  ess_namespace: string;
  ess_release: string;
  ess_installed: boolean;
  /** "absent" | "busy" | "deployed" | "failed".
   *  ess_installed used to be true the moment a release object existed — including
   *  while `helm install` was still running, which is how Setup offered "connect
   *  Matrix login" into an install in flight (§4.88). */
  ess_state?: "absent" | "busy" | "deployed" | "failed";
  ess_version?: string;
  ess_status?: string;
  oidc_configured: boolean;
  bootstrap_active: boolean;
  config_sections: number;
  mas_host?: string;
  /** Fields the registered MAS client lacks that the current version writes.
   *  Empty when complete, absent on an older backend (E30). */
  oidc_client_missing?: string[];
}
interface ESSVersion { version: string }
interface DeployResponse { upgrade_id: string }

const inputStyle: React.CSSProperties = { width: "100%", padding: "9px 12px", border: "1px solid var(--border)", background: "var(--surface-2)", color: "var(--text)", borderRadius: "var(--radius-sm)", fontSize: 13.5, fontFamily: "var(--font)" };
const labelStyle: React.CSSProperties = { display: "block", fontSize: 12.5, fontWeight: 600, color: "var(--text-dim)", marginBottom: 6 };

function WizardHeader({ icon, title, sub }: { icon: IconName; title: string; sub: string }) {
  return (
    <div style={{ display: "flex", alignItems: "center", gap: 12, padding: "14px 18px", background: "var(--accent-soft)", borderBottom: "1px solid var(--border-soft)" }}>
      <div style={{ display: "grid", placeItems: "center", width: 38, height: 38, borderRadius: "var(--radius-sm)", background: "var(--accent)", color: "var(--accent-fg)", flexShrink: 0 }}><Icon name={icon} size={18} /></div>
      <div>
        <div style={{ fontSize: 14, fontWeight: 650, color: "var(--text)" }}>{title}</div>
        <div style={{ fontSize: 12, color: "var(--text-faint)" }}>{sub}</div>
      </div>
    </div>
  );
}

function LogTerm({ logs, done, logRef }: { logs: string[]; done: boolean; logRef: RefObject<HTMLDivElement | null> }) {
  return (
    <div ref={logRef} className="mc-scroll" style={{ background: "oklch(0.13 0.005 256)", borderRadius: "var(--radius-sm)", padding: 12, fontFamily: "var(--mono)", fontSize: 12, color: "oklch(0.82 0.13 150)", maxHeight: 288, overflowY: "auto", lineHeight: 1.6 }}>
      {logs.map((line, i) => <div key={i} style={{ color: line.startsWith("ERROR") ? "var(--status-err)" : line.startsWith("WARNING") ? "var(--status-warn)" : undefined }}>{line}</div>)}
      {!done && <div style={{ animation: "mc-ping 1.2s ease infinite", marginTop: 2 }}>▋</div>}
    </div>
  );
}

function StatusInline({ done, status, map }: { done: boolean; status: string | null; map: Record<string, [string, "ok" | "warn" | "err"]> }) {
  if (!done || !status || !map[status]) return null;
  const [label, tone] = map[status];
  return <span style={{ display: "inline-flex", alignItems: "center", gap: 4, fontSize: 12, fontWeight: 600, color: `var(--status-${tone})` }}><Icon name={tone === "ok" ? "check" : tone === "warn" ? "alert" : "x"} size={14} stroke={2.2} /> {label}</span>;
}

const DEPLOY_MAP: Record<string, [string, "ok" | "warn" | "err"]> = { success: ["Erfolgreich", "ok"], "hooks-failed": ["Hooks fehlgeschlagen", "warn"], failed: ["Fehlgeschlagen", "err"] };

function Setup() {
  const qc = useQueryClient();
  // Which way in. Null means the question has not been asked yet, which is itself the
  // first screen — before etappe 82 there were two answers and no question, and the
  // third answer ("I am moving from another server") had nowhere to go at all.
  const [mode, setMode] = useState<"fresh" | "migrate" | null>(null);
  const { data, isLoading } = useQuery({
    queryKey: ["setup", "status"],
    queryFn: () => api.get<SetupStatus>("/api/v1/setup/status"),
    refetchInterval: 30_000,
  });

  if (isLoading) return <div style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 13, color: "var(--text-faint)" }}><Spinner size={14} /> Lade…</div>;
  if (!data) return null;
  const invalidate = () => qc.invalidateQueries({ queryKey: ["setup", "status"] });

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 20, maxWidth: 820 }}>
      <MatrixLoginHealth />
      {data.ess_state === "busy" ? (
        <WizardCard>
          <WizardHeader icon="clock" title="ESS wird gerade installiert"
            sub={`Helm ist noch dabei (${data.ess_status ?? "läuft"}) — der nächste Schritt wartet darauf`} />
          <div style={{ padding: 18, display: "flex", flexDirection: "column", gap: 10 }}>
            <p style={{ margin: 0, fontSize: 13, color: "var(--text-dim)" }}>
              Solange eine Helm-Operation läuft, scheitert jede weitere daran
              („another operation is in progress"). Diese Seite aktualisiert sich selbst.
            </p>
            <span style={{ fontSize: 12.5, color: "var(--text-faint)" }}><Spinner size={13} /> warte…</span>
          </div>
        </WizardCard>
      ) : data.ess_state === "failed" ? (
        <WizardCard>
          <WizardHeader icon="alert" title="Die ESS-Installation steht nicht sauber da"
            sub={`Zustand: ${data.ess_status ?? "unbekannt"}`} />
          <div style={{ padding: 18, display: "flex", flexDirection: "column", gap: 10 }}>
            <p style={{ margin: 0, fontSize: 13, color: "var(--text-dim)" }}>
              Bevor hier etwas weitergeht, muss dieser Zustand aufgelöst werden — sonst
              scheitert jede weitere Helm-Operation daran.
            </p>
            <pre style={{ margin: 0, padding: 11, fontSize: 11.5, fontFamily: "var(--mono)", background: "var(--bg)", border: "1px solid var(--border)", borderRadius: "var(--radius-sm)", overflowX: "auto", userSelect: "all" }}>
              helm rollback {data.ess_release} -n {data.ess_namespace}
            </pre>
          </div>
        </WizardCard>
      ) : !data.ess_installed ? (
        mode === null ? (
          <StartChoice onPick={setMode} />
        ) : mode === "migrate" ? (
          <MigrateWizard release={data.ess_release} namespace={data.ess_namespace} onDone={invalidate} onBack={() => setMode("fresh")} />
        ) : (
          <DeployWizard release={data.ess_release} namespace={data.ess_namespace} onDone={invalidate} onMigrate={() => setMode("migrate")} />
        )
      ) : data.config_sections === 0 ? (
        <AdoptCard release={data.ess_release} version={data.ess_version} onDone={invalidate} />
      ) : !data.oidc_configured ? (
        <>
          <MatrixAccountCard onDone={invalidate} />
          <ConnectCard masHost={data.mas_host} onDone={invalidate} />
        </>
      ) : (
        <ConnectedCard
          missing={data.oidc_client_missing ?? []}
          masHost={data.mas_host}
          onDone={invalidate}
        />
      )}

      <SetupSteps data={data} />

      <Card style={{ display: "flex", gap: 12, alignItems: "flex-start", background: "var(--panel)" }}>
        <Icon name="info" size={16} style={{ color: "var(--text-faint)", flexShrink: 0, marginTop: 1 }} />
        <div style={{ fontSize: 12.5, color: "var(--text-dim)", lineHeight: 1.55 }}>
          <strong style={{ color: "var(--text)" }}>Phase 1.5:</strong> Greenfield-Deploy (oben) seedet die Config aus den Chart-Defaults und installiert ESS. Noch offen: automatische OIDC-Client-Registrierung via MAS Admin API. Siehe <code style={{ fontFamily: "var(--mono)", background: "var(--surface-2)", padding: "1px 5px", borderRadius: 4 }}>docs/SETUP.md</code>.
        </div>
      </Card>
    </div>
  );
}

function WizardCard({ children }: { children: ReactNode }) {
  return <div style={{ background: "var(--surface)", border: "1px solid color-mix(in oklch, var(--accent) 30%, var(--border))", borderRadius: "var(--radius)", boxShadow: "var(--shadow)", overflow: "hidden" }}>{children}</div>;
}

function AdoptCard({ release, version, onDone }: { release: string; version?: string; onDone: () => void }) {
  const adopt = useMutation({ mutationFn: () => api.post("/api/v1/setup/adopt", {}), onSuccess: onDone });
  return (
    <WizardCard>
      <WizardHeader icon="server" title="Bestehendes ESS übernehmen" sub={`Release „${release}"${version ? ` v${version}` : ""} erkannt — Config übernehmen, um es zu verwalten`} />
      <div style={{ padding: 18, display: "flex", flexDirection: "column", gap: 14 }}>
        <p style={{ margin: 0, fontSize: 13, color: "var(--text-dim)", lineHeight: 1.55 }}>MatrixCtrl liest die aktuellen Helm-Values des Release und legt daraus die versionierten Config-Sektionen an. Danach kannst du es über die UI verwalten.</p>
        <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
          <Button variant="primary" icon={adopt.isPending ? undefined : "server"} disabled={adopt.isPending} onClick={() => adopt.mutate()}>{adopt.isPending ? <><Spinner size={14} /> Übernehme…</> : "Config übernehmen"}</Button>
          {adopt.isError && <span style={{ fontSize: 12, color: "var(--status-err)" }}>{(adopt.error as Error).message}</span>}
          {adopt.isSuccess && <span style={{ display: "inline-flex", alignItems: "center", gap: 4, fontSize: 12, color: "var(--status-ok)" }}><Icon name="check" size={14} stroke={2.2} /> Übernommen</span>}
        </div>
      </div>
    </WizardCard>
  );
}

/** Shown once OIDC is connected.
 *
 *  It also offers to complete the registration when the stored MAS client is
 *  missing a field the current version writes. Without this the repair would be
 *  unreachable: the connect card is replaced the moment OIDC is on, and the
 *  operator would be left hand-editing YAML to stop MAS asking
 *  "Continue to <ULID>?" — the exact task this product exists to remove (E30).
 */
function ConnectedCard({ missing, masHost, onDone }: { missing: string[]; masHost?: string; onDone: () => void }) {
  const [message, setMessage] = useState<string | null>(null);

  const repair = useMutation({
    mutationFn: () => api.post<{ changed: string[]; message: string }>("/api/v1/setup/connect-oidc", {
      issuer: masHost ? `https://${masHost}` : window.location.origin,
      public_url: window.location.origin,
    }),
    onSuccess: (res) => { setMessage(res.message); onDone(); },
  });

  if (missing.length === 0) {
    return (
      <Card style={{ display: "flex", alignItems: "center", gap: 12, background: "color-mix(in oklch, var(--status-ok) 10%, var(--surface))", borderColor: "color-mix(in oklch, var(--status-ok) 30%, var(--border))" }}>
        <Icon name="check" size={20} style={{ color: "var(--status-ok)" }} />
        <span style={{ flex: 1, fontSize: 13, color: "var(--text)" }}>
          {message ?? "Alles verbunden — MatrixCtrl verwaltet dein ESS-Deployment."}
        </span>
      </Card>
    );
  }

  return (
    <Card style={{ display: "flex", alignItems: "flex-start", gap: 12, flexWrap: "wrap", background: "color-mix(in oklch, var(--status-warn) 10%, var(--surface))", borderColor: "color-mix(in oklch, var(--status-warn) 30%, var(--border))" }}>
      <Icon name="alert" size={20} style={{ color: "var(--status-warn)", marginTop: 1 }} />
      <div style={{ flex: 1, minWidth: 240 }}>
        <div style={{ fontSize: 13, color: "var(--text)" }}>
          <strong>Die MAS-Registrierung ist unvollständig.</strong>{" "}
          Es fehlt: <code style={{ fontFamily: "var(--mono)" }}>{missing.join(", ")}</code>.
        </div>
        <div style={{ fontSize: 12, color: "var(--text-dim)", marginTop: 4, lineHeight: 1.55 }}>
          Ohne <code style={{ fontFamily: "var(--mono)" }}>client_name</code> fragt MAS beim Anmelden
          „Continue to &lt;ID&gt;?“ statt nach MatrixCtrl. Ergänzen ändert nur das fehlende Feld —
          Client-ID und Secret bleiben unangetastet.
        </div>
        {message && <div style={{ fontSize: 12, color: "var(--status-ok)", marginTop: 8 }}>{message}</div>}
        {repair.isError && <div style={{ fontSize: 12, color: "var(--status-err)", marginTop: 8 }}>{(repair.error as Error).message}</div>}
      </div>
      <Button variant="soft" size="sm" icon={repair.isPending ? undefined : "key"} disabled={repair.isPending} onClick={() => repair.mutate()}>
        {repair.isPending ? <><Spinner size={13} /> Ergänze…</> : "Registrierung vervollständigen"}
      </Button>
    </Card>
  );
}


/** A value the product worked out, shown with where it came from.
 *
 *  The difference between this and an input box is the difference between telling
 *  someone something and asking them something they cannot answer. */

/** Rebuilding a server from an archive.
 *
 *  The path that did not exist. Migrating meant: deploy ESS by hand, guess which
 *  version the old one ran, then find the backup page and upload there. The order was
 *  written down nowhere, and the archive knew the answer to the guess the whole time —
 *  the preview even displayed it (etappe 82).
 *
 *  It reverses the order of the greenfield wizard: the archive first, and everything
 *  else derived from it. */

/** The first question, which used to not be asked.
 *
 *  Setup showed a deploy wizard or an adopt card depending on what it found. An
 *  operator moving from another server fitted neither: they had to deploy a homeserver
 *  they did not want yet, guess its version, and then find the backup page. */

/** Where you are in the setup, and what is left.
 *
 *  There used to be a checklist under the wizard with the same three facts. A list of
 *  what is true is not the same as a position in a sequence: it says "OIDC: nein"
 *  where the operator needs "you are here, this is the last step". Replaced rather
 *  than added to — two renderings of one truth drift, and the operator has to work out
 *  which one is the real one.
 *
 *  The state comes entirely from what /setup/status already reported. Nothing new is
 *  tracked, so this cannot disagree with the cluster. */
function SetupSteps({ data }: { data: SetupStatus }) {
  const steps: { icon: IconName; title: string; done: boolean; detail: string }[] = [
    {
      icon: "server",
      title: "Homeserver",
      done: data.ess_installed,
      detail: data.ess_installed
        ? `Release „${data.ess_release}" v${data.ess_version ?? "?"} (${data.ess_status ?? "?"}) in ${data.ess_namespace}`
        : `Noch kein Release „${data.ess_release}" in ${data.ess_namespace}`,
    },
    {
      icon: "sliders",
      title: "Konfiguration",
      done: data.config_sections > 0,
      detail: data.config_sections > 0
        ? `${data.config_sections} Sektions-Dateien im versionierten Config-Repo`
        : "Noch keine Konfiguration übernommen",
    },
    {
      icon: "key",
      title: "Matrix-Login",
      done: data.oidc_configured,
      detail: data.oidc_configured
        ? "Admin-only Login über MAS ist aktiv"
        : "Noch Bootstrap-Modus — du meldest dich lokal an",
    },
  ];
  // The current step is the first unfinished one. Everything after it is not yet
  // reachable, and saying so is the point: a step that is merely "not done" reads as
  // something forgotten.
  const current = steps.findIndex((s) => !s.done);

  return (
    <Card pad={false}>
      {steps.map((step, i) => {
        const state = step.done ? "done" : i === current ? "current" : "todo";
        const color = state === "done" ? "var(--status-ok)" : state === "current" ? "var(--accent)" : "var(--text-faint)";
        return (
          <div key={step.title} style={{ display: "flex", alignItems: "flex-start", gap: 12, padding: "14px 18px", borderTop: i ? "1px solid var(--border-soft)" : undefined, background: state === "current" ? "var(--accent-soft)" : undefined }}>
            <div style={{ display: "grid", placeItems: "center", width: 36, height: 36, borderRadius: "var(--radius-sm)", background: "var(--surface-2)", color, flexShrink: 0 }}>
              <Icon name={step.icon} size={17} />
            </div>
            <div style={{ flex: 1, minWidth: 0 }}>
              <div style={{ display: "flex", alignItems: "baseline", gap: 8, flexWrap: "wrap" }}>
                <span style={{ fontSize: 13.5, fontWeight: 600, color: "var(--text)" }}>{step.title}</span>
                <span style={{ fontSize: 11, fontFamily: "var(--mono)", color }}>
                  {state === "done" ? "erledigt" : state === "current" ? "du bist hier" : "danach"}
                </span>
              </div>
              <p style={{ margin: "2px 0 0", fontSize: 12, color: "var(--text-faint)" }}>{step.detail}</p>
            </div>
            <Icon name={step.done ? "check" : state === "current" ? "play" : "clock"} size={18} stroke={2.2} style={{ color, flexShrink: 0 }} />
          </div>
        );
      })}
    </Card>
  );
}

function StartChoice({ onPick }: { onPick: (m: "fresh" | "migrate") => void }) {
  const options: { id: "fresh" | "migrate"; icon: IconName; title: string; sub: string }[] = [
    { id: "fresh", icon: "rocket", title: "Neu aufsetzen", sub: "Einen frischen Homeserver ausrollen. Du brauchst eine Domain." },
    { id: "migrate", icon: "upload", title: "Von einem Backup umziehen", sub: "Archiv von einem anderen Server. Version, Server-Name und Konfiguration kommen daraus." },
  ];
  return (
    <WizardCard>
      <WizardHeader icon="sparkle" title="Was hast du vor?" sub="Hier läuft noch kein Homeserver" />
      <div style={{ padding: 18, display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(240px, 1fr))", gap: 12 }}>
        {options.map((o) => (
          <button key={o.id} onClick={() => onPick(o.id)}
            style={{ textAlign: "left", display: "flex", flexDirection: "column", gap: 8, padding: 16, background: "var(--surface-2)", border: "1px solid var(--border)", borderRadius: "var(--radius-sm)", cursor: "pointer", color: "var(--text)" }}>
            <span style={{ display: "flex", alignItems: "center", gap: 9 }}>
              <Icon name={o.icon} size={17} style={{ color: "var(--accent)" }} />
              <span style={{ fontSize: 14, fontWeight: 600 }}>{o.title}</span>
            </span>
            <span style={{ fontSize: 12.5, color: "var(--text-faint)", lineHeight: 1.55 }}>{o.sub}</span>
          </button>
        ))}
      </div>
    </WizardCard>
  );
}

function MigrateWizard({ release, namespace, onDone, onBack }: { release: string; namespace?: string; onDone: () => void; onBack: () => void }) {
  const [archive, setArchive] = useState<File | null>(null);
  const [manifest, setManifest] = useState<ArchiveManifest | null>(null);
  const [readErr, setReadErr] = useState<string | null>(null);
  const [reading, setReading] = useState(false);

  const [serverOverride, setServerOverride] = useState("");
  const [versionOverride, setVersionOverride] = useState("");
  const [dnsOk, setDnsOk] = useState(false);
  const [dnsAcknowledged, setDnsAcknowledged] = useState(false);

  const [deployId, setDeployId] = useState<string | null>(null);
  const [logs, setLogs] = useState<string[]>([]);
  const [deployDone, setDeployDone] = useState(false);
  const [deployStatus, setDeployStatus] = useState<string | null>(null);
  const logRef = useRef<HTMLDivElement>(null);

  const [restoring, setRestoring] = useState(false);
  const [restoreMsg, setRestoreMsg] = useState<string | null>(null);
  const [restoreErr, setRestoreErr] = useState<string | null>(null);

  const { data: versions } = useQuery({ queryKey: ["helm", "versions"], queryFn: () => api.get<ESSVersion[]>("/api/v1/helm/versions") });

  const serverName = serverOverride || manifest?.server_name || "";
  const archivedVersion = essVersion(manifest?.ess?.chart);
  const version = versionOverride || archivedVersion;
  const versionAvailable = !versions || !archivedVersion || versions.some((v) => v.version === archivedVersion);
  const validDomain = /^[a-z0-9.-]+\.[a-z]{2,}$/i.test(serverName);

  const pick = async (f: File | null) => {
    setArchive(f); setManifest(null); setReadErr(null);
    if (!f) return;
    setReading(true);
    try {
      // The file as the body, not multipart: readArchive() gunzips the request body
      // directly. A FormData envelope reaches it as "kein gültiges gzip-Archiv" —
      // which is what the first version of this did, and it would have failed on the
      // one path built to rescue an operator.
      setManifest(await api.upload<ArchiveManifest>("/api/v1/status/restore/preview", f));
    } catch (e) {
      setReadErr(e instanceof Error ? e.message : "Archiv unlesbar");
    } finally {
      setReading(false);
    }
  };

  const deploy = useMutation({
    mutationFn: () => api.post<DeployResponse>("/api/v1/setup/deploy-ess", { version, server_name: serverName }),
    onSuccess: (res) => { setDeployId(res.upgrade_id); setLogs([]); setDeployDone(false); setDeployStatus(null); },
  });

  // The restore runs by itself once the deploy succeeds — that is the whole point of
  // the path. It stays a visible, repeatable step rather than a hidden side effect: if
  // the tab is closed between the two, the button is still there afterwards.
  const runRestore = async () => {
    if (!archive) return;
    setRestoring(true); setRestoreErr(null); setRestoreMsg(null);
    try {
      const r = await api.upload<{ config_files: number; tables?: string[] }>("/api/v1/status/restore", archive);
      setRestoreMsg(`${r.config_files} Konfigurationsdateien und ${r.tables?.length ?? 0} Tabellen eingespielt.`);
      onDone();
    } catch (e) {
      setRestoreErr(e instanceof Error ? e.message : "Wiederherstellung fehlgeschlagen");
    } finally {
      setRestoring(false);
    }
  };

  useUpgradeStream(deployId, {
    onLog: (line) => { setLogs((p) => [...p, line]); setTimeout(() => logRef.current?.scrollTo({ top: logRef.current.scrollHeight, behavior: "smooth" }), 30); },
    onDone: (s) => {
      setDeployDone(true); setDeployStatus(s);
      if (s === "success") void runRestore();
    },
  });

  return (
    <WizardCard>
      <WizardHeader icon="upload" title="Von einem Backup umziehen"
        sub="Archiv zuerst — ESS-Version, Server-Name und Konfiguration kommen daraus" />

      {!deployId ? (
        <div style={{ padding: 18, display: "flex", flexDirection: "column", gap: 16 }}>
          <div>
            <label style={labelStyle}>Archiv</label>
            <input type="file" accept=".tar.gz,.tgz,application/gzip"
              onChange={(e) => void pick(e.target.files?.[0] ?? null)}
              style={{ ...inputStyle, padding: 8, fontSize: 12.5 }} />
            {reading && <span style={{ fontSize: 12, color: "var(--text-faint)" }}><Spinner size={12} /> Archiv wird gelesen…</span>}
            {readErr && <span style={{ fontSize: 12.5, color: "var(--status-err)" }}>{readErr}</span>}
          </div>

          {manifest && (
            <>
              <div style={{ display: "flex", flexDirection: "column", gap: 1, border: "1px solid var(--border)", borderRadius: "var(--radius-sm)", overflow: "hidden" }}>
                <DerivedValue label="ESS-Version" value={version || "—"}
                  source={archivedVersion ? "aus dem Archiv — wird genau so deployed" : "nicht im Archiv vermerkt — bitte wählen"} />
                <DerivedValue label="Server Name" value={serverName || "—"}
                  source={manifest.server_name ? "aus der archivierten Konfiguration" : "nicht im Archiv gefunden — bitte angeben"} />
                <DerivedValue label="Wird eingespielt" value={`${manifest.config_repo_files} Dateien · ${manifest.tables?.length ?? 0} Tabellen`}
                  source="nach dem Deploy, automatisch" />
                <DerivedValue label="Release" value={release} source={namespace ? `Namespace ${namespace}` : "Helm-Release"} />
              </div>

              {(!manifest.server_name || serverOverride) && (
                <div>
                  <label style={labelStyle}>Server Name</label>
                  <input value={serverOverride} onChange={(e) => setServerOverride(e.target.value.trim())}
                    placeholder={manifest.server_name || "example.com"} style={inputStyle} />
                </div>
              )}

              {!versionAvailable && (
                <div>
                  <label style={labelStyle}>ESS-Version</label>
                  <p style={{ margin: "0 0 6px", fontSize: 12, color: "var(--status-warn)" }}>
                    Version {archivedVersion} wird nicht mehr angeboten — bitte eine verfügbare wählen.
                  </p>
                  <select value={versionOverride} onChange={(e) => setVersionOverride(e.target.value)} style={inputStyle}>
                    <option value="">Version wählen…</option>
                    {versions?.map((v) => <option key={v.version} value={v.version}>{v.version}</option>)}
                  </select>
                </div>
              )}

              {validDomain && <DnsStep serverName={serverName} onReady={setDnsOk} />}

              {validDomain && !dnsOk && (
                <label style={{ display: "flex", alignItems: "flex-start", gap: 9, fontSize: 12.5, color: "var(--text-dim)", cursor: "pointer" }}>
                  <input type="checkbox" checked={dnsAcknowledged} onChange={(e) => setDnsAcknowledged(e.target.checked)} style={{ marginTop: 2 }} />
                  <span>Noch zeigen nicht alle Einträge hierher — trotzdem fortfahren.</span>
                </label>
              )}
            </>
          )}

          <div style={{ display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap" }}>
            <Button variant="primary" icon={deploy.isPending ? undefined : "rocket"}
              disabled={!manifest || !validDomain || !version || deploy.isPending || (!dnsOk && !dnsAcknowledged)}
              onClick={() => deploy.mutate()}>
              {deploy.isPending ? <><Spinner size={14} /> Deploye…</> : "ESS deployen und einspielen"}
            </Button>
            <button onClick={onBack} style={{ background: "none", border: "none", padding: 0, fontSize: 12, color: "var(--text-faint)", cursor: "pointer", textDecoration: "underline", textUnderlineOffset: 2 }}>
              doch neu aufsetzen
            </button>
            {deploy.isError && <span style={{ fontSize: 12, color: "var(--status-err)" }}>{(deploy.error as Error).message}</span>}
          </div>
        </div>
      ) : (
        <div style={{ padding: 18, display: "flex", flexDirection: "column", gap: 12 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
            <span style={{ fontSize: 13, fontWeight: 600, color: "var(--text)" }}>1 · Deploy</span>
            <StatusInline done={deployDone} status={deployStatus} map={DEPLOY_MAP} />
          </div>
          <LogTerm logs={logs} done={deployDone} logRef={logRef} />

          <div style={{ display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap", borderTop: "1px solid var(--border)", paddingTop: 12 }}>
            <span style={{ fontSize: 13, fontWeight: 600, color: "var(--text)" }}>2 · Einspielen</span>
            {restoring && <span style={{ fontSize: 12.5, color: "var(--text-dim)" }}><Spinner size={13} /> läuft…</span>}
            {restoreMsg && <span style={{ fontSize: 12.5, color: "var(--status-ok)" }}>{restoreMsg}</span>}
            {restoreErr && <span style={{ fontSize: 12.5, color: "var(--status-err)" }}>{restoreErr}</span>}
            {deployDone && !restoring && !restoreMsg && (
              <Button size="sm" icon="upload" onClick={() => void runRestore()}>
                {restoreErr ? "Nochmal einspielen" : "Jetzt einspielen"}
              </Button>
            )}
          </div>
          {restoreMsg && (
            <p style={{ margin: 0, fontSize: 12.5, color: "var(--text-dim)" }}>
              MatrixCtrl sollte jetzt neu gestartet werden, damit die eingespielte
              Konfiguration überall greift.
            </p>
          )}
        </div>
      )}
    </WizardCard>
  );
}

function DerivedValue({ label, value, source }: { label: string; value: string; source: string }) {
  return (
    <div style={{ display: "flex", alignItems: "baseline", justifyContent: "space-between", gap: 14, flexWrap: "wrap", padding: "11px 14px", background: "var(--surface-2)" }}>
      <span style={{ fontSize: 12.5, fontWeight: 600, color: "var(--text-dim)" }}>{label}</span>
      <span style={{ display: "flex", alignItems: "baseline", gap: 10, flexWrap: "wrap", minWidth: 0 }}>
        <code style={{ fontFamily: "var(--mono)", fontSize: 12.5, color: value ? "var(--text)" : "var(--status-warn)", wordBreak: "break-all" }}>{value || "—"}</code>
        <span style={{ fontSize: 11, color: "var(--text-faint)" }}>{source}</span>
      </span>
    </div>
  );
}


interface MatrixAdmins { available: boolean; reason?: string; admins: string[]; client_known: boolean }

/** The account that has to exist before Matrix login can be switched on.
 *
 *  A freshly deployed homeserver has none. Switching sign-in over to MAS at that point
 *  closes the local login and opens one nobody can pass — the operator who hit it saw
 *  only "Invalid credentials" from MAS and had no way past it (§4.88). MatrixCtrl could
 *  lock, unlock, deactivate, erase, promote and set passwords; it could not create the
 *  first account. Now it can. */

/** "Matrix login exists but its issuer is unreachable" — said out loud.
 *
 *  The backend has distinguished this from "this install uses local login" for a long
 *  time, and its comment explains exactly why the two must not look alike: they lead to
 *  opposite actions. Nothing displayed it. An operator whose MAS had moved kept a
 *  working session until the pod restarted, and then met a login screen with no
 *  explanation (§4.88).
 *
 *  A state that is only checked at startup is not a state, it is a memory — so this
 *  polls. */
export function MatrixLoginHealth() {
  const { data } = useQuery({
    queryKey: ["auth", "oidc", "available"],
    queryFn: () => api.get<{ enabled: boolean; retrying: boolean }>("/api/v1/auth/oidc/available"),
    refetchInterval: 30_000,
  });
  if (!data?.retrying) return null;

  return (
    <Card>
      <div style={{ display: "flex", gap: 12, alignItems: "flex-start" }}>
        <Icon name="alert" size={19} style={{ color: "var(--status-warn)", flexShrink: 0, marginTop: 1 }} />
        <div style={{ display: "flex", flexDirection: "column", gap: 8, minWidth: 0 }}>
          <strong style={{ fontSize: 13.5, color: "var(--text)" }}>
            Matrix-Login ist eingerichtet, antwortet aber nicht
          </strong>
          <p style={{ margin: 0, fontSize: 12.5, color: "var(--text-dim)" }}>
            MatrixCtrl versucht weiter, den Issuer zu erreichen. Solange das so ist,
            bleibt der lokale Admin-Login offen — du kommst also rein. Häufigste Ursache:
            der MAS-Hostname zeigt nicht mehr auf diesen Server.
          </p>
          <p style={{ margin: 0, fontSize: 12.5, color: "var(--text-dim)" }}>
            Dauerhaft auf den lokalen Login zurückstellen:
          </p>
          <pre style={{ margin: 0, padding: 10, fontSize: 11.5, fontFamily: "var(--mono)", background: "var(--bg)", border: "1px solid var(--border)", borderRadius: "var(--radius-sm)", overflowX: "auto", whiteSpace: "pre-wrap", wordBreak: "break-all", userSelect: "all" }}>
            bash &lt;(curl -fsSL https://raw.githubusercontent.com/bxnnyg/matrixctrl/master/scripts/install.sh) recover-login
          </pre>
        </div>
      </div>
    </Card>
  );
}

function MatrixAccountCard({ onDone }: { onDone: () => void }) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [msg, setMsg] = useState<string | null>(null);

  const { data, refetch, isLoading } = useQuery({
    queryKey: ["setup", "matrix-admins"],
    queryFn: () => api.get<MatrixAdmins>("/api/v1/setup/matrix-admins"),
    refetchInterval: 20_000,
  });

  const create = useMutation({
    mutationFn: () => api.post<{ username: string }>("/api/v1/setup/matrix-admin", { username, password }),
    onSuccess: (res) => {
      setMsg(`${res.username} angelegt und zum Admin gemacht.`);
      setUsername(""); setPassword("");
      void refetch(); onDone();
    },
  });

  const admins = data?.admins ?? [];

  return (
    <WizardCard>
      <WizardHeader icon="users" title="Matrix-Konto für die Anmeldung"
        sub="Ohne ein Admin-Konto in MAS ist der Umstieg auf Matrix-Login eine Tür ohne jemanden dahinter" />
      <div style={{ padding: 18, display: "flex", flexDirection: "column", gap: 14 }}>
        {isLoading ? (
          <span style={{ fontSize: 12.5, color: "var(--text-faint)" }}><Spinner size={13} /> frage MAS…</span>
        ) : !data?.available ? (
          <p style={{ margin: 0, fontSize: 12.5, color: "var(--status-warn)" }}>
            MAS ist noch nicht erreichbar{data?.reason ? `: ${data.reason}` : ""}. Das ist normal, solange
            der Client noch nicht geladen ist — „Verbinden" unten kümmert sich darum.
          </p>
        ) : admins.length > 0 ? (
          <p style={{ margin: 0, fontSize: 13, color: "var(--text-dim)" }}>
            Es gibt {admins.length === 1 ? "ein Admin-Konto" : `${admins.length} Admin-Konten`}:{" "}
            <strong style={{ fontFamily: "var(--mono)" }}>{admins.join(", ")}</strong>. Damit kannst du dich
            nach dem Umstellen anmelden.
          </p>
        ) : (
          <>
            <p style={{ margin: 0, fontSize: 13, color: "var(--text-dim)" }}>
              MAS hat noch kein Admin-Konto. Leg jetzt eines an — <strong>bevor</strong> du
              die Anmeldung umstellst.
            </p>
            <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 16 }} className="mc-dash-grid">
              <div>
                <label style={labelStyle}>Benutzername</label>
                <input value={username} onChange={(e) => setUsername(e.target.value.trim())} placeholder="admin" style={inputStyle} />
              </div>
              <div>
                <label style={labelStyle}>Passwort</label>
                <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} style={inputStyle} />
              </div>
            </div>
          </>
        )}
        {data?.available && admins.length === 0 && (
          <div style={{ display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap" }}>
            <Button variant="primary" icon={create.isPending ? undefined : "users"}
              disabled={!username || !password || create.isPending}
              onClick={() => create.mutate()}>
              {create.isPending ? <><Spinner size={14} /> Lege an…</> : "Konto anlegen und zum Admin machen"}
            </Button>
            {create.isError && <span style={{ fontSize: 12, color: "var(--status-err)" }}>{(create.error as Error).message}</span>}
          </div>
        )}
        {msg && <span style={{ fontSize: 12.5, color: "var(--status-ok)" }}>{msg}</span>}
      </div>
    </WizardCard>
  );
}

function ConnectCard({ masHost, onDone }: { masHost?: string; onDone: () => void }) {
  // Derived, not initial state.
  //
  // These used to be useState(masHost ? … : "") — which reads the prop once, on the
  // render that happens to mount the card. Arrive a moment later, as the value does
  // when the config is written by the deploy that just finished, and the field stays
  // empty for the rest of the session with no way to tell why.
  const derivedIssuer = masHost ? `https://${masHost}` : "";
  const derivedPublicUrl = window.location.origin;

  // Overrides start unset: the values above are facts, and the operator only sees a
  // form if they say they want one.
  const [override, setOverride] = useState(false);
  const [issuerOverride, setIssuerOverride] = useState("");
  const [publicUrlOverride, setPublicUrlOverride] = useState("");
  const issuer = override ? issuerOverride : derivedIssuer;
  const publicUrl = override ? publicUrlOverride : derivedPublicUrl;

  const [runId, setRunId] = useState<string | null>(null);
  const [logs, setLogs] = useState<string[]>([]);
  const [done, setDone] = useState(false);
  const [status, setStatus] = useState<string | null>(null);
  const logRef = useRef<HTMLDivElement>(null);

  const connect = useMutation({
    mutationFn: () => api.post<DeployResponse>("/api/v1/setup/connect-oidc", { issuer, public_url: publicUrl }),
    onSuccess: (res) => { setRunId(res.upgrade_id); setLogs([]); setDone(false); setStatus(null); },
  });
  useUpgradeStream(runId, {
    onLog: (line) => { setLogs((p) => [...p, line]); setTimeout(() => logRef.current?.scrollTo({ top: logRef.current.scrollHeight, behavior: "smooth" }), 30); },
    onDone: (s) => { setDone(true); setStatus(s); if (s === "success") onDone(); },
  });

  return (
    <WizardCard>
      <WizardHeader icon="key" title="Matrix-Login verbinden" sub="Registriert MatrixCtrl als OIDC-Client in MAS — automatisch, kein manuelles Patchen" />
      {!runId ? (
        <div style={{ padding: 18, display: "flex", flexDirection: "column", gap: 16 }}>
          {/* Two facts, not two questions.
              An operator who let MatrixCtrl deploy their homeserver cannot evaluate a
              blank field labelled "MAS URL" — they never chose the hostname, so being
              asked for it is being asked to confirm something they have no way to
              check. Both values are known here; they are shown with where they came
              from, and editing them is a deliberate act. */}
          {!override ? (
            <div style={{ display: "flex", flexDirection: "column", gap: 1, border: "1px solid var(--border)", borderRadius: "var(--radius-sm)", overflow: "hidden" }}>
              <DerivedValue label="MAS (Issuer)" value={derivedIssuer}
                source={derivedIssuer ? "aus deiner ESS-Konfiguration" : "steht nicht in der Konfiguration — bitte selbst angeben"} />
              <DerivedValue label="MatrixCtrl-Adresse" value={derivedPublicUrl}
                source="die Adresse, unter der du gerade bist" />
            </div>
          ) : (
            <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 16 }} className="mc-dash-grid">
              <div>
                <label style={labelStyle}>MAS URL (Issuer)</label>
                <input value={issuerOverride} onChange={(e) => setIssuerOverride(e.target.value)} placeholder={derivedIssuer || "https://mas.example.com"} style={inputStyle} />
              </div>
              <div>
                <label style={labelStyle}>MatrixCtrl URL</label>
                <input value={publicUrlOverride} onChange={(e) => setPublicUrlOverride(e.target.value)} placeholder={derivedPublicUrl} style={inputStyle} />
              </div>
            </div>
          )}
          <div style={{ display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap" }}>
            <Button variant="primary" icon={connect.isPending ? undefined : "key"} disabled={!issuer || !publicUrl || connect.isPending} onClick={() => connect.mutate()}>{connect.isPending ? <><Spinner size={14} /> Verbinde…</> : "Verbinden"}</Button>
            <button onClick={() => { setOverride((v) => !v); if (!override) { setIssuerOverride(derivedIssuer); setPublicUrlOverride(derivedPublicUrl); } }}
              style={{ background: "none", border: "none", padding: 0, fontSize: 12, color: "var(--text-faint)", cursor: "pointer", textDecoration: "underline", textUnderlineOffset: 2 }}>
              {override ? "Vorgaben verwenden" : "abweichend konfigurieren"}
            </button>
            <span style={{ fontSize: 11.5, color: "var(--text-faint)" }}>Schreibt den Client in die MAS-Config + helm upgrade ess + schaltet auf OIDC um</span>
            {connect.isError && <span style={{ fontSize: 12, color: "var(--status-err)" }}>{(connect.error as Error).message}</span>}
          </div>
        </div>
      ) : (
        <div style={{ padding: 18, display: "flex", flexDirection: "column", gap: 12 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
            <span style={{ fontSize: 13, fontWeight: 600, color: "var(--text)" }}>Verbinde…</span>
            <StatusInline done={done} status={status} map={{ success: ["Verbunden", "ok"], "hooks-failed": ["Teilweise", "warn"], "needs-account": ["Kein Matrix-Konto", "warn"], failed: ["Fehlgeschlagen", "err"] }} />
          </div>
          <LogTerm logs={logs} done={done} logRef={logRef} />
          {done && status === "success" && <p style={{ margin: 0, fontSize: 12, color: "var(--status-ok)" }}>Abmelden und über Matrix neu anmelden.</p>}
        </div>
      )}
    </WizardCard>
  );
}


interface DnsRecord {
  /** Identifies the record in the deploy's hostname map — the editor is built from
   *  this rather than from a second list in the frontend, which would drift. */
  key?: string;
  type: string;
  name: string;
  purpose: string;
  want: string[] | null;
  resolved: string[] | null;
  status: "ok" | "elsewhere" | "missing" | "unknown";
  detail?: string;
}
interface DnsResponse {
  server_name: string;
  target: { addresses: string[] | null; source: string; usable: boolean; note?: string };
  records: DnsRecord[];
  all_ok: boolean;
}

const DNS_TONE: Record<DnsRecord["status"], { dot: "ok" | "warn" | "err" | "idle"; label: string }> = {
  ok:        { dot: "ok",   label: "zeigt hierher" },
  elsewhere: { dot: "warn", label: "zeigt woanders" },
  missing:   { dot: "err",  label: "nicht gefunden" },
  // Not an error about the record — an error about the lookup. Saying "missing" here
  // sends people to fix DNS that is already correct.
  unknown:   { dot: "idle", label: "nicht prüfbar" },
};

/** The step that did not exist.
 *
 *  All an operator got about the one part of the install nobody else can do for them
 *  was an eleven-pixel line naming five prefixes — and it named five of the six,
 *  because well-known delegation is served at the server name itself. */
function DnsStep({ serverName, overrides, onReady, onOverride }: {
  serverName: string;
  overrides?: Record<string, string>;
  onReady: (allOk: boolean) => void;
  /** Absent means the names are not editable here — the migration path takes them from
   *  the archive, where changing one would contradict the configuration being restored. */
  onOverride?: (key: string, value: string) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [manualIp, setManualIp] = useState("");
  const [appliedIp, setAppliedIp] = useState("");
  const [copied, setCopied] = useState<string | null>(null);

  const q = new URLSearchParams({ server_name: serverName });
  if (appliedIp) q.set("target", appliedIp);
  for (const [k, v] of Object.entries(overrides ?? {})) {
    if (v.trim()) q.append("override", `${k}=${v.trim()}`);
  }
  const { data, isFetching, refetch, error } = useQuery({
    queryKey: ["setup", "dns", serverName, appliedIp, JSON.stringify(overrides ?? {})],
    queryFn: async () => {
      const r = await api.get<DnsResponse>(`/api/v1/setup/dns?${q.toString()}`);
      onReady(r.all_ok);
      return r;
    },
    enabled: !!serverName,
    staleTime: 15_000,
  });

  const copy = (text: string) => {
    navigator.clipboard?.writeText(text).then(() => { setCopied(text); setTimeout(() => setCopied(null), 1600); }).catch(() => {});
  };

  const target = data?.target;
  const value = target?.addresses?.[0] ?? "—";

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12, flexWrap: "wrap" }}>
        <label style={{ ...labelStyle, marginBottom: 0 }}>Diese DNS-Einträge müssen existieren</label>
        <span style={{ display: "flex", alignItems: "center", gap: 8 }}>
          {onOverride && (
            <button onClick={() => setEditing((v) => !v)}
              style={{ background: "none", border: "none", padding: 0, fontSize: 12, color: "var(--text-faint)", cursor: "pointer", textDecoration: "underline", textUnderlineOffset: 2 }}>
              {editing ? "fertig" : "Namen anpassen"}
            </button>
          )}
          <Button size="sm" variant="ghost" icon="refresh" onClick={() => void refetch()} disabled={isFetching}>
            {isFetching ? <><Spinner size={13} /> Prüfe…</> : "Erneut prüfen"}
          </Button>
        </span>
      </div>

      {target && !target.usable && (
        <div style={{ display: "flex", flexDirection: "column", gap: 8, padding: 12, borderRadius: "var(--radius-sm)", background: "var(--surface-2)", border: "1px solid var(--border)" }}>
          <span style={{ fontSize: 12.5, color: "var(--text-dim)" }}>{target.note}</span>
          <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
            <input value={manualIp} onChange={(e) => setManualIp(e.target.value.trim())} placeholder="203.0.113.10"
              style={{ ...inputStyle, width: 190, fontFamily: "var(--mono)" }} />
            <Button size="sm" disabled={!manualIp} onClick={() => setAppliedIp(manualIp)}>Adresse übernehmen</Button>
          </div>
        </div>
      )}

      {error && <span style={{ fontSize: 12.5, color: "var(--status-err)" }}>{(error as Error).message}</span>}

      <div style={{ overflowX: "auto", border: "1px solid var(--border)", borderRadius: "var(--radius-sm)" }}>
        <table style={{ borderCollapse: "collapse", width: "100%", fontSize: 12.5 }}>
          <thead>
            <tr>
              {["Typ", "Name", "Wert", "Status"].map((h) => (
                <th key={h} style={{ textAlign: "left", padding: "9px 12px", fontSize: 11, fontWeight: 600, letterSpacing: "0.05em", textTransform: "uppercase", color: "var(--text-faint)", borderBottom: "1px solid var(--border)", whiteSpace: "nowrap" }}>{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {(data?.records ?? []).map((r) => {
              const tone = DNS_TONE[r.status];
              return (
                <tr key={r.name}>
                  <td style={dnsCell}>{r.type}</td>
                  <td style={dnsCell}>
                    {editing && onOverride && r.key ? (
                      <input value={overrides?.[r.key] ?? r.name}
                        onChange={(e) => onOverride(r.key!, e.target.value.trim())}
                        style={{ ...inputStyle, padding: "5px 8px", fontSize: 12.5, fontFamily: "var(--mono)", minWidth: 210 }} />
                    ) : (
                    <button onClick={() => copy(r.name)} title="Namen kopieren"
                      style={{ background: "none", border: "none", padding: 0, color: "var(--text)", fontFamily: "var(--mono)", fontSize: 12.5, cursor: "pointer", textAlign: "left" }}>
                      {copied === r.name ? "kopiert" : r.name}
                    </button>
                    )}
                    <div style={{ fontSize: 11, color: "var(--text-faint)", fontFamily: "var(--font)", marginTop: 2, maxWidth: 340 }}>{r.purpose}</div>
                  </td>
                  <td style={dnsCell}>{value}</td>
                  <td style={{ ...dnsCell, whiteSpace: "nowrap" }}>
                    <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
                      <StatusDot status={tone.dot} size={7} />
                      <span style={{ color: "var(--text-dim)", fontFamily: "var(--font)" }}>{tone.label}</span>
                    </span>
                    {r.resolved?.length ? (
                      <div style={{ fontSize: 11, color: "var(--text-faint)", marginTop: 2 }}>{r.resolved.join(", ")}</div>
                    ) : null}
                    {r.status === "unknown" && r.detail && (
                      <div style={{ fontSize: 11, color: "var(--text-faint)", marginTop: 2, fontFamily: "var(--font)", maxWidth: 260 }}>{r.detail}</div>
                    )}
                  </td>
                </tr>
              );
            })}
            {!data && (
              <tr><td colSpan={4} style={{ ...dnsCell, color: "var(--text-faint)", fontFamily: "var(--font)" }}>
                {isFetching ? "Einträge werden geprüft…" : "—"}
              </td></tr>
            )}
          </tbody>
        </table>
      </div>

      <p style={{ margin: 0, fontSize: 11.5, color: "var(--text-faint)" }}>
        DNS-Änderungen brauchen Zeit. Du kannst trotzdem deployen — die Dienste sind
        erreichbar, sobald die Einträge greifen.
      </p>
    </div>
  );
}

const dnsCell: React.CSSProperties = { padding: "10px 12px", borderBottom: "1px solid var(--border-soft)", verticalAlign: "top", fontFamily: "var(--mono)", color: "var(--text-dim)" };

function DeployWizard({ release, namespace, onDone, onMigrate }: { release: string; namespace?: string; onDone: () => void; onMigrate?: () => void }) {
  const [serverName, setServerName] = useState("");
  const [version, setVersion] = useState("");
  // Whether every record already points here. It never blocks the deploy — DNS
  // propagation takes time, and a wizard that insists is a wizard people work around.
  // It only decides whether the button warns first.
  const [dnsOk, setDnsOk] = useState(false);
  const [dnsAcknowledged, setDnsAcknowledged] = useState(false);
  // Per-record hostname overrides, keyed the way the deploy keys them. The derivation
  // stays the default; it stopped being a rule (etappe 89).
  const [hostOverrides, setHostOverrides] = useState<Record<string, string>>({});
  const [deployId, setDeployId] = useState<string | null>(null);
  const [logs, setLogs] = useState<string[]>([]);
  const [done, setDone] = useState(false);
  const [status, setStatus] = useState<string | null>(null);
  const logRef = useRef<HTMLDivElement>(null);

  const { data: versions } = useQuery({ queryKey: ["helm", "versions"], queryFn: () => api.get<ESSVersion[]>("/api/v1/helm/versions") });
  const deploy = useMutation({
    mutationFn: () => api.post<DeployResponse>("/api/v1/setup/deploy-ess", { version, server_name: serverName, hostnames: hostOverrides }),
    onSuccess: (res) => { setDeployId(res.upgrade_id); setLogs([]); setDone(false); setStatus(null); },
  });
  useUpgradeStream(deployId, {
    onLog: (line) => { setLogs((p) => [...p, line]); setTimeout(() => logRef.current?.scrollTo({ top: logRef.current.scrollHeight, behavior: "smooth" }), 30); },
    onDone: (s) => { setDone(true); setStatus(s); if (s === "success") onDone(); },
  });

  const validDomain = /^[a-z0-9.-]+\.[a-z]{2,}$/i.test(serverName);

  return (
    <WizardCard>
      <WizardHeader icon="rocket" title="ESS deployen" sub={`Greenfield — Release „${release}" ist noch nicht installiert`} />
      {!deployId ? (
        <div style={{ padding: 18, display: "flex", flexDirection: "column", gap: 16 }}>
          <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 16 }} className="mc-dash-grid">
            <div>
              <label style={labelStyle}>Server Name</label>
              <input value={serverName} onChange={(e) => setServerName(e.target.value)} placeholder="example.com" style={inputStyle} />
              <p style={{ margin: "6px 0 0", fontSize: 11, color: "var(--text-faint)" }}>Die nötigen DNS-Einträge stehen unten, sobald der Name gültig ist.</p>
            </div>
            <div>
              <label style={labelStyle}>ESS-Version</label>
              <select value={version} onChange={(e) => setVersion(e.target.value)} style={inputStyle}>
                <option value="">Version wählen…</option>
                {versions?.map((v) => <option key={v.version} value={v.version}>{v.version}</option>)}
              </select>
            </div>
          </div>
          {validDomain && (
            <DnsStep serverName={serverName} overrides={hostOverrides} onReady={setDnsOk}
              onOverride={(key, value) => setHostOverrides((o) => ({ ...o, [key]: value }))} />
          )}

          {/* What is about to happen, before it happens.
              "wiso sollte ich die angeben wenn ich keine ahnung habe wie matrixctrl
              die deployed und auf welche url" — the wizard never said what it was
              about to create, so every value it asked for later arrived without a
              context in which it could be judged. */}
          {validDomain && version && (
            <div style={{ display: "flex", flexDirection: "column", gap: 1, border: "1px solid var(--border)", borderRadius: "var(--radius-sm)", overflow: "hidden" }}>
              <DerivedValue label="Chart" value={`matrix-stack ${version}`} source="Element Server Suite" />
              <DerivedValue label="Release" value={release} source={namespace ? `Namespace ${namespace}` : "Helm-Release"} />
              <DerivedValue label="Hostnames" value={`${serverName} und 5 weitere`} source="unten aufgelistet, mit Prüfung" />
            </div>
          )}

          {validDomain && !dnsOk && (
            <label style={{ display: "flex", alignItems: "flex-start", gap: 9, fontSize: 12.5, color: "var(--text-dim)", cursor: "pointer" }}>
              <input type="checkbox" checked={dnsAcknowledged} onChange={(e) => setDnsAcknowledged(e.target.checked)} style={{ marginTop: 2 }} />
              <span>Noch zeigen nicht alle Einträge hierher — trotzdem deployen. Die Dienste
              werden erreichbar, sobald das DNS greift.</span>
            </label>
          )}

          <div style={{ display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap" }}>
            <Button variant="primary" icon={deploy.isPending ? undefined : "rocket"} disabled={!validDomain || !version || deploy.isPending || (!dnsOk && !dnsAcknowledged)} onClick={() => deploy.mutate()}>{deploy.isPending ? <><Spinner size={14} /> Deploye…</> : "ESS deployen"}</Button>
            {onMigrate && (
              <button onClick={onMigrate} style={{ background: "none", border: "none", padding: 0, fontSize: 12, color: "var(--text-faint)", cursor: "pointer", textDecoration: "underline", textUnderlineOffset: 2 }}>
                Ich habe ein Backup
              </button>
            )}
            {serverName && !validDomain && <span style={{ fontSize: 12, color: "var(--status-warn)" }}>Bitte eine gültige Domain eingeben</span>}
            {deploy.isError && <span style={{ fontSize: 12, color: "var(--status-err)" }}>{(deploy.error as Error).message}</span>}
          </div>
        </div>
      ) : (
        <div style={{ padding: 18, display: "flex", flexDirection: "column", gap: 12 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
            <span style={{ fontSize: 13, fontWeight: 600, color: "var(--text)" }}>Deploy</span>
            <StatusInline done={done} status={status} map={DEPLOY_MAP} />
          </div>
          <LogTerm logs={logs} done={done} logRef={logRef} />
        </div>
      )}
    </WizardCard>
  );
}
