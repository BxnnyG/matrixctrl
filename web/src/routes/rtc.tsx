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

/** The gauges describe the SFU right now; the counters describe it since it
 *  started, which is not the same as "ever" — see RTCHistoryResp. */
interface MediaEvidence {
  rooms_completed: number;
  room_seconds: number;
  quality_samples: number;
  forward_samples: number;
  packets_out: number;
  live: { rooms: number; participants: number };
}

interface RTCStatusResp {
  announced_host: string;
  resolved_ips: string[] | null;
  ports: Port[];
  /** Whether the address the SFU announces can still be current — see internal/rtc/address.go. */
  freshness: "ok" | "stale" | "unknown";
  call_paths: CallPaths;
  media?: MediaEvidence;
  /** How long the SFU process has been up. Every counter above is scoped to it, so
   *  this is what lets "0 Räume" be read correctly. */
  sfu_uptime?: string;
  findings: Finding[];
}

interface Totals {
  calls: number;
  seconds: number;
  quality_samples: number;
  sfu_restarts: number;
  samples: number;
  since?: string;
}

interface DailyTotal {
  day: string;
  calls: number;
  seconds: number;
  sfu_restarts: number;
}

/** What MatrixCtrl recorded, as opposed to what the SFU remembers.
 *
 *  LiveKit's counters are process-lifetime and the post-upgrade hook deletes the
 *  SFU pod on every ESS upgrade, so anything older than the current process exists
 *  only because it was sampled and stored (E44). */
interface RTCHistoryResp {
  last_24h: Totals;
  daily: DailyTotal[] | null;
  interval_seconds: number;
}

interface ReachPort {
  protocol: string;
  port: number;
  status: "open" | "closed" | "unknown";
  purpose?: string;
}

interface ReachResp {
  result: {
    address: string;
    ports: ReachPort[] | null;
    control_ok: boolean;
    udp_skipped: number;
    error?: string;
  };
  verdict: { level: "ok" | "warn" | "unknown"; title: string; detail: string; action?: string };
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

function fmtDuration(seconds: number): string {
  if (seconds <= 0) return "0 min";
  if (seconds < 3600) return `${Math.round(seconds / 60)} min`;
  const h = Math.floor(seconds / 3600);
  const m = Math.round((seconds % 3600) / 60);
  return m > 0 ? `${h} h ${m} min` : `${h} h`;
}

/** Live: what is on the SFU at this moment.
 *
 *  Uptime sits beside the numbers rather than under a tooltip, because they are
 *  meaningless without it — the counters reset with the process, and the process is
 *  replaced by the post-upgrade hook on every ESS upgrade. */
function LivePanel({ media, uptime }: { media?: MediaEvidence; uptime?: string }) {
  if (!media) return null;
  const live = media.live;
  const busy = live.rooms > 0 || live.participants > 0;

  return (
    <Card>
      <div style={{ display: "flex", alignItems: "baseline", justifyContent: "space-between", gap: 12, flexWrap: "wrap", marginBottom: 12 }}>
        <div style={{ fontSize: 14, fontWeight: 650, color: "var(--text)" }}>Jetzt auf der SFU</div>
        {uptime && <span style={{ fontSize: 11.5, color: "var(--text-faint)" }}>SFU läuft seit {uptime}</span>}
      </div>

      <div style={{ display: "flex", gap: 28, flexWrap: "wrap" }}>
        <div>
          <div style={{ fontSize: 26, fontWeight: 650, color: busy ? "var(--accent)" : "var(--text-dim)", fontFamily: "var(--mono)", lineHeight: 1.1 }}>{live.rooms}</div>
          <div style={{ fontSize: 12, color: "var(--text-faint)" }}>{live.rooms === 1 ? "Raum" : "Räume"}</div>
        </div>
        <div>
          <div style={{ fontSize: 26, fontWeight: 650, color: busy ? "var(--accent)" : "var(--text-dim)", fontFamily: "var(--mono)", lineHeight: 1.1 }}>{live.participants}</div>
          <div style={{ fontSize: 12, color: "var(--text-faint)" }}>{live.participants === 1 ? "Teilnehmer" : "Teilnehmer"}</div>
        </div>
        <div style={{ borderLeft: "1px solid var(--border-soft)", paddingLeft: 28 }}>
          <div style={{ fontSize: 26, fontWeight: 650, color: "var(--text-dim)", fontFamily: "var(--mono)", lineHeight: 1.1 }}>{media.rooms_completed}</div>
          <div style={{ fontSize: 12, color: "var(--text-faint)" }}>Calls seit SFU-Start</div>
        </div>
      </div>

      {/* Said plainly, because the alternative is an operator reading a fresh
          process's zero as a statement about the deployment. */}
      <div style={{ fontSize: 11.5, color: "var(--text-faint)", marginTop: 12, lineHeight: 1.55 }}>
        Diese Zahlen zählt die SFU selbst und verliert sie bei jedem Neustart —
        also bei jedem ESS-Upgrade. Der Verlauf unten stammt aus eigenen Messungen
        und übersteht das.
      </div>
    </Card>
  );
}

/** Recorded history — the part that survives the SFU. */
function HistoryPanel() {
  const { data, isLoading } = useQuery({
    queryKey: ["rtc", "history"],
    queryFn: () => api.get<RTCHistoryResp>("/api/v1/rtc/history"),
    refetchInterval: 120_000,
  });

  if (isLoading || !data) return null;
  const t = data.last_24h;
  const days = (data.daily ?? []).filter((d) => d.calls > 0 || d.sfu_restarts > 0);
  const minutes = Math.round(data.interval_seconds / 60);

  // Nothing sampled yet is a different statement from nothing happened, and the
  // difference matters most right after this feature ships.
  if (t.samples === 0) {
    return (
      <Card>
        <div style={{ fontSize: 14, fontWeight: 650, color: "var(--text)", marginBottom: 6 }}>Verlauf</div>
        <div style={{ fontSize: 12.5, color: "var(--text-dim)", lineHeight: 1.6 }}>
          Noch keine Messungen aufgezeichnet. MatrixCtrl liest die SFU alle{" "}
          {minutes === 1 ? "Minute" : `${minutes} Minuten`}; der Verlauf beginnt ab jetzt.
        </div>
      </Card>
    );
  }

  return (
    <Card>
      <div style={{ display: "flex", alignItems: "baseline", justifyContent: "space-between", gap: 12, flexWrap: "wrap", marginBottom: 12 }}>
        <div style={{ fontSize: 14, fontWeight: 650, color: "var(--text)" }}>Verlauf (24 h)</div>
        {t.since && (
          <span style={{ fontSize: 11.5, color: "var(--text-faint)" }}>
            aufgezeichnet seit {new Date(t.since).toLocaleString("de-DE", { dateStyle: "short", timeStyle: "short" })}
          </span>
        )}
      </div>

      <div style={{ display: "flex", gap: 28, flexWrap: "wrap", marginBottom: 14 }}>
        <div>
          <div style={{ fontSize: 22, fontWeight: 650, fontFamily: "var(--mono)", color: "var(--text)", lineHeight: 1.1 }}>{t.calls}</div>
          <div style={{ fontSize: 12, color: "var(--text-faint)" }}>Calls</div>
        </div>
        <div>
          <div style={{ fontSize: 22, fontWeight: 650, fontFamily: "var(--mono)", color: "var(--text)", lineHeight: 1.1 }}>{fmtDuration(t.seconds)}</div>
          <div style={{ fontSize: 12, color: "var(--text-faint)" }}>Gesprächszeit</div>
        </div>
        {t.sfu_restarts > 0 && (
          <div>
            <div style={{ fontSize: 22, fontWeight: 650, fontFamily: "var(--mono)", color: "var(--status-warn)", lineHeight: 1.1 }}>{t.sfu_restarts}</div>
            <div style={{ fontSize: 12, color: "var(--text-faint)" }}>SFU-Neustarts</div>
          </div>
        )}
      </div>

      {days.length > 0 && (
        <div style={{ borderTop: "1px solid var(--border-soft)", paddingTop: 10 }}>
          {days.slice(-14).reverse().map((d) => (
            <div key={d.day} style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12, padding: "6px 0", fontSize: 12.5 }}>
              <span style={{ color: "var(--text-dim)" }}>{new Date(d.day).toLocaleDateString("de-DE", { day: "2-digit", month: "short" })}</span>
              <span style={{ display: "flex", gap: 14, alignItems: "center" }}>
                {d.sfu_restarts > 0 && <span style={{ fontSize: 11, color: "var(--status-warn)" }}>{d.sfu_restarts}× Neustart</span>}
                <span style={{ fontFamily: "var(--mono)", color: "var(--text-faint)" }}>{fmtDuration(d.seconds)}</span>
                <span style={{ fontFamily: "var(--mono)", color: "var(--text)", minWidth: 28, textAlign: "right" }}>{d.calls}</span>
              </span>
            </div>
          ))}
        </div>
      )}

      <div style={{ fontSize: 11.5, color: "var(--text-faint)", marginTop: 12, lineHeight: 1.55 }}>
        Gemessen alle {minutes === 1 ? "Minute" : `${minutes} Minuten`}. Die Summen
        stimmen auch für Calls, die vollständig zwischen zwei Messungen lagen — nur
        der genaue Zeitpunkt ist auf dieses Intervall genau.
        {" "}Teilnehmer-Identitäten werden nicht gelesen und nicht gespeichert.
      </div>
    </Card>
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

  // The one call in this product that leaves the cluster. Never on load, never on a
  // timer — it sends the deployment's public address to a third party, and that is
  // the operator's decision to make each time.
  const reach = useMutation({
    mutationFn: () => api.post<ReachResp>("/api/v1/rtc/reachability", {}),
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

      {/* Usage before configuration. The call-path card above stays first because
          it says which mechanism everything else is about (§4.22); after that, an
          operator asking about calls wants to know whether anyone is on one — the
          question every check on this page was green about while the feature was
          dead (P1-10). */}
      <LivePanel media={data?.media} uptime={data?.sfu_uptime} />
      <HistoryPanel />

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

      {/* E19 recorded inbound reachability as a permanent unknown, which was true
          from inside and quietly implied nothing could be done. One request to an
          outside vantage point answered in seconds what three days of inside-out
          measurement could not (P1-15). */}
      <Card>
        <div style={{ fontSize: 14, fontWeight: 650, color: "var(--text)" }}>
          Von außen prüfen
        </div>
        <div style={{ fontSize: 12.5, color: "var(--text-dim)", marginTop: 4, marginBottom: 12, lineHeight: 1.55 }}>
          Ob die Ports aus dem Internet erreichbar sind, lässt sich von innen nicht feststellen — von außen schon.
          Diese Prüfung <strong>verlässt den Cluster</strong> und übermittelt die öffentliche Adresse dieser
          Installation an <code style={{ fontFamily: "var(--mono)" }}>api.ipify.org</code> und{" "}
          <code style={{ fontFamily: "var(--mono)" }}>portchecker.io</code>. Sie läuft nur auf Klick,
          nie automatisch, und es wird nichts gespeichert.
        </div>

        <Button variant="soft" size="sm" icon="search" onClick={() => reach.mutate()} disabled={reach.isPending}>
          {reach.isPending ? "Prüfe von außen…" : "Jetzt von außen prüfen"}
        </Button>

        {reach.isError && (
          <div style={{ fontSize: 12.5, color: "var(--text-dim)", marginTop: 10 }}>
            Die Prüfung konnte nicht ausgeführt werden. Das ist keine Aussage über die Ports.
          </div>
        )}

        {reach.data && (
          <div style={{ marginTop: 14 }}>
            <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 8 }}>
              <Badge tone={reach.data.verdict.level === "ok" ? "ok" : reach.data.verdict.level === "warn" ? "warn" : "info"} size="sm">
                {reach.data.verdict.title}
              </Badge>
            </div>
            <div style={{ fontSize: 12.5, color: "var(--text-dim)", lineHeight: 1.55 }}>{reach.data.verdict.detail}</div>
            {reach.data.verdict.action && (
              <div style={{ fontSize: 12.5, color: "var(--text)", marginTop: 8, paddingLeft: 11, borderLeft: "2px solid var(--accent)", lineHeight: 1.55 }}>
                {reach.data.verdict.action}
              </div>
            )}
            {!!reach.data.result.ports?.length && (
              <div style={{ display: "flex", flexWrap: "wrap", gap: 8, marginTop: 12 }}>
                {reach.data.result.ports.map((p) => (
                  <Badge key={`${p.protocol}-${p.port}`} tone={p.status === "open" ? "ok" : p.status === "closed" ? "warn" : "info"} size="sm">
                    {p.protocol} {p.port}: {p.status === "open" ? "offen" : p.status === "closed" ? "geschlossen" : "unklar"}
                  </Badge>
                ))}
              </div>
            )}
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
