import { useMutation } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Card, Icon, Button } from "@/components/mc";

/** Synapse's admin API is read with the operator's own authority, granted in a
 *  separate authorization (E36). This is the screen for "that has not happened yet",
 *  which is also the state after every restart — the refresh token is held in memory
 *  only and deliberately never written to disk.
 *
 *  Shared rather than duplicated because the connection is **per operator, not per
 *  page**: one Matrix token serves rooms and the report queue alike, so both screens
 *  show this panel and both start the same authorization. That is also why the
 *  endpoint is still `/api/v1/rooms/connect` — renaming it would imply a second
 *  connection exists (§3, etappe 46). */
export function MatrixConnect({ reason, error }: { reason?: string; error?: string }) {
  const connect = useMutation({
    mutationFn: () => api.post<{ url: string }>("/api/v1/rooms/connect", {}),
    onSuccess: (r) => { window.location.href = r.url; },
  });

  return (
    <Card>
      <div style={{ padding: 24, display: "flex", flexDirection: "column", gap: 14, alignItems: "flex-start" }}>
        <div style={{ display: "flex", alignItems: "center", gap: 9 }}>
          <Icon name="shield" size={17} />
          <span style={{ fontSize: 14.5, fontWeight: 650, color: "var(--text)" }}>
            Matrix-Admin-Zugriff verbinden
          </span>
        </div>

        {error && (
          <div style={{ fontSize: 13, color: "var(--status-err)", background: "color-mix(in oklch, var(--status-err) 10%, var(--surface))", border: "1px solid color-mix(in oklch, var(--status-err) 30%, var(--border))", borderRadius: "var(--radius-sm)", padding: "9px 11px" }}>
            {error}
          </div>
        )}

        <p style={{ fontSize: 13, color: "var(--text-dim)", lineHeight: 1.65, maxWidth: 620, margin: 0 }}>
          {reason ?? "Für diesen Bereich wird einmalig der Matrix-Admin-Zugriff verbunden."}{" "}
          MatrixCtrl fragt dabei <strong style={{ color: "var(--text)" }}>deine eigenen</strong> Rechte
          ab — nicht mehr, als du selbst hast. Matrix erteilt sie nur Konten, die
          Administrator sein dürfen.
        </p>

        {/* This paragraph used to claim the access was "nur für die
            Admin-Schnittstelle, nicht für deine Nachrichten". That was false once
            the client-API scope was added — and it had to be added, because Synapse
            cannot tell whose token it is without it. "Kann nicht" and "tut nicht"
            are different claims and only one of them is true (E42). */}
        <p style={{ fontSize: 12.5, color: "var(--text-faint)", lineHeight: 1.6, maxWidth: 620, margin: 0 }}>
          Der Zugriff umfasst technisch die volle Matrix-API deines Kontos — Synapse
          kann ein Token sonst keiner Person zuordnen. <strong>MatrixCtrl nutzt davon
          ausschließlich die Admin-Schnittstelle</strong> und liest keine Nachrichten.
          Es wird <strong>kein Gerät</strong> auf deinem Konto angelegt.
        </p>

        <p style={{ fontSize: 12.5, color: "var(--text-faint)", lineHeight: 1.6, maxWidth: 620, margin: 0 }}>
          Der Zugriff wird <strong>nicht gespeichert</strong>: nach einem Neustart des
          Panels ist er weg und muss neu verbunden werden. Abmelden beendet ihn sofort.
        </p>

        <Button variant="primary" icon="shield" onClick={() => connect.mutate()} disabled={connect.isPending}>
          {connect.isPending ? "Weiterleitung…" : "Verbinden"}
        </Button>
      </div>
    </Card>
  );
}
