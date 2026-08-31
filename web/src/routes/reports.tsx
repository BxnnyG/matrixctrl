import { createFileRoute } from "@tanstack/react-router";
import { useQuery, useMutation, useQueryClient, keepPreviousData } from "@tanstack/react-query";
import { useState, useEffect } from "react";
import { api, ApiError } from "@/lib/api";
import { Card, Badge, Icon, EmptyState, Button, SectionTitle, Spinner, Tabs } from "@/components/mc";
import { MatrixConnect, clearConnectAttempt } from "@/components/MatrixConnect";

export const Route = createFileRoute("/reports")({
  component: Reports,
  validateSearch: (s: Record<string, unknown>): { error?: string } => ({
    error: typeof s.error === "string" ? s.error : undefined,
  }),
});

type State = "open" | "handled" | "dismissed";

interface Report {
  id: number;
  event_id: string;
  room_id: string;
  name?: string;
  canonical_alias?: string;
  /** Who filed the report. Synapse calls this `user_id`. */
  reporter: string;
  /** Who wrote the reported event. Synapse calls this `sender`. */
  sender: string;
  reason?: string;
  received_ts: number;
  state: State;
  note?: string;
  actor?: string;
}

interface ReportList {
  reports: Report[] | null;
  total: number;
  open_on_page: number;
  next_token?: number | null;
}

/** A reported *user*. Far thinner than an event report: no room, no event, no score. */
interface UserReport {
  id: number;
  /** Who filed the report. Synapse calls this `user_id`. */
  reporter: string;
  /** Who was reported. Synapse calls this `target_user_id`. */
  target: string;
  reason?: string;
  received_ts: number;
  state: State;
  note?: string;
  actor?: string;
}

interface UserReportList {
  reports: UserReport[] | null;
  total: number;
  open_on_page: number;
  next_token?: number | null;
}

interface MediaRow {
  server: string;
  id: string;
  kind: "media" | "thumbnail" | "encrypted";
  mxc: string;
  quarantined: boolean;
  by?: string;
  protected: boolean;
  unknown?: boolean;
}

interface QuarantineResult {
  requested: boolean;
  quarantined: boolean;
  by?: string;
  /** False when Synapse accepted the request and changed nothing — which it does,
   *  with a 200 and an empty body, for protected media. */
  changed: boolean;
  protected: boolean;
}

interface ReportDetail {
  report: Report;
  body: string;
  event_type: string;
  media: MediaRow[] | null;
}

const KIND_LABEL: Record<MediaRow["kind"], string> = {
  media: "Datei",
  thumbnail: "Vorschaubild",
  encrypted: "Datei (verschlüsselt)",
};

/** One media item from the reported event, with the state Synapse actually holds.
 *
 *  The quarantine endpoint answers `200 {}` whatever it did, and silently skips
 *  media marked safe_from_quarantine — so this panel never echoes the request back.
 *  Every state shown here was read from Synapse after the write (E47). */
function MediaItem({ m, onDone }: { m: MediaRow; onDone: () => void }) {
  const [result, setResult] = useState<QuarantineResult | null>(null);

  const act = useMutation({
    mutationFn: (quarantine: boolean) =>
      api.put<QuarantineResult>(`/api/v1/media/${encodeURIComponent(m.server)}/${encodeURIComponent(m.id)}/quarantine`, { quarantine }),
    onSuccess: (r) => { setResult(r); onDone(); },
  });

  const quarantined = result ? result.quarantined : m.quarantined;

  return (
    <div style={{ display: "flex", alignItems: "flex-start", gap: 10, padding: "8px 0", borderTop: "1px solid var(--border-soft)" }}>
      <div style={{ flex: 1, minWidth: 0 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
          <span style={{ fontSize: 12, color: "var(--text-dim)" }}>{KIND_LABEL[m.kind]}</span>
          {quarantined && <Badge tone="err" size="sm">gesperrt</Badge>}
          {m.protected && <Badge tone="neutral" size="sm">geschützt</Badge>}
          {m.unknown && <Badge tone="neutral" size="sm">Status unbekannt</Badge>}
        </div>
        <div style={{ fontFamily: "var(--mono)", fontSize: 11.5, color: "var(--text-faint)", overflowWrap: "anywhere", marginTop: 2 }}>
          {m.mxc}
        </div>

        {/* The whole point of the etappe: Synapse answers 200 either way, so when
            nothing changed the operator has to be told, or they walk away believing
            they took something down. */}
        {result && !result.changed && (
          <div style={{ fontSize: 11.5, color: "var(--status-warn)", marginTop: 4, lineHeight: 1.5 }}>
            {result.protected
              ? "Synapse hat die Anfrage angenommen und nichts geändert — diese Datei ist vor Quarantäne geschützt."
              : "Synapse hat die Anfrage angenommen, der Status ist danach aber unverändert."}
          </div>
        )}
        {result?.changed && result.by && (
          <div style={{ fontSize: 11.5, color: "var(--text-faint)", marginTop: 4 }}>gesperrt von {result.by}</div>
        )}
      </div>

      <Button
        variant={quarantined ? "outline" : "soft"}
        size="sm"
        disabled={act.isPending || m.unknown}
        onClick={() => act.mutate(!quarantined)}>
        {act.isPending ? "…" : quarantined ? "Freigeben" : "Sperren"}
      </Button>
    </div>
  );
}

interface MatrixState { connected: boolean; reason?: string }

const PAGE = 50;

const STATE_LABEL: Record<State, string> = {
  open: "offen",
  handled: "bearbeitet",
  dismissed: "verworfen",
};
const STATE_TONE: Record<State, "warn" | "ok" | "neutral"> = {
  open: "warn",
  handled: "ok",
  dismissed: "neutral",
};

function fmtTime(ms: number): string {
  if (!ms) return "—";
  return new Date(ms).toLocaleString("de-DE", { dateStyle: "short", timeStyle: "short" });
}

/** One report, expandable to the reported event and the disposition controls. */
function ReportRow({ r, last }: { r: Report; last: boolean }) {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [note, setNote] = useState(r.note ?? "");

  const detail = useQuery({
    queryKey: ["reports", "detail", r.id],
    queryFn: () => api.get<ReportDetail>(`/api/v1/reports/${r.id}`),
    enabled: open,
  });

  const decide = useMutation({
    mutationFn: (state: State) =>
      api.put(`/api/v1/reports/${r.id}/disposition`, { state, note }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["reports"] }),
  });

  return (
    <div style={{ borderTop: last ? "none" : "1px solid var(--border)" }}>
      <div
        onClick={() => setOpen((v) => !v)}
        style={{ display: "flex", alignItems: "flex-start", gap: 12, padding: "12px 14px", cursor: "pointer" }}>
        <Icon name={open ? "chevDown" : "chevRight"} size={13} style={{ color: "var(--text-faint)", flexShrink: 0, marginTop: 3 }} />
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
            <Badge tone={STATE_TONE[r.state]} size="sm">{STATE_LABEL[r.state]}</Badge>
            <span style={{ fontSize: 13, color: "var(--text)", fontWeight: 550 }}>
              {r.name || r.canonical_alias || r.room_id}
            </span>
            <span style={{ fontSize: 11.5, color: "var(--text-faint)" }}>{fmtTime(r.received_ts)}</span>
          </div>

          {/* Two user IDs on one record, and mixing them up accuses the wrong
              person. Neither is ever rendered without its role. */}
          <div style={{ display: "flex", gap: 16, flexWrap: "wrap", marginTop: 5, fontSize: 12 }}>
            <span style={{ color: "var(--text-faint)" }}>
              gemeldet von <span style={{ fontFamily: "var(--mono)", color: "var(--text-dim)" }}>{r.reporter}</span>
            </span>
            <span style={{ color: "var(--text-faint)" }}>
              Absender <span style={{ fontFamily: "var(--mono)", color: "var(--text-dim)" }}>{r.sender}</span>
            </span>
          </div>

          {r.reason && (
            <div style={{ fontSize: 12.5, color: "var(--text-dim)", marginTop: 6, overflowWrap: "anywhere" }}>
              „{r.reason}"
            </div>
          )}
        </div>
      </div>

      {open && (
        <div style={{ padding: "0 14px 16px 39px", display: "flex", flexDirection: "column", gap: 12 }}>
          {detail.isFetching && !detail.data && (
            <div style={{ fontSize: 12.5, color: "var(--text-faint)" }}><Spinner size={13} /> Lade gemeldetes Ereignis…</div>
          )}

          {detail.data && (
            <div style={{ background: "var(--surface-2)", border: "1px solid var(--border)", borderRadius: "var(--radius-sm)", padding: "10px 12px" }}>
              <div style={{ fontSize: 11.5, color: "var(--text-faint)", marginBottom: 5, fontFamily: "var(--mono)" }}>
                {detail.data.event_type || "unbekannter Typ"}
              </div>
              {detail.data.body ? (
                <div style={{ fontSize: 13, color: "var(--text)", lineHeight: 1.6, whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>
                  {detail.data.body}
                </div>
              ) : (
                // Not a failure. An encrypted event is the case where the admin must
                // know the *server* cannot read it either.
                <div style={{ fontSize: 12.5, color: "var(--text-faint)", lineHeight: 1.6 }}>
                  {detail.data.event_type === "m.room.encrypted"
                    ? "Verschlüsselt — der Server kann den Inhalt nicht lesen, MatrixCtrl also auch nicht."
                    : "Kein Textinhalt (z. B. Bild, Reaktion oder Zustandsänderung)."}
                </div>
              )}
            </div>
          )}

          {detail.data && (
            <div>
              <div style={{ fontSize: 12, fontWeight: 600, color: "var(--text-dim)", marginBottom: 2 }}>
                Dateien im gemeldeten Ereignis
              </div>
              {(detail.data.media ?? []).length === 0 ? (
                // An event with no media is the common case, and must not look like
                // a panel that failed to load.
                <div style={{ fontSize: 12, color: "var(--text-faint)" }}>Keine Dateien.</div>
              ) : (
                (detail.data.media ?? []).map((m) => (
                  <MediaItem key={m.mxc + m.kind} m={m}
                    onDone={() => qc.invalidateQueries({ queryKey: ["reports", "detail", r.id] })} />
                ))
              )}
            </div>
          )}

          <DispositionControls
            state={r.state} note={note} setNote={setNote} actor={r.actor}
            pending={decide.isPending} onDecide={(s) => decide.mutate(s)} />
        </div>
      )}
    </div>
  );
}

/**
 * The disposition footer, shared by both queues.
 *
 * Extracted rather than copied when the user queue arrived (etappe 48): the states,
 * the wording and the reopen semantics are the same for both, and two copies are how
 * the two screens start telling the operator different things about what a status
 * means.
 */
function DispositionControls({ state, note, setNote, actor, pending, onDecide }: {
  state: State;
  note: string;
  setNote: (v: string) => void;
  actor?: string;
  pending: boolean;
  onDecide: (s: State) => void;
}) {
  return (
    <>
      <div style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap" }}>
        <input
          value={note}
          onChange={(e) => setNote(e.target.value)}
          placeholder="Notiz (optional)"
          style={{ flex: "1 1 240px", padding: "7px 10px", border: "1px solid var(--border)", background: "var(--surface-2)", color: "var(--text)", borderRadius: "var(--radius-sm)", fontSize: 12.5, fontFamily: "var(--font)" }}
        />
        {state === "open" ? (
          <>
            <Button variant="soft" size="sm" icon="check" disabled={pending}
              onClick={() => onDecide("handled")}>Bearbeitet</Button>
            <Button variant="ghost" size="sm" disabled={pending}
              onClick={() => onDecide("dismissed")}>Verwerfen</Button>
          </>
        ) : (
          <Button variant="outline" size="sm" disabled={pending}
            onClick={() => onDecide("open" as State)}>Wieder öffnen</Button>
        )}
      </div>

      {actor && (
        <div style={{ fontSize: 11.5, color: "var(--text-faint)" }}>
          entschieden von {actor}
        </div>
      )}

      {/* Said once per report rather than once per page: this is the sentence
          that explains why the queue never shrinks upstream. */}
      <div style={{ fontSize: 11.5, color: "var(--text-faint)", lineHeight: 1.55 }}>
        Der Status wird nur in MatrixCtrl gespeichert. Die Meldung selbst bleibt in
        Synapse erhalten — so bleibt sichtbar, wenn dieselbe Person mehrfach
        gemeldet wurde.
      </div>
    </>
  );
}

/**
 * One reported *user*.
 *
 * Deliberately not a copy of ReportRow: there is no detail fetch, because Synapse's
 * `/user_reports/<id>` returns exactly the five fields the list already carried
 * (etappe 48). Everything shown here is already in hand when the row renders.
 */
function UserReportRow({ r, last }: { r: UserReport; last: boolean }) {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [note, setNote] = useState(r.note ?? "");

  const decide = useMutation({
    mutationFn: (state: State) =>
      api.put(`/api/v1/reports/users/${r.id}/disposition`, { state, note }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["reports"] }),
  });

  return (
    <div style={{ borderTop: last ? "none" : "1px solid var(--border)" }}>
      <div
        onClick={() => setOpen((v) => !v)}
        style={{ display: "flex", alignItems: "flex-start", gap: 12, padding: "12px 14px", cursor: "pointer" }}>
        <Icon name={open ? "chevDown" : "chevRight"} size={13} style={{ color: "var(--text-faint)", flexShrink: 0, marginTop: 3 }} />
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
            <Badge tone={STATE_TONE[r.state]} size="sm">{STATE_LABEL[r.state]}</Badge>
            {/* The reported user is the headline — it is what the row is about. */}
            <span style={{ fontSize: 13, color: "var(--text)", fontWeight: 550, fontFamily: "var(--mono)", overflowWrap: "anywhere" }}>
              {r.target}
            </span>
            <span style={{ fontSize: 11.5, color: "var(--text-faint)" }}>{fmtTime(r.received_ts)}</span>
          </div>

          {/* Same rule as the event queue: two user IDs, neither ever rendered
              without its role. Here they are both plain user ids, so there is
              nothing in the value itself to tell them apart. */}
          <div style={{ marginTop: 5, fontSize: 12, color: "var(--text-faint)" }}>
            gemeldet von <span style={{ fontFamily: "var(--mono)", color: "var(--text-dim)" }}>{r.reporter}</span>
          </div>

          {r.reason && (
            <div style={{ fontSize: 12.5, color: "var(--text-dim)", marginTop: 6, overflowWrap: "anywhere" }}>
              „{r.reason}"
            </div>
          )}
        </div>
      </div>

      {open && (
        <div style={{ padding: "0 14px 16px 39px", display: "flex", flexDirection: "column", gap: 12 }}>
          <DispositionControls
            state={r.state} note={note} setNote={setNote} actor={r.actor}
            pending={decide.isPending} onDecide={(s) => decide.mutate(s)} />
        </div>
      )}
    </div>
  );
}

type Queue = "events" | "users";

function Reports() {
  const { error } = Route.useSearch();
  const [queue, setQueue] = useState<Queue>("events");
  // One offset per queue. A shared one would silently page the other queue to an
  // offset nobody asked for the moment the tab is switched.
  const [from, setFrom] = useState(0);
  const [userFrom, setUserFrom] = useState(0);
  const [showAll, setShowAll] = useState(false);

  const state = useQuery({
    queryKey: ["rooms", "state"],
    queryFn: () => api.get<MatrixState>("/api/v1/rooms/state"),
  });
  const connected = state.data?.connected === true;

  const list = useQuery({
    queryKey: ["reports", "list", from],
    queryFn: () => api.get<ReportList>(`/api/v1/reports?from=${from}&limit=${PAGE}`),
    enabled: connected,
    placeholderData: keepPreviousData,
  });

  // Both queues are fetched whichever tab is showing, so both tab counts are real.
  // This is the whole point of the etappe: E46 shipped one queue and said nothing
  // about the other, so an admin could empty this screen and still have reports
  // waiting. A tab with no number would reproduce that exactly.
  const userList = useQuery({
    queryKey: ["reports", "users", userFrom],
    queryFn: () => api.get<UserReportList>(`/api/v1/reports/users?from=${userFrom}&limit=${PAGE}`),
    enabled: connected,
    placeholderData: keepPreviousData,
  });

  // See rooms.tsx: a successful load is the only proof the token works.
  useEffect(() => {
    if (list.isSuccess) clearConnectAttempt();
  }, [list.isSuccess]);

  if (state.isLoading) {
    return <div style={{ padding: 28, color: "var(--text-faint)", fontSize: 13 }}><Spinner size={14} /> Laden…</div>;
  }

  if (!connected) {
    return (
      <div className="mc-page">
        <SectionTitle sub="Gemeldete Ereignisse">Moderation</SectionTitle>
        <MatrixConnect reason="Für gemeldete Ereignisse wird einmalig der Matrix-Admin-Zugriff verbunden." error={error} returnTo="/reports" auto />
      </div>
    );
  }

  // The active queue's error drives the page: a 403 or an expired token affects both
  // equally, and reading it from whichever tab is showing keeps the message about the
  // thing being looked at.
  const active = queue === "events" ? list : userList;
  const err = active.error instanceof ApiError ? active.error : null;
  const notAdmin = err?.status === 403;
  const needsConnect = err?.status === 409;

  const all = list.data?.reports ?? [];
  const userAll = userList.data?.reports ?? [];
  const total = list.data?.total ?? 0;
  const userTotal = userList.data?.total ?? 0;

  const rowsAll: (Report | UserReport)[] = queue === "events" ? all : userAll;
  const shown = showAll ? rowsAll : rowsAll.filter((r) => r.state === "open");
  const hidden = rowsAll.length - shown.length;
  const pageFrom = queue === "events" ? from : userFrom;
  const setPageFrom = queue === "events" ? setFrom : setUserFrom;
  const nextToken = active.data?.next_token;

  return (
    <div className="mc-page">
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12, flexWrap: "wrap" }}>
        <SectionTitle sub="Gemeldete Ereignisse und gemeldete Nutzer">Moderation</SectionTitle>
        {hidden > 0 || showAll ? (
          <Button variant="ghost" size="sm" icon="eye" onClick={() => setShowAll((v) => !v)}>
            {showAll ? "Nur offene" : `${hidden} bearbeitete zeigen`}
          </Button>
        ) : null}
      </div>

      {/* Counts on both tabs, always. Synapse keeps two independent queues and an
          empty one here never means the other is empty too. */}
      {!notAdmin && !needsConnect && (
        <Tabs<Queue>
          tabs={[
            { id: "events", label: "Ereignisse", icon: "shield", count: total },
            { id: "users", label: "Nutzer", icon: "users", count: userTotal },
          ]}
          active={queue}
          onChange={(id) => { setQueue(id); setShowAll(false); }}
        />
      )}

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

      {needsConnect && (
        <MatrixConnect reason="Der Matrix-Zugriff wurde abgelehnt oder ist abgelaufen. Ein erneutes Verbinden fordert die Rechte neu an." returnTo="/reports" auto />
      )}

      {!notAdmin && !needsConnect && (
        <Card pad={false}>
          {active.isFetching && rowsAll.length === 0 ? (
            <div style={{ padding: 24, color: "var(--text-faint)", fontSize: 13 }}><Spinner size={14} /> Laden…</div>
          ) : shown.length === 0 ? (
            // "No open reports" is a *result*, and a good one. It must never read as
            // "could not load", which is the failure this empty state exists to
            // distinguish itself from.
            <EmptyState
              icon="shield"
              title={rowsAll.length === 0 ? "Keine Meldungen" : "Keine offenen Meldungen"}
              sub={rowsAll.length === 0
                ? queue === "events"
                  ? "Auf diesem Server wurde noch kein Ereignis gemeldet."
                  : "Auf diesem Server wurde noch kein Nutzer gemeldet."
                : `Alle ${rowsAll.length} Meldungen auf dieser Seite sind bearbeitet.`}
            />
          ) : queue === "events" ? (
            (shown as Report[]).map((r, i) => <ReportRow key={r.id} r={r} last={i === shown.length - 1} />)
          ) : (
            (shown as UserReport[]).map((r, i) => <UserReportRow key={r.id} r={r} last={i === shown.length - 1} />)
          )}
        </Card>
      )}

      {!notAdmin && !needsConnect && (pageFrom > 0 || nextToken) && (
        <div style={{ display: "flex", gap: 8 }}>
          <Button variant="ghost" size="sm" icon="chevLeft" disabled={pageFrom === 0}
            onClick={() => setPageFrom(Math.max(0, pageFrom - PAGE))}>Zurück</Button>
          <Button variant="ghost" size="sm" iconRight="chevRight" disabled={!nextToken}
            onClick={() => setPageFrom(pageFrom + PAGE)}>Weiter</Button>
        </div>
      )}
    </div>
  );
}
