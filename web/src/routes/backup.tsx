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
  const [busy, setBusy] = useState<string | null>(null);
  const [dlErr, setDlErr] = useState<string | null>(null);

  // One helper for all three downloads. There were two near-identical copies before,
  // which is how the second one ended up with a different error message than the first.
  const grab = async (path: string, fallback: string, label: string) => {
    setBusy(label); setDlErr(null);
    try {
      const res = await fetch(path, {
        headers: { Authorization: `Bearer ${localStorage.getItem("matrixctrl_token") ?? ""}` },
      });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const blob = await res.blob();
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
            onClick={() => void grab("/api/v1/status/backup/full", "matrixctrl-full.tar.gz", "full")}>
            {busy === "full" ? "Wird erstellt…" : "Herunterladen"}
          </Button>
        </div>

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
