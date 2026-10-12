import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { api } from "@/lib/api";
import { Badge, Button, Card, Icon, SectionTitle, Spinner } from "@/components/mc";

export const Route = createFileRoute("/bridges")({ component: BridgesPage });

interface Pod { name: string; ready: boolean; restarts: number; image: string }
interface Hookshot { enabled: boolean; pods: Pod[] | null; version?: string; bot: string; webhook_prefix?: string; webhooks: boolean; feeds: boolean }
interface Registration { id: string; url?: string; sender_localpart: string; user_namespaces?: string[]; protocols?: string[] }
interface Appservice { source: string; registration?: Registration; error?: string; reach: string }
export interface BridgesResponse { hookshot: Hookshot; appservices: Appservice[] }

/** Bridges (etappe 119): Hookshot, which ESS ships, and every other bridge Synapse knows
 *  through a registration. Said plainly which is which: ESS runs the first; the others
 *  run elsewhere, and MatrixCtrl can only show whether Synapse reaches them. */
function BridgesPage() {
  const { data, isLoading, error, isFetching, refetch } = useQuery({
    queryKey: ["bridges"],
    queryFn: () => api.get<BridgesResponse>("/api/v1/bridges"),
  });
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 18, maxWidth: 920 }}>
      <SectionTitle icon="globe" sub="Räume mit anderen Diensten und Netzen verbinden"
        right={<Button size="sm" variant="outline" icon="refresh" disabled={isFetching} onClick={() => void refetch()}>{isFetching ? <Spinner size={13} /> : "Neu laden"}</Button>}>
        Bridges
      </SectionTitle>
      {isLoading && <Card style={{ fontSize: 13, color: "var(--text-faint)" }}><Spinner size={13} /> Lade…</Card>}
      {error && <Card style={{ fontSize: 13, color: "var(--status-err)" }}>{(error as Error).message}</Card>}
      {data && <BridgesView data={data} />}
    </div>
  );
}

export function BridgesView({ data }: { data: BridgesResponse }) {
  return (
    <>
      <HookshotCard h={data.hookshot} />
      <AppservicesCard list={data.appservices} />
    </>
  );
}

function Copyable({ text }: { text: string }) {
  const [done, setDone] = useState(false);
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
      <code style={{ fontFamily: "var(--mono)", fontSize: 12.5, padding: "2px 7px", background: "var(--surface-2)", border: "1px solid var(--border)", borderRadius: 6, userSelect: "all" }}>{text}</code>
      <button title="Kopieren" onClick={() => { void navigator.clipboard?.writeText(text).then(() => { setDone(true); setTimeout(() => setDone(false), 1400); }); }}
        style={{ background: "none", border: "none", cursor: "pointer", color: done ? "var(--status-ok)" : "var(--text-faint)", padding: 2 }}>
        <Icon name={done ? "check" : "file"} size={13} />
      </button>
    </span>
  );
}

function HookshotCard({ h }: { h: Hookshot }) {
  const navigate = useNavigate();
  const pods = h.pods ?? [];
  const running = pods.some((p) => p.ready);
  const toSettings = () => navigate({ to: "/config", search: { mode: "tasks", card: "bridges" } });
  const state = !h.enabled ? <Badge tone="neutral" size="sm">Aus</Badge>
    : running ? <Badge tone="ok" size="sm" icon="check">Läuft</Badge>
    : pods.length > 0 ? <Badge tone="warn" size="sm">Startet</Badge>
    : <Badge tone="warn" size="sm" icon="alert">Eingeschaltet, läuft aber nicht</Badge>;
  return (
    <Card style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      <div style={{ display: "flex", alignItems: "center", gap: 10, flexWrap: "wrap" }}>
        <span style={{ fontSize: 15, fontWeight: 650 }}>Hookshot</span>
        {state}
        {h.version && <span style={{ fontSize: 12, fontFamily: "var(--mono)", color: "var(--text-faint)" }}>{h.version}</span>}
        <span style={{ flex: 1 }} />
        <Button size="sm" variant="outline" icon="sliders" onClick={toSettings}>{h.enabled ? "Einstellungen" : "In den Einstellungen einschalten"}</Button>
      </div>
      <p style={{ margin: 0, fontSize: 13, color: "var(--text-dim)", lineHeight: 1.6 }}>
        Teil von ESS: verbindet Räume mit Webhooks und RSS-/Atom-Feeds — und, mit eigener App-Einrichtung, mit GitHub, GitLab und Jira.
      </p>
      {h.enabled && (
        <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
          <Badge tone={h.webhooks ? "ok" : "neutral"} size="sm">{h.webhooks ? "Webhooks an" : "Webhooks aus"}</Badge>
          <Badge tone={h.feeds ? "ok" : "neutral"} size="sm">{h.feeds ? "Feeds an" : "Feeds aus"}</Badge>
        </div>
      )}
      {h.enabled && !h.webhooks && !h.feeds && (
        <div style={{ fontSize: 12.5, color: "var(--status-warn)" }}>
          Hookshot läuft, nimmt aber noch nichts an: Webhooks und Feeds sind in ESS nicht von selbst an. Beides lässt sich in den Einstellungen einschalten.
        </div>
      )}
      {h.enabled && running && (
        <div style={{ display: "flex", flexDirection: "column", gap: 8, borderTop: "1px solid var(--border-soft)", paddingTop: 12 }}>
          <div style={{ fontSize: 13, fontWeight: 600 }}>So benutzt du es</div>
          {/* A group room, not a DM: a DM with the bot is Hookshot's admin room, which
              takes no `!hookshot` prefix and, with nothing enabled, knows only `help` —
              the operator's first try on 2026-10-12 (etappe 119). */}
          {/* Every step below was a stumble in the first real run on 2026-10-12: a DM is
              Hookshot's admin room; without moderator rights it configures nothing; the
              secret address arrives in the DM, not in the room (etappe 119). */}
          <Step n={1}>In einem <strong>Gruppenraum</strong> (kein Direktchat) <Copyable text={h.bot} /> einladen.</Step>
          <Step n={2}>Den Bot dort zum <strong>Moderator</strong> machen — sonst richtet er nichts ein.</Step>
          {h.webhooks && <Step n={3}>Webhook anlegen: <Copyable text="!hookshot webhook name" />. Die geheime Adresse{h.webhook_prefix ? <> (unter <code style={{ fontFamily: "var(--mono)", fontSize: 12 }}>{h.webhook_prefix}/…</code>)</> : null} schickt Hookshot dir <strong>im Direktchat</strong>, nicht in den Raum. Ausprobieren:
            <div style={{ marginTop: 4 }}><Copyable text={`curl -X POST -H 'Content-Type: application/json' -d '{"text":"Hallo"}' <Adresse>`} /></div></Step>}
          {h.feeds && <Step n={h.webhooks ? 4 : 3}>Feed abonnieren: <Copyable text="!hookshot feed https://…" /></Step>}
          <Step n={3 + (h.webhooks ? 1 : 0) + (h.feeds ? 1 : 0)}>Alle Befehle: <Copyable text="!hookshot help" /></Step>
        </div>
      )}
    </Card>
  );
}

function Step({ n, children }: { n: number; children: React.ReactNode }) {
  return (
    <div style={{ display: "flex", gap: 10, fontSize: 13, color: "var(--text-dim)", lineHeight: 1.7, alignItems: "baseline" }}>
      <span style={{ flexShrink: 0, width: 20, height: 20, borderRadius: 999, background: "var(--accent-soft)", color: "var(--accent)", display: "grid", placeItems: "center", fontSize: 11, fontWeight: 700 }}>{n}</span>
      <span>{children}</span>
    </div>
  );
}

function AppservicesCard({ list }: { list: Appservice[] }) {
  return (
    <Card style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      <div style={{ fontSize: 15, fontWeight: 650 }}>Andere Bridges</div>
      {list.length === 0 ? (
        <p style={{ margin: 0, fontSize: 13, color: "var(--text-dim)", lineHeight: 1.65 }}>
          Keine angemeldet. WhatsApp, Telegram, Signal und Co. laufen als eigene Dienste — etwa die mautrix-Bridges — und werden
          über ihre Registrierungsdatei bei Synapse angemeldet. Betreiben musst du sie selbst; MatrixCtrl zeigt hier, welche Synapse kennt
          und ob es sie erreicht.
        </p>
      ) : list.map((a) => (
        <div key={a.source} style={{ display: "grid", gridTemplateColumns: "14px 1fr", gap: "2px 10px", alignItems: "baseline", borderTop: "1px solid var(--border-soft)", paddingTop: 10 }}>
          <span style={{ width: 9, height: 9, borderRadius: 999, background: a.error ? "var(--status-err)" : a.reach ? "var(--status-warn)" : "var(--status-ok)" }} />
          <div style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "baseline" }}>
            <span style={{ fontSize: 13.5, fontWeight: 600 }}>{a.registration?.id ?? "Registrierung"}</span>
            {a.registration && <span style={{ fontSize: 12, color: "var(--text-faint)" }}>Bot <code style={{ fontFamily: "var(--mono)" }}>@{a.registration.sender_localpart}</code></span>}
          </div>
          <span />
          <div style={{ fontSize: 12.5, color: "var(--text-dim)", lineHeight: 1.6 }}>
            {a.error ? <span style={{ color: "var(--status-err)" }}>{a.error}</span> : (
              <>
                {a.registration?.url && <div>Adresse <code style={{ fontFamily: "var(--mono)" }}>{a.registration.url}</code> — {a.reach ? <span style={{ color: "var(--status-warn)" }}>{a.reach}</span> : "erreichbar"}</div>}
                {a.registration?.user_namespaces?.length ? <div>Nutzer <code style={{ fontFamily: "var(--mono)" }}>{a.registration.user_namespaces.join(", ")}</code></div> : null}
              </>
            )}
            <div style={{ color: "var(--text-faint)", fontSize: 11.5 }}>{a.source}</div>
          </div>
        </div>
      ))}
    </Card>
  );
}
