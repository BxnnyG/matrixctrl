import { useMutation } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import { api } from "@/lib/api";
import { Card, Icon, Button, Spinner } from "@/components/mc";

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

/** How long to wait before a second automatic attempt is allowed.
 *
 *  The guard that matters. An automatic redirect triggered by a failing condition is
 *  a redirect loop unless something stops it, and the dangerous case is not a failed
 *  authorization — that comes back with `?error=` — but a *successful* one whose token
 *  is then rejected again immediately. That would loop through MAS forever with no
 *  error to show for it (etappe 52). */
const RETRY_AFTER_MS = 30_000;
const ATTEMPT_KEY = "mc.matrixconnect.lastAttempt";

function recentlyAttempted(): boolean {
  try {
    const last = Number(sessionStorage.getItem(ATTEMPT_KEY) ?? 0);
    return Number.isFinite(last) && Date.now() - last < RETRY_AFTER_MS;
  } catch {
    // Private mode, storage disabled: no memory means no loop protection, so the
    // automatic path is refused rather than run unguarded.
    return true;
  }
}

function markAttempt() {
  try {
    sessionStorage.setItem(ATTEMPT_KEY, String(Date.now()));
  } catch {
    /* nothing to do — recentlyAttempted() fails closed */
  }
}

/** Called on a successful load so the next genuine disconnect reconnects at once
 *  rather than waiting out a stale timer. */
export function clearConnectAttempt() {
  try {
    sessionStorage.removeItem(ATTEMPT_KEY);
  } catch {
    /* ignore */
  }
}

export function MatrixConnect({
  reason,
  error,
  returnTo,
  auto = false,
}: {
  reason?: string;
  error?: string;
  /** Which screen to come back to. The server maps it onto an allowlist. */
  returnTo?: string;
  /** Start the authorization without waiting for a click. */
  auto?: boolean;
}) {
  const connect = useMutation({
    mutationFn: () =>
      api.post<{ url: string }>("/api/v1/rooms/connect", { return_to: returnTo }),
    onSuccess: (r) => {
      window.location.href = r.url;
    },
  });

  // Only ever fired once per mount, on top of the session-wide timer: React can run
  // an effect twice in development, and two authorizations would consume two states
  // and land the operator wherever the second one finishes.
  const started = useRef(false);

  // Auto-connect is suppressed when the previous attempt failed. `error` is set by the
  // callback's failure redirect, so its presence means "the last round trip did not
  // work" — retrying it silently would hide the reason behind an endless bounce.
  const mayAuto = auto && !error && !started.current && !recentlyAttempted();

  useEffect(() => {
    if (!mayAuto) return;
    started.current = true;
    markAttempt();
    connect.mutate();
    // connect.mutate is stable for the life of the mutation object; re-running this
    // on every render is exactly the loop being guarded against.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mayAuto]);

  // While the silent path is running there is nothing to decide, so the panel would
  // only flash a button the operator never needed to press.
  if (mayAuto || (auto && connect.isPending)) {
    return (
      <Card>
        <div style={{ padding: 24, display: "flex", alignItems: "center", gap: 10 }}>
          <Spinner size={15} />
          <span style={{ fontSize: 13.5, color: "var(--text-dim)" }}>
            Matrix-Zugriff wird wiederhergestellt…
          </span>
        </div>
      </Card>
    );
  }

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

        {/* Still not stored — E52 made the reconnect invisible, not persistent. Saying
            "wird neu verbunden" instead of "muss neu verbunden werden" keeps the
            security property stated while describing what the operator now sees. */}
        <p style={{ fontSize: 12.5, color: "var(--text-faint)", lineHeight: 1.6, maxWidth: 620, margin: 0 }}>
          Der Zugriff wird <strong>nicht gespeichert</strong>: nach einem Neustart des
          Panels ist er weg und wird beim nächsten Aufruf automatisch neu verbunden —
          ohne Klick, solange die Matrix-Sitzung noch gilt. Abmelden beendet ihn sofort.
        </p>

        <Button variant="primary" icon="shield" onClick={() => { markAttempt(); connect.mutate(); }} disabled={connect.isPending}>
          {connect.isPending ? "Weiterleitung…" : "Verbinden"}
        </Button>
      </div>
    </Card>
  );
}
