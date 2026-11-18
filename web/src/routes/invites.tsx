import { createFileRoute, Link } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api, ApiError } from "@/lib/api";
import { Badge, Button, Card, ConfirmDialog, EmptyState, Icon, SectionTitle, SkeletonRows } from "@/components/mc";

export const Route = createFileRoute("/invites")({
  component: InvitesPage,
});

export interface Invite {
  id: string;
  /** Empty when the session may only look: a code is an access (§4.134). */
  token: string;
  token_hidden?: boolean;
  valid: boolean;
  state: "valid" | "used" | "expired" | "revoked";
  usage_limit: number | null;
  times_used: number;
  created_at: string;
  last_used_at: string | null;
  expires_at: string | null;
  revoked_at: string | null;
}
interface InvitesResp { configured: boolean; invites: Invite[]; register_url?: string }
interface TasksResp { values: Record<string, { value: unknown }> }
interface VersionResp { may_write?: boolean }

const STATE: Record<Invite["state"], { label: string; tone: "ok" | "warn" | "info" | "err" }> = {
  valid: { label: "gültig", tone: "ok" },
  used: { label: "aufgebraucht", tone: "info" },
  expired: { label: "abgelaufen", tone: "warn" },
  revoked: { label: "widerrufen", tone: "err" },
};

const inputStyle: React.CSSProperties = { width: "100%", padding: "8px 10px", border: "1px solid var(--border)", background: "var(--surface-2)", color: "var(--text)", borderRadius: "var(--radius-sm)", fontSize: 13, fontFamily: "var(--font)" };

const fmt = (iso: string | null) => (iso ? new Date(iso).toLocaleString("de-DE", { dateStyle: "medium", timeStyle: "short" }) : "");

/** Days from now as RFC3339, which is what the API wants. Empty or not a positive number: none. */
export function daysToRFC3339(days: string): string | null {
  const n = Number(days);
  if (!days.trim() || !Number.isFinite(n) || n <= 0) return null;
  return new Date(Date.now() + n * 86_400_000).toISOString();
}

const intOrNull = (s: string): number | null => {
  const n = parseInt(s, 10);
  return Number.isFinite(n) && n > 0 ? n : null;
};

export function InvitesPage() {
  const qc = useQueryClient();
  const list = useQuery({
    queryKey: ["invites"],
    queryFn: () => api.get<InvitesResp>("/api/v1/invites"),
    retry: false,
  });
  const tasks = useQuery({ queryKey: ["config", "tasks"], queryFn: () => api.get<TasksResp>("/api/v1/config/tasks") });
  const version = useQuery({ queryKey: ["version"], queryFn: () => api.get<VersionResp>("/api/v1/version") });
  const mayWrite = version.data?.may_write !== false;
  const refresh = () => qc.invalidateQueries({ queryKey: ["invites"] });

  // 503 is the calm "MAS not connected" case, not an error to shout about.
  const noMAS = list.error instanceof ApiError && list.error.status === 503;

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 20, maxWidth: 960 }}>
      <RegistrationState values={tasks.data?.values} />
      {noMAS ? (
        <Card><EmptyState icon="users" title="MAS nicht verbunden"
          sub="Einladungs-Codes liegen im Matrix Authentication Service. Sobald MatrixCtrl damit verbunden ist (Setup), erscheinen sie hier." /></Card>
      ) : list.isLoading ? (
        <Card><SkeletonRows rows={3} /></Card>
      ) : list.error ? (
        <div style={{ fontSize: 13, color: "var(--status-err)" }}>{(list.error as Error).message}</div>
      ) : (
        <>
          {mayWrite && <CreateCard onDone={refresh} registerUrl={list.data?.register_url} />}
          <InviteList invites={list.data?.invites ?? []} registerUrl={list.data?.register_url} mayWrite={mayWrite} onChanged={refresh} />
        </>
      )}
    </div>
  );
}

function RegistrationState({ values }: { values?: TasksResp["values"] }) {
  if (!values) return null;
  const password = values["mas.password_registration"]?.value === true;
  const needCode = values["mas.registration_token_required"]?.value === true;
  const text = !password ? "Registrierung mit Benutzername und Passwort ist aus."
    : needCode ? "Registrierung ist nur mit Einladungs-Code möglich." : "Registrierung ist für alle offen — ein Code ist nicht nötig.";
  return (
    <Card>
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12, flexWrap: "wrap" }}>
        <div style={{ fontSize: 13.5, fontWeight: 600 }}>{text}</div>
        <Link to="/config" search={{ mode: "tasks", card: "registration" }} style={{ fontSize: 13, color: "var(--accent)" }}>Einstellung ändern</Link>
      </div>
      {needCode && !password && (
        <div role="alert" style={{ marginTop: 10, fontSize: 12.5, color: "var(--status-warn)", lineHeight: 1.55 }}>
          Die Code-Pflicht ist an, aber die Registrierung mit Passwort ist aus. Solange sie aus ist, kann sich niemand selbst registrieren — auch nicht mit einem Code.
        </div>
      )}
      <div style={{ marginTop: 10, fontSize: 12, color: "var(--text-faint)", lineHeight: 1.55 }}>
        Codes gelten nur für die Registrierung mit Passwort. Anmeldung über externe Anbieter ist davon nicht betroffen.
      </div>
    </Card>
  );
}

function CreateCard({ onDone, registerUrl }: { onDone: () => void; registerUrl?: string }) {
  const [token, setToken] = useState("");
  const [limit, setLimit] = useState("");
  const [days, setDays] = useState("");
  const [created, setCreated] = useState<Invite | null>(null);
  const create = useMutation({
    mutationFn: () => api.post<Invite>("/api/v1/invites", {
      token: token.trim(), usage_limit: intOrNull(limit), expires_at: daysToRFC3339(days),
    }),
    onSuccess: (inv) => { setCreated(inv); setToken(""); setLimit(""); setDays(""); onDone(); },
  });
  return (
    <Card>
      <SectionTitle icon="plus" sub="Wer den Code hat, kann sich registrieren — so oft und so lange, wie du es erlaubst.">Neuer Einladungs-Code</SectionTitle>
      <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(200px, 1fr))", gap: 12, marginTop: 12 }}>
        <label style={{ fontSize: 12, color: "var(--text-dim)" }}>Code (leer = zufällig)
          <input aria-label="Code" value={token} onChange={(e) => setToken(e.target.value)} style={inputStyle} />
        </label>
        <label style={{ fontSize: 12, color: "var(--text-dim)" }}>Nutzungen (leer = unbegrenzt)
          <input aria-label="Nutzungen" inputMode="numeric" value={limit} onChange={(e) => setLimit(e.target.value)} style={inputStyle} />
        </label>
        <label style={{ fontSize: 12, color: "var(--text-dim)" }}>Gültig für Tage (leer = unbegrenzt)
          <input aria-label="Tage" inputMode="numeric" value={days} onChange={(e) => setDays(e.target.value)} style={inputStyle} />
        </label>
      </div>
      <div style={{ marginTop: 12, display: "flex", gap: 10, alignItems: "center" }}>
        <Button variant="primary" icon="plus" disabled={create.isPending} onClick={() => create.mutate()}>Code anlegen</Button>
        {create.isError && <span style={{ fontSize: 12.5, color: "var(--status-err)" }}>{(create.error as Error).message}</span>}
      </div>
      {created && (
        <div style={{ marginTop: 14, padding: 12, borderRadius: "var(--radius-sm)", background: "var(--surface-2)", fontSize: 13 }}>
          <div style={{ marginBottom: 6 }}>Code angelegt. Gib ihn weiter, zum Beispiel so:</div>
          <CopyText text={created.token} />
          <RegisterAddress url={registerUrl} />
        </div>
      )}
    </Card>
  );
}

function RegisterAddress({ url }: { url?: string }) {
  if (!url) return null;
  return (
    <div style={{ marginTop: 6, fontSize: 12, color: "var(--text-dim)" }}>
      <CopyText text={url} label="Adresse kopieren" /> Registrierung hier, Code im Formular eingeben
    </div>
  );
}

function CopyText({ text, label = "Code kopieren" }: { text: string; label?: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
      <code style={{ fontFamily: "var(--mono)", fontSize: 12.5, padding: "2px 6px", borderRadius: 4, background: "var(--surface)", overflowWrap: "anywhere" }}>{text}</code>
      <button type="button" title={label} aria-label={label}
        onClick={() => { navigator.clipboard?.writeText(text).then(() => { setCopied(true); setTimeout(() => setCopied(false), 1500); }); }}
        style={{ display: "inline-grid", placeItems: "center", border: "none", background: "transparent", cursor: "pointer", color: copied ? "var(--status-ok)" : "var(--text-faint)", padding: 2 }}>
        <Icon name={copied ? "check" : "copy"} size={13} />
      </button>
    </span>
  );
}

function InviteList({ invites, registerUrl, mayWrite, onChanged }: { invites: Invite[]; registerUrl?: string; mayWrite: boolean; onChanged: () => void }) {
  const [editing, setEditing] = useState<Invite | null>(null);
  const [revoking, setRevoking] = useState<Invite | null>(null);
  const [limit, setLimit] = useState("");
  const [expiry, setExpiry] = useState("");

  const edit = useMutation({
    mutationFn: () => {
      const inv = editing!;
      const unchanged = expiry === (inv.expires_at ? inv.expires_at.slice(0, 10) : "");
      const expires_at = unchanged ? inv.expires_at : expiry ? `${expiry}T23:59:59Z` : null;
      return api.put(`/api/v1/invites/${inv.id}`, { usage_limit: intOrNull(limit), expires_at });
    },
    onSuccess: () => { setEditing(null); onChanged(); },
  });
  const revoke = useMutation({
    mutationFn: (inv: Invite) => api.post(`/api/v1/invites/${inv.id}/${inv.state === "revoked" ? "unrevoke" : "revoke"}`, {}),
    onSuccess: () => { setRevoking(null); onChanged(); },
  });

  if (invites.length === 0) {
    return <Card><EmptyState icon="users" title="Noch keine Einladungs-Codes" sub={mayWrite ? "Lege oben den ersten Code an." : undefined} /></Card>;
  }
  const restoring = revoking?.state === "revoked";
  return (
    <Card pad={false}>
      {invites.map((inv, i) => {
        const st = STATE[inv.state] ?? STATE.valid;
        return (
          <div key={inv.id} style={{ display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap", padding: "12px 16px", borderTop: i ? "1px solid var(--border)" : undefined }}>
            <div style={{ flex: 1, minWidth: 200 }}>
              {inv.token_hidden ? <span style={{ fontSize: 12.5, color: "var(--text-faint)" }}>Code nur für Konten mit Schreibrecht sichtbar</span> : <CopyText text={inv.token} />}
              <div style={{ fontSize: 12, color: "var(--text-faint)", marginTop: 4 }}>
                {inv.times_used} von {inv.usage_limit ?? "∞"} benutzt
                {inv.expires_at && ` · läuft ab ${fmt(inv.expires_at)}`}
              </div>
              <RegisterAddress url={registerUrl} />
            </div>
            <Badge tone={st.tone}>{st.label}</Badge>
            {mayWrite && (
              <div style={{ display: "flex", gap: 6 }}>
                <Button size="sm" variant="ghost" icon="edit" onClick={() => { setEditing(inv); setLimit(inv.usage_limit?.toString() ?? ""); setExpiry(inv.expires_at ? inv.expires_at.slice(0, 10) : ""); }}>Ändern</Button>
                <Button size="sm" variant="ghost" onClick={() => setRevoking(inv)}>{inv.state === "revoked" ? "Wiederherstellen" : "Widerrufen"}</Button>
              </div>
            )}
          </div>
        );
      })}
      <ConfirmDialog open={!!editing} title="Code ändern" tone="primary" confirmLabel="Speichern" busy={edit.isPending}
        error={edit.isError ? (edit.error as Error).message : null} onConfirm={() => edit.mutate()} onCancel={() => setEditing(null)}>
        <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          <label style={{ fontSize: 12, color: "var(--text-dim)" }}>Nutzungen (leer = unbegrenzt)
            <input aria-label="Nutzungen ändern" inputMode="numeric" value={limit} onChange={(e) => setLimit(e.target.value)} style={inputStyle} />
          </label>
          <label style={{ fontSize: 12, color: "var(--text-dim)" }}>Läuft ab am (leer = nie)
            <input aria-label="Ablauf ändern" type="date" value={expiry} onChange={(e) => setExpiry(e.target.value)} style={inputStyle} />
          </label>
        </div>
      </ConfirmDialog>
      <ConfirmDialog open={!!revoking} title={restoring ? "Code wiederherstellen?" : "Code widerrufen?"} tone={restoring ? "primary" : "danger"}
        confirmLabel={restoring ? "Wiederherstellen" : "Widerrufen"} busy={revoke.isPending}
        error={revoke.isError ? (revoke.error as Error).message : null}
        onConfirm={() => revoke.mutate(revoking!)} onCancel={() => setRevoking(null)}>
        {restoring ? "Der Code kann danach wieder zur Registrierung benutzt werden." : "Mit dem Code kann sich danach niemand mehr registrieren. Wer sich schon damit registriert hat, behält sein Konto."}
      </ConfirmDialog>
    </Card>
  );
}
