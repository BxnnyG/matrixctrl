import { useState } from "react";
import { Icon, Badge } from "@/components/mc";

interface DiffLine {
  type: "add" | "remove" | "context";
  content: string;
  oldNum?: number;
  newNum?: number;
}
interface DiffHunk {
  header: string;
  lines: DiffLine[];
}
interface DiffFile {
  fromFile: string;
  toFile: string;
  displayName: string;
  hunks: DiffHunk[];
}

export function parseDiff(raw: string): DiffFile[] {
  const files: DiffFile[] = [];
  let cur: DiffFile | null = null;
  let curHunk: DiffHunk | null = null;
  let oldLine = 0;
  let newLine = 0;

  for (const line of raw.split("\n")) {
    if (line.startsWith("--- ")) {
      if (cur) files.push(cur);
      const name = line.slice(4).replace(/^[ab]\//, "");
      cur = { fromFile: line.slice(4), toFile: "", displayName: name, hunks: [] };
      curHunk = null;
    } else if (line.startsWith("+++ ") && cur) {
      cur.toFile = line.slice(4);
      const name = cur.toFile.replace(/^[ab]\//, "");
      if (name !== "/dev/null") cur.displayName = name;
    } else if (line.startsWith("@@ ") && cur) {
      const m = line.match(/@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/);
      oldLine = m ? parseInt(m[1]) : 1;
      newLine = m ? parseInt(m[2]) : 1;
      curHunk = { header: line, lines: [] };
      cur.hunks.push(curHunk);
    } else if (curHunk) {
      if (line.startsWith("+")) {
        curHunk.lines.push({ type: "add", content: line.slice(1), newNum: newLine++ });
      } else if (line.startsWith("-")) {
        curHunk.lines.push({ type: "remove", content: line.slice(1), oldNum: oldLine++ });
      } else if (line.startsWith(" ") || line === "") {
        curHunk.lines.push({ type: "context", content: line.slice(1), oldNum: oldLine++, newNum: newLine++ });
      }
    }
  }
  if (cur) files.push(cur);
  return files;
}

function countChanges(f: DiffFile): { added: number; removed: number } {
  let added = 0, removed = 0;
  for (const h of f.hunks) {
    for (const l of h.lines) {
      if (l.type === "add") added++;
      else if (l.type === "remove") removed++;
    }
  }
  return { added, removed };
}

const emptyStyle: React.CSSProperties = { padding: "16px 18px", fontSize: 12.5, color: "var(--text-faint)", fontStyle: "italic" };

function FileBlock({ file, defaultOpen }: { file: DiffFile; defaultOpen: boolean }) {
  const [open, setOpen] = useState(defaultOpen);
  const { added, removed } = countChanges(file);
  const cellNum: React.CSSProperties = {
    width: 44, padding: "0 8px", textAlign: "right", color: "var(--text-faint)", userSelect: "none",
    borderRight: "1px solid var(--border-soft)", lineHeight: "20px", verticalAlign: "top",
  };

  return (
    <div style={{ border: "1px solid var(--border)", borderRadius: "var(--radius-sm)", overflow: "hidden", background: "var(--surface)" }}>
      <button onClick={() => setOpen((o) => !o)} className="mc-row"
        style={{ width: "100%", display: "flex", alignItems: "center", gap: 10, padding: "10px 13px", background: "var(--surface-2)", border: "none", cursor: "pointer", textAlign: "left" }}>
        <Icon name={open ? "chevDown" : "chevRight"} size={14} style={{ color: "var(--text-faint)", flexShrink: 0 }} />
        <Icon name="file" size={14} style={{ color: "var(--text-faint)", flexShrink: 0 }} />
        <span style={{ flex: 1, minWidth: 0, fontFamily: "var(--mono)", fontSize: 12.5, fontWeight: 600, color: "var(--text)", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>
          {file.displayName}
        </span>
        <span style={{ display: "flex", gap: 6, flexShrink: 0, fontFamily: "var(--mono)", fontSize: 11.5, fontWeight: 600 }}>
          {added > 0 && <span style={{ color: "var(--status-ok)" }}>+{added}</span>}
          {removed > 0 && <span style={{ color: "var(--status-err)" }}>−{removed}</span>}
        </span>
      </button>

      {open && file.hunks.map((hunk, hi) => (
        <div key={hi}>
          <div style={{ padding: "3px 13px", background: "var(--accent-soft)", color: "var(--accent)", fontFamily: "var(--mono)", fontSize: 11.5, borderTop: "1px solid var(--border-soft)", borderBottom: "1px solid var(--border-soft)" }}>
            {hunk.header}
          </div>
          <table style={{ width: "100%", borderCollapse: "collapse", fontFamily: "var(--mono)", fontSize: 12 }}>
            <tbody>
              {hunk.lines.map((line, li) => {
                const bg = line.type === "add" ? "color-mix(in oklch, var(--status-ok) 13%, transparent)"
                  : line.type === "remove" ? "color-mix(in oklch, var(--status-err) 13%, transparent)" : "transparent";
                const fg = line.type === "add" ? "var(--status-ok)" : line.type === "remove" ? "var(--status-err)" : "var(--text-dim)";
                return (
                  <tr key={li} style={{ background: bg }}>
                    <td style={cellNum}>{line.type !== "add" ? line.oldNum : ""}</td>
                    <td style={cellNum}>{line.type !== "remove" ? line.newNum : ""}</td>
                    <td style={{ width: 20, padding: "0 4px", textAlign: "center", userSelect: "none", lineHeight: "20px", color: fg, verticalAlign: "top" }}>
                      {line.type === "add" ? "+" : line.type === "remove" ? "−" : ""}
                    </td>
                    <td style={{ padding: "0 16px 0 0", lineHeight: "20px", whiteSpace: "pre-wrap", wordBreak: "break-word", color: line.type === "context" ? "var(--text-dim)" : fg }}>
                      {line.content || " "}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      ))}
    </div>
  );
}

interface DiffViewProps {
  raw: string;
  maxHeight?: number;
  /** Renders the "N Dateien · +X −Y" summary bar above the files. */
  showSummary?: boolean;
}

export function DiffView({ raw, maxHeight, showSummary = true }: DiffViewProps) {
  if (!raw || raw.startsWith("(")) {
    return <p style={emptyStyle}>{raw || "Kein Diff verfügbar."}</p>;
  }
  const files = parseDiff(raw);
  if (files.length === 0) return <p style={emptyStyle}>Kein Diff verfügbar.</p>;

  const totals = files.reduce(
    (acc, f) => {
      const c = countChanges(f);
      return { added: acc.added + c.added, removed: acc.removed + c.removed };
    },
    { added: 0, removed: 0 },
  );

  return (
    <div className={maxHeight ? "mc-scroll" : undefined} style={{ maxHeight, overflowY: maxHeight ? "auto" : undefined, padding: 12, display: "flex", flexDirection: "column", gap: 10 }}>
      {showSummary && (
        <div style={{ display: "flex", alignItems: "center", gap: 10, flexWrap: "wrap" }}>
          <Badge tone="neutral" icon="diff">{files.length} {files.length === 1 ? "Datei" : "Dateien"}</Badge>
          <span style={{ fontFamily: "var(--mono)", fontSize: 12.5, fontWeight: 600, color: "var(--status-ok)" }}>+{totals.added}</span>
          <span style={{ fontFamily: "var(--mono)", fontSize: 12.5, fontWeight: 600, color: "var(--status-err)" }}>−{totals.removed}</span>
        </div>
      )}
      {files.map((f) => <FileBlock key={f.displayName} file={f} defaultOpen={files.length <= 3} />)}
    </div>
  );
}
