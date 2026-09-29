import { createFileRoute } from "@tanstack/react-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Button, Card, EmptyState, Icon, SectionTitle, Spinner } from "@/components/mc";

export const Route = createFileRoute("/tls-dns")({ component: TLSDNSPage });

interface Cert {
  reachable: boolean; error?: string; issuer?: string; names?: string[];
  not_after?: string; self_signed?: boolean; name_matches: boolean; days_left: number; expired?: boolean;
}
interface HostReport {
  key: string; host: string; label: string; purpose: string;
  dns: { status: "ok" | "elsewhere" | "missing" | "unknown"; resolved?: string[]; detail?: string };
  proxied: boolean; served?: string;
  public: Cert; origin?: Cert;
  summary: string; level: "ok" | "warn" | "err";
}
interface Response { hosts: HostReport[]; node_addresses?: string[]; origin_note?: string; note?: string }

const TONE = { ok: "var(--status-ok)", warn: "var(--status-warn)", err: "var(--status-err)" } as const;
const DNS_TEXT: Record<string, string> = {
  ok: "zeigt auf diesen Server",
  elsewhere: "zeigt woandershin",
  missing: "kein Eintrag",
  unknown: "nicht nachschlagbar",
};

/** What the internet sees, and what the server itself serves (etappe 115).
 *
 *  Both, because the difference is where three call outages hid: behind Cloudflare a
 *  self-signed origin certificate is invisible from outside and fatal for services
 *  inside the cluster that call the same name. */
function TLSDNSPage() {
  const qc = useQueryClient();
  const { data, isLoading, isFetching, error } = useQuery({
    queryKey: ["tls-dns"],
    queryFn: () => api.get<Response>("/api/v1/tls-dns"),
    staleTime: 60_000,
  });

  if (isLoading) return <div style={{ display: "flex", gap: 8, padding: 24, fontSize: 13, color: "var(--text-faint)" }}><Spinner size={14} /> Prüfe Adressen, DNS und Zertifikate…</div>;
  if (error) return <div style={{ padding: 24, fontSize: 13, color: "var(--status-err)" }}>{(error as Error).message}</div>;

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16, maxWidth: 900 }}>
      <SectionTitle icon="lock" sub="Zeigen die Adressen hierher, und sind die Zertifikate gültig?"
        right={<Button variant="outline" size="sm" icon="refresh" onClick={() => qc.invalidateQueries({ queryKey: ["tls-dns"] })}>{isFetching ? <Spinner size={13} /> : "Neu prüfen"}</Button>}>
        TLS &amp; DNS
      </SectionTitle>

      {data?.note && <Card><EmptyState icon="globe" title="Keine Adressen eingetragen" sub={data.note} /></Card>}
      {data?.origin_note && (
        <Card style={{ display: "flex", gap: 10, alignItems: "center", fontSize: 12.5, color: "var(--text-dim)" }}>
          <Icon name="info" size={16} /> {data.origin_note}
        </Card>
      )}

      {(data?.hosts ?? []).map((h) => (
        <Card key={h.key} style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          <div style={{ display: "flex", alignItems: "baseline", gap: 10, flexWrap: "wrap" }}>
            <span style={{ width: 9, height: 9, borderRadius: 999, background: TONE[h.level], flexShrink: 0, alignSelf: "center" }} />
            <span style={{ fontSize: 14, fontWeight: 650, color: "var(--text)" }}>{h.label}</span>
            <code style={{ fontFamily: "var(--mono)", fontSize: 12.5, color: "var(--text-dim)" }}>{h.host}</code>
            <span style={{ fontSize: 11.5, color: "var(--text-faint)" }}>· {h.purpose}</span>
          </div>
          <div style={{ fontSize: 13, color: "var(--text)", lineHeight: 1.55, paddingLeft: 19 }}>{h.summary}</div>
          <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(230px, 1fr))", gap: "6px 18px", paddingLeft: 19 }}>
            {/* With the proxy on, "points elsewhere" is true and misleading: it points
                at Cloudflare, which is the intent. */}
            <Detail label="DNS" value={`${h.proxied && h.dns.status === "elsewhere" ? "zeigt auf Cloudflare" : DNS_TEXT[h.dns.status] ?? h.dns.status}${h.dns.resolved?.length ? ` (${h.dns.resolved.join(", ")})` : ""}`} />
            <Detail label="Ausgeliefert von" value={h.proxied ? "Cloudflare (Proxy an)" : h.served || "direkt vom Server"} />
            <Detail label="Zertifikat von außen" value={certText(h.public)} tone={h.public.self_signed || h.public.expired || !h.public.name_matches ? "err" : undefined} />
            {h.origin && <Detail label="Zertifikat am Server" value={certText(h.origin)} tone={h.origin.self_signed ? "warn" : undefined} />}
          </div>
        </Card>
      ))}
    </div>
  );
}

function certText(c: Cert): string {
  if (!c.reachable) return c.error ?? "nicht erreichbar";
  if (c.self_signed) return `selbstsigniert (${c.issuer})`;
  if (c.expired) return `abgelaufen (${c.issuer})`;
  if (!c.name_matches) return `gilt für ${c.names?.join(", ") || "andere Namen"}`;
  return `${c.issuer}, noch ${c.days_left} Tage`;
}

function Detail({ label, value, tone }: { label: string; value: string; tone?: "warn" | "err" }) {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 1, minWidth: 0 }}>
      <span style={{ fontSize: 11, color: "var(--text-faint)" }}>{label}</span>
      <span style={{ fontSize: 12.5, color: tone ? TONE[tone] : "var(--text-dim)", overflowWrap: "anywhere" }}>{value}</span>
    </div>
  );
}
