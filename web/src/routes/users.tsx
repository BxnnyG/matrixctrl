import { createFileRoute } from "@tanstack/react-router";
import { useQuery, keepPreviousData } from "@tanstack/react-query";
import { useState } from "react";
import { api } from "@/lib/api";
import { Card, Badge, Icon, EmptyState, Button } from "@/components/mc";

export const Route = createFileRoute("/users")({
  component: Users,
});

interface User {
  id: string;
  username: string;
  created_at: string;
  locked_at: string | null;
  deactivated_at: string | null;
  /** locked and deactivated are deliberately distinct: locked is reversible and
   *  usually temporary, deactivated is the account being gone. */
  state: "active" | "locked" | "deactivated";
  admin: boolean;
  legacy_guest: boolean;
}

interface UsersResp {
  configured: boolean;
  users: User[];
  total?: number;
  next?: string;
  prev?: string;
  source: string;
}

const STATE: Record<User["state"], { label: string; tone: "ok" | "warn" | "info" }> = {
  active: { label: "aktiv", tone: "ok" },
  locked: { label: "gesperrt", tone: "warn" },
  deactivated: { label: "deaktiviert", tone: "info" },
};

const FILTERS: { id: string; label: string }[] = [
  { id: "", label: "alle" },
  { id: "active", label: "aktiv" },
  { id: "locked", label: "gesperrt" },
  { id: "deactivated", label: "deaktiviert" },
];

function fmtDate(iso: string | null) {
  if (!iso) return "—";
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? "—" : d.toLocaleDateString("de-DE", { day: "2-digit", month: "2-digit", year: "numeric" });
}

function Users() {
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState("");
  // MAS pages by cursor, not offset — there is no page number to show, and
  // inventing one in the client would be a lie told in the UI.
  const [cursor, setCursor] = useState<{ after?: string; before?: string }>({});

  const params = new URLSearchParams();
  if (search) params.set("search", search);
  if (status) params.set("status", status);
  if (cursor.after) params.set("after", cursor.after);
  if (cursor.before) params.set("before", cursor.before);

  const { data, isLoading, error } = useQuery({
    queryKey: ["users", search, status, cursor],
    queryFn: () => api.get<UsersResp>(`/api/v1/users?${params.toString()}`),
    // Without this the table blanks on every keystroke and every page turn, which
    // reads as "it broke" rather than "it is loading".
    placeholderData: keepPreviousData,
  });

  function resetTo(next: { after?: string; before?: string }) {
    setCursor(next);
  }

  if (error) {
    return (
      <EmptyState
        icon="alert"
        title="Benutzer nicht abrufbar"
        sub="Der Matrix Authentication Service antwortet nicht. Das ist keine Aussage über die Konten — nur darüber, dass gerade keine gelesen werden konnten."
      />
    );
  }

  if (data && !data.configured) {
    return (
      <EmptyState
        icon="info"
        title="Benutzerverwaltung braucht MAS-Zugang"
        sub="MatrixCtrl läuft im Bootstrap-Modus und hat keine OIDC-Zugangsdaten, mit denen es die Admin-API des Matrix Authentication Service ansprechen könnte. Nach dem Verbinden mit MAS erscheinen die Konten hier."
      />
    );
  }

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 18 }}>
      <Card>
        <div style={{ display: "flex", gap: 12, flexWrap: "wrap", alignItems: "center" }}>
          <div style={{ position: "relative", flex: 1, minWidth: 220 }}>
            <Icon name="search" size={15} style={{ position: "absolute", left: 11, top: 10, color: "var(--text-faint)" }} />
            <input
              value={search}
              onChange={(e) => { setSearch(e.target.value); resetTo({}); }}
              placeholder="Benutzername suchen…"
              style={{
                width: "100%", padding: "8px 12px 8px 32px", fontSize: 13,
                borderRadius: "var(--radius-sm)", border: "1px solid var(--border)",
                background: "var(--surface-2)", color: "var(--text)", outline: "none",
              }}
            />
          </div>
          <div style={{ display: "flex", gap: 6 }}>
            {FILTERS.map((f) => (
              <Button
                key={f.id}
                variant={status === f.id ? "soft" : "ghost"}
                size="sm"
                onClick={() => { setStatus(f.id); resetTo({}); }}
              >
                {f.label}
              </Button>
            ))}
          </div>
        </div>

        <div style={{ fontSize: 12, color: "var(--text-faint)", marginTop: 10 }}>
          Gelesen aus dem Matrix Authentication Service, der unter MSC3861 für Konten maßgeblich ist.
          Synapse führt eine eigene Benutzertabelle; auf einer migrierten Installation können beide abweichen.
          {typeof data?.total === "number" && ` · ${data.total} Konten insgesamt`}
        </div>
      </Card>

      <Card>
        {isLoading && !data ? (
          <div style={{ fontSize: 12.5, color: "var(--text-faint)", padding: "8px 2px" }}>Lade…</div>
        ) : !data?.users.length ? (
          <div style={{ fontSize: 12.5, color: "var(--text-faint)", padding: "8px 2px" }}>
            {search || status ? "Keine Konten passen zu diesem Filter." : "Keine Konten gefunden."}
          </div>
        ) : (
          <div style={{ overflowX: "auto" }}>
            <table style={{ width: "100%", borderCollapse: "collapse", fontSize: 13 }}>
              <thead>
                <tr style={{ textAlign: "left", color: "var(--text-faint)", fontSize: 11.5, textTransform: "uppercase", letterSpacing: 0.4 }}>
                  <th style={{ padding: "0 10px 8px 2px", fontWeight: 600 }}>Benutzer</th>
                  <th style={{ padding: "0 10px 8px", fontWeight: 600 }}>Status</th>
                  <th style={{ padding: "0 10px 8px", fontWeight: 600 }}>Erstellt</th>
                  <th style={{ padding: "0 2px 8px 10px", fontWeight: 600 }}>Seit</th>
                </tr>
              </thead>
              <tbody>
                {data.users.map((u) => {
                  const s = STATE[u.state];
                  return (
                    <tr key={u.id} style={{ borderTop: "1px solid var(--border)" }}>
                      <td style={{ padding: "10px 10px 10px 2px" }}>
                        <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
                          <span style={{ fontFamily: "var(--mono)", color: "var(--text)" }}>{u.username}</span>
                          {u.admin && <Badge tone="info" size="sm">Admin</Badge>}
                          {u.legacy_guest && <Badge tone="info" size="sm">Gast (Alt)</Badge>}
                        </div>
                      </td>
                      <td style={{ padding: "10px" }}><Badge tone={s.tone} size="sm">{s.label}</Badge></td>
                      <td style={{ padding: "10px", color: "var(--text-dim)" }}>{fmtDate(u.created_at)}</td>
                      {/* The timestamp of whichever state it is in — an operator
                          deciding what to do needs to know when, not just what. */}
                      <td style={{ padding: "10px 2px 10px 10px", color: "var(--text-dim)" }}>
                        {fmtDate(u.deactivated_at ?? u.locked_at)}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}

        {(data?.prev || data?.next) && (
          <div style={{ display: "flex", gap: 8, marginTop: 14, justifyContent: "flex-end" }}>
            <Button variant="ghost" size="sm" icon="chevLeft" disabled={!data?.prev}
              onClick={() => resetTo({ before: data!.prev })}>Zurück</Button>
            <Button variant="ghost" size="sm" iconRight="chevRight" disabled={!data?.next}
              onClick={() => resetTo({ after: data!.next })}>Weiter</Button>
          </div>
        )}
      </Card>
    </div>
  );
}
