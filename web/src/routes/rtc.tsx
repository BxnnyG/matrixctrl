import { createFileRoute } from "@tanstack/react-router";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Card, Badge, Icon, EmptyState, Button } from "@/components/mc";

export const Route = createFileRoute("/rtc")({
  component: RTCStatus,
});

interface Port {
  protocol: string;
  port: number;
  service: string;
  purpose: string;
  source_ip_preserved: boolean;
}

interface Finding {
  level: "ok" | "warn" | "unknown";
  title: string;
  detail: string;
  action?: string;
}

/**
 * Calling is two independent mechanisms, and this page used to report on one of
 * them. Element Call routes media through the SFU; a classic 1:1 call is
 * peer-to-peer and never touches it, needing a TURN relay from Synapse instead.
 * On 2026-08-02 the entire SFU path was green and calling still failed, because
 * the calls being made were the other kind (P1-12).
 */
interface CallPaths {
  element_call: boolean;
  turn: "present" | "absent" | "unknown";
  turn_uris?: string[];
}

interface RTCStatusResp {
  announced_host: string;
  resolved_ips: string[] | null;
  ports: Port[];
  /** Whether the address the SFU announces can still be current — see internal/rtc/address.go. */
  freshness: "ok" | "stale" | "unknown";
  call_paths: CallPaths;
  findings: Finding[];
}

const TONE = {
  ok: { tone: "ok" as const, icon: "check", label: "geprüft" },
  warn: { tone: "warn" as const, icon: "alert", label: "Problem" },
  // Its own visual weight on purpose. An unknown rendered like an OK is exactly
  // how "we never checked" came to read as "fine".
  unknown: { tone: "info" as const, icon: "info", label: "nicht prüfbar" },
};

/** One call mechanism and whether this deployment can carry it. `unknown` keeps its
 *  own neutral tone rather than borrowing the warning colour: not knowing and being
 *  broken look different because they are different. */
function PathRow({ name, sub, state, ok, unknown }: {
  name: string; sub: string; state: string; ok: boolean; unknown?: boolean;
}) {
  const tone = unknown ? "info" : ok ? "ok" : "warn";
  return (
    <div style={{ display: "flex", alignItems: "center", gap: 14, padding: "11px 14px", borderRadius: "var(--radius-sm)", background: "var(--surface-2)" }}>
      <div style={{ flex: 1, minWidth: 0 }}>
        <div style={{ fontSize: 13, fontWeight: 650, color: "var(--text)" }}>{name}</div>
        <div style={{ fontSize: 12, color: "var(--text-faint)", marginTop: 2 }}>{sub}</div>
      </div>
      <Badge tone={tone} size="sm">{state}</Badge>
    </div>
  );
}

function RTCStatus() {
  const qc = useQueryClient();
  const { data, isLoading, error } = useQuery({
    queryKey: ["rtc", "status"],
    queryFn: () => api.get<RTCStatusResp>("/api/v1/rtc/status"),
    refetchInterval: 60_000,
  });

  // Replacing the pod is the fix for a stale announcement. The backend deletes the
  // pod rather than rolling the deployment, because a rolling update of a
  // hostNetwork workload with one replica deadlocks (P2-23).
  const restart = useMutation({
    mutationFn: () => api.post("/api/v1/rtc/restart-sfu", {}),
    onSuccess: () => {
      // The new pod needs a moment to start and re-discover its address; refetching
      // immediately would show the old verdict and look like the button did nothing.
      setTimeout(() => qc.invalidateQueries({ queryKey: ["rtc"] }), 6000);
    },
  });

  if (error) {
    return (
      <EmptyState
        icon="alert"
        title="RTC-Status nicht abrufbar"
        sub="Der Cluster antwortet nicht auf die Service-Abfrage. Das ist keine Aussage über Calling — nur darüber, dass gerade nichts geprüft werden konnte."
      />
    );
  }

  const paths = data?.call_paths;

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 18 }}>
      {paths && (
        <Card>
          <div style={{ fontSize: 14, fontWeight: 650, color: "var(--text)" }}>
            Welche Anrufwege diese Installation unterstützt
          </div>
          <div style={{ fontSize: 12.5, color: "var(--text-dim)", marginTop: 4, marginBottom: 14 }}>
            Zwei getrennte Mechanismen mit getrennten Fehlerbildern. Alles Weitere auf dieser
            Seite betrifft nur den ersten.
          </div>
          <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
            <PathRow
              name="Element Call"
              sub="Gruppenanrufe und Element X — läuft über die SFU"
              ok={paths.element_call}
              state={
                paths.element_call
                  ? `SFU vorhanden, ${data!.ports.length} ${data!.ports.length === 1 ? "Port" : "Ports"}`
                  : "keine SFU gefunden"
              }
            />
            <PathRow
              name="Klassische 1:1-Anrufe"
              sub="Direkte Peer-to-Peer-Verbindung — braucht ein TURN-Relay von Synapse"
              ok={paths.turn === "present"}
              unknown={paths.turn === "unknown"}
              state={
                paths.turn === "present"
                  ? `Relay konfiguriert (${paths.turn_uris?.length ?? 0})`
                  : paths.turn === "unknown"
                    ? "nicht feststellbar"
                    : "kein Relay"
              }
            />
          </div>
        </Card>
      )}

      <Card>
        <div style={{ fontSize: 14, fontWeight: 650, color: "var(--text)" }}>
          Diese Ports müssen aus dem Internet auf diesen Node zeigen
        </div>
        <div style={{ fontSize: 12.5, color: "var(--text-dim)", marginTop: 4, marginBottom: 14 }}>
          Live aus den NodePort-Services gelesen, nicht aus der Dokumentation. Das Protokoll
          entscheidet mit: eine TCP-Weiterleitung auf einen UDP-Port sieht im Router richtig aus
          und tut nichts.
        </div>

        {data?.ports.length ? (
          <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
            {data.ports.map((p) => (
              <div key={`${p.protocol}-${p.port}`} style={{ display: "flex", alignItems: "center", gap: 14, padding: "11px 14px", borderRadius: "var(--radius-sm)", background: "var(--surface-2)" }}>
                <span style={{ fontFamily: "var(--mono)", fontSize: 13.5, fontWeight: 700, color: "var(--accent)", minWidth: 96 }}>
                  {p.protocol} {p.port}
                </span>
                <span style={{ flex: 1, fontSize: 12.5, color: "var(--text-dim)" }}>{p.purpose}</span>
                {!p.source_ip_preserved && (
                  <Badge tone="warn" size="sm">Client-IP geht verloren</Badge>
                )}
              </div>
            ))}
          </div>
        ) : (
          !isLoading && <div style={{ fontSize: 12.5, color: "var(--text-faint)" }}>Keine RTC-NodePorts gefunden.</div>
        )}

        {data?.announced_host && (
          <div style={{ marginTop: 14, fontSize: 12, color: "var(--text-faint)", fontFamily: "var(--mono)" }}>
            Clients wird angekündigt: {data.announced_host}
            {data.resolved_ips?.length ? ` → ${data.resolved_ips.join(", ")}` : ""}
          </div>
        )}
      </Card>

      {data?.freshness === "stale" && (
        <Card style={{ display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap", background: "color-mix(in oklch, var(--status-warn) 10%, var(--surface))", borderColor: "color-mix(in oklch, var(--status-warn) 30%, var(--border))" }}>
          <Icon name="alert" size={18} style={{ color: "var(--status-warn)" }} />
          <span style={{ flex: 1, minWidth: 220, fontSize: 13, color: "var(--text)" }}>
            Ein Neustart der SFU behebt das sofort — bis zur nächsten Adressänderung.
          </span>
          <Button variant="soft" size="sm" icon="rotate" onClick={() => restart.mutate()} disabled={restart.isPending}>
            {restart.isPending ? "Starte neu…" : "SFU neu starten"}
          </Button>
        </Card>
      )}

      <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
        {data?.findings.map((f, i) => {
          const t = TONE[f.level];
          return (
            <Card key={i}>
              <div style={{ display: "flex", alignItems: "flex-start", gap: 12 }}>
                <Icon name={t.icon} size={18} style={{ color: `var(--${t.tone === "ok" ? "ok" : t.tone === "warn" ? "warn" : "text-dim"})`, marginTop: 2 }} />
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
                    <span style={{ fontSize: 13.5, fontWeight: 650, color: "var(--text)" }}>{f.title}</span>
                    <Badge tone={t.tone} size="sm">{t.label}</Badge>
                  </div>
                  <div style={{ fontSize: 12.5, color: "var(--text-dim)", marginTop: 5, lineHeight: 1.55 }}>{f.detail}</div>
                  {f.action && (
                    <div style={{ fontSize: 12.5, color: "var(--text)", marginTop: 8, paddingLeft: 11, borderLeft: "2px solid var(--accent)" }}>
                      {f.action}
                    </div>
                  )}
                </div>
              </div>
            </Card>
          );
        })}
      </div>
    </div>
  );
}
