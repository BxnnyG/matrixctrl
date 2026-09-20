import { useRef, useState, type DragEvent } from "react";
import { Icon, Spinner } from "@/components/mc";

const mb = (n: number) => (n / 1024 / 1024).toFixed(1);

/** Choosing an archive, with the two things the bare input never had.
 *
 *  It was `<input type="file">` and nothing else. Dropping a file onto the page did
 *  nothing, because a plain input only accepts a drop onto the input itself — and
 *  after picking one, the page showed no sign that a 85 MB upload had begun. Reported
 *  exactly that way: "drag and drop geht nicht … dann ist es da aber dann nichts mehr
 *  kein lade balken skeleton loading oder sonstiges".
 *
 *  Shared, because two screens take an archive: restoring onto this server, and
 *  rebuilding a server from one. */
export function ArchivePicker({ file, busy, sent, error, onPick }: {
  file: File | null;
  /** True while the archive is being sent and read. */
  busy?: boolean;
  /** Bytes gone out so far. Counted rather than turned into a percentage only when
   *  the total is known — here it is, so a real bar is honest. */
  sent?: number;
  error?: string | null;
  onPick: (f: File | null) => void;
}) {
  const input = useRef<HTMLInputElement>(null);
  const [over, setOver] = useState(false);

  const drop = (e: DragEvent) => {
    e.preventDefault();
    setOver(false);
    const f = e.dataTransfer.files?.[0];
    if (f) onPick(f);
  };

  const pct = file && sent != null && file.size > 0 ? Math.min(100, (sent / file.size) * 100) : null;

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
      <div
        onDragOver={(e) => { e.preventDefault(); setOver(true); }}
        onDragLeave={() => setOver(false)}
        onDrop={drop}
        onClick={() => !busy && input.current?.click()}
        role="button"
        tabIndex={0}
        onKeyDown={(e) => { if ((e.key === "Enter" || e.key === " ") && !busy) input.current?.click(); }}
        style={{
          display: "flex", alignItems: "center", gap: 12, padding: "16px 18px",
          border: `1.5px dashed ${over ? "var(--accent)" : "var(--border)"}`,
          borderRadius: "var(--radius-sm)",
          background: over ? "var(--accent-soft)" : "var(--surface-2)",
          cursor: busy ? "default" : "pointer",
        }}
      >
        <Icon name={busy ? "clock" : "upload"} size={18} style={{ color: over ? "var(--accent)" : "var(--text-faint)", flexShrink: 0 }} />
        <div style={{ minWidth: 0, flex: 1 }}>
          {file ? (
            <>
              <div style={{ fontSize: 13, color: "var(--text)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{file.name}</div>
              <div style={{ fontSize: 11.5, color: "var(--text-faint)", marginTop: 2 }}>
                {busy
                  ? <>Wird hochgeladen… {mb(sent ?? 0)} / {mb(file.size)} MB</>
                  : <>{mb(file.size)} MB — zum Ersetzen hier ablegen oder klicken</>}
              </div>
            </>
          ) : (
            <>
              <div style={{ fontSize: 13, color: "var(--text)" }}>Archiv hier ablegen oder klicken</div>
              <div style={{ fontSize: 11.5, color: "var(--text-faint)", marginTop: 2 }}>.tar.gz aus „Vollständiges Archiv"</div>
            </>
          )}
        </div>
        {busy && <Spinner size={15} />}
      </div>

      {/* A real bar, because the total is known. The download counter next door shows
          bytes without one precisely because there the total is not (§4.73). */}
      {busy && pct != null && (
        <div style={{ height: 4, borderRadius: 999, background: "var(--surface-2)", overflow: "hidden" }}>
          <div style={{ width: `${pct}%`, height: "100%", background: "var(--accent)", transition: "width .2s linear" }} />
        </div>
      )}
      {busy && pct == null && (
        <span style={{ fontSize: 11.5, color: "var(--text-faint)" }}>
          <Spinner size={11} /> Archiv wird gelesen…
        </span>
      )}

      <input ref={input} type="file" accept=".gz,.tgz,application/gzip" style={{ display: "none" }}
        onChange={(e) => onPick(e.target.files?.[0] ?? null)} />

      {error && <span style={{ fontSize: 12.5, color: "var(--status-err)" }}>{error}</span>}
    </div>
  );
}
