import { createFileRoute } from "@tanstack/react-router";
import { useInfiniteQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Card, Badge, Button, EmptyState } from "@/components/mc";

export const Route = createFileRoute("/audit")({
  component: AuditLog,
});

interface AuditEntry {
  id: number;
  ts: string;
  user_id: string;
  action: string;
  resource: string;
  result: string;
  detail?: { status?: number; duration_ms?: number };
}

interface AuditPage {
  entries: AuditEntry[];
  next_before: number | null;
}

// The audit table stores "POST /api/v1/helm/releases/{name}/upgrade". That is
// the right thing to store — route patterns aggregate, URLs do not — but it is
// not what an operator wants to read. Mapped rather than parsed: a pattern this
// list does not know falls back to the raw value, which is honest, instead of a
// clever regex producing confident nonsense.
const ACTIONS: Record<string, string> = {
  "POST /api/v1/helm/releases/{name}/upgrade": "ESS-Upgrade gestartet",
  "POST /api/v1/helm/releases/{name}/apply-config": "Config deployt",
  "POST /api/v1/helm/releases/{name}/rollback": "Rollback ausgeführt",
  "POST /api/v1/setup/deploy-ess": "ESS deployt (Greenfield)",
  "POST /api/v1/setup/connect-oidc": "Matrix-Login verbunden",
  "POST /api/v1/config/sections/{name}": "Config-Sektion gespeichert",
  "POST /api/v1/config/rollback": "Config zurückgerollt",
  "POST /api/v1/hooks": "Hook angelegt",
  "PUT /api/v1/hooks/{id}": "Hook geändert",
  "DELETE /api/v1/hooks/{id}": "Hook gelöscht",
  "POST /api/v1/hooks/{id}/run": "Hook ausgeführt",
  "POST /api/v1/auth/logout": "Abgemeldet",
  "DELETE /api/v1/status/pods/{pod}": "Pod neu gestartet",
  "DELETE /api/v1/status/evicted-pods": "Evicted Pods entfernt",
};

const label = (action: string) => ACTIONS[action] ?? action;

function AuditLog() {
  const { data, fetchNextPage, hasNextPage, isFetchingNextPage, isLoading } =
    useInfiniteQuery({
      queryKey: ["audit"],
      initialPageParam: 0,
      queryFn: ({ pageParam }) =>
        api.get<AuditPage>(`/api/v1/audit?limit=50${pageParam ? `&before=${pageParam}` : ""}`),
      // A full page is not proof there is another one; the backend says so.
      getNextPageParam: (last) =>
        last.entries.length === 50 && last.next_before ? last.next_before : undefined,
    });

  const entries = data?.pages.flatMap((p) => p.entries) ?? [];

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 18 }}>
      <Card pad={false}>
        <div style={{ padding: "14px 18px 4px", fontSize: 10.5, fontWeight: 600, letterSpacing: "0.06em", textTransform: "uppercase", color: "var(--text-faint)" }}>
          {entries.length ? `${entries.length} Einträge` : "Verlauf"}
        </div>

        <div style={{ padding: "0 8px 8px" }}>
          {entries.map((e, i) => (
            <div key={e.id} style={{ display: "flex", alignItems: "center", gap: 14, padding: "12px 14px", borderBottom: i < entries.length - 1 ? "1px solid var(--border-soft)" : "none" }}>
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ fontSize: 13.5, fontWeight: 600, color: "var(--text)" }}>
                  {label(e.action)}
                </div>
                <div style={{ fontSize: 11.5, color: "var(--text-faint)", marginTop: 2, fontFamily: "var(--mono)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                  {e.user_id === "-" ? "unauthentifiziert" : e.user_id} · {e.resource}
                  {e.detail?.duration_ms != null && ` · ${e.detail.duration_ms} ms`}
                </div>
              </div>
              <div style={{ fontSize: 11.5, color: "var(--text-faint)", fontFamily: "var(--mono)", whiteSpace: "nowrap" }}>
                {new Date(e.ts).toLocaleString("de-DE")}
              </div>
              <Badge tone={e.result === "ok" ? "ok" : "err"} size="sm">
                {e.detail?.status ?? e.result}
              </Badge>
            </div>
          ))}

          {!entries.length && !isLoading && (
            <EmptyState
              icon="audit"
              title="Noch keine Einträge"
              sub="Jede ändernde Aktion — Upgrade, Config-Deploy, Hook, Rollback — erscheint hier mit Benutzer, Zeitpunkt und Ergebnis. Lesende Zugriffe werden bewusst nicht protokolliert."
            />
          )}
        </div>

        {hasNextPage && (
          <div style={{ padding: "0 18px 16px" }}>
            <Button variant="ghost" size="sm" onClick={() => fetchNextPage()} disabled={isFetchingNextPage}>
              {isFetchingNextPage ? "Lädt…" : "Ältere anzeigen"}
            </Button>
          </div>
        )}
      </Card>
    </div>
  );
}
