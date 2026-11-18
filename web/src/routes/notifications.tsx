import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Badge, Button, Card, EmptyState, Icon, SectionTitle, Spinner, StatusDot, Toggle } from "@/components/mc";

export const Route = createFileRoute("/notifications")({ component: NotificationsPage });

type Encryption = "tls" | "starttls" | "plain";
type Reminder = "off" | "6h" | "24h";
export interface NotifySettings {
  webhook: { enabled: boolean; urlSet: boolean };
  email: { enabled: boolean };
  recipients: string[];
  smtpSource: "mas" | "own";
  own: { from: string; host: string; port: number; encryption: Encryption; username: string; passwordSet: boolean };
  reminder: Reminder;
  triggers: Record<string, boolean>;
  masAvailable: boolean; saved: boolean; secretsUsable: boolean;
}
interface ChannelState { lastSentAt: string | null; lastError: string; lastErrorAt: string | null }
export interface NotifyState {
  alerts: { key: string; trigger: string; title: string; detail: string; since: string; lastSent: string | null; pending?: string[] }[];
  channels: Record<string, ChannelState | undefined>;
  lastRun: null | { at: string; unknown: string[]; error?: string };
}

const TRIGGERS: { id: string; label: string; hint: string }[] = [
  { id: "dns", label: "DNS-Ausfall", hint: "Ein Name deines Servers löst im Cluster nicht mehr auf. Während eines solchen Ausfalls kommt die Nachricht nur an, wenn Webhook bzw. Mailserver ohne Namensauflösung im Cluster erreichbar sind (z. B. per IP); sonst wird sie danach nachgeholt." },
  { id: "cert", label: "Zertifikat läuft ab", hint: "Ein Zertifikat ist in weniger als 14 Tagen nicht mehr gültig." },
  { id: "update", label: "Neues MatrixCtrl-Update", hint: "Es gibt eine neuere Version von MatrixCtrl." },
  { id: "pod", label: "Pod hängt", hint: "Ein Pod startet seit über 10 Minuten nicht richtig." },
  { id: "backup", label: "Off-Site-Backup gescheitert", hint: "Die letzte Sicherung ins externe Ziel ist fehlgeschlagen." },
  { id: "release", label: "ESS-Update gescheitert", hint: "Der Helm-Release des ESS steht auf „failed“ oder hängt seit über 10 Minuten in „pending“." },
];
const REMINDERS: { value: Reminder; label: string }[] = [
  { value: "off", label: "Nicht erinnern" }, { value: "6h", label: "Alle 6 Stunden" }, { value: "24h", label: "Alle 24 Stunden" },
];
const ENCRYPTION: { value: Encryption; label: string }[] = [
  { value: "starttls", label: "STARTTLS (Port 587)" }, { value: "tls", label: "TLS (Port 465)" }, { value: "plain", label: "Keine" },
];

const fmt = (iso: string | null | undefined) => (iso ? new Date(iso).toLocaleString("de-DE", { dateStyle: "short", timeStyle: "short" }) : "—");
const input = { width: "100%", padding: "7px 10px", borderRadius: "var(--radius-sm)", border: "1px solid var(--border)", background: "var(--surface-2)", color: "var(--text)", fontSize: 13 } as const;

function Field({ label, children }: { label: string; children: ReactNode }) {
  return <label style={{ display: "flex", flexDirection: "column", gap: 4, fontSize: 12, color: "var(--text-dim)" }}>{label}{children}</label>;
}
function Row({ title, hint, children }: { title: string; hint?: string; children: ReactNode }) {
  return (
    <div style={{ display: "flex", alignItems: "center", gap: 12, justifyContent: "space-between" }}>
      <div><div style={{ fontSize: 13, fontWeight: 550 }}>{title}</div>{hint && <div style={{ fontSize: 12, color: "var(--text-faint)" }}>{hint}</div>}</div>
      {children}
    </div>
  );
}

/** Problems the panel noticed, and where it reports them (etappe 123). */
export function NotificationsPage() {
  const qc = useQueryClient();
  const settings = useQuery({ queryKey: ["notify-settings"], queryFn: () => api.get<NotifySettings>("/api/v1/notifications/settings") });
  const state = useQuery({ queryKey: ["notify-state"], queryFn: () => api.get<NotifyState>("/api/v1/notifications/state"), refetchInterval: 30000 });

  const [form, setForm] = useState<NotifySettings | null>(null);
  const [url, setUrl] = useState(""); const [clearUrl, setClearUrl] = useState(false);
  const [password, setPassword] = useState(""); const [clearPass, setClearPass] = useState(false);
  const [recipients, setRecipients] = useState("");
  const [savedOk, setSavedOk] = useState(false);
  useEffect(() => {
    if (settings.data && !form) { setForm(settings.data); setRecipients(settings.data.recipients.join(", ")); }
  }, [settings.data, form]);

  const save = useMutation({
    mutationFn: () => api.put<unknown>("/api/v1/notifications/settings", {
      ...form, recipients: recipients.split(/[\s,;]+/).filter(Boolean),
      webhook: { enabled: form!.webhook.enabled, url, clearUrl },
      own: { ...form!.own, password, clearPassword: clearPass },
    }),
    onSuccess: () => {
      setUrl(""); setClearUrl(false); setPassword(""); setClearPass(false); setSavedOk(true);
      setForm(null); test.reset();
      qc.invalidateQueries({ queryKey: ["notify-settings"] }); qc.invalidateQueries({ queryKey: ["notify-state"] });
    },
  });
  const test = useMutation({ mutationFn: (channel: "webhook" | "email") => api.post<{ ok: boolean; error: string }>("/api/v1/notifications/test", { channel }) });

  if (settings.isLoading || !form) return <div style={{ display: "flex", gap: 8, padding: 24, fontSize: 13, color: "var(--text-faint)" }}><Spinner size={14} /> Lade…</div>;
  const set = (p: Partial<NotifySettings>) => { setSavedOk(false); setForm({ ...form, ...p }); };
  const setOwn = (p: Partial<NotifySettings["own"]>) => set({ own: { ...form.own, ...p } });
  const st = state.data;
  const ch = (n: string) => st?.channels?.[n];
  const testResult = (n: "webhook" | "email") =>
    test.variables === n && test.data ? (
      <div style={{ fontSize: 12.5, color: test.data.ok ? "var(--ok, var(--accent))" : "var(--err, #e5484d)" }}>
        {test.data.ok ? "Testnachricht verschickt." : `Fehlgeschlagen: ${test.data.error}`}
      </div>
    ) : null;
  const history = (n: string) => {
    const c = ch(n);
    if (!c) return null;
    return (
      <div style={{ fontSize: 12, color: "var(--text-faint)" }}>
        Letzter Versand: {fmt(c.lastSentAt)}
        {c.lastError && <div style={{ color: "var(--err, #e5484d)" }}>Letzter Fehler ({fmt(c.lastErrorAt)}): {c.lastError}</div>}
      </div>
    );
  };
  const testButton = (n: "webhook" | "email") => (
    <Button size="sm" icon="play" disabled={test.isPending} onClick={() => test.mutate(n)}>Testnachricht senden</Button>
  );

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 18, maxWidth: 760 }}>
      <SectionTitle icon="bell" sub="Damit du von Problemen erfährst, bevor jemand anruft">Benachrichtigungen</SectionTitle>

      {!form.saved && <Card style={{ fontSize: 13, color: "var(--text-dim)" }}>Noch nichts eingerichtet — alles ist aus.</Card>}
      {!form.secretsUsable && (
        <Card style={{ fontSize: 13, color: "var(--err, #e5484d)" }}>
          Geheimnisse können hier gerade nicht gespeichert werden. Webhook-Adresse und Passwort würden verloren gehen.
        </Card>
      )}

      <Card style={{ display: "flex", flexDirection: "column", gap: 10 }}>
        <div style={{ fontSize: 14, fontWeight: 600 }}>Aktive Probleme</div>
        {st && st.alerts.length === 0 && (
          <EmptyState icon="check" title="Alles ruhig" sub={st.lastRun ? `Letzte Prüfung: ${fmt(st.lastRun.at)}` : "Noch keine Prüfung gelaufen."} />
        )}
        {st?.alerts.map((a) => (
          <div key={a.key} style={{ display: "flex", gap: 10, alignItems: "flex-start" }}>
            <StatusDot status="warn" />
            <div style={{ flex: 1, fontSize: 13 }}>
              <div><b>{a.title}</b> <Badge size="sm">{TRIGGERS.find((t) => t.id === a.trigger)?.label ?? a.trigger}</Badge></div>
              {a.detail && <div style={{ color: "var(--text-dim)" }}>{a.detail}</div>}
              <div style={{ fontSize: 12, color: "var(--text-faint)" }}>Seit {fmt(a.since)} · zuletzt gemeldet: {a.lastSent ? fmt(a.lastSent) : "noch nicht"}</div>
              {a.pending && a.pending.length > 0 && (
                <div style={{ fontSize: 12, color: "var(--warn, var(--text-dim))" }}>
                  {a.pending.map((c) => (c === "email" ? "E-Mail" : c === "webhook" ? "Webhook" : c)).join(" und ")} noch nicht zugestellt
                </div>
              )}
            </div>
          </div>
        ))}
        {st?.lastRun?.unknown && st.lastRun.unknown.length > 0 && (
          <div style={{ fontSize: 12.5, color: "var(--text-dim)" }}>Konnte nicht geprüft werden: {st.lastRun.unknown.join(", ")}</div>
        )}
        {st?.lastRun?.error && <div style={{ fontSize: 12.5, color: "var(--err, #e5484d)" }}>Prüfung fehlgeschlagen: {st.lastRun.error}</div>}
      </Card>

      <Card style={{ display: "flex", flexDirection: "column", gap: 12 }}>
        <Row title="Admin-Raum (Webhook)" hint="z. B. ein Hookshot-Webhook aus der Bridges-Seite">
          <Toggle checked={form.webhook.enabled} onChange={(v) => set({ webhook: { ...form.webhook, enabled: v } })} />
        </Row>
        <Field label={form.webhook.urlSet && !clearUrl ? "Webhook-Adresse (gesetzt — leer lassen = unverändert)" : "Webhook-Adresse"}>
          <input type="password" autoComplete="off" style={input} value={url} placeholder={form.webhook.urlSet && !clearUrl ? "••••••••" : "https://…"}
            onChange={(e) => { setSavedOk(false); setUrl(e.target.value); }} />
        </Field>
        {form.webhook.urlSet && (
          <label style={{ fontSize: 12, display: "flex", gap: 6 }}>
            <input type="checkbox" checked={clearUrl} onChange={(e) => { setSavedOk(false); setClearUrl(e.target.checked); }} /> Adresse entfernen
          </label>
        )}
        <div style={{ display: "flex", gap: 10, alignItems: "center" }}>{testButton("webhook")}{test.isPending && test.variables === "webhook" && <Spinner size={14} />}</div>
        <div style={{ fontSize: 12, color: "var(--text-faint)" }}>Die Testnachricht nutzt die zuletzt gespeicherten Angaben.</div>
        {testResult("webhook")}{history("webhook")}
      </Card>

      <Card style={{ display: "flex", flexDirection: "column", gap: 12 }}>
        <Row title="E-Mail" hint="Mehrere Empfänger mit Komma trennen">
          <Toggle checked={form.email.enabled} onChange={(v) => set({ email: { enabled: v } })} />
        </Row>
        <Field label="Empfänger">
          <input style={input} value={recipients} placeholder="admin@example.org" onChange={(e) => { setSavedOk(false); setRecipients(e.target.value); }} />
        </Field>
        <div style={{ display: "flex", flexDirection: "column", gap: 6, fontSize: 13 }}>
          <label style={{ display: "flex", gap: 6, opacity: form.masAvailable ? 1 : 0.5 }}>
            <input type="radio" name="smtp" disabled={!form.masAvailable} checked={form.smtpSource === "mas"} onChange={() => set({ smtpSource: "mas" })} />
            Wie Matrix (MAS)
          </label>
          {!form.masAvailable && <div style={{ fontSize: 12, color: "var(--text-faint)", marginLeft: 22 }}>Nicht verfügbar — richte zuerst auf der E-Mail-Seite den Versand ein.</div>}
          <label style={{ display: "flex", gap: 6 }}>
            <input type="radio" name="smtp" checked={form.smtpSource === "own"} onChange={() => set({ smtpSource: "own" })} />
            Eigener Zugang
          </label>
        </div>
        {form.smtpSource === "own" && (
          <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 10 }}>
            <Field label="Absender"><input style={input} value={form.own.from} onChange={(e) => setOwn({ from: e.target.value })} /></Field>
            <Field label="Server"><input style={input} value={form.own.host} onChange={(e) => setOwn({ host: e.target.value })} /></Field>
            <Field label="Port"><input style={input} type="number" value={form.own.port || ""} onChange={(e) => setOwn({ port: Number(e.target.value) })} /></Field>
            <Field label="Verschlüsselung">
              <select style={input} value={form.own.encryption} onChange={(e) => setOwn({ encryption: e.target.value as Encryption })}>
                {ENCRYPTION.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
              </select>
            </Field>
            <Field label="Benutzername"><input style={input} value={form.own.username} onChange={(e) => setOwn({ username: e.target.value })} /></Field>
            <Field label={form.own.passwordSet && !clearPass ? "Passwort (gesetzt — leer = unverändert)" : "Passwort"}>
              <input type="password" autoComplete="new-password" style={input} value={password} onChange={(e) => { setSavedOk(false); setPassword(e.target.value); }} />
            </Field>
            {form.own.passwordSet && (
              <label style={{ fontSize: 12, display: "flex", gap: 6 }}>
                <input type="checkbox" checked={clearPass} onChange={(e) => { setSavedOk(false); setClearPass(e.target.checked); }} /> Passwort entfernen
              </label>
            )}
          </div>
        )}
        <div style={{ display: "flex", gap: 10, alignItems: "center" }}>{testButton("email")}{test.isPending && test.variables === "email" && <Spinner size={14} />}</div>
        <div style={{ fontSize: 12, color: "var(--text-faint)" }}>Die Testnachricht nutzt die zuletzt gespeicherten Angaben.</div>
        {testResult("email")}{history("email")}
      </Card>

      <Card style={{ display: "flex", flexDirection: "column", gap: 10 }}>
        <Row title="Erinnerung" hint="Wie oft ein weiterhin bestehendes Problem erneut gemeldet wird">
          <select style={{ ...input, width: "auto" }} value={form.reminder} onChange={(e) => set({ reminder: e.target.value as Reminder })}>
            {REMINDERS.map((r) => <option key={r.value} value={r.value}>{r.label}</option>)}
          </select>
        </Row>
      </Card>

      <Card style={{ display: "flex", flexDirection: "column", gap: 12 }}>
        <div style={{ fontSize: 14, fontWeight: 600 }}>Wovon du erfahren willst</div>
        {TRIGGERS.map((t) => (
          <Row key={t.id} title={t.label} hint={t.hint}>
            <Toggle checked={!!form.triggers[t.id]} onChange={(v) => set({ triggers: { ...form.triggers, [t.id]: v } })} />
          </Row>
        ))}
      </Card>

      <div style={{ display: "flex", gap: 12, alignItems: "center", flexWrap: "wrap" }}>
        <Button variant="primary" icon="check" disabled={save.isPending} onClick={() => save.mutate()}>Speichern</Button>
        {save.isPending && <Spinner size={14} />}
        {savedOk && <span style={{ fontSize: 13 }}><Icon name="check" size={14} /> Gespeichert.</span>}
        {save.isError && <span role="alert" style={{ fontSize: 13, color: "var(--err, #e5484d)" }}>{(save.error as Error).message}</span>}
      </div>
    </div>
  );
}
