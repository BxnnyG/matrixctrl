import { createFileRoute, Link } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { api, ApiError } from "@/lib/api";
import { Button, Card, EmptyState, Icon, SectionTitle, Spinner } from "@/components/mc";
import { MatrixConnect, clearConnectAttempt } from "@/components/MatrixConnect";

export const Route = createFileRoute("/federation")({
  component: FederationPage,
  // Annotated, not inferred — see rooms.tsx (E43).
  validateSearch: (s: Record<string, unknown>): { error?: string } => ({
    error: typeof s.error === "string" ? s.error : undefined,
  }),
});

interface Step { key: string; title: string; level: "ok" | "warn" | "err" | "skip"; value?: string; detail: string }
interface Reach {
  server_name?: string; target?: string; target_from?: string; reachable?: boolean;
  software?: string; steps?: Step[]; checked_at?: string; note?: string;
}
interface Destination {
  destination: string; failure_ts: number | null; retry_last_ts: number | null;
  retry_interval: number; failing: boolean; next_retry_ts?: number;
}
interface Destinations { destinations: Destination[]; total: number; failing: number; cut: boolean }
interface DestRoom { room_id: string; name?: string; alias?: string }

const TONE = { ok: "var(--status-ok)", warn: "var(--status-warn)", err: "var(--status-err)", skip: "var(--text-faint)" } as const;

/** Federation, in the two questions an operator has about it (etappe 117): can other
 *  servers reach mine — and if not, at which step of the path they walk it breaks —
 *  and which of the servers mine talks to are failing right now. */
function FederationPage() {
  const { error } = Route.useSearch();
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 22, maxWidth: 960 }}>
      <ReachCard />
      <DestinationsSection error={error} />
    </div>
  );
}

export function ReachCard() {
  const qc = useQueryClient();
  const { data, isLoading, isFetching, error } = useQuery({
    queryKey: ["federation", "reach"],
    queryFn: () => api.get<Reach>("/api/v1/federation/reach"),
    staleTime: 5 * 60_000,
  });

  const refresh = (
    <Button variant="outline" size="sm" icon="refresh" disabled={isFetching}
      onClick={() => qc.invalidateQueries({ queryKey: ["federation", "reach"] })}>
      {isFetching ? <Spinner size={13} /> : "Neu prüfen"}
    </Button>
  );

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      <SectionTitle icon="globe" sub="Der Weg, den ein fremder Server zu deinem nimmt — Schritt für Schritt" right={refresh}>
        Erreichbarkeit
      </SectionTitle>
      {isLoading && <Card style={{ fontSize: 13, color: "var(--text-faint)" }}><Spinner size={13} /> Gehe den Weg eines fremden Servers ab…</Card>}
      {error && <Card style={{ fontSize: 13, color: "var(--status-err)" }}>{(error as Error).message}</Card>}
      {data?.note && <Card><EmptyState icon="globe" title="Kein Server-Name" sub={data.note} /></Card>}
      {data?.steps && (
        <Card style={{ display: "flex", flexDirection: "column", gap: 14 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap" }}>
            <span style={{ width: 11, height: 11, borderRadius: 999, background: data.reachable ? "var(--status-ok)" : "var(--status-err)", flexShrink: 0 }} />
            <span style={{ fontSize: 15, fontWeight: 650 }}>
              {data.reachable
                ? <>Andere Server erreichen <code style={{ fontFamily: "var(--mono)" }}>{data.server_name}</code></>
                : <>Andere Server erreichen <code style={{ fontFamily: "var(--mono)" }}>{data.server_name}</code> nicht</>}
            </span>
            {data.software && <span style={{ fontSize: 12.5, color: "var(--text-faint)", fontFamily: "var(--mono)" }}>{data.software}</span>}
          </div>
          <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
            {data.steps.map((s, i) => (
              <div key={s.key} style={{ display: "grid", gridTemplateColumns: "20px 1fr", gap: "2px 10px", alignItems: "baseline" }}>
                <span style={{ width: 9, height: 9, borderRadius: 999, background: TONE[s.level], justifySelf: "center" }} />
                <div style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "baseline", minWidth: 0 }}>
                  <span style={{ fontSize: 13, fontWeight: 600, color: "var(--text)" }}>{i + 1}. {s.title}</span>
                  {s.value && <code style={{ fontFamily: "var(--mono)", fontSize: 12, color: "var(--text-dim)", wordBreak: "break-all" }}>{s.value}</code>}
                </div>
                <span />
                <span style={{ fontSize: 12.5, color: s.level === "err" ? "var(--status-err)" : "var(--text-dim)", lineHeight: 1.55 }}>{s.detail}</span>
              </div>
            ))}
          </div>
          {/* Said, because "reachable from here" and "reachable from outside" can differ:
              a firewall that lets this server out is not a firewall that lets others in. */}
          <div style={{ fontSize: 12, color: "var(--text-faint)", lineHeight: 1.6, borderTop: "1px solid var(--border-soft)", paddingTop: 10 }}>
            Geprüft von diesem Server aus über das öffentliche Netz{data.checked_at ? `, ${new Date(data.checked_at).toLocaleTimeString("de-DE", { hour: "2-digit", minute: "2-digit" })}` : ""}.
            Den Blick von außen gibt der{" "}
            <a href={`https://federationtester.matrix.org/#${encodeURIComponent(data.server_name ?? "")}`} target="_blank" rel="noreferrer" style={{ color: "var(--accent)" }}>
              Federation Tester von matrix.org
            </a>{" "}— ein Klick von dir; MatrixCtrl schickt nichts dorthin.
          </div>
        </Card>
      )}
    </div>
  );
}

interface RoomsState { connected: boolean; reason?: string }

const SHOW = 100;

export function DestinationsSection({ error }: { error?: string }) {
  const state = useQuery({
    queryKey: ["rooms", "state"],
    queryFn: () => api.get<RoomsState>("/api/v1/rooms/state"),
  });
  const connected = state.data?.connected === true;
  const list = useQuery({
    queryKey: ["federation", "destinations"],
    queryFn: () => api.get<Destinations>("/api/v1/federation/destinations"),
    enabled: connected,
    staleTime: 30_000,
  });
  useEffect(() => {
    if (list.isSuccess) clearConnectAttempt();
  }, [list.isSuccess]);

  const [search, setSearch] = useState("");
  const [onlyFailing, setOnlyFailing] = useState(false);
  const [limit, setLimit] = useState(SHOW);
  const [open, setOpen] = useState<string | null>(null);

  const title = (sub: string) => <SectionTitle icon="server" sub={sub}>Gegenstellen</SectionTitle>;

  if (state.isLoading) return <div>{title("Server, mit denen deiner redet")}<Spinner size={13} /></div>;
  if (!connected) {
    return (
      <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
        {title("Server, mit denen deiner redet — aus Synapse, mit deinem Matrix-Zugriff")}
        <MatrixConnect reason={state.data?.reason} error={error} returnTo="/federation" auto />
      </div>
    );
  }

  const err = list.error instanceof ApiError ? list.error : null;
  const data = list.data;
  const q = search.trim().toLowerCase();
  const shown = (data?.destinations ?? []).filter((d) => (!onlyFailing || d.failing) && (!q || d.destination.toLowerCase().includes(q)));

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      {title(data ? `${data.total} Server · ${data.failing} mit Problemen` : "Server, mit denen deiner redet")}
      {err?.status === 403 && <Card style={{ fontSize: 13, color: "var(--text-dim)" }}><strong style={{ color: "var(--text)" }}>Dieses Konto hat keine Synapse-Administratorrechte.</strong> Die Berechtigung wird in Matrix vergeben, nicht hier.</Card>}
      {err?.status === 409 && <MatrixConnect returnTo="/federation" />}
      {list.error && !err && <Card style={{ fontSize: 13, color: "var(--status-err)" }}>{(list.error as Error).message}</Card>}
      {list.isLoading && <Card style={{ fontSize: 13, color: "var(--text-faint)" }}><Spinner size={13} /> Lade Gegenstellen…</Card>}
      {data && (
        <Card style={{ padding: 0, overflow: "hidden" }}>
          <div style={{ display: "flex", gap: 10, alignItems: "center", flexWrap: "wrap", padding: "12px 16px", borderBottom: "1px solid var(--border-soft)" }}>
            <input value={search} onChange={(e) => { setSearch(e.target.value); setLimit(SHOW); }} placeholder="Server suchen…"
              style={{ flex: 1, minWidth: 180, padding: "7px 10px", border: "1px solid var(--border)", background: "var(--surface-2)", color: "var(--text)", borderRadius: "var(--radius-sm)", fontSize: 13 }} />
            <label style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 12.5, color: "var(--text-dim)", cursor: "pointer" }}>
              <input type="checkbox" checked={onlyFailing} onChange={(e) => { setOnlyFailing(e.target.checked); setLimit(SHOW); }} />
              nur Probleme ({data.failing})
            </label>
          </div>
          {data.total === 0 && <EmptyState icon="globe" title="Noch keine Gegenstellen" sub="Dein Server hat noch mit keinem anderen geredet — das beginnt mit dem ersten Raum, in dem jemand von woanders ist." />}
          {data.total > 0 && shown.length === 0 && <div style={{ padding: 16, fontSize: 13, color: "var(--text-faint)" }}>{onlyFailing ? "Mit keinem Server gibt es gerade Probleme." : "Kein Server passt zur Suche."}</div>}
          {shown.slice(0, limit).map((d) => (
            <DestinationRow key={d.destination} d={d} open={open === d.destination} onToggle={() => setOpen(open === d.destination ? null : d.destination)} />
          ))}
          {shown.length > limit && (
            <div style={{ padding: 12, textAlign: "center" }}>
              <Button variant="ghost" size="sm" onClick={() => setLimit(limit + SHOW)}>Weitere {Math.min(SHOW, shown.length - limit)} von {shown.length - limit} anzeigen</Button>
            </div>
          )}
          {data.cut && <div style={{ padding: "10px 16px", fontSize: 12, color: "var(--text-faint)" }}>Die Liste endet bei {data.total} Servern; Synapse kennt mehr.</div>}
        </Card>
      )}
    </div>
  );
}

function ago(ms: number): string {
  const mins = Math.max(0, Math.round((Date.now() - ms) / 60000));
  if (mins < 60) return `${mins} Min.`;
  const h = Math.round(mins / 60);
  if (h < 48) return `${h} Std.`;
  return `${Math.round(h / 24)} Tagen`;
}

function until(ms: number): string {
  const mins = Math.round((ms - Date.now()) / 60000);
  if (mins <= 0) return "jetzt fällig";
  if (mins < 60) return `in ${mins} Min.`;
  const h = Math.round(mins / 60);
  if (h < 48) return `in ${h} Std.`;
  return `in ${Math.round(h / 24)} Tagen`;
}

function DestinationRow({ d, open, onToggle }: { d: Destination; open: boolean; onToggle: () => void }) {
  const qc = useQueryClient();
  const reset = useMutation({
    mutationFn: () => api.post(`/api/v1/federation/destinations/${encodeURIComponent(d.destination)}/reset`, {}),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["federation", "destinations"] }),
  });
  return (
    <div style={{ borderBottom: "1px solid var(--border-soft)" }}>
      <div style={{ display: "flex", alignItems: "center", gap: 12, padding: "10px 16px", flexWrap: "wrap" }}>
        <span style={{ width: 8, height: 8, borderRadius: 999, background: d.failing ? "var(--status-err)" : "var(--status-ok)", flexShrink: 0 }} />
        <code style={{ fontFamily: "var(--mono)", fontSize: 12.5, color: "var(--text)", flex: "1 1 200px", minWidth: 0, wordBreak: "break-all" }}>{d.destination}</code>
        <span style={{ fontSize: 12, color: d.failing ? "var(--status-err)" : "var(--text-faint)", flex: "0 1 auto" }}>
          {d.failing && d.failure_ts
            ? <>scheitert seit {ago(d.failure_ts)}{d.next_retry_ts ? <span style={{ color: "var(--text-faint)" }}> · nächster Versuch {until(d.next_retry_ts)}</span> : null}</>
            : "in Ordnung"}
        </span>
        <span style={{ display: "flex", gap: 6 }}>
          {d.failing && (
            <Button size="sm" variant="soft" icon={reset.isPending ? undefined : "refresh"} disabled={reset.isPending} onClick={() => reset.mutate()}
              title="Synapse wartet nach Fehlern immer länger bis zum nächsten Versuch — bis zu Tagen. Das setzt die Wartezeit zurück.">
              {reset.isPending ? <Spinner size={12} /> : "Jetzt neu versuchen"}
            </Button>
          )}
          <Button size="sm" variant="ghost" icon="room" onClick={onToggle}>{open ? "Räume ausblenden" : "Räume"}</Button>
        </span>
      </div>
      {reset.isError && <div style={{ padding: "0 16px 10px 36px", fontSize: 12, color: "var(--status-err)" }}>{(reset.error as Error).message}</div>}
      {reset.isSuccess && <div style={{ padding: "0 16px 10px 36px", fontSize: 12, color: "var(--text-dim)" }}>Zurückgesetzt — Synapse versucht es mit der nächsten Nachricht an diesen Server.</div>}
      {open && <DestinationRooms destination={d.destination} />}
    </div>
  );
}

function DestinationRooms({ destination }: { destination: string }) {
  const { data, isLoading, error } = useQuery({
    queryKey: ["federation", "rooms", destination],
    queryFn: () => api.get<{ rooms: DestRoom[]; total: number }>(`/api/v1/federation/destinations/${encodeURIComponent(destination)}/rooms`),
  });
  return (
    <div style={{ padding: "0 16px 12px 36px", display: "flex", flexDirection: "column", gap: 4 }}>
      {isLoading && <span style={{ fontSize: 12, color: "var(--text-faint)" }}><Spinner size={12} /> Lade Räume…</span>}
      {error && <span style={{ fontSize: 12, color: "var(--status-err)" }}>{(error as Error).message}</span>}
      {data && data.rooms.length === 0 && <span style={{ fontSize: 12, color: "var(--text-faint)" }}>Keine gemeinsamen Räume mehr.</span>}
      {data?.rooms.map((r) => (
        <Link key={r.room_id} to="/rooms/$id" params={{ id: r.room_id }} style={{ display: "flex", gap: 8, alignItems: "baseline", fontSize: 12.5, color: "var(--text)", textDecoration: "none" }}>
          <Icon name="room" size={12} style={{ color: "var(--text-faint)" }} />
          <span>{r.name || r.alias || "Unbenannter Raum"}</span>
          <code style={{ fontFamily: "var(--mono)", fontSize: 11, color: "var(--text-faint)" }}>{r.room_id}</code>
        </Link>
      ))}
      {data && data.total > data.rooms.length && <span style={{ fontSize: 12, color: "var(--text-faint)" }}>… und {data.total - data.rooms.length} weitere</span>}
    </div>
  );
}
