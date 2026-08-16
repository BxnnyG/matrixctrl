import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Card, Icon, Badge, Button, SectionTitle, StatusDot, EmptyState, Spinner } from "@/components/mc";
import { Markdown } from "@/components/Markdown";
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
interface ReleaseNotes {
  version: string;
  available: boolean;
  body?: string;
  url?: string;
  reason?: string;
}


const STATUS_MAP: Record<string, { tone: "ok" | "err" | "warn" | "info"; icon: string }> = {
  deployed: { tone: "ok", icon: "check" },
  failed: { tone: "err", icon: "x" },
  "hooks-failed": { tone: "warn", icon: "alert" },
  pending: { tone: "info", icon: "clock" },
};

/** One version, with what is known about it.
 *
 *  The row used to be a version number and an empty date slot: ListVersions reads
 *  the GHCR *tag list*, which is a list of strings, so `published_at` was never set
 *  on any row for any version. It is now filled from the GitHub release index, and
 *  the row expands to the notes for that version — the endpoint and its cache
 *  already existed for the upgrade screen (E32, E43). */
function VersionRow({ version: v, isFirst, isCurrent, isNewer, last, expanded, onToggle, onUpgrade }: {
  version: ESSVersion;
  isFirst: boolean; isCurrent: boolean; isNewer: boolean; last: boolean;
  expanded: boolean; onToggle: () => void; onUpgrade: () => void;
}) {
  // Fetched only while open, so opening the page does not fire 25 requests at
  // GitHub's 60-per-hour unauthenticated budget.
  const { data: notes, isFetching } = useQuery({
    queryKey: ["helm", "notes", v.version],
    queryFn: () => api.get<ReleaseNotes>(`/api/v1/helm/versions/${encodeURIComponent(v.version)}/notes`),
    enabled: expanded,
    staleTime: Infinity,
  });

  return (
    <div style={{ borderBottom: last ? "none" : "1px solid var(--border-soft)" }}>
      <div
        onClick={onToggle}
        style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 14, padding: "11px 14px", borderRadius: "var(--radius-sm)", background: isCurrent ? "var(--accent-soft)" : "transparent", cursor: "pointer" }}>
        <div style={{ display: "flex", alignItems: "center", gap: 10, minWidth: 0 }}>
          <Icon name={expanded ? "chevDown" : "chevRight"} size={13} style={{ color: "var(--text-faint)", flexShrink: 0 }} />
          <StatusDot status={isCurrent ? "accent" : isNewer ? "warn" : "idle"} />
          <span style={{ fontFamily: "var(--mono)", fontSize: 13.5, fontWeight: 600, color: isCurrent ? "var(--accent)" : "var(--text)" }}>{v.version}</span>
          {isFirst && !v.prerelease && <Badge tone="accent" size="sm">latest</Badge>}
          {isCurrent && <Badge tone="ok" size="sm">installiert</Badge>}
          {v.prerelease && <Badge tone="neutral" size="sm">pre-release</Badge>}
        </div>
        <div style={{ display: "flex", alignItems: "center", gap: 10, flexShrink: 0 }}>
          {v.published_at && (
            <span style={{ fontSize: 11.5, color: "var(--text-faint)" }}>
              {new Date(v.published_at).toLocaleDateString("de-DE", { day: "2-digit", month: "short", year: "numeric" })}
            </span>
          )}
          {/* The click is stopped on a wrapper rather than by widening Button's
              onClick: the primitive is shared by every screen, and one caller
              needing the event is not a reason to change its signature. */}
          {isNewer && (
            <span onClick={(e) => e.stopPropagation()}>
              <Button variant="ghost" size="sm" iconRight="chevRight" onClick={onUpgrade}>Upgrade</Button>
            </span>
          )}
        </div>
      </div>

      {expanded && (
        <div style={{ padding: "2px 14px 14px 37px" }}>
          {isFetching && !notes && (
            <div style={{ fontSize: 12.5, color: "var(--text-faint)" }}><Spinner size={13} /> Lade Release Notes…</div>
          )}
          {notes && !notes.available && (
            <div style={{ fontSize: 12.5, color: "var(--text-faint)" }}>{notes.reason ?? "Keine Release Notes verfügbar."}</div>
          )}
          {notes?.available && (
            <div style={{ borderRadius: "var(--radius-sm)", background: "var(--surface-2)", border: "1px solid var(--border)", padding: "12px 14px" }}>
              {notes.url && (
                <a href={notes.url} target="_blank" rel="noreferrer noopener"
                  style={{ float: "right", fontSize: 11.5, color: "var(--accent)", textDecoration: "none" }}>auf GitHub ↗</a>
              )}
              <div className="mc-scroll" style={{ maxHeight: 260, overflowY: "auto" }}>
                <Markdown text={notes.body ?? ""} />
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function HelmPage() {
  const navigate = useNavigate();
  const [expanded, setExpanded] = useState<string | null>(null);

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

  // No pre-release filter any more. The twelve tags it used to hide were all
  // `0.x.y-dev` build tags from the chart's first months, and they are now dropped
  // where the other build tags are — in parseReleaseTag, on the server. What
  // reaches this list is upgrade targets, so the list shows them (E43).
  //
  // `prerelease` survives on the row: a real `26.9.0-rc.1` would be a legitimate
  // target worth marking. ESS has never published one.
  const visible = versions ?? [];

  const SHOWN = 25;
  const shown = visible.slice(0, SHOWN);
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
            <SectionTitle
              sub={versions
                ? visible.length > SHOWN
                  ? `${shown.length} von ${visible.length} Versionen aus der OCI-Registry`
                  : `${visible.length} Versionen aus der OCI-Registry`
                : "Lade aus der OCI-Registry…"}
              icon="git">ESS-Versionen</SectionTitle>
          </div>
          <div style={{ padding: "0 8px 8px" }}>
            {versionsLoading && <div style={{ display: "flex", alignItems: "center", gap: 8, padding: "16px", fontSize: 13, color: "var(--text-faint)" }}><Spinner size={14} /> Lade Versionen…</div>}
            {!versionsLoading && shown.map((v, i) => (
              <VersionRow
                key={v.version}
                version={v}
                isFirst={i === 0}
                isCurrent={v.version === current}
                isNewer={current ? cmpVersion(v.version, current) > 0 : false}
                last={i === shown.length - 1}
                expanded={expanded === v.version}
                onToggle={() => setExpanded(expanded === v.version ? null : v.version)}
                onUpgrade={() => navigate({ to: "/helm/upgrade", search: { version: v.version } })}
              />
            ))}
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
