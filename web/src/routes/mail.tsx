import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Button, Card, Icon, SectionTitle, Spinner, Toggle } from "@/components/mc";

export const Route = createFileRoute("/mail")({ component: MailPage });

type Encryption = "tls" | "starttls" | "plain";
interface Settings {
  enabled: boolean; from: string; reply_to?: string; host: string; port: number;
  encryption: Encryption; username?: string; has_password: boolean;
}
interface MailResponse {
  settings: Settings; suggested_from: string;
  wiring: { in_config: boolean; deployed: boolean };
}
interface Check { ok: boolean; note?: string }

const ENCRYPTION: { value: Encryption; label: string; hint: string }[] = [
  { value: "starttls", label: "STARTTLS", hint: "üblich, Port 587" },
  { value: "tls", label: "TLS", hint: "durchgehend verschlüsselt, Port 465" },
  { value: "plain", label: "Keine", hint: "nur für einen Mailserver im selben Cluster" },
];

/** E-mail for registration confirmations and forgotten passwords (etappe 114b).
 *
 *  Its own page rather than a row in the task layer: a password, a connection check and
 *  a test message do not fit on one line — and without them nobody can tell whether it
 *  works before the first user needs it. */
function MailPage() {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const { data, isLoading } = useQuery({ queryKey: ["mail"], queryFn: () => api.get<MailResponse>("/api/v1/mail") });

  const [form, setForm] = useState<Settings | null>(null);
  const [password, setPassword] = useState("");
  const [to, setTo] = useState("");
  const [saved, setSaved] = useState<string | null>(null);
  useEffect(() => { if (data && !form) setForm({ ...data.settings, from: data.settings.from || data.suggested_from }); }, [data, form]);

  const save = useMutation({
    mutationFn: () => api.put<{ next: string }>("/api/v1/mail", { ...form, password }),
    onSuccess: (r) => { setPassword(""); setSaved(r.next); probe.reset(); test.reset(); qc.invalidateQueries({ queryKey: ["mail"] }); },
  });
  const probe = useMutation({ mutationFn: () => api.post<Check>("/api/v1/mail/probe", {}) });
  const test = useMutation({ mutationFn: () => api.post<Check>("/api/v1/mail/test", { to }) });

  if (isLoading || !form) return <div style={{ display: "flex", gap: 8, padding: 24, fontSize: 13, color: "var(--text-faint)" }}><Spinner size={14} /> Lade…</div>;
  const set = <K extends keyof Settings>(k: K, v: Settings[K]) => setForm({ ...form!, [k]: v });
  const pending = data && !data.wiring.deployed && form.enabled;

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 18, maxWidth: 720 }}>
      <SectionTitle icon="globe" sub="Damit dein Server Bestätigungen und vergessene Passwörter verschicken kann">E-Mail</SectionTitle>

      <Card style={{ display: "flex", gap: 12, fontSize: 13, lineHeight: 1.6, color: "var(--text-dim)" }}>
        <Icon name="info" size={18} style={{ color: "var(--accent)", flexShrink: 0, marginTop: 2 }} />
        <div>
          Ohne E-Mail-Versand kann niemand ein vergessenes Passwort zurücksetzen, und „E-Mail-Adresse bei der Registrierung verlangen“ hat keine Wirkung.
          Du brauchst die Zugangsdaten eines Postfachs oder eines Versanddienstes — dieselben, die du in einem Mailprogramm eintragen würdest.
        </div>
      </Card>

      <Card style={{ display: "flex", flexDirection: "column", gap: 16 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
          <div style={{ flex: 1 }}>
            <div style={{ fontSize: 14, fontWeight: 600, color: "var(--text)" }}>E-Mail-Versand</div>
            <div style={{ fontSize: 12.5, color: "var(--text-faint)" }}>
              {form.enabled ? "An: Bestätigungen und Passwort-Zurücksetzen werden verschickt." : "Aus: dein Server verschickt nichts."}
            </div>
          </div>
          <Toggle checked={form.enabled} onChange={(v) => set("enabled", v)} />
        </div>

        {form.enabled && (
          <>
            <Field label="Absender" hint="Von dieser Adresse kommen die Mails. Manche Anbieter verlangen, dass sie zum Konto gehört.">
              <input value={form.from} onChange={(e) => set("from", e.target.value)} style={input} placeholder={data!.suggested_from} />
            </Field>
            <Field label="Antwort-an (optional)" hint="Wohin Antworten gehen, wenn jemand auf eine Mail antwortet.">
              <input value={form.reply_to ?? ""} onChange={(e) => set("reply_to", e.target.value)} style={input} />
            </Field>
            <div style={{ display: "flex", gap: 12, flexWrap: "wrap" }}>
              <Field label="Mailserver" hint="Der SMTP-Server deines Anbieters.">
                <input value={form.host} onChange={(e) => set("host", e.target.value)} style={{ ...input, width: 300 }} placeholder="smtp.example.org" />
              </Field>
              <Field label="Port">
                <input value={form.port || ""} inputMode="numeric" style={{ ...input, width: 100 }}
                  onChange={(e) => {
                    const p = Number(e.target.value.replace(/\D/g, "")) || 0;
                    // Following the port is a suggestion, not a rule: it only moves
                    // while the operator has not chosen an encryption themselves.
                    const enc: Encryption = p === 465 ? "tls" : p === 25 ? "plain" : "starttls";
                    setForm({ ...form!, port: p, encryption: enc });
                  }} />
              </Field>
            </div>
            <Field label="Verschlüsselung">
              <div style={{ display: "flex", gap: 14, flexWrap: "wrap" }}>
                {ENCRYPTION.map((e) => (
                  <label key={e.value} style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 13, color: "var(--text)", cursor: "pointer" }}>
                    <input type="radio" checked={form.encryption === e.value} onChange={() => set("encryption", e.value)} />
                    {e.label} <span style={{ fontSize: 11.5, color: "var(--text-faint)" }}>{e.hint}</span>
                  </label>
                ))}
              </div>
            </Field>
            <div style={{ display: "flex", gap: 12, flexWrap: "wrap" }}>
              <Field label="Benutzername" hint="Leer lassen, wenn der Server keine Anmeldung verlangt.">
                <input value={form.username ?? ""} onChange={(e) => set("username", e.target.value)} style={{ ...input, width: 260 }} autoComplete="off" />
              </Field>
              <Field label="Passwort" hint={form.has_password ? "Gespeichert — leer lassen, um es zu behalten." : "Wird nur im Cluster gespeichert."}>
                <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} style={{ ...input, width: 260 }} autoComplete="new-password" />
              </Field>
            </div>
          </>
        )}

        <div style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap" }}>
          <Button variant="primary" size="sm" icon={save.isPending ? undefined : "check"} disabled={save.isPending} onClick={() => save.mutate()}>
            {save.isPending ? <Spinner size={13} /> : "Speichern"}
          </Button>
          {save.isError && <span style={{ fontSize: 12.5, color: "var(--status-err)" }}>{(save.error as Error).message}</span>}
          {saved === "restarting" && <span style={{ fontSize: 12.5, color: "var(--status-ok)" }}>Gespeichert — die Anmeldung startet damit neu.</span>}
        </div>

        {pending && saved && (
          <div style={{ display: "flex", alignItems: "center", gap: 10, padding: "10px 12px", borderRadius: "var(--radius-sm)", border: "1px solid var(--status-warn)", background: "color-mix(in oklch, var(--status-warn) 8%, transparent)" }}>
            <Icon name="alert" size={16} style={{ color: "var(--status-warn)" }} />
            <span style={{ flex: 1, fontSize: 12.5, color: "var(--text)" }}>Gespeichert, aber noch nicht aktiv: die Einbindung wartet in den Einstellungen auf „Übernehmen“.</span>
            <Button variant="outline" size="sm" onClick={() => navigate({ to: "/config" })}>Zu den Einstellungen</Button>
          </div>
        )}
      </Card>

      {form.enabled && (
        <Card style={{ display: "flex", flexDirection: "column", gap: 14 }}>
          <div style={{ fontSize: 13.5, fontWeight: 600, color: "var(--text)" }}>Funktioniert es?</div>
          <div style={{ display: "flex", gap: 10, alignItems: "center", flexWrap: "wrap" }}>
            <Button variant="outline" size="sm" disabled={probe.isPending} onClick={() => probe.mutate()}>
              {probe.isPending ? <Spinner size={13} /> : "Verbindung prüfen"}
            </Button>
            <span style={{ fontSize: 12, color: "var(--text-faint)" }}>Verbindet, meldet sich an und legt wieder auf — verschickt nichts.</span>
          </div>
          {probe.data && <Result ok={probe.data.ok} note={probe.data.note} okText="Der Mailserver nimmt deine Zugangsdaten an." />}

          <div style={{ display: "flex", gap: 10, alignItems: "flex-end", flexWrap: "wrap" }}>
            <Field label="Testnachricht an">
              <input value={to} onChange={(e) => setTo(e.target.value)} style={{ ...input, width: 260 }} placeholder="du@example.org" />
            </Field>
            <Button variant="outline" size="sm" disabled={!to || test.isPending} onClick={() => test.mutate()}>
              {test.isPending ? <Spinner size={13} /> : "Senden"}
            </Button>
          </div>
          {test.data && <Result ok={test.data.ok} note={test.data.note} okText="Abgeschickt — schau in dein Postfach (auch in den Spam-Ordner)." />}
          {(probe.isError || test.isError) && <span style={{ fontSize: 12.5, color: "var(--status-err)" }}>{((probe.error ?? test.error) as Error).message}</span>}
        </Card>
      )}
    </div>
  );
}

function Result({ ok, note, okText }: { ok: boolean; note?: string; okText: string }) {
  return (
    <div style={{ display: "flex", gap: 8, alignItems: "flex-start", fontSize: 12.5, lineHeight: 1.55, color: ok ? "var(--status-ok)" : "var(--status-warn)" }}>
      <Icon name={ok ? "check" : "alert"} size={15} style={{ flexShrink: 0, marginTop: 1 }} />
      <span>{ok ? okText : note}</span>
    </div>
  );
}

function Field({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <label style={{ display: "flex", flexDirection: "column", gap: 5 }}>
      <span style={{ fontSize: 12.5, fontWeight: 600, color: "var(--text-dim)" }}>{label}</span>
      {children}
      {hint && <span style={{ fontSize: 11.5, color: "var(--text-faint)", maxWidth: 420 }}>{hint}</span>}
    </label>
  );
}

const input: React.CSSProperties = {
  padding: "8px 10px", fontSize: 13, background: "var(--surface-2)", border: "1px solid var(--border)",
  color: "var(--text)", borderRadius: "var(--radius-sm)", fontFamily: "var(--font)", width: 420,
};
