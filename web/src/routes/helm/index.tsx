import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Card, Icon, Badge, Button, SectionTitle, StatusDot, EmptyState, Spinner } from "@/components/mc";
import { cmpVersion, essVersion } from "@/lib/version";

export const Route = createFileRoute("/helm/")({
  component: HelmPage,
});

interface HelmRelease {
  name: string;
  namespace: string;
  chart_version: string;
  revision: number;
  status: string;
  deployed_at?: string;
}
interface ESSVersion {
  version: string;
  published_at?: string;
  prerelease?: boolean;
}


const STATUS_MAP: Record<string, { tone: "ok" | "err" | "warn" | "info"; icon: string }> = {
  deployed: { tone: "ok", icon: "check" },
  failed: { tone: "err", icon: "x" },
  "hooks-failed": { tone: "warn", icon: "alert" },
  pending: { tone: "info", icon: "clock" },
};

function HelmPage() {
  const navigate = useNavigate();
  const [showPre, setShowPre] = useState(false);

  const { data: release } = useQuery({
    queryKey: ["helm", "release"],
    queryFn: () => api.get<HelmRelease>("/api/v1/helm/releases/ess"),
    refetchInterval: 15_000,
  });
  const { data: versions, isLoading: versionsLoading } = useQuery({
    queryKey: ["helm", "versions"],
    queryFn: () => api.get<ESSVersion[]>("/api/v1/helm/versions"),
    staleTime: 5 * 60_000,
  });

  const st = release ? STATUS_MAP[release.status] ?? STATUS_MAP.pending : STATUS_MAP.pending;
  const current = release ? essVersion(release.chart_version) : "";

  const visible = (versions ?? []).filter((v) => showPre || !v.prerelease);
  const latest = visible.find((v) => !v.prerelease) ?? visible[0];
  const behind = current && latest ? visible.filter((v) => !v.prerelease && cmpVersion(v.version, current) > 0) : [];
  const upToDate = !!current && !!latest && cmpVersion(latest.version, current) <= 0;

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 22 }}>
      {/* Release hero */}
      <Card style={{ padding: 0, overflow: "hidden", position: "relative" }}>
        <div style={{ position: "absolute", inset: 0, background: "radial-gradient(120% 140% at 0% 0%, var(--accent-soft), transparent 55%)", pointerEvents: "none" }} />
        <div style={{ position: "relative", display: "flex", alignItems: "center", justifyContent: "space-between", gap: 24, padding: "22px 24px", flexWrap: "wrap" }}>
          <div style={{ display: "flex", alignItems: "center", gap: 18 }}>
            <div style={{ display: "grid", placeItems: "center", width: 52, height: 52, borderRadius: "var(--radius)", background: "var(--accent-soft)", color: "var(--accent)" }}><Icon name="helm" size={26} /></div>
            <div>
              <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 4, flexWrap: "wrap" }}>
                <span style={{ fontSize: 19, fontWeight: 650, letterSpacing: "-0.02em" }}>{release?.name || "ess"}</span>
                {release && <Badge tone={st.tone} icon={st.icon}>{release.status}</Badge>}
                {upToDate && <Badge tone="ok" icon="check">Aktuell</Badge>}
                {behind.length > 0 && <Badge tone="warn" icon="upload">{behind.length} Update{behind.length > 1 ? "s" : ""} verfügbar</Badge>}
              </div>
              <div style={{ display: "flex", gap: 16, flexWrap: "wrap", fontSize: 12.5, color: "var(--text-faint)" }}>
                <span style={{ fontFamily: "var(--mono)" }}>matrix-stack {current || "—"}</span>
                <span>·</span><span>Revision #{release?.revision ?? "—"}</span>
                <span>·</span><span>ns {release?.namespace || "ess"}</span>
              </div>
            </div>
          </div>
          <Button variant="primary" icon="upload" onClick={() => navigate({ to: "/helm/upgrade" })}>Upgrade</Button>
        </div>

        {/* Upgrade path: current → latest */}
        {behind.length > 0 && latest && (
          <div style={{ position: "relative", display: "flex", alignItems: "center", gap: 14, padding: "14px 24px", borderTop: "1px solid var(--border-soft)", background: "color-mix(in oklch, var(--status-warn) 7%, transparent)", flexWrap: "wrap" }}>
            <Icon name="upload" size={17} style={{ color: "var(--status-warn)" }} />
            <div style={{ display: "flex", alignItems: "center", gap: 10, fontFamily: "var(--mono)", fontSize: 14, fontWeight: 600 }}>
              <span style={{ color: "var(--text-dim)" }}>{current}</span>
              <Icon name="chevRight" size={14} style={{ color: "var(--text-faint)" }} />
              <span style={{ color: "var(--accent)" }}>{latest.version}</span>
            </div>
            <span style={{ fontSize: 12, color: "var(--text-faint)" }}>
              {behind.length} Release{behind.length > 1 ? "s" : ""} dazwischen
            </span>
            <div style={{ flex: 1 }} />
            <Button variant="soft" size="sm" icon="rocket" onClick={() => navigate({ to: "/helm/upgrade", search: { version: latest.version } })}>Auf {latest.version} upgraden</Button>
          </div>
        )}
      </Card>

      <div style={{ display: "grid", gridTemplateColumns: "minmax(0, 1.5fr) minmax(280px, 1fr)", gap: 22, alignItems: "start" }} className="mc-dash-grid">
        <Card pad={false}>
          <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12, padding: "16px 18px 12px", flexWrap: "wrap" }}>
            <SectionTitle sub={versions ? `${visible.length} Versionen aus der OCI-Registry` : "Lade aus der OCI-Registry…"} icon="git">ESS-Versionen</SectionTitle>
            <Button variant="ghost" size="sm" icon="eye" onClick={() => setShowPre((v) => !v)}>
              {showPre ? "Nur Releases" : "Pre-Releases zeigen"}
            </Button>
          </div>
          <div style={{ padding: "0 8px 8px" }}>
            {versionsLoading && <div style={{ display: "flex", alignItems: "center", gap: 8, padding: "16px", fontSize: 13, color: "var(--text-faint)" }}><Spinner size={14} /> Lade Versionen…</div>}
            {!versionsLoading && visible.length > 0 && visible.slice(0, 25).map((v, i) => {
              const isCurrent = v.version === current;
              const isNewer = current ? cmpVersion(v.version, current) > 0 : false;
              return (
                <div key={v.version} style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 14, padding: "11px 14px", borderRadius: "var(--radius-sm)", background: isCurrent ? "var(--accent-soft)" : "transparent", borderBottom: i < Math.min(visible.length, 25) - 1 ? "1px solid var(--border-soft)" : "none" }}>
                  <div style={{ display: "flex", alignItems: "center", gap: 10, minWidth: 0 }}>
                    <StatusDot status={isCurrent ? "accent" : isNewer ? "warn" : "idle"} />
                    <span style={{ fontFamily: "var(--mono)", fontSize: 13.5, fontWeight: 600, color: isCurrent ? "var(--accent)" : "var(--text)" }}>{v.version}</span>
                    {i === 0 && !v.prerelease && <Badge tone="accent" size="sm">latest</Badge>}
                    {isCurrent && <Badge tone="ok" size="sm">installiert</Badge>}
                    {v.prerelease && <Badge tone="neutral" size="sm">pre-release</Badge>}
                  </div>
                  <div style={{ display: "flex", alignItems: "center", gap: 10, flexShrink: 0 }}>
                    {v.published_at && <span style={{ fontSize: 11.5, color: "var(--text-faint)" }}>{new Date(v.published_at).toLocaleDateString("de-DE")}</span>}
                    {isNewer && <Button variant="ghost" size="sm" iconRight="chevRight" onClick={() => navigate({ to: "/helm/upgrade", search: { version: v.version } })}>Upgrade</Button>}
                  </div>
                </div>
              );
            })}
            {!versionsLoading && visible.length === 0 && <EmptyState icon="git" title="Keine Versionen entdeckt" sub="MatrixCtrl konnte die OCI-Registry nicht erreichen." />}
          </div>
        </Card>

        <div style={{ display: "flex", flexDirection: "column", gap: 22 }}>
          <Card>
            <SectionTitle sub="Aktueller Release-State">Details</SectionTitle>
            {[["Chart", release?.chart_version || "—"], ["Revision", release ? `#${release.revision}` : "—"], ["Status", release?.status || "—"], ["Deployed", release?.deployed_at ? new Date(release.deployed_at).toLocaleString("de-DE", { dateStyle: "medium", timeStyle: "short" }) : "—"]].map(([k, v], i) => (
              <div key={k} style={{ display: "flex", justifyContent: "space-between", gap: 16, padding: "9px 0", borderBottom: i < 3 ? "1px solid var(--border-soft)" : "none" }}>
                <span style={{ fontSize: 13, color: "var(--text-faint)" }}>{k}</span>
                <span style={{ fontSize: 12.5, color: "var(--text)", fontFamily: "var(--mono)", textAlign: "right" }}>{v}</span>
              </div>
            ))}
            <Button variant="ghost" size="sm" full iconRight="chevRight" onClick={() => navigate({ to: "/helm/history" })} style={{ marginTop: 10 }}>Upgrade-History</Button>
          </Card>

          <Card>
            <SectionTitle sub="Läuft nach jedem Upgrade" icon="hook">Hooks</SectionTitle>
            <p style={{ margin: "0 0 12px", fontSize: 12.5, color: "var(--text-dim)", lineHeight: 1.55 }}>
              Post-Upgrade-Hooks stellen die SFU-Patches wieder her, die ein Helm-Upgrade sonst überschreibt.
            </p>
            <Button variant="outline" size="sm" full iconRight="chevRight" onClick={() => navigate({ to: "/hooks" })}>Hooks verwalten</Button>
          </Card>
        </div>
      </div>
    </div>
  );
}
