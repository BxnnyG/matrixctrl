import { useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { api } from "@/lib/api";
import { Button, Icon } from "@/components/mc";
import { Markdown } from "@/components/Markdown";

const SEEN_KEY = "mc.whatsnew.seen";
interface Section { version: string; date?: string; body: string }
interface NotesResp { current: string; sections: Section[] }

function getSeen(): string | null { try { return localStorage.getItem(SEEN_KEY); } catch { return null; } }
function setSeen(v: string) { try { localStorage.setItem(SEEN_KEY, v); } catch { /* ignore */ } }
const real = (v?: string) => !!v && v !== "dev";

export function NotesDialog({ sections, onClose }: { sections: Section[]; onClose: () => void }) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") onClose(); };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);
  return (
    <div style={{ position: "fixed", inset: 0, background: "oklch(0 0 0 / 0.55)", display: "flex", alignItems: "center", justifyContent: "center", zIndex: 60, padding: 16 }} onClick={onClose}>
      <div role="dialog" aria-modal="true" aria-label="Was ist neu" onClick={(e) => e.stopPropagation()}
        style={{ background: "var(--surface)", border: "1px solid var(--border)", borderRadius: "var(--radius-lg)", padding: 24, maxWidth: 640, width: "100%", maxHeight: "80dvh", display: "flex", flexDirection: "column", gap: 14 }}>
        <h2 style={{ margin: 0, fontSize: 15, fontWeight: 650, color: "var(--text)", display: "flex", gap: 8, alignItems: "center" }}><Icon name="info" size={18} /> Was ist neu</h2>
        <div className="mc-scroll" style={{ overflowY: "auto", display: "flex", flexDirection: "column", gap: 16 }}>
          {sections.map((s) => (
            <div key={s.version}>
              <div style={{ fontSize: 13.5, fontWeight: 650, color: "var(--text)", marginBottom: 4 }}>Version {s.version}{s.date ? ` · ${s.date}` : ""}</div>
              <Markdown text={s.body} />
            </div>
          ))}
        </div>
        <div style={{ display: "flex", justifyContent: "flex-end" }}><Button variant="primary" size="sm" onClick={onClose}>Schließen</Button></div>
      </div>
    </div>
  );
}

const notesQuery = (since: string) => ({
  queryKey: ["whatsnew", since],
  queryFn: () => api.get<NotesResp>(`/api/v1/whatsnew?since=${encodeURIComponent(since)}`),
  staleTime: Infinity, retry: false,
});

/** Dialog for an explicit request (version label in the rail): `since` empty = the current section. */
export function WhatsNewDialog({ since, onClose }: { since: string; onClose: () => void }) {
  const { data } = useQuery(notesQuery(since));
  if (!data || data.sections.length === 0) return null;
  return <NotesDialog sections={data.sections} onClose={onClose} />;
}

/** After an update: shows what changed since the version last seen. First visit only
 *  remembers the version; dev builds show nothing. */
export function WhatsNew() {
  const { data: ver } = useQuery({ queryKey: ["version"], queryFn: () => api.get<{ version: string }>("/api/v1/version"), staleTime: 10 * 60_000 });
  const current = ver?.version;
  const [seen] = useState<string | null>(getSeen);
  const [closed, setClosed] = useState(false);
  const active = real(current) && seen !== null && seen !== current;
  useEffect(() => { if (real(current) && seen === null) setSeen(current!); }, [current, seen]);
  const { data } = useQuery({ ...notesQuery(seen ?? ""), enabled: active });
  if (!active || closed || !data || data.sections.length === 0) return null;
  return <NotesDialog sections={data.sections} onClose={() => { setSeen(current!); setClosed(true); }} />;
}
