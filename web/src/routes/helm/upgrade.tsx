import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useRef, useState } from "react";
import { useQuery, useMutation } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { useUpgradeStream } from "@/lib/ws";
import { Card, Icon, Badge, Button, SectionTitle, Spinner } from "@/components/mc";

export const Route = createFileRoute("/helm/upgrade")({
  component: UpgradeWizard,
});

interface HelmRelease { chart_version: string; revision: number }
interface ESSVersion { version: string }
interface UpgradeResponse { upgrade_id: string }

const essVersion = (v: string) => v.replace(/^matrix-stack-/, "");

function UpgradeWizard() {
  const navigate = useNavigate();
  const logRef = useRef<HTMLDivElement>(null);
  const [selectedVersion, setSelectedVersion] = useState("");
  const [upgradeId, setUpgradeId] = useState<string | null>(null);
  const [logs, setLogs] = useState<string[]>([]);
  const [done, setDone] = useState(false);
  const [finalStatus, setFinalStatus] = useState<string | null>(null);

  const { data: current } = useQuery({
    queryKey: ["helm", "release"],
    queryFn: () => api.get<HelmRelease>("/api/v1/helm/releases/ess"),
  });
  const { data: versions } = useQuery({
    queryKey: ["helm", "versions"],
    queryFn: () => api.get<ESSVersion[]>("/api/v1/helm/versions"),
  });

  const upgrade = useMutation({
    mutationFn: (toVersion: string) =>
      api.post<UpgradeResponse>("/api/v1/helm/releases/ess/upgrade", { to_version: toVersion }),
    onSuccess: (res) => {
      setUpgradeId(res.upgrade_id);
      setLogs([]);
      setDone(false);
      setFinalStatus(null);
    },
  });

  useUpgradeStream(upgradeId, {
    onLog: (line) => {
      setLogs((prev) => [...prev, line]);
      setTimeout(() => logRef.current?.scrollTo({ top: logRef.current.scrollHeight, behavior: "smooth" }), 30);
    },
    onDone: (status) => {
      setDone(true);
      setFinalStatus(status);
      if (status === "success") setTimeout(() => navigate({ to: "/helm" }), 3000);
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
          <div>
            <Button variant="primary" icon="upload" disabled={!selectedVersion || upgrade.isPending} onClick={() => upgrade.mutate(selectedVersion)}>
              {upgrade.isPending ? <><Spinner size={14} /> Starte…</> : "Upgrade starten"}
            </Button>
          </div>
          {upgrade.isError && <div style={{ fontSize: 13, color: "var(--status-err)" }}>{(upgrade.error as Error).message}</div>}
        </Card>
      ) : (
        <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
          <div ref={logRef} className="mc-scroll" style={{ background: "oklch(0.13 0.005 256)", borderRadius: "var(--radius)", border: "1px solid var(--border)", padding: 16, fontFamily: "var(--mono)", fontSize: 12, color: "oklch(0.82 0.13 150)", minHeight: 200, maxHeight: 400, overflowY: "auto", lineHeight: 1.6 }}>
            {logs.map((line, i) => <div key={i}>{line}</div>)}
            {!done && <div style={{ animation: "mc-ping 1.2s ease infinite", marginTop: 2 }}>▋</div>}
          </div>

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
