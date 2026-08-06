import { useEffect } from "react";
import { api } from "./api";

interface UseUpgradeStreamOptions {
  onLog: (line: string) => void;
  onDone: (status: string) => void;
}

/** Reconnect backoff — see docs/plans/etappe-14-*.md (P1-7). */
const RECONNECT_DELAYS_MS = [1_000, 2_000, 5_000, 10_000, 15_000];

/**
 * The server replays the complete log buffer on every connect, so a client that
 * simply appends what it receives would duplicate the whole history on each
 * reconnect. The gate tracks how many lines have already been shown and skips
 * exactly that many at the start of every connection.
 *
 * Exported for testing.
 */
export function createLogGate() {
  let delivered = 0;
  let seenOnThisConnection = 0;

  return {
    /** Call when a connection is opened, before any line arrives. */
    reset() {
      seenOnThisConnection = 0;
    },
    /** True when this line has not been shown yet. */
    accept(): boolean {
      seenOnThisConnection++;
      if (seenOnThisConnection <= delivered) return false;
      delivered = seenOnThisConnection;
      return true;
    },
    get deliveredCount() {
      return delivered;
    },
  };
}

export function useUpgradeStream(
  upgradeId: string | null,
  opts: UseUpgradeStreamOptions,
) {
  useEffect(() => {
    if (!upgradeId) return;

    let cancelled = false;
    let finished = false;
    let attempt = 0;
    let ws: WebSocket | null = null;
    let retryTimer: ReturnType<typeof setTimeout> | undefined;

    const gate = createLogGate();

    // One backoff for both reasons a connection can fail to establish: the socket
    // closed, or the ticket request did not come back. Returns false when the
    // attempts are used up, so the caller decides what to tell the operator.
    const scheduleRetry = (): boolean => {
      if (attempt >= RECONNECT_DELAYS_MS.length) return false;
      const delay = RECONNECT_DELAYS_MS[attempt];
      attempt++;
      retryTimer = setTimeout(connect, delay);
      return true;
    };

    const finish = (status: string) => {
      if (finished) return;
      finished = true;
      opts.onDone(status);
    };

    // A browser cannot set an Authorization header on a WebSocket, so something has
    // to go in the URL — and URLs are logged, by the server and by every proxy in
    // between. This used to be the session token, which duly appeared in the
    // container log once per upgrade. It is now a single-use ticket, so the copy left
    // in those logs is spent by the time anyone reads it (E35).
    //
    // Fetched per attempt, never cached: a reconnect needs a fresh ticket because the
    // previous one was consumed by the connection that just dropped.
    const connect = async () => {
      if (cancelled || finished) return;

      let ticket: string;
      try {
        const res = await api.post<{ ticket: string }>("/api/v1/auth/ws-ticket", {});
        ticket = res.ticket;
      } catch {
        // Treat like a failed connection rather than a fatal error: the retry path
        // below already handles a backend that is briefly unreachable.
        scheduleRetry();
        return;
      }
      if (cancelled || finished) return;

      const proto = window.location.protocol === "https:" ? "wss" : "ws";
      const url = `${proto}://${window.location.host}/api/v1/helm/releases/ess/upgrade/${upgradeId}/logs?ticket=${encodeURIComponent(ticket)}`;

      gate.reset();
      ws = new WebSocket(url);

      ws.onopen = () => {
        attempt = 0;
      };

      ws.onmessage = (e) => {
        let msg: { type?: string; line?: string; status?: string };
        try {
          msg = JSON.parse(e.data);
        } catch {
          if (gate.accept()) opts.onLog(e.data);
          return;
        }

        switch (msg.type) {
          case "ping":
            // Keepalive during helm's silent phase — never shown to the operator.
            return;
          case "log":
            if (gate.accept()) opts.onLog(msg.line ?? "");
            return;
          case "done":
            finish(msg.status ?? "unknown");
            return;
          default:
            // Unknown types are ignored rather than echoed, so a future
            // server-side addition cannot dump raw JSON into the log view.
            return;
        }
      };

      // onerror is always followed by onclose; recovery lives there only.
      ws.onerror = () => {};

      ws.onclose = () => {
        if (cancelled || finished) return;
        void recover();
      };
    };

    /**
     * A dropped socket says nothing about the upgrade — the real 26.7.2 upgrade
     * succeeded while the UI showed "[Verbindung getrennt]". So ask the server
     * what actually happened before concluding anything.
     */
    const recover = async () => {
      if (cancelled || finished) return;

      try {
        const status = await api.get<{ status: string; done: boolean }>(
          `/api/v1/helm/releases/ess/upgrade/${upgradeId}`,
        );
        if (cancelled) return;
        if (status.done) {
          finish(status.status);
          return;
        }
      } catch {
        // Status unreachable (network still down, or the record is gone) —
        // fall through and try to reconnect.
      }

      if (!scheduleRetry()) {
        opts.onLog(
          "[Verbindung verloren — der Vorgang läuft im Hintergrund weiter. " +
            "Seite neu laden, um den aktuellen Stand zu sehen.]",
        );
      }
    };

    connect();

    return () => {
      cancelled = true;
      if (retryTimer) clearTimeout(retryTimer);
      if (ws) {
        // Drop the handlers first: closing would otherwise trigger recovery for
        // a stream this component no longer shows.
        ws.onclose = null;
        ws.onerror = null;
        ws.onmessage = null;
        ws.close();
      }
    };
  }, [upgradeId]);
}
