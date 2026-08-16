import { createFileRoute } from "@tanstack/react-router";
import { useQuery, useMutation, useQueryClient, keepPreviousData } from "@tanstack/react-query";
import { useState } from "react";
import { api, ApiError } from "@/lib/api";
import { Card, Badge, Icon, EmptyState, Button, SectionTitle, Spinner } from "@/components/mc";
import { MatrixConnect } from "@/components/MatrixConnect";

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

interface ReportDetail {
  report: Report;
  body: string;
  event_type: string;
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

          <div style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap" }}>
            <input
              value={note}
              onChange={(e) => setNote(e.target.value)}
              placeholder="Notiz (optional)"
              style={{ flex: "1 1 240px", padding: "7px 10px", border: "1px solid var(--border)", background: "var(--surface-2)", color: "var(--text)", borderRadius: "var(--radius-sm)", fontSize: 12.5, fontFamily: "var(--font)" }}
            />
            {r.state === "open" ? (
              <>
                <Button variant="soft" size="sm" icon="check" disabled={decide.isPending}
                  onClick={() => decide.mutate("handled")}>Bearbeitet</Button>
                <Button variant="ghost" size="sm" disabled={decide.isPending}
                  onClick={() => decide.mutate("dismissed")}>Verwerfen</Button>
              </>
            ) : (
              <Button variant="outline" size="sm" disabled={decide.isPending}
                onClick={() => decide.mutate("open" as State)}>Wieder öffnen</Button>
            )}
          </div>

          {r.actor && (
            <div style={{ fontSize: 11.5, color: "var(--text-faint)" }}>
              entschieden von {r.actor}
            </div>
          )}

          {/* Said once per report rather than once per page: this is the sentence
              that explains why the queue never shrinks upstream. */}
          <div style={{ fontSize: 11.5, color: "var(--text-faint)", lineHeight: 1.55 }}>
            Der Status wird nur in MatrixCtrl gespeichert. Die Meldung selbst bleibt in
            Synapse erhalten — so bleibt sichtbar, wenn dieselbe Person mehrfach
            gemeldet wurde.
          </div>
        </div>
      )}
    </div>
  );
}

function Reports() {
  const { error } = Route.useSearch();
  const [from, setFrom] = useState(0);
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

  if (state.isLoading) {
    return <div style={{ padding: 28, color: "var(--text-faint)", fontSize: 13 }}><Spinner size={14} /> Laden…</div>;
  }

  if (!connected) {
    return (
      <div style={{ padding: 28, display: "flex", flexDirection: "column", gap: 18 }}>
        <SectionTitle sub="Gemeldete Ereignisse">Moderation</SectionTitle>
        <MatrixConnect reason="Für gemeldete Ereignisse wird einmalig der Matrix-Admin-Zugriff verbunden." error={error} />
      </div>
    );
  }

  const err = list.error instanceof ApiError ? list.error : null;
  const notAdmin = err?.status === 403;
  const needsConnect = err?.status === 409;

  const all = list.data?.reports ?? [];
  const shown = showAll ? all : all.filter((r) => r.state === "open");
  const total = list.data?.total ?? 0;
  const hidden = all.length - shown.length;

  return (
    <div style={{ padding: 28, display: "flex", flexDirection: "column", gap: 18 }}>
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12, flexWrap: "wrap" }}>
        <SectionTitle sub={total > 0 ? `${total} Meldungen insgesamt` : "Gemeldete Ereignisse"}>Moderation</SectionTitle>
        {hidden > 0 || showAll ? (
          <Button variant="ghost" size="sm" icon="eye" onClick={() => setShowAll((v) => !v)}>
            {showAll ? "Nur offene" : `${hidden} bearbeitete zeigen`}
          </Button>
        ) : null}
      </div>

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
        <MatrixConnect reason="Der Matrix-Zugriff wurde abgelehnt oder ist abgelaufen. Ein erneutes Verbinden fordert die Rechte neu an." />
      )}

      {!notAdmin && !needsConnect && (
        <Card pad={false}>
          {list.isFetching && all.length === 0 ? (
            <div style={{ padding: 24, color: "var(--text-faint)", fontSize: 13 }}><Spinner size={14} /> Laden…</div>
          ) : shown.length === 0 ? (
            // "No open reports" is a *result*, and a good one. It must never read as
            // "could not load", which is the failure this empty state exists to
            // distinguish itself from.
            <EmptyState
              icon="shield"
              title={all.length === 0 ? "Keine Meldungen" : "Keine offenen Meldungen"}
              sub={all.length === 0
                ? "Auf diesem Server wurde noch nichts gemeldet."
                : `Alle ${all.length} Meldungen auf dieser Seite sind bearbeitet.`}
            />
          ) : (
            shown.map((r, i) => <ReportRow key={r.id} r={r} last={i === shown.length - 1} />)
          )}
        </Card>
      )}

      {!notAdmin && !needsConnect && (from > 0 || list.data?.next_token) && (
        <div style={{ display: "flex", gap: 8 }}>
          <Button variant="ghost" size="sm" icon="chevLeft" disabled={from === 0}
            onClick={() => setFrom(Math.max(0, from - PAGE))}>Zurück</Button>
          <Button variant="ghost" size="sm" iconRight="chevRight" disabled={!list.data?.next_token}
            onClick={() => setFrom(from + PAGE)}>Weiter</Button>
        </div>
      )}
    </div>
  );
}
