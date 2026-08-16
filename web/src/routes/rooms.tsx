import { createFileRoute, Link } from "@tanstack/react-router";
import { useQuery, keepPreviousData } from "@tanstack/react-query";
import { useState } from "react";
import { api, ApiError } from "@/lib/api";
import { Card, Badge, EmptyState, Button, SectionTitle, Spinner } from "@/components/mc";
import { MatrixConnect } from "@/components/MatrixConnect";

export const Route = createFileRoute("/rooms")({
  component: Rooms,
  // The return type is annotated with `error?:` rather than inferred. Inference
  // produces `{ error: string | undefined }`, and TanStack reads a key that is
  // merely *nullable* as required — which makes every `<Link to="/rooms">` in the
  // app a type error demanding a `search` prop. `tsc -b` catches it; the bare
  // `tsc --noEmit` in the old CLAUDE.md checked nothing at all (E43).
  validateSearch: (s: Record<string, unknown>): { error?: string } => ({
    error: typeof s.error === "string" ? s.error : undefined,
  }),
});

interface Room {
  room_id: string;
  name: string;
  canonical_alias: string;
  joined_members: number;
  joined_local_members: number;
  version: string;
  creator: string;
  encryption: string;
  federatable: boolean;
  public: boolean;
  join_rules: string;
  history_visibility: string;
  state_events: number;
  room_type: string;
}

interface RoomPage {
  rooms: Room[] | null;
  offset: number;
  total_rooms: number;
  next_batch?: number | null;
  prev_batch?: number | null;
}

interface RoomsState {
  connected: boolean;
  reason?: string;
}

const PAGE = 50;

function Rooms() {
  const { error } = Route.useSearch();
  const [from, setFrom] = useState(0);
  const [search, setSearch] = useState("");
  const [applied, setApplied] = useState("");

  const state = useQuery({
    queryKey: ["rooms", "state"],
    queryFn: () => api.get<RoomsState>("/api/v1/rooms/state"),
  });

  const connected = state.data?.connected === true;

  const rooms = useQuery({
    queryKey: ["rooms", "list", from, applied],
    queryFn: () => {
      const q = new URLSearchParams({ from: String(from), limit: String(PAGE) });
      if (applied) q.set("search", applied);
      return api.get<RoomPage>(`/api/v1/rooms?${q}`);
    },
    enabled: connected,
    placeholderData: keepPreviousData,
  });

  if (state.isLoading) {
    return <div style={{ padding: 28, color: "var(--text-faint)", fontSize: 13 }}><Spinner size={14} /> Laden…</div>;
  }

  if (!connected) {
    return (
      <div style={{ padding: 28, display: "flex", flexDirection: "column", gap: 18 }}>
        <SectionTitle sub="Räume auf diesem Server">Räume</SectionTitle>
        <MatrixConnect reason={state.data?.reason} error={error} />
      </div>
    );
  }

  // 403 means this account is not a Synapse admin — signing in again will never fix
  // it, so it must not be offered as the remedy. 409 means the Matrix token lapsed,
  // which reconnecting does fix. Deliberately not 401: that ends the whole session.
  const err = rooms.error instanceof ApiError ? rooms.error : null;
  const notAdmin = err?.status === 403;
  const needsConnect = err?.status === 409;

  const list = rooms.data?.rooms ?? [];
  const total = rooms.data?.total_rooms ?? 0;

  return (
    <div style={{ padding: 28, display: "flex", flexDirection: "column", gap: 18 }}>
      <SectionTitle sub={total > 0 ? `${total} Räume` : "Räume auf diesem Server"}>Räume</SectionTitle>

      {notAdmin && (
        <Card>
          <div style={{ padding: 20, fontSize: 13, color: "var(--text-dim)", lineHeight: 1.6 }}>
            <strong style={{ color: "var(--text)" }}>Dieses Konto hat keine Synapse-Administratorrechte.</strong>
            <div style={{ marginTop: 6 }}>
              Eine erneute Anmeldung ändert daran nichts — die Berechtigung wird in Matrix
              vergeben, nicht hier.
            </div>
          </div>
        </Card>
      )}

      {/* A 409 after a successful connect used to render exactly the same panel as
          "never connected", which is why the report read as "the button does
          nothing" — it had in fact worked, and the token was being refused (E42). */}
      {needsConnect && (
        <MatrixConnect reason="Der Matrix-Zugriff wurde abgelehnt oder ist abgelaufen. Ein erneutes Verbinden fordert die Rechte neu an." />
      )}

      {!notAdmin && !needsConnect && (
        <>
          <form
            onSubmit={(e) => { e.preventDefault(); setFrom(0); setApplied(search.trim()); }}
            style={{ display: "flex", gap: 8, alignItems: "center" }}
          >
            <input
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Name oder Alias suchen…"
              style={{ flex: "0 1 320px", padding: "8px 11px", border: "1px solid var(--border)", background: "var(--surface-2)", color: "var(--text)", borderRadius: "var(--radius-sm)", fontSize: 13, fontFamily: "var(--font)" }}
            />
            <Button variant="soft" size="sm" icon="search" type="submit">Suchen</Button>
            {applied && (
              <Button variant="ghost" size="sm" onClick={() => { setSearch(""); setApplied(""); setFrom(0); }}>
                Zurücksetzen
              </Button>
            )}
          </form>

          <Card>
            {rooms.isFetching && list.length === 0 ? (
              <div style={{ padding: 24, color: "var(--text-faint)", fontSize: 13 }}><Spinner size={14} /> Laden…</div>
            ) : list.length === 0 ? (
              <EmptyState
                icon="room"
                title={applied ? "Keine Treffer" : "Keine Räume"}
                sub={applied ? "Andere Suche versuchen." : "Auf diesem Server existieren noch keine Räume."}
              />
            ) : (
              <div style={{ overflowX: "auto" }}>
                <table style={{ width: "100%", borderCollapse: "collapse", fontSize: 13 }}>
                  <thead>
                    <tr style={{ textAlign: "left", color: "var(--text-faint)", fontSize: 12 }}>
                      <th style={{ padding: "9px 12px", fontWeight: 600 }}>Raum</th>
                      <th style={{ padding: "9px 12px", fontWeight: 600 }}>Mitglieder</th>
                      <th style={{ padding: "9px 12px", fontWeight: 600 }}>Zugang</th>
                      <th style={{ padding: "9px 12px", fontWeight: 600 }}>Events</th>
                    </tr>
                  </thead>
                  <tbody>
                    {list.map((r) => (
                      <tr key={r.room_id} style={{ borderTop: "1px solid var(--border)" }}>
                        <td style={{ padding: "10px 12px" }}>
                          {/* The room ID goes in the path, and a room ID is not a
                              word: `!AbCd:example.org`. Link's params encode it. */}
                          <Link to="/rooms/$id" params={{ id: r.room_id }} style={{ textDecoration: "none" }}>
                            <div style={{ color: "var(--text)", fontWeight: 550 }}>
                              {r.name || r.canonical_alias || r.room_id}
                            </div>
                            <div style={{ color: "var(--text-faint)", fontSize: 11.5, fontFamily: "var(--font-mono)" }}>
                              {r.canonical_alias || r.room_id}
                            </div>
                          </Link>
                        </td>
                        <td style={{ padding: "10px 12px", color: "var(--text-dim)" }}>
                          {r.joined_members}
                          {r.joined_local_members !== r.joined_members && (
                            <span style={{ color: "var(--text-faint)" }}> ({r.joined_local_members} lokal)</span>
                          )}
                        </td>
                        <td style={{ padding: "10px 12px", display: "flex", gap: 6, flexWrap: "wrap" }}>
                          {r.public ? <Badge tone="warn">öffentlich</Badge> : <Badge>privat</Badge>}
                          {r.encryption && <Badge tone="ok">verschlüsselt</Badge>}
                          {r.room_type === "m.space" && <Badge>Space</Badge>}
                        </td>
                        <td style={{ padding: "10px 12px", color: "var(--text-dim)" }}>{r.state_events}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Card>

          {/* Synapse pages by offset and hands back a real total, unlike MAS's cursors
              on the users page. Showing the range is honest about which of the two
              this is. */}
          {(list.length > 0 || from > 0) && (
            <div style={{ display: "flex", alignItems: "center", gap: 10, fontSize: 12.5, color: "var(--text-faint)" }}>
              <Button
                variant="ghost" size="sm" icon="chevLeft"
                disabled={from === 0 || rooms.isFetching}
                onClick={() => setFrom(Math.max(0, from - PAGE))}
              >
                Zurück
              </Button>
              <span>{from + 1}–{from + list.length} von {total}</span>
              <Button
                variant="ghost" size="sm" iconRight="chevRight"
                disabled={rooms.data?.next_batch == null || rooms.isFetching}
                onClick={() => setFrom(from + PAGE)}
              >
                Weiter
              </Button>
            </div>
          )}
        </>
      )}
    </div>
  );
}
