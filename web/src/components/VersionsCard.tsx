import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { api } from "@/lib/api";
import { cmpVersion, essVersion } from "@/lib/version";
import { Badge, Card, Icon, SectionTitle } from "@/components/mc";

// "Versionen von allem auf den ersten Blick" (etappe 112). Before this the dashboard
// showed health and not one version: which ESS, which Synapse, whether anything was
// behind — three different pages, or kubectl.

interface Component { name: string; version?: string; image?: string }

/** What each workload is, for someone who does not know the chart's names. */
const LABELS: Record<string, string> = {
  "synapse-main": "Synapse (Homeserver)",
  "matrix-authentication-service": "Anmeldung (MAS)",
  "element-web": "Element Web",
  "element-admin": "Element Admin",
  "matrix-rtc-sfu": "Anrufe (LiveKit)",
  "matrix-rtc-authorisation-service": "Anruf-Anmeldung",
  "postgres": "Datenbank (PostgreSQL)",
  "valkey": "Cache (Valkey)",
  "haproxy": "Proxy (HAProxy)",
  "hookshot": "Hookshot",
};
const ORDER = Object.keys(LABELS);

const short = (name: string) => name.replace(/^ess-/, "");

export function VersionsCard({ components, chartVersion }: { components: Component[]; chartVersion?: string }) {
  const navigate = useNavigate();
  // Same keys as the rail and the update page: one request, three readers.
  const { data: mc } = useQuery({
    queryKey: ["version"],
    queryFn: () => api.get<{ version: string; update?: { latest?: string; available: boolean } }>("/api/v1/version"),
    staleTime: 10 * 60_000,
  });
  const { data: ess } = useQuery({
    queryKey: ["helm", "versions"],
    queryFn: () => api.get<{ version: string }[]>("/api/v1/helm/versions"),
    staleTime: 30 * 60_000,
  });

  const running = chartVersion ? essVersion(chartVersion) : undefined;
  const newest = ess?.[0]?.version ? essVersion(ess[0].version) : undefined;
  const essBehind = !!running && !!newest && cmpVersion(running, newest) < 0;
  const mcBehind = !!mc?.update?.available && !!mc.update.latest;

  const rows = components
    .filter((c) => c.version || c.image)
    .sort((a, b) => {
      const ia = ORDER.indexOf(short(a.name)), ib = ORDER.indexOf(short(b.name));
      return (ia < 0 ? 99 : ia) - (ib < 0 ? 99 : ib) || a.name.localeCompare(b.name);
    });

  return (
    <Card style={{ display: "flex", flexDirection: "column", gap: 14 }}>
      <SectionTitle icon="git" sub="Was läuft, und ob es Neueres gibt">Versionen</SectionTitle>

      <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(240px, 1fr))", gap: 10 }}>
        <Headline label="ESS (Element Server Suite)" version={running} behind={essBehind} newest={newest}
          onUpdate={() => navigate({ to: "/helm" })} />
        <Headline label="MatrixCtrl" version={mc?.version?.replace(/^v/, "")} behind={mcBehind} newest={mc?.update?.latest?.replace(/^v/, "")} />
      </div>

      {rows.length > 0 && (
        <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(230px, 1fr))", gap: "4px 18px" }}>
          {rows.map((c) => (
            <div key={c.name} title={c.image} style={{ display: "flex", alignItems: "baseline", justifyContent: "space-between", gap: 10, padding: "6px 0", borderBottom: "1px solid var(--border-soft)", minWidth: 0 }}>
              <span style={{ fontSize: 12.5, color: "var(--text-dim)", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>{LABELS[short(c.name)] ?? short(c.name)}</span>
              <code style={{ fontFamily: "var(--mono)", fontSize: 12, color: "var(--text)", flexShrink: 0 }}>{c.version || "Digest"}</code>
            </div>
          ))}
        </div>
      )}
      <span style={{ fontSize: 11.5, color: "var(--text-faint)" }}>
        Die Dienste kommen mit ESS: ein ESS-Update bringt ihre neuen Versionen mit. Einzeln festgeschriebene Versionen meldet die Update-Seite.
      </span>
    </Card>
  );
}

function Headline({ label, version, behind, newest, onUpdate }: {
  label: string; version?: string; behind: boolean; newest?: string; onUpdate?: () => void;
}) {
  return (
    <div style={{ display: "flex", alignItems: "center", gap: 10, padding: "10px 12px", borderRadius: "var(--radius-sm)", background: "var(--surface-2)", border: "1px solid var(--border-soft)" }}>
      <div style={{ flex: 1, minWidth: 0 }}>
        <div style={{ fontSize: 12, color: "var(--text-faint)" }}>{label}</div>
        <code style={{ fontFamily: "var(--mono)", fontSize: 15, fontWeight: 600, color: "var(--text)" }}>{version ?? "—"}</code>
      </div>
      {behind ? (
        <span onClick={onUpdate} style={{ cursor: onUpdate ? "pointer" : "default" }}>
          <Badge tone="warn" size="sm" icon="upload">{newest} verfügbar</Badge>
        </span>
      ) : version && newest ? (
        <Badge tone="ok" size="sm" icon="check">aktuell</Badge>
      ) : (
        <Icon name="info" size={14} style={{ color: "var(--text-faint)" }} />
      )}
    </div>
  );
}
