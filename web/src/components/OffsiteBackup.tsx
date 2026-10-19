import { useState, type CSSProperties, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Card, Icon, Button, Badge, Spinner, Toggle, ConfirmDialog, StatusDot } from "@/components/mc";
import { api, ApiError } from "@/lib/api";

/** The automatic, encrypted backups to S3 (etappe 120).
 *
 *  Everything secret here is handled the same way: the private key is shown once, from
 *  component state only. It is never written to localStorage, never put in a query cache,
 *  and gone when the component unmounts. The server keeps only the public half. */

const BASE = "/api/v1/status/backup/offsite";
const mb = (n: number) => (n / 1024 / 1024).toFixed(1);
const size = (n: number) => (n >= 1024 ** 3 ? `${(n / 1024 ** 3).toFixed(2)} GB` : `${mb(n)} MB`);

interface Settings {
  endpoint: string; region: string; bucket: string; prefix: string; path_style: boolean;
  schedule: boolean; schedule_time: string; media: boolean; public_key: string; keys_set: boolean;
}
interface ProbeResult { ok: boolean; steps: { step: string; ok: boolean; error?: string }[] }
interface Run {
  started_at: string; finished_at?: string; occasion: string; status: "running" | "ok" | "failed" | "skipped";
  object?: string; size_bytes?: number; media?: boolean; error?: string; note?: string;
}
interface OffsiteStatus {
  configured: boolean; missing?: string[]; running: boolean; running_since?: string;
  schedule_enabled: boolean; next_run?: string; last?: Run; last_ok?: Run; recent?: Run[];
}
interface BucketObject { key: string; name: string; time: string; occasion: string; size: number }

const input: CSSProperties = {
  padding: "8px 10px", fontSize: 13, background: "var(--surface-2)", color: "var(--text)",
  border: "1px solid var(--border)", borderRadius: "var(--radius-sm)", fontFamily: "var(--font)", width: "100%", boxSizing: "border-box",
};
const mono: CSSProperties = { fontFamily: "var(--mono)" };

function Field({ label, hint, children }: { label: string; hint?: ReactNode; children: ReactNode }) {
  return (
    <label style={{ display: "flex", flexDirection: "column", gap: 5, fontSize: 12.5, fontWeight: 600, color: "var(--text-dim)", minWidth: 0 }}>
      {label}
      {children}
      {hint && <span style={{ fontWeight: 400, fontSize: 11.5, color: "var(--text-faint)" }}>{hint}</span>}
    </label>
  );
}

const errMsg = (e: unknown, fallback: string) => (e instanceof Error && e.message ? e.message : fallback);

/** The private key, shown once and large. Nothing here persists it. */
export function PrivateKeyOnce({ privateKey, publicKey, onHide }: { privateKey: string; publicKey: string; onHide: () => void }) {
  const [copied, setCopied] = useState(false);
  const [ack, setAck] = useState(false);
  const copy = async () => {
    try { await navigator.clipboard.writeText(privateKey); setCopied(true); } catch { setCopied(false); }
  };
  return (
    <div role="alert" style={{ display: "flex", flexDirection: "column", gap: 10, padding: "16px 18px", border: "1px solid var(--status-warn)", borderRadius: "var(--radius-sm)", background: "color-mix(in oklch, var(--status-warn) 8%, transparent)" }}>
      <strong style={{ fontSize: 14, color: "var(--text)" }}>Privater Schlüssel — jetzt sichern</strong>
      <p style={{ margin: 0, fontSize: 12.5, color: "var(--text-dim)", maxWidth: "64ch", lineHeight: 1.6 }}>
        <strong style={{ color: "var(--status-warn)" }}>Nur jetzt sichtbar; ohne ihn ist kein Backup lesbar.</strong>{" "}
        Dieser Server behält nur den öffentlichen Schlüssel und kann den privaten nie wieder anzeigen.
        Leg ihn in einem Passwortmanager ab, nicht auf diesem Server.
      </p>
      <code data-testid="private-key" style={{ ...mono, fontSize: 16, lineHeight: 1.5, letterSpacing: "0.03em", color: "var(--text)", background: "var(--bg)", border: "1px solid var(--border)", borderRadius: "var(--radius-sm)", padding: "14px 16px", userSelect: "all", wordBreak: "break-all" }}>
        {privateKey}
      </code>
      <div style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap" }}>
        <Button variant="primary" size="sm" icon={copied ? "check" : "copy"} onClick={() => void copy()}>
          {copied ? "Kopiert" : "Kopieren"}
        </Button>
        <span style={{ fontSize: 11.5, color: "var(--text-faint)" }}>Öffentlich (bleibt hier gespeichert): <span style={mono}>{publicKey}</span></span>
      </div>
      <label style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 12.5, color: "var(--text-dim)", cursor: "pointer" }}>
        <input type="checkbox" checked={ack} onChange={(e) => setAck(e.target.checked)} />
        Ich habe ihn gesichert
      </label>
      {ack && <div><Button size="sm" variant="ghost" onClick={onHide}>Ausblenden</Button></div>}
    </div>
  );
}

/** Generating a key pair, or pasting one's own public key. */
export function KeyPairBlock({ publicKey, onChanged }: { publicKey: string; onChanged: () => void }) {
  // Component state only, deliberately: it must not survive the view.
  const [fresh, setFresh] = useState<{ privateKey: string; publicKey: string } | null>(null);
  const [replaceOpen, setReplaceOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [own, setOwn] = useState("");
  const [ownErr, setOwnErr] = useState<string | null>(null);

  const generate = async (replace: boolean) => {
    setBusy(true); setErr(null);
    try {
      const r = await api.post<{ public_key: string; private_key: string }>(`${BASE}/keypair`, replace ? { replace: true } : {});
      setReplaceOpen(false);
      setFresh({ privateKey: r.private_key, publicKey: r.public_key });
      onChanged();
    } catch (e) {
      // 409: a key is already set. Replacing makes new backups unreadable with the old
      // key, so it is a question, not a retry.
      if (e instanceof ApiError && e.status === 409) setReplaceOpen(true);
      else setErr(errMsg(e, "Der Schlüssel konnte nicht erzeugt werden."));
    } finally {
      setBusy(false);
    }
  };

  const savePublic = async () => {
    setBusy(true); setOwnErr(null);
    try {
      await api.put(`${BASE}/public-key`, { public_key: own.trim() });
      setOwn("");
      onChanged();
    } catch (e) {
      setOwnErr(e instanceof ApiError && e.status === 422
        ? `${errMsg(e, "Das ist kein gültiger öffentlicher Schlüssel.")} Hier gehört nur der öffentliche Schlüssel (age1…) hin, nie der private.`
        : errMsg(e, "Speichern fehlgeschlagen."));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
      <div style={{ fontSize: 12.5, fontWeight: 600, color: "var(--text-dim)" }}>Verschlüsselung</div>
      <div style={{ fontSize: 12.5, color: "var(--text-dim)", lineHeight: 1.6 }}>
        Jedes Backup wird mit einem öffentlichen Schlüssel verschlüsselt, bevor es den Server verlässt.
        Beim S3-Anbieter liegt nichts Lesbares; zum Wiederherstellen brauchst du den privaten Schlüssel.
      </div>

      {fresh && <PrivateKeyOnce privateKey={fresh.privateKey} publicKey={fresh.publicKey} onHide={() => setFresh(null)} />}

      {!fresh && publicKey && (
        <div style={{ fontSize: 12, color: "var(--text-dim)" }}>
          Öffentlicher Schlüssel: <span style={{ ...mono, wordBreak: "break-all", color: "var(--text)" }}>{publicKey}</span>
        </div>
      )}

      <div style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "center" }}>
        <Button variant={publicKey ? "outline" : "primary"} size="sm" icon="key" disabled={busy} onClick={() => void generate(false)}>
          Schlüsselpaar erzeugen
        </Button>
      </div>
      {err && <div style={{ fontSize: 12.5, color: "var(--status-err)" }}>{err}</div>}

      <div style={{ display: "flex", gap: 8, alignItems: "flex-end", flexWrap: "wrap" }}>
        <div style={{ flex: "1 1 280px" }}>
          <Field label="Oder eigenen öffentlichen Schlüssel einfügen" hint="age1… — den privaten behältst du selbst.">
            <input style={{ ...input, ...mono }} value={own} onChange={(e) => setOwn(e.target.value)} placeholder="age1…"
              autoComplete="off" spellCheck={false} />
          </Field>
        </div>
        <Button variant="outline" size="sm" disabled={busy || own.trim() === ""} onClick={() => void savePublic()}>Übernehmen</Button>
      </div>
      {ownErr && <div style={{ fontSize: 12.5, color: "var(--status-err)" }}>{ownErr}</div>}

      <ConfirmDialog open={replaceOpen} title="Schlüssel ersetzen?" confirmLabel="Ersetzen" confirmIcon="key" busy={busy}
        onConfirm={() => void generate(true)} onCancel={() => setReplaceOpen(false)}>
        Es ist schon ein Schlüssel eingerichtet. Neue Backups sind dann nur noch mit dem neuen privaten Schlüssel lesbar.
        Bestehende Backups bleiben nur mit dem alten lesbar — hebe ihn weiter auf.
      </ConfirmDialog>
    </div>
  );
}

function RunLine({ r }: { r: Run }) {
  const tone = r.status === "ok" ? "ok" : r.status === "failed" ? "err" : r.status === "running" ? "info" : "neutral";
  const label = { ok: "Gesichert", failed: "Fehlgeschlagen", running: "Läuft", skipped: "Übersprungen" }[r.status];
  return (
    <div style={{ display: "flex", gap: 10, alignItems: "baseline", flexWrap: "wrap", fontSize: 12.5, color: "var(--text-dim)" }}>
      <Badge tone={tone} size="sm">{label}</Badge>
      <span>{new Date(r.started_at).toLocaleString("de-DE")}</span>
      <span style={{ color: "var(--text-faint)" }}>{r.occasion}</span>
      {r.status === "ok" && r.size_bytes != null && <span style={mono}>{size(r.size_bytes)}</span>}
      {r.error && <span style={{ color: "var(--status-err)" }}>{r.error}</span>}
      {r.note && <span style={{ color: "var(--text-faint)" }}>{r.note}</span>}
    </div>
  );
}

/** The "Automatisch" card: target, key, schedule, media, run now. */
export function OffsiteCard() {
  const qc = useQueryClient();
  const settingsQ = useQuery({ queryKey: ["offsite", "settings"], queryFn: () => api.get<Settings>(`${BASE}/settings`) });
  const statusQ = useQuery({
    queryKey: ["offsite", "status"],
    queryFn: () => api.get<OffsiteStatus>(`${BASE}/status`),
    refetchInterval: (q) => (q.state.data?.running ? 3000 : 30_000),
  });
  // The existing media-size endpoint, shared with the download section (same cache entry).
  const { data: sizes } = useQuery({
    queryKey: ["backup", "sizes"],
    queryFn: () => api.get<{ media_bytes: number; media_available: boolean; media_note?: string; media_reason?: string }>("/api/v1/status/backup/sizes"),
    staleTime: 5 * 60_000,
  });

  const [edited, setForm] = useState<Settings | null>(null);
  // The loaded settings until the operator edits something.
  const form = edited ?? settingsQ.data ?? null;
  const [accessKey, setAccessKey] = useState("");
  const [secretKey, setSecretKey] = useState("");

  const [saving, setSaving] = useState(false);
  const [saveMsg, setSaveMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const [probing, setProbing] = useState(false);
  const [probe, setProbe] = useState<ProbeResult | null>(null);
  const [probeErr, setProbeErr] = useState<string | null>(null);
  const [runMsg, setRunMsg] = useState<{ ok: boolean; text: string } | null>(null);

  const set = <K extends keyof Settings>(k: K, v: Settings[K]) => setForm((f) => { const b = f ?? settingsQ.data; return b ? { ...b, [k]: v } : f; });
  const refresh = () => {
    void qc.invalidateQueries({ queryKey: ["offsite"] });
  };

  const save = async () => {
    if (!form) return;
    setSaving(true); setSaveMsg(null);
    try {
      await api.put(`${BASE}/settings`, { ...form, access_key: accessKey, secret_key: secretKey });
      setAccessKey(""); setSecretKey("");
      setSaveMsg({ ok: true, text: "Gespeichert." });
      setProbe(null);
      const fresh = await api.get<Settings>(`${BASE}/settings`);
      qc.setQueryData(["offsite", "settings"], fresh);
      setForm(fresh);
      void qc.invalidateQueries({ queryKey: ["offsite", "status"] });
    } catch (e) {
      setSaveMsg({ ok: false, text: errMsg(e, "Speichern fehlgeschlagen.") });
    } finally {
      setSaving(false);
    }
  };

  const doProbe = async () => {
    setProbing(true); setProbe(null); setProbeErr(null);
    try { setProbe(await api.post<ProbeResult>(`${BASE}/probe`, {})); }
    catch (e) { setProbeErr(errMsg(e, "Die Prüfung konnte nicht laufen.")); }
    finally { setProbing(false); }
  };

  const runNow = async () => {
    setRunMsg(null);
    try {
      await api.post(`${BASE}/run`, { media: form?.media ?? false });
      setRunMsg({ ok: true, text: "Backup gestartet." });
      refresh();
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) setRunMsg({ ok: false, text: "Es läuft schon ein Backup." });
      else if (e instanceof ApiError && e.status === 422) {
        const missing = Array.isArray(e.data?.missing) ? (e.data.missing as string[]) : [];
        setRunMsg({ ok: false, text: `Das Ziel ist noch nicht vollständig${missing.length ? ` (es fehlt: ${missing.join(", ")})` : ""}.` });
      } else setRunMsg({ ok: false, text: errMsg(e, "Das Backup konnte nicht gestartet werden.") });
    }
  };

  const status = statusQ.data;
  const running = !!status?.running;

  return (
    <Card style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12, flexWrap: "wrap" }}>
        <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
          <Icon name="database" size={17} />
          <h2 style={{ margin: 0, fontSize: 15, fontWeight: 600 }}>Automatisch</h2>
        </div>
        {status && (
          <div style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 12.5, color: "var(--text-dim)" }}>
            <StatusDot status={running ? "info" : status.configured ? "ok" : "idle"} pulse={running} />
            {running ? "Backup läuft…" : status.configured ? "Ziel eingerichtet" : "Noch nicht eingerichtet"}
          </div>
        )}
      </div>

      <div style={{ fontSize: 12.5, color: "var(--text-dim)", lineHeight: 1.65 }}>
        Sichert täglich ein verschlüsseltes Archiv in einen S3-kompatiblen Bucket — außerhalb dieses Servers.
        Vor jedem ESS-Upgrade läuft zusätzlich eins ohne Medien.
      </div>

      {(settingsQ.isLoading || !form) && !settingsQ.isError && <div style={{ fontSize: 12.5, color: "var(--text-faint)" }}><Spinner size={13} /> Lade…</div>}
      {settingsQ.isError && <div style={{ fontSize: 12.5, color: "var(--status-err)" }}>Die Einstellungen konnten nicht geladen werden.</div>}

      {form && (
        <>
          <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(240px, 1fr))", gap: 12 }}>
            <Field label="Endpoint" hint="z. B. s3.example.com"><input style={input} value={form.endpoint} onChange={(e) => set("endpoint", e.target.value)} autoComplete="off" spellCheck={false} /></Field>
            <Field label="Region"><input style={input} value={form.region} onChange={(e) => set("region", e.target.value)} autoComplete="off" spellCheck={false} /></Field>
            <Field label="Bucket"><input style={input} value={form.bucket} onChange={(e) => set("bucket", e.target.value)} autoComplete="off" spellCheck={false} /></Field>
            <Field label="Präfix" hint="Ordner im Bucket; hier wird nur gelöscht, was von MatrixCtrl stammt."><input style={input} value={form.prefix} onChange={(e) => set("prefix", e.target.value)} autoComplete="off" spellCheck={false} /></Field>
            <Field label="Zugangsschlüssel" hint={form.keys_set ? "Gesetzt. Leer lassen, um ihn zu behalten." : "Noch nicht gesetzt."}>
              <input style={input} value={accessKey} onChange={(e) => setAccessKey(e.target.value)} autoComplete="off" spellCheck={false} placeholder={form.keys_set ? "••••••••" : ""} />
            </Field>
            <Field label="Geheimer Schlüssel" hint={form.keys_set ? "Gesetzt. Leer lassen, um ihn zu behalten." : "Noch nicht gesetzt."}>
              <input style={input} type="password" value={secretKey} onChange={(e) => setSecretKey(e.target.value)} autoComplete="off" placeholder={form.keys_set ? "••••••••" : ""} />
            </Field>
          </div>
          <label style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 12.5, color: "var(--text-dim)", cursor: "pointer" }}>
            <input type="checkbox" checked={form.path_style} onChange={(e) => set("path_style", e.target.checked)} />
            Path-Style-Adressierung (nötig bei MinIO und manchen anderen Anbietern)
          </label>

          <div style={{ display: "flex", alignItems: "center", gap: 10, flexWrap: "wrap" }}>
            <Button variant="primary" size="sm" icon="check" disabled={saving} onClick={() => void save()}>
              {saving ? <><Spinner size={13} /> Speichere…</> : "Speichern"}
            </Button>
            <Button variant="outline" size="sm" icon="activity" disabled={probing || saving} onClick={() => void doProbe()}>
              {probing ? <><Spinner size={13} /> Prüfe…</> : "Verbindung prüfen"}
            </Button>
            <span style={{ fontSize: 11.5, color: "var(--text-faint)" }}>Geprüft wird das gespeicherte Ziel: Objekt schreiben, lesen, löschen.</span>
          </div>
          {saveMsg && <div style={{ fontSize: 12.5, color: saveMsg.ok ? "var(--status-ok)" : "var(--status-err)" }}>{saveMsg.text}</div>}
          {probeErr && <div style={{ fontSize: 12.5, color: "var(--status-err)" }}>{probeErr}</div>}
          {probe && (
            <div style={{ display: "flex", flexDirection: "column", gap: 4, padding: "10px 12px", background: "var(--surface-2)", borderRadius: "var(--radius-sm)", fontSize: 12.5 }}>
              <strong style={{ color: probe.ok ? "var(--status-ok)" : "var(--status-err)" }}>
                {probe.ok ? "Verbindung in Ordnung." : "Die Prüfung ist fehlgeschlagen."}
              </strong>
              {probe.steps.map((s) => (
                <div key={s.step} style={{ color: s.ok ? "var(--text-dim)" : "var(--status-err)" }}>
                  {s.ok ? "✓" : "✗"} {s.step}{s.error ? ` — ${s.error}` : ""}
                </div>
              ))}
            </div>
          )}

          <div style={{ borderTop: "1px solid var(--border-soft)", paddingTop: 14 }}>
            <KeyPairBlock publicKey={form.public_key} onChanged={refresh} />
          </div>

          <div style={{ borderTop: "1px solid var(--border-soft)", paddingTop: 14, display: "flex", flexDirection: "column", gap: 12 }}>
            <div style={{ display: "flex", alignItems: "center", gap: 10, flexWrap: "wrap" }}>
              <Toggle checked={form.schedule} onChange={(v) => set("schedule", v)} />
              <span style={{ fontSize: 13, color: "var(--text)" }}>Täglich sichern um</span>
              <input type="time" aria-label="Uhrzeit (UTC)" value={form.schedule_time} onChange={(e) => set("schedule_time", e.target.value)}
                style={{ ...input, width: 110 }} disabled={!form.schedule} />
              <span style={{ fontSize: 12, color: "var(--text-faint)" }}>UTC</span>
            </div>
            <div style={{ display: "flex", alignItems: "flex-start", gap: 10 }}>
              <Toggle checked={form.media} onChange={(v) => set("media", v)} />
              <div style={{ fontSize: 12.5, color: "var(--text-dim)", lineHeight: 1.6 }}>
                <strong style={{ color: "var(--text)" }}>Hochgeladene Dateien einschließen</strong>
                {sizes?.media_available
                  ? <> — <span style={mono}>{mb(sizes.media_bytes)} MB</span></>
                  : <> — {sizes?.media_note ? "Größe zurzeit nicht verfügbar" : "Größe wird ermittelt…"}</>}
                <div style={{ color: "var(--text-faint)", fontSize: 11.5 }}>Das Backup vor einem Upgrade lässt sie immer weg.</div>
              </div>
            </div>
          </div>

          <div style={{ display: "flex", alignItems: "center", gap: 10, flexWrap: "wrap", borderTop: "1px solid var(--border-soft)", paddingTop: 14 }}>
            <Button variant="soft" size="sm" icon="upload" disabled={running} onClick={() => void runNow()}>
              {running ? <><Spinner size={13} /> Läuft…</> : "Jetzt sichern"}
            </Button>
            <span style={{ fontSize: 11.5, color: "var(--text-faint)" }}>Nutzt das gespeicherte Ziel; Zeitplan und Medien-Schalter erst nach „Speichern“.</span>
          </div>
          {runMsg && <div style={{ fontSize: 12.5, color: runMsg.ok ? "var(--status-ok)" : "var(--status-err)" }}>{runMsg.text}</div>}
        </>
      )}

      {status && !status.configured && status.missing && status.missing.length > 0 && (
        <div style={{ fontSize: 12.5, color: "var(--status-warn)" }}>Es fehlt noch: {status.missing.join(", ")}.</div>
      )}
      {status && (
        <div style={{ display: "flex", flexDirection: "column", gap: 6, borderTop: "1px solid var(--border-soft)", paddingTop: 12 }}>
          {status.schedule_enabled && status.next_run && (
            <div style={{ fontSize: 12.5, color: "var(--text-dim)" }}>Nächster Lauf: {new Date(status.next_run).toLocaleString("de-DE")}</div>
          )}
          {status.last_ok && <div style={{ fontSize: 12.5, color: "var(--text-dim)" }}>Zuletzt erfolgreich: {new Date(status.last_ok.started_at).toLocaleString("de-DE")}</div>}
          {(status.recent ?? []).slice(0, 5).map((r) => <RunLine key={r.started_at + r.occasion} r={r} />)}
          {!status.last && <div style={{ fontSize: 12.5, color: "var(--text-faint)" }}>Noch kein Lauf.</div>}
        </div>
      )}
    </Card>
  );
}

/** The backups in the bucket, with download and restore. onRestore must start the restore
 *  and reject with the server's message if it is refused (a wrong key answers at once). */
export function OffsiteObjects({ onRestore, restoring }: { onRestore: (name: string, key: string) => Promise<void>; restoring?: boolean }) {
  const q = useQuery({ queryKey: ["offsite", "objects"], queryFn: () => api.get<{ objects: BucketObject[] }>(`${BASE}/objects`) });
  const [target, setTarget] = useState<BucketObject | null>(null);
  const [key, setKey] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [dl, setDl] = useState<string | null>(null);
  const [dlErr, setDlErr] = useState<string | null>(null);
  const [started, setStarted] = useState<string | null>(null);

  const close = () => { setTarget(null); setKey(""); setErr(null); };

  const confirm = async () => {
    if (!target) return;
    setBusy(true); setErr(null);
    try {
      await onRestore(target.name, key.trim());
      setStarted(target.name);
      close();
    } catch (e) {
      setErr(errMsg(e, "Wiederherstellung fehlgeschlagen."));
    } finally {
      setBusy(false);
    }
  };

  const download = async (o: BucketObject) => {
    setDl(o.name); setDlErr(null);
    try {
      const res = await fetch(`${BASE}/objects/download?name=${encodeURIComponent(o.name)}`, {
        headers: { Authorization: `Bearer ${localStorage.getItem("matrixctrl_token") ?? ""}` },
      });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const url = URL.createObjectURL(await res.blob());
      const a = document.createElement("a");
      a.href = url; a.download = o.name; a.click();
      URL.revokeObjectURL(url);
    } catch (e) {
      setDlErr(errMsg(e, "Download fehlgeschlagen."));
    } finally {
      setDl(null);
    }
  };

  const objects = q.data?.objects ?? [];
  return (
    <Card style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 10 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
          <Icon name="server" size={17} />
          <h2 style={{ margin: 0, fontSize: 15, fontWeight: 600 }}>Backups im Bucket</h2>
        </div>
        <Button variant="ghost" size="sm" icon="refresh" disabled={q.isFetching} onClick={() => void q.refetch()}>Aktualisieren</Button>
      </div>
      {q.isLoading && <div style={{ fontSize: 12.5, color: "var(--text-faint)" }}><Spinner size={13} /> Lade…</div>}
      {q.isError && <div style={{ fontSize: 12.5, color: "var(--status-err)" }}>{errMsg(q.error, "Die Liste konnte nicht geladen werden.")}</div>}
      {q.data && objects.length === 0 && <div style={{ fontSize: 12.5, color: "var(--text-faint)" }}>Noch keine Backups im Bucket.</div>}
      {objects.map((o) => (
        <div key={o.key} style={{ display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap", padding: "9px 0", borderTop: "1px solid var(--border-soft)" }}>
          <div style={{ flex: "1 1 240px", minWidth: 0 }}>
            <div style={{ fontSize: 13, color: "var(--text)" }}>{new Date(o.time).toLocaleString("de-DE")}</div>
            <div style={{ fontSize: 11.5, color: "var(--text-faint)" }}>{o.occasion} · <span style={mono}>{size(o.size)}</span></div>
          </div>
          <Button variant="ghost" size="sm" icon="download" disabled={dl !== null} onClick={() => void download(o)}>
            {dl === o.name ? "Lädt…" : "Herunterladen"}
          </Button>
          <Button variant="soft" size="sm" icon="rotate" disabled={!!restoring} onClick={() => setTarget(o)}>Wiederherstellen</Button>
        </div>
      ))}
      {dlErr && <div style={{ fontSize: 12.5, color: "var(--status-err)" }}>Download fehlgeschlagen: {dlErr}</div>}
      {started && <div style={{ fontSize: 12.5, color: "var(--status-ok)" }}>Wiederherstellung gestartet — der Fortschritt steht weiter unten.</div>}

      <ConfirmDialog open={!!target} title="Aus diesem Backup wiederherstellen?" confirmLabel="Wiederherstellen" confirmIcon="rotate"
        confirmDisabled={key.trim() === ""} busy={busy} error={err} onConfirm={() => void confirm()} onCancel={close}>
        <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          <span>
            Backup vom {target ? new Date(target.time).toLocaleString("de-DE") : ""}. Der Server ist währenddessen nicht erreichbar;
            die bestehenden Datenbanken werden beiseitegelegt, nicht überschrieben.
          </span>
          <Field label="Privater Schlüssel" hint="Wird nur für diese Anfrage benutzt und nirgends gespeichert.">
            <input style={{ ...input, ...mono }} type="password" value={key} onChange={(e) => setKey(e.target.value)}
              placeholder="AGE-SECRET-KEY-1…" autoComplete="off" spellCheck={false} />
          </Field>
        </div>
      </ConfirmDialog>
    </Card>
  );
}
