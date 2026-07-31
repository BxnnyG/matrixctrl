import { createFileRoute } from "@tanstack/react-router";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, useRef, type ReactNode, type RefObject } from "react";
import { api } from "@/lib/api";
import { useUpgradeStream } from "@/lib/ws";
import { Card, Icon, Button, Spinner } from "@/components/mc";

export const Route = createFileRoute("/setup")({
  component: Setup,
});

interface SetupStatus {
  ess_namespace: string;
  ess_release: string;
  ess_installed: boolean;
  ess_version?: string;
  ess_status?: string;
  oidc_configured: boolean;
  bootstrap_active: boolean;
  config_sections: number;
  mas_host?: string;
}
interface ESSVersion { version: string }
interface DeployResponse { upgrade_id: string }

const inputStyle: React.CSSProperties = { width: "100%", padding: "9px 12px", border: "1px solid var(--border)", background: "var(--surface-2)", color: "var(--text)", borderRadius: "var(--radius-sm)", fontSize: 13.5, fontFamily: "var(--font)" };
const labelStyle: React.CSSProperties = { display: "block", fontSize: 12.5, fontWeight: 600, color: "var(--text-dim)", marginBottom: 6 };

function WizardHeader({ icon, title, sub }: { icon: string; title: string; sub: string }) {
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

function Row({ ok, warn, icon, title, detail }: { ok: boolean; warn?: boolean; icon: string; title: string; detail: string; }) {
  const tone = ok ? "ok" : warn ? "warn" : "err";
  return (
    <div style={{ display: "flex", alignItems: "flex-start", gap: 12, padding: "14px 18px" }}>
      <div style={{ display: "grid", placeItems: "center", width: 36, height: 36, borderRadius: "var(--radius-sm)", background: "var(--surface-2)", color: "var(--text-dim)", flexShrink: 0 }}><Icon name={icon} size={17} /></div>
      <div style={{ flex: 1, minWidth: 0 }}>
        <div style={{ fontSize: 13.5, fontWeight: 600, color: "var(--text)" }}>{title}</div>
        <p style={{ margin: "2px 0 0", fontSize: 12, color: "var(--text-faint)" }}>{detail}</p>
      </div>
      <Icon name={ok ? "check" : warn ? "alert" : "x"} size={18} stroke={2.2} style={{ color: `var(--status-${tone})`, flexShrink: 0 }} />
    </div>
  );
}

function Setup() {
  const qc = useQueryClient();
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
      {!data.ess_installed ? (
        <DeployWizard release={data.ess_release} onDone={invalidate} />
      ) : data.config_sections === 0 ? (
        <AdoptCard release={data.ess_release} version={data.ess_version} onDone={invalidate} />
      ) : !data.oidc_configured ? (
        <ConnectCard masHost={data.mas_host} onDone={invalidate} />
      ) : (
        <Card style={{ display: "flex", alignItems: "center", gap: 12, background: "color-mix(in oklch, var(--status-ok) 10%, var(--surface))", borderColor: "color-mix(in oklch, var(--status-ok) 30%, var(--border))" }}>
          <Icon name="check" size={20} style={{ color: "var(--status-ok)" }} />
          <span style={{ fontSize: 13, color: "var(--text)" }}>Alles verbunden — MatrixCtrl verwaltet dein ESS-Deployment.</span>
        </Card>
      )}

      <Card pad={false}>
        <Row ok={data.ess_installed} icon="server" title="ESS deployed"
          detail={data.ess_installed ? `Release „${data.ess_release}" v${data.ess_version ?? "?"} (${data.ess_status ?? "?"}) in ${data.ess_namespace}` : `Kein Release „${data.ess_release}" in ${data.ess_namespace}`} />
        <div style={{ borderTop: "1px solid var(--border-soft)" }} />
        <Row ok={data.config_sections > 0} icon="sliders" title="Config-Sektionen" detail={`${data.config_sections} Sektions-Dateien im versionierten Config-Repo`} />
        <div style={{ borderTop: "1px solid var(--border-soft)" }} />
        <Row ok={data.oidc_configured} warn={!data.oidc_configured} icon="key" title="Matrix-Login (OIDC)"
          detail={data.oidc_configured ? "Admin-only Login via MAS ist aktiv" : "Bootstrap-Modus (lokaler Admin) — nach ESS-Deploy auf OIDC umschalten"} />
      </Card>

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

function ConnectCard({ masHost, onDone }: { masHost?: string; onDone: () => void }) {
  const [issuer, setIssuer] = useState(masHost ? `https://${masHost}` : "");
  const [publicUrl, setPublicUrl] = useState(window.location.origin);
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
          <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 16 }} className="mc-dash-grid">
            <div>
              <label style={labelStyle}>MAS URL (Issuer)</label>
              <input value={issuer} onChange={(e) => setIssuer(e.target.value)} placeholder="https://mas.example.com" style={inputStyle} />
            </div>
            <div>
              <label style={labelStyle}>MatrixCtrl URL</label>
              <input value={publicUrl} onChange={(e) => setPublicUrl(e.target.value)} style={inputStyle} />
            </div>
          </div>
          <div style={{ display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap" }}>
            <Button variant="primary" icon={connect.isPending ? undefined : "key"} disabled={!issuer || !publicUrl || connect.isPending} onClick={() => connect.mutate()}>{connect.isPending ? <><Spinner size={14} /> Verbinde…</> : "Verbinden"}</Button>
            <span style={{ fontSize: 11.5, color: "var(--text-faint)" }}>Schreibt den Client in die MAS-Config + helm upgrade ess + schaltet auf OIDC um</span>
            {connect.isError && <span style={{ fontSize: 12, color: "var(--status-err)" }}>{(connect.error as Error).message}</span>}
          </div>
        </div>
      ) : (
        <div style={{ padding: 18, display: "flex", flexDirection: "column", gap: 12 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
            <span style={{ fontSize: 13, fontWeight: 600, color: "var(--text)" }}>Verbinde…</span>
            <StatusInline done={done} status={status} map={{ success: ["Verbunden", "ok"], "hooks-failed": ["Teilweise", "warn"], failed: ["Fehlgeschlagen", "err"] }} />
          </div>
          <LogTerm logs={logs} done={done} logRef={logRef} />
          {done && status === "success" && <p style={{ margin: 0, fontSize: 12, color: "var(--status-ok)" }}>Abmelden und über Matrix neu anmelden.</p>}
        </div>
      )}
    </WizardCard>
  );
}

function DeployWizard({ release, onDone }: { release: string; onDone: () => void }) {
  const [serverName, setServerName] = useState("");
  const [version, setVersion] = useState("");
  const [deployId, setDeployId] = useState<string | null>(null);
  const [logs, setLogs] = useState<string[]>([]);
  const [done, setDone] = useState(false);
  const [status, setStatus] = useState<string | null>(null);
  const logRef = useRef<HTMLDivElement>(null);

  const { data: versions } = useQuery({ queryKey: ["helm", "versions"], queryFn: () => api.get<ESSVersion[]>("/api/v1/helm/versions") });
  const deploy = useMutation({
    mutationFn: () => api.post<DeployResponse>("/api/v1/setup/deploy-ess", { version, server_name: serverName }),
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
              <p style={{ margin: "6px 0 0", fontSize: 11, color: "var(--text-faint)" }}>Hostnames werden abgeleitet: matrix., mas., element., admin., mrtc.</p>
            </div>
            <div>
              <label style={labelStyle}>ESS-Version</label>
              <select value={version} onChange={(e) => setVersion(e.target.value)} style={inputStyle}>
                <option value="">Version wählen…</option>
                {versions?.map((v) => <option key={v.version} value={v.version}>{v.version}</option>)}
              </select>
            </div>
          </div>
          <div style={{ display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap" }}>
            <Button variant="primary" icon={deploy.isPending ? undefined : "rocket"} disabled={!validDomain || !version || deploy.isPending} onClick={() => deploy.mutate()}>{deploy.isPending ? <><Spinner size={14} /> Deploye…</> : "ESS deployen"}</Button>
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
