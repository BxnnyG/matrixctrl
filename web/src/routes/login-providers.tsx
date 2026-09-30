import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Badge, Button, Card, ConfirmDialog, EmptyState, Icon, SectionTitle, Spinner, Toggle } from "@/components/mc";

export const Route = createFileRoute("/login-providers")({ component: LoginProvidersPage });

type Kind = "google" | "github" | "oidc";

interface Provider {
  id: string; kind: Kind; name: string; issuer?: string; client_id?: string;
  has_secret: boolean; enabled: boolean; complete: boolean; callback?: string; link?: string;
}
interface ListResponse {
  mas_host: string;
  account_url?: string;
  providers: Provider[];
  wiring: { in_config: boolean; deployed: boolean };
}
type Next = "apply" | "apply-pending" | "restarting" | "unchanged";

const KIND_LABEL: Record<Kind, string> = { google: "Google", github: "GitHub", oidc: "Eigener OIDC-Anbieter" };

/** Sign-in through Google, GitHub or an OIDC provider (etappe 110).
 *
 *  Configured once, in MAS: Element and MatrixCtrl both sign in there, and MatrixCtrl
 *  still admits only MAS admins. The client secret goes into a Kubernetes Secret and
 *  never comes back to the browser. */
function LoginProvidersPage() {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const { data, isLoading, error } = useQuery({
    queryKey: ["login-providers"],
    queryFn: () => api.get<ListResponse>("/api/v1/login-providers"),
  });
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<string | null>(null);
  const [next, setNext] = useState<Next | null>(null);
  const refresh = () => qc.invalidateQueries({ queryKey: ["login-providers"] });

  const verify = useQuery({
    queryKey: ["login-providers", "verify"],
    queryFn: () => api.get<{ reachable: boolean; shown?: Record<string, boolean>; note?: string }>("/api/v1/login-providers/verify"),
    enabled: !!data?.wiring.deployed,
    // After a restart MAS needs a moment; ask again until it answers.
    refetchInterval: next === "restarting" ? 10_000 : false,
  });

  if (isLoading) return <div style={{ display: "flex", gap: 8, padding: 24, color: "var(--text-faint)", fontSize: 13 }}><Spinner size={14} /> Lade…</div>;
  if (error) return <div style={{ padding: 24, color: "var(--status-err)", fontSize: 13 }}>{(error as Error).message}</div>;
  const d = data!;
  const live = d.providers.filter((p) => p.complete);
  const needsApply = live.length > 0 && !d.wiring.deployed;

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 18, maxWidth: 820 }}>
      <SectionTitle icon="key" sub="Mit Google, GitHub oder einem eigenen Anbieter bei Matrix und MatrixCtrl anmelden"
        right={!adding && <Button variant="primary" size="sm" icon="plus" onClick={() => setAdding(true)}>Anbieter hinzufügen</Button>}>
        Anmeldung
      </SectionTitle>

      <Card style={{ display: "flex", gap: 12, fontSize: 13, lineHeight: 1.6, color: "var(--text-dim)" }}>
        <Icon name="info" size={18} style={{ color: "var(--accent)", flexShrink: 0, marginTop: 2 }} />
        <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
          <span><strong style={{ color: "var(--text)" }}>Einmal eingerichtet, überall gültig.</strong> Element und MatrixCtrl melden sich beide über die Anmeldung von Matrix (MAS) an — ein Anbieter hier gilt für beide.</span>
          <span><strong style={{ color: "var(--text)" }}>Registrierung ist offen:</strong> Jeder mit einem Konto beim Anbieter kann sich einen Matrix-Account anlegen. Er ist damit Nutzer, nie Admin — Admin-Rechte vergibst du einzeln unter Benutzer.</span>
          <span><strong style={{ color: "var(--text)" }}>Bestehende Accounts</strong> werden nicht automatisch verknüpft (das wäre ein Weg zur Übernahme fremder Accounts). Wer schon einen Account hat, verknüpft ihn selbst — angemeldet, auf {d.mas_host ? <a href={`https://${d.mas_host}/account/`} target="_blank" rel="noreferrer" style={{ color: "var(--accent)" }}>der Kontoseite</a> : "der Kontoseite"}.</span>
          <span>Die 2-Faktor-Anmeldung kommt vom Anbieter. Der Notzugang zu MatrixCtrl ohne Anbieter bleibt: <code style={{ fontFamily: "var(--mono)" }}>install.sh recover-login</code>.</span>
        </div>
      </Card>

      {needsApply && (
        <Card style={{ display: "flex", alignItems: "center", gap: 12, borderColor: "var(--status-warn)" }}>
          <Icon name="alert" size={18} style={{ color: "var(--status-warn)" }} />
          <span style={{ flex: 1, fontSize: 13, color: "var(--text)" }}>
            Gespeichert, aber noch nicht aktiv: die Einbindung in MAS wartet in den Einstellungen auf „Übernehmen". Dabei startet MAS einmal neu.
          </span>
          <Button variant="primary" size="sm" onClick={() => navigate({ to: "/config" })}>Zu den Einstellungen</Button>
        </Card>
      )}
      {next === "restarting" && (
        <Card style={{ display: "flex", alignItems: "center", gap: 10, fontSize: 13, color: "var(--text-dim)" }}>
          <Spinner size={14} /> MAS startet mit den neuen Anbietern neu — die Anmeldeseite wird geprüft, sobald er wieder antwortet.
        </Card>
      )}

      {adding && <AddProvider onDone={(id) => { setAdding(false); refresh(); if (id) setEditing(id); }} />}

      {d.providers.length === 0 && !adding ? (
        <Card><EmptyState icon="key" title="Noch kein Anbieter" sub="Füge Google, GitHub oder einen eigenen OIDC-Anbieter hinzu." /></Card>
      ) : d.providers.map((p) => (
        <ProviderCard key={p.id} p={p} masHost={d.mas_host} accountUrl={d.account_url}
          shown={verify.data?.shown?.[p.id]}
          editing={editing === p.id} onEdit={() => setEditing(editing === p.id ? null : p.id)}
          onSaved={(n) => { setEditing(null); setNext(n); refresh(); }} onChanged={refresh} />
      ))}
    </div>
  );
}

function AddProvider({ onDone }: { onDone: (id?: string) => void }) {
  const [kind, setKind] = useState<Kind | null>(null);
  const [name, setName] = useState("");
  const [issuer, setIssuer] = useState("");
  const create = useMutation({
    mutationFn: () => api.post<Provider>("/api/v1/login-providers", { kind, name, issuer }),
    onSuccess: (p) => onDone(p.id),
  });
  return (
    <Card style={{ display: "flex", flexDirection: "column", gap: 14 }}>
      <div style={{ fontSize: 13.5, fontWeight: 600, color: "var(--text)" }}>1. Welcher Anbieter?</div>
      <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
        {(["google", "github", "oidc"] as Kind[]).map((k) => (
          <Button key={k} variant={kind === k ? "primary" : "outline"} size="sm" onClick={() => setKind(k)}>{KIND_LABEL[k]}</Button>
        ))}
      </div>
      {kind && (
        <>
          <Field label="Anzeigename auf der Anmeldeseite" hint="Leer lassen für den Standard.">
            <input value={name} onChange={(e) => setName(e.target.value)} placeholder={kind === "oidc" ? "z. B. Firmen-Login" : KIND_LABEL[kind]} style={inputStyle} />
          </Field>
          {kind === "oidc" && (
            <Field label="Issuer-URL" hint="Exakt wie im Discovery-Dokument des Anbieters, inklusive abschließendem Schrägstrich, falls er einen hat.">
              <input value={issuer} onChange={(e) => setIssuer(e.target.value)} placeholder="https://id.example.org/" style={inputStyle} />
            </Field>
          )}
          <div style={{ display: "flex", gap: 8 }}>
            <Button variant="primary" size="sm" disabled={create.isPending || (kind === "oidc" && !issuer)} onClick={() => create.mutate()}>
              {create.isPending ? <Spinner size={13} /> : "Weiter — Callback-URL erzeugen"}
            </Button>
            <Button variant="ghost" size="sm" onClick={() => onDone()}>Abbrechen</Button>
          </div>
          {create.isError && <span style={{ fontSize: 12.5, color: "var(--status-err)" }}>{(create.error as Error).message}</span>}
        </>
      )}
    </Card>
  );
}

function ProviderCard({ p, masHost, accountUrl, shown, editing, onEdit, onSaved, onChanged }: {
  p: Provider; masHost: string; accountUrl?: string; shown?: boolean; editing: boolean;
  onEdit: () => void; onSaved: (n: Next) => void; onChanged: () => void;
}) {
  const [clientId, setClientId] = useState(p.client_id ?? "");
  const [issuer, setIssuer] = useState(p.issuer ?? "");
  const [clientSecret, setClientSecret] = useState("");
  const [confirmDelete, setConfirmDelete] = useState(false);

  const save = useMutation({
    mutationFn: (body: Record<string, unknown>) => api.put<{ next: Next }>(`/api/v1/login-providers/${p.id}`, body),
    onSuccess: (r) => { setClientSecret(""); onSaved(r.next); },
  });
  // Checks the issuer as typed: a changed one is stored first (it is not secret, and
  // a draft is not rendered for MAS), so the answer is about what is on screen.
  const check = useMutation({
    mutationFn: async () => {
      if (p.kind === "oidc" && issuer && issuer !== p.issuer) {
        await api.put(`/api/v1/login-providers/${p.id}`, { issuer });
      }
      return api.post<{ checked: boolean; ok?: boolean; note?: string }>(`/api/v1/login-providers/${p.id}/check`, {});
    },
  });
  const del = useMutation({
    mutationFn: () => api.delete(`/api/v1/login-providers/${p.id}`),
    onSuccess: () => { setConfirmDelete(false); onChanged(); },
  });

  const state = !p.complete ? <Badge tone="warn" size="sm">Entwurf</Badge>
    : !p.enabled ? <Badge tone="neutral" size="sm">Aus</Badge>
    : shown === true ? <Badge tone="ok" size="sm" icon="check">Auf der Anmeldeseite</Badge>
    : <Badge tone="info" size="sm">Aktiv</Badge>;

  return (
    <Card style={{ display: "flex", flexDirection: "column", gap: 14 }}>
      <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
        <Icon name="key" size={18} style={{ color: "var(--accent)" }} />
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
            <span style={{ fontSize: 14, fontWeight: 600, color: "var(--text)" }}>{p.name}</span>
            {state}
          </div>
          <div style={{ fontSize: 12, color: "var(--text-faint)" }}>{KIND_LABEL[p.kind]}{p.issuer ? ` · ${p.issuer}` : ""}{p.client_id ? ` · Client-ID ${p.client_id}` : ""}</div>
        </div>
        {p.complete && (
          <Toggle checked={p.enabled} onChange={(v) => save.mutate({ enabled: v })} />
        )}
        <Button variant="ghost" size="sm" onClick={onEdit}>{editing ? "Schließen" : p.complete ? "Ändern" : "Einrichten"}</Button>
      </div>

      {/* The address that connects an existing account. Nothing links automatically —
          that would be an account-takeover path (§4.112) — so this is the only way, and
          until this was added it existed nowhere in the product. */}
      {p.link && (
        <div style={{ display: "flex", flexDirection: "column", gap: 8, padding: "12px 14px", marginLeft: 28, borderRadius: "var(--radius-sm)", background: "var(--surface-2)", border: "1px solid var(--border-soft)" }}>
          <div style={{ fontSize: 13, fontWeight: 600, color: "var(--text)" }}>Bestehenden Account mit {p.name} verknüpfen</div>
          <ol style={{ margin: 0, paddingLeft: 18, display: "flex", flexDirection: "column", gap: 5, fontSize: 12.5, color: "var(--text-dim)", lineHeight: 1.55 }}>
            <li>
              Im selben Browser {accountUrl
                ? <a href={accountUrl} target="_blank" rel="noreferrer" style={{ color: "var(--accent)" }}>auf der Kontoseite</a>
                : "auf der Kontoseite"} mit Benutzername und Passwort anmelden.
            </li>
            <li style={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: 6 }}>
              Dann diese Adresse öffnen: <Copyable text={p.link} />
              <a href={p.link} target="_blank" rel="noreferrer" style={{ color: "var(--accent)" }}>öffnen</a>
            </li>
            <li>{p.name} fragt nach der Anmeldung, ob verknüpft werden soll — bestätigen.</li>
          </ol>
          <span style={{ fontSize: 11.5, color: "var(--text-faint)" }}>
            Ohne vorherige Anmeldung legt {p.name} stattdessen einen neuen Account an. Automatisch verknüpft wird nichts — sonst könnte ein fremdes Konto mit gleichem Namen einen Account übernehmen.
          </span>
        </div>
      )}

      {(editing || !p.complete) && (
        <div style={{ display: "flex", flexDirection: "column", gap: 14, paddingLeft: 28 }}>
          <div style={{ fontSize: 13.5, fontWeight: 600, color: "var(--text)" }}>2. Beim Anbieter eintragen</div>
          {p.callback ? <Steps kind={p.kind} callback={p.callback} masHost={masHost} /> : (
            <span style={{ fontSize: 12.5, color: "var(--status-err)" }}>Ohne Hostnamen der Anmeldung gibt es keine Callback-URL.</span>
          )}
          <div style={{ fontSize: 13.5, fontWeight: 600, color: "var(--text)" }}>3. Zugangsdaten hier eintragen</div>
          {/* Editable here too: "Anbieter prüfen" is what finds a wrong issuer — a
              trailing slash, typically — and it has to be fixable where it is found. */}
          {p.kind === "oidc" && (
            <Field label="Issuer-URL" hint="Exakt wie im Discovery-Dokument des Anbieters.">
              <input value={issuer} onChange={(e) => setIssuer(e.target.value)} style={inputStyle} />
            </Field>
          )}
          <Field label="Client-ID">
            <input value={clientId} onChange={(e) => setClientId(e.target.value)} style={inputStyle} autoComplete="off" />
          </Field>
          <Field label="Client-Secret" hint={p.has_secret ? "Gespeichert — leer lassen, um es zu behalten. Es wird nie wieder angezeigt." : "Wird nur im Cluster gespeichert und nie wieder angezeigt."}>
            <input type="password" value={clientSecret} onChange={(e) => setClientSecret(e.target.value)} style={inputStyle} autoComplete="new-password" />
          </Field>
          <div style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap" }}>
            <Button variant="primary" size="sm" icon={save.isPending ? undefined : "check"}
              disabled={save.isPending || !clientId || (!clientSecret && !p.has_secret) || (p.kind === "oidc" && !issuer)}
              onClick={() => save.mutate({ client_id: clientId, client_secret: clientSecret, ...(p.kind === "oidc" ? { issuer } : {}) })}>
              {save.isPending ? <Spinner size={13} /> : "Speichern"}
            </Button>
            {p.kind !== "github" && (
              <Button variant="outline" size="sm" disabled={check.isPending} onClick={() => check.mutate()}>
                {check.isPending ? <Spinner size={13} /> : "Anbieter prüfen"}
              </Button>
            )}
            {!p.complete && <Button variant="dangerGhost" size="sm" onClick={() => setConfirmDelete(true)}>Entwurf löschen</Button>}
          </div>
          {check.data && (
            <span style={{ fontSize: 12.5, color: check.data.ok ? "var(--status-ok)" : "var(--status-warn)" }}>
              {check.data.ok ? "Der Anbieter antwortet, der Issuer stimmt." : check.data.note}
            </span>
          )}
          {save.isError && <span style={{ fontSize: 12.5, color: "var(--status-err)" }}>{(save.error as Error).message}</span>}
        </div>
      )}

      <ConfirmDialog open={confirmDelete} title="Entwurf löschen?" confirmLabel="Löschen" confirmIcon="trash"
        busy={del.isPending} onConfirm={() => del.mutate()} onCancel={() => setConfirmDelete(false)}>
        Die Callback-URL dieses Entwurfs wird ungültig. Falls du sie beim Anbieter schon eingetragen hast, entferne sie dort auch.
      </ConfirmDialog>
    </Card>
  );
}

/** What to do at the provider, with the callback URL where it is needed. */
function Steps({ kind, callback, masHost }: { kind: Kind; callback: string; masHost: string }) {
  const cb = <Copyable text={callback} />;
  const steps: ReactNode[] = kind === "google" ? [
    <>In der <a href="https://console.cloud.google.com/apis/credentials" target="_blank" rel="noreferrer" style={{ color: "var(--accent)" }}>Google Cloud Console</a> unter „Anmeldedaten" eine <strong>OAuth-Client-ID</strong> erstellen, Typ <strong>Webanwendung</strong>.</>,
    <>Bei „Autorisierte Weiterleitungs-URIs" eintragen: {cb}</>,
    <>Den <strong>OAuth-Zustimmungsbildschirm</strong> auf Nutzertyp „Extern" stellen und <strong>veröffentlichen</strong> — sonst kommen nur eingetragene Testnutzer herein.</>,
    <>Client-ID und Clientschlüssel unten eintragen.</>,
  ] : kind === "github" ? [
    <>Auf GitHub <a href="https://github.com/settings/applications/new" target="_blank" rel="noreferrer" style={{ color: "var(--accent)" }}>eine neue OAuth-App anlegen</a> (für eine Organisation: in deren Einstellungen unter „Developer settings").</>,
    <>Homepage URL: <Copyable text={`https://${masHost}`} /></>,
    <>Authorization callback URL: {cb}</>,
    <>Nach „Register application" auf <strong>Generate a new client secret</strong> klicken; Client ID und Secret unten eintragen.</>,
  ] : [
    <>Im Anbieter eine Anwendung anlegen: vertraulich (confidential), Ablauf „Authorization Code", Scopes <code style={{ fontFamily: "var(--mono)" }}>openid profile email</code>, Client-Authentifizierung <strong>Basic</strong> (<code style={{ fontFamily: "var(--mono)" }}>client_secret_basic</code>).</>,
    <>Als Redirect-URI eintragen: {cb}</>,
    <>Client-ID und Client-Secret unten eintragen, dann „Anbieter prüfen".</>,
    <><strong>Zitadel:</strong> Projekt → Neue Anwendung → Typ <em>Web</em> → Authentifizierung <em>Code</em> → Methode <em>Basic</em>. Die Issuer-URL ist die Domain der Instanz ohne Schrägstrich am Ende (z. B. <code style={{ fontFamily: "var(--mono)" }}>https://login.example.org</code>) — „Anbieter prüfen" sagt dir, ob sie exakt stimmt.</>,
  ];
  return (
    <ol style={{ margin: 0, paddingLeft: 20, display: "flex", flexDirection: "column", gap: 8, fontSize: 13, lineHeight: 1.6, color: "var(--text-dim)" }}>
      {steps.map((s, i) => <li key={i}>{s}</li>)}
    </ol>
  );
}

function Copyable({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 6, maxWidth: "100%" }}>
      <code style={{ fontFamily: "var(--mono)", fontSize: 12, padding: "2px 6px", borderRadius: 4, background: "var(--surface-2)", color: "var(--text)", overflowWrap: "anywhere" }}>{text}</code>
      <button type="button" title="Kopieren" onClick={() => { navigator.clipboard?.writeText(text).then(() => { setCopied(true); setTimeout(() => setCopied(false), 1500); }); }}
        style={{ display: "inline-grid", placeItems: "center", border: "none", background: "transparent", cursor: "pointer", color: copied ? "var(--status-ok)" : "var(--text-faint)", padding: 2 }}>
        <Icon name={copied ? "check" : "copy"} size={13} />
      </button>
    </span>
  );
}

function Field({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <label style={{ display: "flex", flexDirection: "column", gap: 5 }}>
      <span style={{ fontSize: 12.5, fontWeight: 600, color: "var(--text-dim)" }}>{label}</span>
      {children}
      {hint && <span style={{ fontSize: 11.5, color: "var(--text-faint)" }}>{hint}</span>}
    </label>
  );
}

const inputStyle: React.CSSProperties = {
  width: "100%", maxWidth: 480, padding: "8px 10px", fontSize: 13, background: "var(--surface-2)",
  border: "1px solid var(--border)", color: "var(--text)", borderRadius: "var(--radius-sm)", fontFamily: "var(--font)",
};
