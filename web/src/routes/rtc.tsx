import { createFileRoute } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Card, Badge, Icon, EmptyState } from "@/components/mc";

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

interface RTCStatusResp {
  announced_host: string;
  resolved_ips: string[] | null;
  ports: Port[];
  findings: Finding[];
}

const TONE = {
  ok: { tone: "ok" as const, icon: "check", label: "geprüft" },
  warn: { tone: "warn" as const, icon: "alert", label: "Problem" },
  // Its own visual weight on purpose. An unknown rendered like an OK is exactly
  // how "we never checked" came to read as "fine".
  unknown: { tone: "info" as const, icon: "info", label: "nicht prüfbar" },
};

function RTCStatus() {
  const { data, isLoading, error } = useQuery({
    queryKey: ["rtc", "status"],
    queryFn: () => api.get<RTCStatusResp>("/api/v1/rtc/status"),
    refetchInterval: 60_000,
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

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 18 }}>
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
