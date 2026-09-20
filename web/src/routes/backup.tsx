import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Card, Icon, Button } from "@/components/mc";
import { api } from "@/lib/api";
import { essVersion, type ArchiveManifest } from "@/lib/archive";
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
    queryFn: () => api.get<{ media_bytes: number; media_available: boolean; media_note?: string }>("/api/v1/status/backup/sizes"),
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

  const doRestore = async () => {
    if (!archive) return;
    setRestoring(true); setRestoreErr(null);
    try {
      const r = await api.upload<{ config_files: number; tables?: string[] }>("/api/v1/status/restore", archive);
      setRestoreMsg(`${r.config_files} Konfigurationsdateien und ${r.tables?.length ?? 0} Tabellen wiederhergestellt. MatrixCtrl sollte jetzt neu gestartet werden.`);
      setPreview(null); setArchive(null);
    } catch (e) {
      setRestoreErr(e instanceof Error ? e.message : "Wiederherstellung fehlgeschlagen");
    } finally {
      setRestoring(false);
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
                : <> — {sizes?.media_note ? "zurzeit nicht lesbar" : "Größe wird ermittelt…"}</>}
              . Sie werden aus dem Synapse-Pod gestreamt.
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
          that they put a 26.8.0 configuration onto a different cluster. */}
      <Card style={{ display: "flex", flexDirection: "column", gap: 12 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
          <Icon name="upload" size={17} />
          <h2 style={{ margin: 0, fontSize: 15, fontWeight: 600 }}>Wiederherstellen</h2>
        </div>

        <div style={{ fontSize: 12.5, color: "var(--text-dim)", lineHeight: 1.65 }}>
          Spielt Konfiguration und MatrixCtrl-Datenbank aus einem Archiv zurück — inklusive
          Hostnames, serverName, TLS-Issuer und RTC-Einstellungen, also derselbe Server.
          <strong style={{ color: "var(--text)" }}> Konten, Räume und Nachrichten kommen nicht zurück</strong>,
          die liegen in Synapses eigener Datenbank.
        </div>

        <ArchivePicker file={archive} busy={reading} sent={sent} error={restoreErr}
          onPick={(f) => void pick(f)} />

        {preview && (
          <div style={{ fontSize: 12.5, color: "var(--text-dim)", background: "var(--surface-2)", borderRadius: "var(--radius-sm)", padding: "10px 12px", lineHeight: 1.7 }}>
            <div><strong style={{ color: "var(--text)" }}>Archiv vom {new Date(preview.created_at).toLocaleString("de-DE")}</strong></div>
            <div>MatrixCtrl {preview.app_version}
              {preview.ess?.chart ? ` · ESS ${essVersion(preview.ess.chart)} (Revision ${preview.ess.revision})` : " · ESS-Version nicht im Archiv vermerkt"}</div>
            <div>{preview.config_repo_files} Konfigurationsdateien, {preview.tables?.length ?? 0} Tabellen</div>
          </div>
        )}

        {restoreMsg && <div style={{ fontSize: 12.5, color: "var(--status-ok)" }}>{restoreMsg}</div>}

        {preview && (
          <div style={{ display: "flex", alignItems: "center", gap: 10, flexWrap: "wrap" }}>
            <Button variant="primary" icon="upload" onClick={() => void doRestore()} disabled={restoring}>
              {restoring ? "Wird eingespielt…" : "Jetzt wiederherstellen"}
            </Button>
            <span style={{ fontSize: 12, color: "var(--status-warn)" }}>
              Überschreibt die aktuelle Konfiguration und Datenbank.
            </span>
          </div>
        )}
      </Card>

    </div>
  );
}
