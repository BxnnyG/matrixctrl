import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useRef, useState } from "react";
import { useQuery, useMutation } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { useUpgradeStream } from "@/lib/ws";
import { Card, Icon, Badge, Button, SectionTitle, Spinner } from "@/components/mc";

export const Route = createFileRoute("/helm/upgrade")({
  component: UpgradeWizard,
  // The version travels from the list page so "Upgrade auf 26.8.0" arrives with
  // 26.8.0 selected, instead of asking the operator to pick it a second time (E32).
  validateSearch: (search: Record<string, unknown>): { version?: string } => ({
    version: typeof search.version === "string" ? search.version : undefined,
  }),
});

interface ReleaseNotes {
  version: string;
  available: boolean;
  title?: string;
  published_at?: string;
  body?: string;
  url?: string;
  /** Why it is unavailable. "could not be fetched" and "none published" lead to
   *  different conclusions, so they are not collapsed into one empty state. */
  reason?: string;
}

interface HelmRelease { chart_version: string; revision: number }
interface ESSVersion { version: string }
interface UpgradeResponse { upgrade_id: string }

const essVersion = (v: string) => v.replace(/^matrix-stack-/, "");

/** The release notes for the version about to be installed.
 *
 *  Not decoration: 26.8.0's notes say "Upgrade Element Web to v1.12.25" and
 *  "Upgrade Synapse to v1.158.0" — exactly the upgrades the operator's pinned image
 *  tags were silently preventing. This screen now carries both halves: what the
 *  version brings, and (from the upgrade log) what a pin will stop it bringing. */
function NotesPanel({ notes, loading, version }: { notes?: ReleaseNotes; loading: boolean; version: string }) {
  if (loading && !notes) {
    return <div style={{ fontSize: 12.5, color: "var(--text-faint)" }}><Spinner size={13} /> Lade Release Notes…</div>;
  }
  if (!notes?.available) {
    return (
      <div style={{ fontSize: 12.5, color: "var(--text-faint)", padding: "10px 12px", borderRadius: "var(--radius-sm)", background: "var(--surface-2)" }}>
        {notes?.reason ?? "Keine Release Notes verfügbar."}
      </div>
    );
  }
  return (
    <div style={{ borderRadius: "var(--radius-sm)", background: "var(--surface-2)", border: "1px solid var(--border)" }}>
      <div style={{ display: "flex", alignItems: "center", gap: 10, padding: "10px 14px", borderBottom: "1px solid var(--border)", flexWrap: "wrap" }}>
        <Icon name="file" size={15} style={{ color: "var(--text-dim)" }} />
        <span style={{ fontSize: 13, fontWeight: 650, color: "var(--text)" }}>{notes.title || version}</span>
        {notes.published_at && (
          <span style={{ fontSize: 11.5, color: "var(--text-faint)" }}>
            {new Date(notes.published_at).toLocaleDateString("de-DE", { day: "2-digit", month: "2-digit", year: "numeric" })}
          </span>
        )}
        {notes.url && (
          <a href={notes.url} target="_blank" rel="noreferrer noopener"
            style={{ marginLeft: "auto", fontSize: 11.5, color: "var(--accent)", textDecoration: "none" }}>
            auf GitHub ↗
          </a>
        )}
      </div>
      <div className="mc-scroll" style={{ maxHeight: 320, overflowY: "auto", padding: "12px 14px" }}>
        <Markdown text={notes.body ?? ""} />
      </div>
    </div>
  );
}

/** A deliberately small markdown subset: headings, list items, links and inline
 *  code. Release notes have a fixed shape, and a markdown library for four
 *  constructs is a lot of bundle for a little text — the same reasoning that keeps
 *  Monaco behind a lazy boundary. */
function Markdown({ text }: { text: string }) {
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

function UpgradeWizard() {
  const navigate = useNavigate();
  const logRef = useRef<HTMLDivElement>(null);
  const preselected = Route.useSearch().version;
  const [selectedVersion, setSelectedVersion] = useState(preselected ?? "");
  const [upgradeId, setUpgradeId] = useState<string | null>(null);
  const [logs, setLogs] = useState<string[]>([]);
  const [done, setDone] = useState(false);
  const [finalStatus, setFinalStatus] = useState<string | null>(null);

  const { data: current } = useQuery({
    queryKey: ["helm", "release"],
    queryFn: () => api.get<HelmRelease>("/api/v1/helm/releases/ess"),
  });
  const { data: versions } = useQuery({
    queryKey: ["helm", "versions"],
    queryFn: () => api.get<ESSVersion[]>("/api/v1/helm/versions"),
  });

  const { data: notes, isFetching: notesLoading } = useQuery({
    queryKey: ["helm", "notes", selectedVersion],
    queryFn: () => api.get<ReleaseNotes>(`/api/v1/helm/versions/${encodeURIComponent(selectedVersion)}/notes`),
    enabled: selectedVersion !== "",
    // Published notes do not change, and GitHub's unauthenticated limit is 60
    // requests an hour — refetching on every render would exhaust it.
    staleTime: Infinity,
  });

  const upgrade = useMutation({
    mutationFn: (toVersion: string) =>
      api.post<UpgradeResponse>("/api/v1/helm/releases/ess/upgrade", { to_version: toVersion }),
    onSuccess: (res) => {
      setUpgradeId(res.upgrade_id);
      setLogs([]);
      setDone(false);
      setFinalStatus(null);
    },
  });

  useUpgradeStream(upgradeId, {
    onLog: (line) => {
      setLogs((prev) => [...prev, line]);
      setTimeout(() => logRef.current?.scrollTo({ top: logRef.current.scrollHeight, behavior: "smooth" }), 30);
    },
    onDone: (status) => {
      setDone(true);
      setFinalStatus(status);
      if (status === "success") setTimeout(() => navigate({ to: "/helm" }), 3000);
    },
  });

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 20, maxWidth: 720 }}>
      <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
        <Button variant="ghost" size="sm" icon="chevLeft" onClick={() => navigate({ to: "/helm" })}>Release</Button>
      </div>

      {current && (
        <Card style={{ display: "flex", alignItems: "center", gap: 12, padding: "14px 18px" }}>
          <Icon name="helm" size={18} style={{ color: "var(--accent)" }} />
          <span style={{ fontSize: 13, color: "var(--text-dim)" }}>Aktuell:</span>
          <code style={{ fontFamily: "var(--mono)", fontSize: 13, fontWeight: 600, color: "var(--text)" }}>{essVersion(current.chart_version)}</code>
          <Badge tone="neutral" size="sm">Revision #{current.revision}</Badge>
        </Card>
      )}

      <Card style={{ display: "flex", gap: 12, alignItems: "flex-start", background: "color-mix(in oklch, var(--status-warn) 9%, var(--surface))", borderColor: "color-mix(in oklch, var(--status-warn) 30%, var(--border))" }}>
        <Icon name="alert" size={18} style={{ color: "var(--status-warn)", flexShrink: 0, marginTop: 1 }} />
        <div style={{ fontSize: 13, lineHeight: 1.55 }}>
          <strong style={{ color: "var(--text)" }}>Post-Upgrade-Hooks laufen automatisch.</strong>
          <div style={{ color: "var(--text-dim)", marginTop: 2 }}>SFU <code style={{ fontFamily: "var(--mono)" }}>hostNetwork</code> und Service <code style={{ fontFamily: "var(--mono)" }}>externalTrafficPolicy</code> werden nach dem Upgrade gepatcht.</div>
        </div>
      </Card>

      {!upgradeId ? (
        <Card style={{ display: "flex", flexDirection: "column", gap: 16 }}>
          <SectionTitle sub="Zielversion für das ESS-Helm-Release wählen">Upgrade konfigurieren</SectionTitle>
          <div>
            <label style={{ display: "block", fontSize: 12.5, fontWeight: 600, color: "var(--text-dim)", marginBottom: 8 }}>Zielversion</label>
            <select value={selectedVersion} onChange={(e) => setSelectedVersion(e.target.value)}
              className="mc-input" style={{ width: "100%", padding: "9px 12px", borderRadius: "var(--radius-sm)", background: "var(--surface-2)", border: "1px solid var(--border)", color: "var(--text)", fontSize: 13.5, fontFamily: "var(--font)" }}>
              <option value="">Version wählen…</option>
              {versions?.map((v, i) => (
                <option key={v.version} value={v.version}>{essVersion(v.version)}{i === 0 ? " — latest" : ""}{v.version === current?.chart_version ? " (aktuell)" : ""}</option>
              ))}
            </select>
          </div>
          {selectedVersion && (
            <NotesPanel notes={notes} loading={notesLoading} version={essVersion(selectedVersion)} />
          )}

          <div>
            <Button variant="primary" icon="upload" disabled={!selectedVersion || upgrade.isPending} onClick={() => upgrade.mutate(selectedVersion)}>
              {upgrade.isPending ? <><Spinner size={14} /> Starte…</> : "Upgrade starten"}
            </Button>
          </div>
          {upgrade.isError && <div style={{ fontSize: 13, color: "var(--status-err)" }}>{(upgrade.error as Error).message}</div>}
        </Card>
      ) : (
        <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
          <div ref={logRef} className="mc-scroll" style={{ background: "oklch(0.13 0.005 256)", borderRadius: "var(--radius)", border: "1px solid var(--border)", padding: 16, fontFamily: "var(--mono)", fontSize: 12, color: "oklch(0.82 0.13 150)", minHeight: 200, maxHeight: 400, overflowY: "auto", lineHeight: 1.6 }}>
            {logs.map((line, i) => <div key={i}>{line}</div>)}
            {!done && <div style={{ animation: "mc-ping 1.2s ease infinite", marginTop: 2 }}>▋</div>}
          </div>

          {done && finalStatus === "success" && (
            <Card style={{ display: "flex", alignItems: "center", gap: 12, background: "color-mix(in oklch, var(--status-ok) 10%, var(--surface))", borderColor: "color-mix(in oklch, var(--status-ok) 30%, var(--border))" }}>
              <Icon name="check" size={20} style={{ color: "var(--status-ok)" }} />
              <div style={{ fontSize: 13 }}><strong style={{ color: "var(--text)" }}>Upgrade erfolgreich.</strong> <span style={{ color: "var(--text-faint)" }}>Weiterleitung…</span></div>
            </Card>
          )}

          {done && finalStatus === "hooks-failed" && (
            <Card style={{ display: "flex", gap: 12, alignItems: "flex-start", background: "color-mix(in oklch, var(--status-warn) 9%, var(--surface))", borderColor: "color-mix(in oklch, var(--status-warn) 30%, var(--border))" }}>
              <Icon name="alert" size={20} style={{ color: "var(--status-warn)", flexShrink: 0, marginTop: 1 }} />
              <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
                <div style={{ fontSize: 13, lineHeight: 1.55 }}>
                  <strong style={{ color: "var(--text)" }}>Helm-Upgrade erfolgreich, aber Hooks fehlgeschlagen.</strong>
                  <div style={{ color: "var(--text-dim)", marginTop: 3 }}>Der ESS-Release ist auf dem neuen Stand. Die Post-Upgrade-Patches (SFU hostNetwork etc.) wurden jedoch nicht vollständig angewendet — WebRTC-Calling könnte beeinträchtigt sein.</div>
                </div>
                <div style={{ display: "flex", gap: 8 }}>
                  <Button variant="soft" size="sm" icon="hook" onClick={() => navigate({ to: "/hooks" })}>Hooks manuell ausführen</Button>
                  <Button variant="outline" size="sm" onClick={() => navigate({ to: "/helm" })}>Zur Übersicht</Button>
                </div>
              </div>
            </Card>
          )}

          {done && finalStatus === "failed" && (
            <Card style={{ display: "flex", gap: 12, alignItems: "flex-start", background: "color-mix(in oklch, var(--status-err) 9%, var(--surface))", borderColor: "color-mix(in oklch, var(--status-err) 30%, var(--border))" }}>
              <Icon name="x" size={20} style={{ color: "var(--status-err)", flexShrink: 0, marginTop: 1 }} />
              <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
                <div style={{ fontSize: 13, lineHeight: 1.55 }}>
                  <strong style={{ color: "var(--text)" }}>Upgrade fehlgeschlagen.</strong>
                  <div style={{ color: "var(--text-dim)", marginTop: 3 }}>Helm hat die vorherige Revision automatisch wiederhergestellt. Sieh die Logs oben für Details.</div>
                </div>
                <div><Button variant="outline" size="sm" onClick={() => navigate({ to: "/helm" })}>Zur Übersicht</Button></div>
              </div>
            </Card>
          )}
        </div>
      )}
    </div>
  );
}
