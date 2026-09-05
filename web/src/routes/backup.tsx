import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { Card, Icon, Button } from "@/components/mc";

export const Route = createFileRoute("/backup")({ component: BackupPage });

interface BackupManifest {
  created_at: string;
  app_version: string;
  ess?: { name?: string; chart?: string; revision?: number };
  config_repo_files: number;
  tables?: { name: string; rows: number; regenerable: boolean }[];
}


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
  const [downloading, setDownloading] = useState(false);
  const [downloadErr, setDownloadErr] = useState<string | null>(null);

  // Restore is two steps on purpose: read the archive, show what it holds and which ESS
  // release it came from, and only then write (etappe 69).
  const [preview, setPreview] = useState<BackupManifest | null>(null);
  const [archive, setArchive] = useState<File | null>(null);
  const [restoring, setRestoring] = useState(false);
  const [restoreMsg, setRestoreMsg] = useState<string | null>(null);
  const [restoreErr, setRestoreErr] = useState<string | null>(null);

  const post = async (path: string, body: BodyInit) => {
    const res = await fetch(path, {
      method: "POST",
      headers: { Authorization: `Bearer ${localStorage.getItem("matrixctrl_token") ?? ""}` },
      body,
    });
    const text = await res.text();
    if (!res.ok) throw new Error(JSON.parse(text || "{}").error ?? `HTTP ${res.status}`);
    return text ? JSON.parse(text) : {};
  };

  const pick = async (f: File | null) => {
    setArchive(f); setPreview(null); setRestoreErr(null); setRestoreMsg(null);
    if (!f) return;
    try {
      setPreview(await post("/api/v1/status/restore/preview", f));
    } catch (e) {
      setRestoreErr(e instanceof Error ? e.message : "Archiv unlesbar");
    }
  };

  const [hsDownloading, setHsDownloading] = useState(false);
  const [hsErr, setHsErr] = useState<string | null>(null);
  const downloadHomeserver = async () => {
    setHsDownloading(true); setHsErr(null);
    try {
      const res = await fetch("/api/v1/status/backup/homeserver", {
        headers: { Authorization: `Bearer ${localStorage.getItem("matrixctrl_token") ?? ""}` },
      });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = res.headers.get("Content-Disposition")?.match(/filename="([^"]+)"/)?.[1] ?? "synapse-db.tar.gz";
      a.click();
      URL.revokeObjectURL(url);
    } catch (e) {
      setHsErr(e instanceof Error ? e.message : "unbekannter Fehler");
    } finally {
      setHsDownloading(false);
    }
  };

  const doRestore = async () => {
    if (!archive) return;
    setRestoring(true); setRestoreErr(null);
    try {
      const r = await post("/api/v1/status/restore", archive);
      setRestoreMsg(`${r.config_files} Konfigurationsdateien und ${r.tables?.length ?? 0} Tabellen wiederhergestellt. MatrixCtrl sollte jetzt neu gestartet werden.`);
      setPreview(null); setArchive(null);
    } catch (e) {
      setRestoreErr(e instanceof Error ? e.message : "Wiederherstellung fehlgeschlagen");
    } finally {
      setRestoring(false);
    }
  };
  const download = async () => {
    setDownloading(true);
    setDownloadErr(null);
    try {
      const res = await fetch("/api/v1/status/backup", {
        headers: { Authorization: `Bearer ${localStorage.getItem("matrixctrl_token") ?? ""}` },
      });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = (res.headers.get("Content-Disposition")?.match(/filename="([^"]+)"/)?.[1])
        ?? "matrixctrl-backup.tar.gz";
      a.click();
      URL.revokeObjectURL(url);
    } catch (e) {
      // A backup that failed must say so. A button that flickers and produces no file
      // is indistinguishable from one that worked, which is the worst outcome here.
      setDownloadErr(e instanceof Error ? e.message : "unbekannter Fehler");
    } finally {
      setDownloading(false);
    }
  };

  return (
    <div className="mc-page">
      {/* Backup. The card states what the archive does NOT hold, because an operator
          who believes they have a backup of their homeserver and discovers otherwise
          during a restore is the failure this whole feature exists to avoid. */}
      <Card style={{ display: "flex", flexDirection: "column", gap: 12 }}>
        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12, flexWrap: "wrap" }}>
          <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
            <Icon name="download" size={17} />
            <h2 style={{ margin: 0, fontSize: 15, fontWeight: 600 }}>Backup</h2>
          </div>
          <Button variant="primary" icon="download" onClick={() => void download()} disabled={downloading}>
            {downloading ? "Wird erstellt…" : "Herunterladen"}
          </Button>
        </div>
        <div style={{ fontSize: 12.5, color: "var(--text-dim)", lineHeight: 1.65 }}>
          <strong style={{ color: "var(--text)" }}>Enthalten:</strong> das Konfigurations-Repository
          mit vollständiger Git-Historie — also jeder ESS-Wert samt Änderungsverlauf — und die
          Datenbank von MatrixCtrl: Hooks, Upgrade-Verlauf, Melde-Entscheidungen, Node-Verlauf.
        </div>
        {downloadErr && (
          <div style={{ fontSize: 12.5, color: "var(--status-err)" }}>
            Das Backup konnte nicht erstellt werden: {downloadErr}
          </div>
        )}
        <div style={{ fontSize: 12.5, color: "var(--status-warn)", lineHeight: 1.65 }}>
          <strong>Nicht enthalten: der Homeserver selbst.</strong> Weder Synapses Datenbank
          (Konten, Räume, Nachrichten) noch die hochgeladenen Dateien. Beide liegen auf Volumes,
          die dieser Pod nicht einbindet. Dieses Archiv ersetzt kein Backup von Synapse.
        </div>
      </Card>

      {/* The homeserver's own data. A second card rather than a second file in the
          first archive: the two answer different questions, have different sizes, and
          only one of them can be restored by pressing something (etappe 70). */}
      <Card style={{ display: "flex", flexDirection: "column", gap: 12 }}>
        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12, flexWrap: "wrap" }}>
          <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
            <Icon name="database" size={17} />
            <h2 style={{ margin: 0, fontSize: 15, fontWeight: 600 }}>Homeserver-Datenbank</h2>
          </div>
          <Button variant="outline" icon="download" onClick={() => void downloadHomeserver()} disabled={hsDownloading}>
            {hsDownloading ? "Wird exportiert…" : "Exportieren"}
          </Button>
        </div>
        <div style={{ fontSize: 12.5, color: "var(--text-dim)", lineHeight: 1.65 }}>
          Synapses eigene Datenbank: <strong style={{ color: "var(--text)" }}>Konten, Räume und Nachrichten</strong>.
          Das ist der Teil, der aus einem wiederhergestellten Server denselben Server macht
          statt eines neuen mit denselben Hostnames. Alle Tabellen stammen aus einer einzigen
          Transaktion, also aus demselben Moment.
        </div>
        {hsErr && <div style={{ fontSize: 12.5, color: "var(--status-err)" }}>Export fehlgeschlagen: {hsErr}</div>}
        <div style={{ fontSize: 12.5, color: "var(--status-warn)", lineHeight: 1.65 }}>
          <strong>Nicht enthalten: die hochgeladenen Dateien.</strong> Die liegen auf einem
          Volume, das nur der Synapse-Pod einbindet. Und <strong>Zurückspielen ist bewusst kein
          Knopf</strong> — dafür muss Synapse gestoppt sein; im laufenden Betrieb beschädigt es,
          was da ist. Das Archiv wird bewusst mit psql eingespielt.
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

        <input type="file" accept=".gz,.tgz,application/gzip"
          onChange={(e) => void pick(e.target.files?.[0] ?? null)}
          style={{ fontSize: 12.5, color: "var(--text-dim)" }} />

        {preview && (
          <div style={{ fontSize: 12.5, color: "var(--text-dim)", background: "var(--surface-2)", borderRadius: "var(--radius-sm)", padding: "10px 12px", lineHeight: 1.7 }}>
            <div><strong style={{ color: "var(--text)" }}>Archiv vom {new Date(preview.created_at).toLocaleString("de-DE")}</strong></div>
            <div>MatrixCtrl {preview.app_version}
              {preview.ess?.chart ? ` · ESS ${preview.ess.chart} (Revision ${preview.ess.revision})` : " · ESS-Version nicht im Archiv vermerkt"}</div>
            <div>{preview.config_repo_files} Konfigurationsdateien, {preview.tables?.length ?? 0} Tabellen</div>
          </div>
        )}

        {restoreErr && <div style={{ fontSize: 12.5, color: "var(--status-err)" }}>{restoreErr}</div>}
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
