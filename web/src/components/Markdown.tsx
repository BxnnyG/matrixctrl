/** A deliberately small markdown subset: headings, list items, links and inline
 *  code. Release notes have a fixed shape, and a markdown library for four
 *  constructs is a lot of bundle for a little text — the same reasoning that keeps
 *  Monaco behind a lazy boundary (§4.10).
 *
 *  Lifted out of routes/helm/upgrade.tsx when the version list gained inline notes
 *  and a second screen needed it (§3, E43). */
export function Markdown({ text }: { text: string }) {
  const lines = text.split("\n");
  return (
    <div style={{ fontSize: 12.5, color: "var(--text-dim)", lineHeight: 1.65 }}>
      {lines.map((raw, i) => {
        const line = raw.trimEnd();
        if (line.trim() === "") return <div key={i} style={{ height: 6 }} />;

        const heading = /^(#{1,4})\s+(.*)$/.exec(line);
        if (heading) {
          const level = heading[1].length;
          return (
            <div key={i} style={{ fontSize: level <= 2 ? 13.5 : 13, fontWeight: 650, color: "var(--text)", marginTop: i === 0 ? 0 : 12, marginBottom: 4 }}>
              {inline(heading[2])}
            </div>
          );
        }

        const bullet = /^(\s*)[-*]\s+(.*)$/.exec(line);
        if (bullet) {
          return (
            <div key={i} style={{ display: "flex", gap: 8, paddingLeft: bullet[1].length * 6 }}>
              <span style={{ color: "var(--text-faint)" }}>•</span>
              <span style={{ flex: 1, minWidth: 0 }}>{inline(bullet[2])}</span>
            </div>
          );
        }

        return <div key={i} style={{ paddingLeft: /^\s+/.test(raw) ? 14 : 0 }}>{inline(line.trim())}</div>;
      })}
    </div>
  );
}

/** Inline links and code. Everything else is rendered as text — an unrecognised
 *  construct should look plain, never be interpreted as markup. */
function inline(text: string): React.ReactNode[] {
  const out: React.ReactNode[] = [];
  const pattern = /\[([^\]]+)\]\(([^)]+)\)|`([^`]+)`/g;
  let last = 0;
  let m: RegExpExecArray | null;
  let key = 0;

  while ((m = pattern.exec(text)) !== null) {
    if (m.index > last) out.push(text.slice(last, m.index));
    if (m[1] !== undefined && /^https?:\/\//.test(m[2])) {
      // Only http(s). A javascript: or data: URL in third-party text must not
      // become a clickable link in an admin panel.
      out.push(<a key={key++} href={m[2]} target="_blank" rel="noreferrer noopener" style={{ color: "var(--accent)", textDecoration: "none" }}>{m[1]}</a>);
    } else if (m[1] !== undefined) {
      out.push(m[1]);
    } else {
      out.push(<code key={key++} style={{ fontFamily: "var(--mono)", fontSize: 11.5, background: "var(--surface)", padding: "1px 4px", borderRadius: 3 }}>{m[3]}</code>);
    }
    last = pattern.lastIndex;
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}
