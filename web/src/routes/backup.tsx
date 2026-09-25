import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Card, Icon, Button } from "@/components/mc";
import { api } from "@/lib/api";
import { essVersion, PART_LABELS, type ArchiveManifest, type RestoreProgress } from "@/lib/archive";
import { ArchivePicker } from "@/components/ArchivePicker";

export const Route = createFileRoute("/backup")({ component: BackupPage });



/** Backup and restore.
 *
 *  Its own page rather than a card on /system (etappe 70). The navigation already had a
 *  "Backup" entry, greyed out because it had no route — so the feature shipped in E68
 *  and E69 was invisible to anyone who looked where the label said it would be. A
 *  disabled item next to a working feature is worse than no item at all. */
function BackupPage() {
  // fetch + blob rather than a plain link. A navigation cannot carry an Authorization
  // header, and the alternatives were both worse: putting the session token in the URL
  // is exactly the leak E35 removed, and widening the single-use WebSocket ticket to
  // cover ordinary downloads would loosen a mechanism built narrow on purpose.
  //
  // The cost is that the browser holds the archive in memory. That is a property of
  // authenticated downloads, not a design choice — the server still streams it.
  const [busy, setBusy] = useState<string | null>(null);
  const [got, setGot] = useState(0);
  const [dlErr, setDlErr] = useState<string | null>(null);
  const mb = (n: number) => (n / 1024 / 1024).toFixed(1);

  // One helper for all three downloads. There were two near-identical copies before,
  // which is how the second one ended up with a different error message than the first.
  const grab = async (path: string, fallback: string, label: string) => {
    setBusy(label); setDlErr(null); setGot(0);
    try {
      const res = await fetch(path, {
        headers: { Authorization: `Bearer ${localStorage.getItem("matrixctrl_token") ?? ""}` },
      });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);

      // Counted as it arrives rather than shown as a percentage. The archive is
      // streamed, so there is no Content-Length to divide by — and a progress bar with
      // an invented denominator is the thing §4.41 exists to forbid. Bytes are a number
      // that is actually known (etappe 73).
      const reader = res.body?.getReader();
      let blob: Blob;
      if (reader) {
        const chunks: BlobPart[] = [];
        let total = 0;
        for (;;) {
          const { done, value } = await reader.read();
          if (done) break;
          chunks.push(value);
          total += value.length;
          setGot(total);
        }
        blob = new Blob(chunks);
      } else {
        blob = await res.blob();
      }
      // The key arrives in a header, ahead of the body, because the body is a stream
      // and the operator has to see it before they close the tab. It is generated per
      // archive and stored nowhere — see internal/backup/seal.go.
      const key = res.headers.get("X-MatrixCtrl-Recovery-Key");
      if (key) setRecoveryKey(key);

      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = res.headers.get("Content-Disposition")?.match(/filename="([^"]+)"/)?.[1] ?? fallback;
      a.click();
      URL.revokeObjectURL(url);
    } catch (e) {
      setDlErr(e instanceof Error ? e.message : "unbekannter Fehler");
    } finally {
      setBusy(null);
    }
  };

  const [withMedia, setWithMedia] = useState(false);
  const [withSecrets, setWithSecrets] = useState(true);
  const [recoveryKey, setRecoveryKey] = useState<string | null>(null);
  const [acknowledged, setAcknowledged] = useState(false);

  // What the media option costs, asked once when the page opens rather than guessed at.
  const { data: sizes } = useQuery({
    queryKey: ["backup", "sizes"],
    queryFn: () => api.get<{ media_bytes: number; media_available: boolean; media_note?: string; media_reason?: string }>("/api/v1/status/backup/sizes"),
    staleTime: 5 * 60_000,
  });

  const [preview, setPreview] = useState<ArchiveManifest | null>(null);
  const [archive, setArchive] = useState<File | null>(null);
  const [restoring, setRestoring] = useState(false);
  const [restoreMsg, setRestoreMsg] = useState<string | null>(null);
  const [restoreErr, setRestoreErr] = useState<string | null>(null);

  const [reading, setReading] = useState(false);
  const [sent, setSent] = useState(0);

  const pick = async (f: File | null) => {
    setArchive(f); setPreview(null); setRestoreErr(null); setRestoreMsg(null); setSent(0);
    if (!f) return;
    setReading(true);
    try {
      setPreview(await api.upload<ArchiveManifest>("/api/v1/status/restore/preview", f, setSent));
    } catch (e) {
      setRestoreErr(e instanceof Error ? e.message : "Archiv unlesbar");
    } finally {
      setReading(false);
    }
  };

  // What to put back. Both default to on: an archive that carries them was made by
  // somebody who wanted them, and the point of the whole etappe is that "restore" means
  // the server comes back rather than its settings.
  const [putBackMedia, setPutBackMedia] = useState(true);
  const [putBackKeys, setPutBackKeys] = useState(true);
  const [restoreKey, setRestoreKey] = useState("");
  const [progress, setProgress] = useState<RestoreProgress | null>(null);
  const logEnd = useRef<HTMLDivElement>(null);

  // Polled, not streamed. A restore stops Synapse and waits for it to build a schema —
  // minutes, while the proxy in front of this panel gives up after about a hundred
  // seconds. So the upload starts the work and this asks how far it has got, which also
  // means closing the tab does not stop anything (etappe 106).
  useEffect(() => {
    if (!restoring) return;
    let stop = false;
    const tick = async () => {
      try {
        const p = await api.get<RestoreProgress>("/api/v1/status/restore/progress");
        if (stop) return;
        setProgress(p);
        if (p.done) {
          setRestoring(false);
          if (p.failed) setRestoreErr(p.failed);
          else setRestoreMsg("Wiederhergestellt. Die Dienste laufen mit den zurückgespielten Daten.");
          return;
        }
      } catch {
        // A failed poll is not a failed restore: the panel itself is being restored and
        // may restart underneath this request. Keep asking.
      }
      if (!stop) window.setTimeout(() => void tick(), 1500);
    };
    void tick();
    return () => { stop = true; };
  }, [restoring]);

  useEffect(() => { logEnd.current?.scrollIntoView({ block: "nearest" }); }, [progress?.steps?.length]);

  const needsKey = putBackKeys && (preview?.parts?.includes("secrets/") ?? false);

  const doRestore = async () => {
    if (!archive) return;
    setRestoring(true); setRestoreErr(null); setRestoreMsg(null); setProgress(null); setSent(0);
    try {
      const q = `?media=${putBackMedia}&keys=${putBackKeys}`;
      await api.upload<{ started: boolean }>("/api/v1/status/restore/full" + q, archive, setSent,
        needsKey ? { "X-MatrixCtrl-Recovery-Key": restoreKey } : undefined);
    } catch (e) {
      setRestoring(false);
      setRestoreErr(e instanceof Error ? e.message : "Wiederherstellung fehlgeschlagen");
    }
  };

  return (
    <div className="mc-page">
      {/* One archive, one button. This page used to show two downloads and three
          warning blocks explaining what each half could not do — every sentence true,
          and the arrangement still showed the order the features were built in rather
          than the operator's task (etappe 72). */}
      <Card style={{ display: "flex", flexDirection: "column", gap: 14 }}>
        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12, flexWrap: "wrap" }}>
          <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
            <Icon name="download" size={17} />
            <h2 style={{ margin: 0, fontSize: 15, fontWeight: 600 }}>Vollständiges Backup</h2>
          </div>
          <Button variant="primary" icon="download" disabled={!!busy}
            onClick={() => void grab(
              `/api/v1/status/backup/full?media=${withMedia ? 1 : 0}&secrets=${withSecrets ? 1 : 0}`,
              "matrixctrl-full.tar.gz", "full")}>
            {busy === "full" ? (got ? `${mb(got)} MB…` : "Wird erstellt…") : "Herunterladen"}
          </Button>
        </div>

        {/* Two choices, both with their consequence written out. The keys are on by
            default because without them a restore keeps no sessions and cannot decrypt
            the accounts; the media are off because they are 39 MB here and hundreds of
            gigabytes elsewhere (etappe 102). */}
        <div style={{ display: "flex", flexDirection: "column", gap: 9, padding: "12px 14px", background: "var(--surface-2)", borderRadius: "var(--radius-sm)" }}>
          <label style={{ display: "flex", alignItems: "flex-start", gap: 9, fontSize: 12.5, color: "var(--text-dim)", cursor: "pointer" }}>
            <input type="checkbox" checked={withSecrets} onChange={(e) => setWithSecrets(e.target.checked)} style={{ marginTop: 2 }} />
            <span>
              <strong style={{ color: "var(--text)" }}>Schlüssel des Homeservers einschließen</strong> — verschlüsselt.
              Ohne sie ist nach dem Zurückspielen jede Sitzung ungültig und die Konten-Datenbank
              nicht entschlüsselbar. Du bekommst beim Herunterladen einen Wiederherstellungsschlüssel,
              der <em>nirgends gespeichert</em> wird.
            </span>
          </label>
          <label style={{ display: "flex", alignItems: "flex-start", gap: 9, fontSize: 12.5, color: "var(--text-dim)", cursor: "pointer" }}>
            <input type="checkbox" checked={withMedia} disabled={sizes?.media_available === false}
              onChange={(e) => setWithMedia(e.target.checked)} style={{ marginTop: 2 }} />
            <span>
              <strong style={{ color: "var(--text)" }}>Hochgeladene Dateien einschließen</strong>
              {sizes?.media_available
                ? <> — <span style={{ fontFamily: "var(--mono)" }}>{mb(sizes.media_bytes)} MB</span></>
                : <> — {sizes?.media_note ? "zurzeit nicht verfügbar" : "Größe wird ermittelt…"}</>}
              . Sie werden aus dem Synapse-Pod gestreamt.
              {sizes?.media_reason === "rbac" && (
                <> <strong style={{ color: "var(--text)" }}>MatrixCtrl fehlt die Berechtigung dafür</strong>{" "}
                  (<span style={{ fontFamily: "var(--mono)" }}>pods/exec</span> im ESS-Namespace).
                  Sie kommt mit dem nächsten Update mit — das Kästchen bleibt bis dahin gesperrt,
                  der Rest des Archivs ist davon nicht betroffen.</>
              )}
            </span>
          </label>
        </div>

        {recoveryKey && (
          <div style={{ display: "flex", flexDirection: "column", gap: 9, padding: "14px 16px", border: "1px solid var(--status-warn)", borderRadius: "var(--radius-sm)", background: "color-mix(in oklch, var(--status-warn) 8%, transparent)" }}>
            <strong style={{ fontSize: 13.5, color: "var(--text)" }}>Wiederherstellungsschlüssel — jetzt sichern</strong>
            <p style={{ margin: 0, fontSize: 12.5, color: "var(--text-dim)", maxWidth: "62ch" }}>
              Er wird <strong>einmal</strong> angezeigt und nirgends gespeichert — weder im Archiv
              noch auf diesem Server. Ohne ihn kommen Konten, Räume, Nachrichten und Dateien
              trotzdem zurück; nur die Sitzungen brechen, weil die Schlüssel verschlüsselt bleiben.
            </p>
            <code style={{ fontFamily: "var(--mono)", fontSize: 14, letterSpacing: "0.04em", color: "var(--text)", background: "var(--bg)", border: "1px solid var(--border)", borderRadius: "var(--radius-sm)", padding: "10px 12px", userSelect: "all", wordBreak: "break-all" }}>
              {recoveryKey}
            </code>
            <label style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 12.5, color: "var(--text-dim)", cursor: "pointer" }}>
              <input type="checkbox" checked={acknowledged} onChange={(e) => setAcknowledged(e.target.checked)} />
              Ich habe ihn gesichert
            </label>
            {acknowledged && (
              <Button size="sm" variant="ghost" onClick={() => { setRecoveryKey(null); setAcknowledged(false); }}>
                Ausblenden
              </Button>
            )}
          </div>
        )}

        <div style={{ fontSize: 12.5, color: "var(--text-dim)", lineHeight: 1.7 }}>
          Ein Archiv mit allem, was von hier aus erreichbar ist: die <strong style={{ color: "var(--text)" }}>vollständige
          ESS-Konfiguration mit Git-Historie</strong> (Hostnames, serverName, TLS-Issuer, RTC),
          die <strong style={{ color: "var(--text)" }}>MatrixCtrl-Datenbank</strong> mit Hooks und Verläufen,
          und <strong style={{ color: "var(--text)" }}>Synapses Datenbank</strong> mit Konten, Räumen und Nachrichten.
          Alle Tabellen aus jeweils einem Moment.
        </div>

        <div style={{ fontSize: 12, color: "var(--text-faint)", lineHeight: 1.6 }}>
          Nicht enthalten sind die hochgeladenen Dateien — die liegen auf einem Volume,
          das dieser Pod nicht einbindet.
        </div>

        {dlErr && <div style={{ fontSize: 12.5, color: "var(--status-err)" }}>Download fehlgeschlagen: {dlErr}</div>}

        {/* Kept because the sizes differ by two orders of magnitude: moving only the
            configuration should not mean moving 300 MB. */}
        <div style={{ display: "flex", alignItems: "center", gap: 14, flexWrap: "wrap", paddingTop: 4, borderTop: "1px solid var(--border-soft)" }}>
          <span style={{ fontSize: 11.5, color: "var(--text-faint)" }}>Einzeln:</span>
          <Button variant="ghost" size="sm" icon="sliders" disabled={!!busy}
            onClick={() => void grab("/api/v1/status/backup", "matrixctrl-backup.tar.gz", "config")}>
            {busy === "config" ? "…" : "Nur Konfiguration"}
          </Button>
          <Button variant="ghost" size="sm" icon="database" disabled={!!busy}
            onClick={() => void grab("/api/v1/status/backup/homeserver", "synapse-db.tar.gz", "hs")}>
            {busy === "hs" ? "…" : "Nur Homeserver-Datenbank"}
          </Button>
        </div>
      </Card>

      {/* Restore. Two steps, and the preview exists so nobody discovers after the fact
          that they put a 26.8.0 configuration onto a different cluster. Since etappe 106
          it puts back every part the archive holds, not only MatrixCtrl's own. */}
      <Card style={{ display: "flex", flexDirection: "column", gap: 12 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
          <Icon name="upload" size={17} />
          <h2 style={{ margin: 0, fontSize: 15, fontWeight: 600 }}>Wiederherstellen</h2>
        </div>

        <div style={{ fontSize: 12.5, color: "var(--text-dim)", lineHeight: 1.65 }}>
          Spielt zurück, was im Archiv steht: Konfiguration, Konten, Räume, Nachrichten,
          hochgeladene Dateien und die Schlüssel des Servers. Die bestehenden Datenbanken
          werden dabei <strong style={{ color: "var(--text)" }}>nicht überschrieben, sondern beiseitegelegt</strong> —
          schlägt etwas fehl, steht der vorherige Stand wieder.
        </div>

        <ArchivePicker file={archive} busy={reading} sent={sent} error={restoreErr}
          onPick={(f) => void pick(f)} />

        {preview && (
          <div style={{ fontSize: 12.5, color: "var(--text-dim)", background: "var(--surface-2)", borderRadius: "var(--radius-sm)", padding: "10px 12px", lineHeight: 1.7 }}>
            <div><strong style={{ color: "var(--text)" }}>Archiv vom {new Date(preview.created_at).toLocaleString("de-DE")}</strong></div>
            <div>MatrixCtrl {preview.app_version}
              {preview.ess?.chart ? ` · ESS ${essVersion(preview.ess.chart)} (Revision ${preview.ess.revision})` : " · ESS-Version nicht im Archiv vermerkt"}
              {preview.server_name ? ` · ${preview.server_name}` : ""}</div>
            <div style={{ marginTop: 6 }}>
              {(preview.parts ?? ["matrixctrl/"]).map((p) => (
                <div key={p}>· {PART_LABELS[p] ?? p}
                  {p === "homeserver/" || p === "mas/"
                    ? (() => {
                        const row = preview.part_rows?.find((r) => r.name === p.replace("/", ""));
                        return row ? ` — ${row.tables} Tabellen, ${row.rows.toLocaleString("de-DE")} Zeilen` : "";
                      })()
                    : ""}
                </div>
              ))}
            </div>
            {preview.counters === false && (
              <div style={{ marginTop: 8, color: "var(--status-warn)" }}>
                Dieses Archiv stammt aus einer älteren MatrixCtrl-Version und enthält die
                Zähler der Datenbank nicht. Räume, Nachrichten und Konten kommen zurück —
                der Server vergibt danach aber Nummern neu, die er schon vergeben hat.
                Ein neues Archiv dieser Installation hat sie.
              </div>
            )}
          </div>
        )}

        {preview && (
          <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
            {preview.parts?.includes("media/") && (
              <label style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 12.5 }}>
                <input type="checkbox" checked={putBackMedia} onChange={(e) => setPutBackMedia(e.target.checked)} />
                Hochgeladene Dateien mit zurückspielen
              </label>
            )}
            {preview.parts?.includes("secrets/") && (
              <>
                <label style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 12.5 }}>
                  <input type="checkbox" checked={putBackKeys} onChange={(e) => setPutBackKeys(e.target.checked)} />
                  Schlüssel des Homeservers zurückspielen — nur damit bleiben bestehende Anmeldungen gültig
                </label>
                {putBackKeys && (
                  <input
                    value={restoreKey}
                    onChange={(e) => setRestoreKey(e.target.value)}
                    placeholder="Wiederherstellungsschlüssel (beim Erstellen des Archivs einmal angezeigt)"
                    style={{
                      fontFamily: "var(--font-mono)", fontSize: 12.5, padding: "8px 10px",
                      background: "var(--surface-2)", color: "var(--text)",
                      border: "1px solid var(--border)", borderRadius: "var(--radius-sm)",
                    }}
                  />
                )}
              </>
            )}
          </div>
        )}

        {/* The live log. A restore that says nothing for four minutes is one an operator
            interrupts, and interrupting this one is the worst moment to do it. */}
        {progress?.steps && progress.steps.length > 0 && (
          <div style={{
            maxHeight: 220, overflowY: "auto", fontFamily: "var(--font-mono)", fontSize: 11.5,
            background: "var(--surface-2)", borderRadius: "var(--radius-sm)", padding: "10px 12px",
            lineHeight: 1.7, color: "var(--text-dim)",
          }}>
            {progress.steps.map((st, i) => (
              <div key={i}>
                <span style={{ color: "var(--text-faint)" }}>{new Date(st.at).toLocaleTimeString("de-DE")} </span>
                <span style={{ color: st.step === "rollback" || st.step === "warn" ? "var(--status-warn)" : "var(--text)" }}>{st.step}</span>
                {" "}{st.detail}
              </div>
            ))}
            <div ref={logEnd} />
          </div>
        )}

        {progress?.summary?.databases?.map((d) => (
          <div key={d.database} style={{ fontSize: 12.5, color: "var(--text-dim)" }}>
            {d.database}: {d.tables} Tabellen, {d.total_rows.toLocaleString("de-DE")} Zeilen, {d.sequences} Zähler
            {d.previous_name ? ` — der vorherige Stand liegt als ${d.previous_name} daneben` : ""}
          </div>
        ))}

        {restoreMsg && <div style={{ fontSize: 12.5, color: "var(--status-ok)" }}>{restoreMsg}</div>}

        {preview && (
          <div style={{ display: "flex", alignItems: "center", gap: 10, flexWrap: "wrap" }}>
            <Button variant="primary" icon="upload" onClick={() => void doRestore()}
              disabled={restoring || (needsKey && restoreKey.trim() === "")}>
              {restoring ? "Wird eingespielt…" : "Jetzt wiederherstellen"}
            </Button>
            <span style={{ fontSize: 12, color: "var(--status-warn)" }}>
              Der Server ist währenddessen nicht erreichbar.
            </span>
          </div>
        )}
      </Card>

    </div>
  );
}
