import { createFileRoute, Link } from "@tanstack/react-router";
import { useQuery, useMutation, useQueryClient, keepPreviousData } from "@tanstack/react-query";
import { useState } from "react";
import { api, ApiError } from "@/lib/api";
import { Card, Badge, Icon, Button, SectionTitle, Spinner, EmptyState } from "@/components/mc";

export const Route = createFileRoute("/rooms/$id")({ component: RoomDetail });

interface RoomDetail {
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
  guest_access: string;
  history_visibility: string;
  state_events: number;
  room_type: string;
  blocked: boolean;
}

interface MemberPage {
  members: string[];
  total: number;
  offset: number;
  returned: number;
}

const PAGE = 50;

/** What blocking actually does, stated before the operator acts rather than after.
 *
 *  Synapse's block flag refuses *new joins* and nothing else. Everyone already in
 *  the room stays, every message stays, and the conversation carries on. The word
 *  suggests something far more final — the mirror image of E28's finding, where
 *  `deactivate` quietly did *more* than it sounded like. */
function BlockPanel({ room, onChanged }: { room: RoomDetail; onChanged: () => void }) {
  const [confirming, setConfirming] = useState(false);

  const setBlocked = useMutation({
    mutationFn: (block: boolean) =>
      api.put<{ blocked: boolean }>(`/api/v1/rooms/${encodeURIComponent(room.room_id)}/block`, { block }),
    onSuccess: () => { setConfirming(false); onChanged(); },
  });

  const err = setBlocked.error instanceof ApiError ? setBlocked.error : null;

  return (
    <Card>
      <div style={{ padding: 20, display: "flex", flexDirection: "column", gap: 12, alignItems: "flex-start" }}>
        <div style={{ display: "flex", alignItems: "center", gap: 9 }}>
          <Icon name="shield" size={16} />
          <span style={{ fontSize: 14, fontWeight: 650, color: "var(--text)" }}>Beitritt</span>
          {room.blocked
            ? <Badge tone="err">gesperrt</Badge>
            : <Badge tone="ok">offen</Badge>}
        </div>

        <p style={{ fontSize: 13, color: "var(--text-dim)", lineHeight: 1.65, maxWidth: 640, margin: 0 }}>
          {room.blocked
            ? "Neue Beitritte werden abgelehnt. Wer bereits im Raum ist, bleibt drin und kann weiter schreiben."
            : "Eine Sperre verhindert ausschließlich neue Beitritte."}
        </p>

        {!room.blocked && (
          <p style={{ fontSize: 12.5, color: "var(--text-faint)", lineHeight: 1.6, maxWidth: 640, margin: 0 }}>
            Sie entfernt <strong>niemanden</strong> aus dem Raum, löscht <strong>keine</strong> Nachrichten
            und beendet <strong>kein</strong> laufendes Gespräch. Wer ein akutes Problem stoppen will, ist mit
            einer Sperre allein nicht fertig.
          </p>
        )}

        {err && (
          <div style={{ fontSize: 13, color: "var(--status-err)", background: "color-mix(in oklch, var(--status-err) 10%, var(--surface))", border: "1px solid color-mix(in oklch, var(--status-err) 30%, var(--border))", borderRadius: "var(--radius-sm)", padding: "9px 11px" }}>
            {err.message}
          </div>
        )}

        {/* Unblocking needs no confirmation: it restores the default and nothing is
            lost by doing it. Blocking does, because it changes what other people
            can do. */}
        {room.blocked ? (
          <Button variant="soft" size="sm" icon="rotate"
            disabled={setBlocked.isPending}
            onClick={() => setBlocked.mutate(false)}>
            {setBlocked.isPending ? "Wird aufgehoben…" : "Sperre aufheben"}
          </Button>
        ) : confirming ? (
          <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
            <Button variant="danger" size="sm"
              disabled={setBlocked.isPending}
              onClick={() => setBlocked.mutate(true)}>
              {setBlocked.isPending ? "Wird gesperrt…" : "Ja, neue Beitritte sperren"}
            </Button>
            <Button variant="ghost" size="sm" onClick={() => setConfirming(false)}>Abbrechen</Button>
          </div>
        ) : (
          <Button variant="soft" size="sm" icon="shield" onClick={() => setConfirming(true)}>
            Neue Beitritte sperren
          </Button>
        )}
      </div>
    </Card>
  );
}

function Fact({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 3, minWidth: 0 }}>
      <span style={{ fontSize: 11, color: "var(--text-faint)", textTransform: "uppercase", letterSpacing: 0.4 }}>{label}</span>
      <span style={{ fontSize: 13, color: "var(--text)", overflow: "hidden", textOverflow: "ellipsis" }}>{children}</span>
    </div>
  );
}

function RoomDetail() {
  const { id } = Route.useParams();
  const qc = useQueryClient();
  const [from, setFrom] = useState(0);

  const room = useQuery({
    queryKey: ["rooms", "detail", id],
    queryFn: () => api.get<RoomDetail>(`/api/v1/rooms/${encodeURIComponent(id)}`),
  });

  const members = useQuery({
    queryKey: ["rooms", "members", id, from],
    queryFn: () => api.get<MemberPage>(`/api/v1/rooms/${encodeURIComponent(id)}/members?from=${from}&limit=${PAGE}`),
    enabled: !!room.data,
    placeholderData: keepPreviousData,
  });

  if (room.isLoading) {
    return <div style={{ padding: 28, color: "var(--text-faint)", fontSize: 13 }}><Spinner size={14} /> Laden…</div>;
  }

  // 403 is "not a Synapse admin" and signing in again never fixes it; 409 is an
  // expired Matrix token, which reconnecting does fix. 404 is about the room, not
  // about the session — collapsing it into either would send the operator to the
  // wrong remedy (E36).
  const err = room.error instanceof ApiError ? room.error : null;
  if (err) {
    return (
      <div style={{ padding: 28, display: "flex", flexDirection: "column", gap: 16 }}>
        <Link to="/rooms" style={{ fontSize: 13, color: "var(--text-dim)", textDecoration: "none" }}>← Räume</Link>
        <Card>
          <div style={{ padding: 20, fontSize: 13, color: "var(--text-dim)", lineHeight: 1.6 }}>
            <strong style={{ color: "var(--text)" }}>
              {err.status === 403 ? "Dieses Konto hat keine Synapse-Administratorrechte."
                : err.status === 409 ? "Der Matrix-Zugriff ist abgelaufen."
                : err.status === 404 ? "Diesen Raum gibt es auf diesem Server nicht."
                : "Der Raum konnte nicht geladen werden."}
            </strong>
            {err.status === 409 && (
              <div style={{ marginTop: 8 }}>
                <Link to="/rooms" style={{ color: "var(--accent)" }}>Auf der Raumliste neu verbinden</Link>
              </div>
            )}
          </div>
        </Card>
      </div>
    );
  }

  const r = room.data!;
  const list = members.data?.members ?? [];
  const total = members.data?.total ?? 0;

  return (
    <div style={{ padding: 28, display: "flex", flexDirection: "column", gap: 18 }}>
      <Link to="/rooms" style={{ fontSize: 13, color: "var(--text-dim)", textDecoration: "none" }}>← Räume</Link>

      <SectionTitle sub={r.canonical_alias || r.room_id}>
        {r.name || r.canonical_alias || r.room_id}
      </SectionTitle>

      <Card>
        <div style={{ padding: 20, display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(150px, 1fr))", gap: 18 }}>
          <Fact label="Mitglieder">
            {r.joined_members}
            {r.joined_local_members !== r.joined_members && (
              <span style={{ color: "var(--text-faint)" }}> ({r.joined_local_members} lokal)</span>
            )}
          </Fact>
          <Fact label="Zugang">
            <span style={{ display: "inline-flex", gap: 6, flexWrap: "wrap" }}>
              {r.public ? <Badge tone="warn">öffentlich</Badge> : <Badge>privat</Badge>}
              {r.encryption && <Badge tone="ok">verschlüsselt</Badge>}
            </span>
          </Fact>
          <Fact label="Beitrittsregel">{r.join_rules || "—"}</Fact>
          <Fact label="Historie sichtbar für">{r.history_visibility || "—"}</Fact>
          <Fact label="Ersteller">
            <span style={{ fontFamily: "var(--font-mono)", fontSize: 12 }}>{r.creator || "—"}</span>
          </Fact>
          <Fact label="Raumversion">{r.version || "—"}</Fact>
          <Fact label="Föderierbar">{r.federatable ? "ja" : "nein"}</Fact>
          <Fact label="State-Events">{r.state_events}</Fact>
        </div>
      </Card>

      <BlockPanel room={r} onChanged={() => qc.invalidateQueries({ queryKey: ["rooms", "detail", id] })} />

      <Card>
        <div style={{ padding: "14px 18px 6px", display: "flex", alignItems: "center", gap: 9 }}>
          <Icon name="users" size={15} />
          <span style={{ fontSize: 14, fontWeight: 650, color: "var(--text)" }}>Mitglieder</span>
          {total > 0 && <span style={{ fontSize: 12, color: "var(--text-faint)" }}>{total}</span>}
        </div>
        {members.isFetching && list.length === 0 ? (
          <div style={{ padding: 20, color: "var(--text-faint)", fontSize: 13 }}><Spinner size={14} /> Laden…</div>
        ) : list.length === 0 ? (
          <EmptyState icon="users" title="Keine Mitglieder" sub="In diesem Raum ist niemand." />
        ) : (
          <div style={{ padding: "4px 18px 16px" }}>
            {list.map((m) => (
              <div key={m} style={{ padding: "7px 0", borderTop: "1px solid var(--border-soft)", fontSize: 12.5, fontFamily: "var(--font-mono)", color: "var(--text-dim)" }}>
                {m}
              </div>
            ))}
          </div>
        )}
      </Card>

      {(list.length > 0 || from > 0) && (
        <div style={{ display: "flex", alignItems: "center", gap: 10, fontSize: 12.5, color: "var(--text-faint)" }}>
          <Button variant="ghost" size="sm" icon="chevLeft"
            disabled={from === 0 || members.isFetching}
            onClick={() => setFrom(Math.max(0, from - PAGE))}>Zurück</Button>
          <span>{from + 1}–{from + list.length} von {total}</span>
          <Button variant="ghost" size="sm" iconRight="chevRight"
            disabled={from + list.length >= total || members.isFetching}
            onClick={() => setFrom(from + PAGE)}>Weiter</Button>
        </div>
      )}
    </div>
  );
}
