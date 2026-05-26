import { useEffect, useRef } from "react";

interface UseUpgradeStreamOptions {
  onLog: (line: string) => void;
  onDone: (status: string) => void;
}

export function useUpgradeStream(
  upgradeId: string | null,
  opts: UseUpgradeStreamOptions,
) {
  const wsRef = useRef<WebSocket | null>(null);

  useEffect(() => {
    if (!upgradeId) return;

    const token = localStorage.getItem("matrixctrl_token");
    const proto = window.location.protocol === "https:" ? "wss" : "ws";
    const url = `${proto}://${window.location.host}/api/v1/helm/releases/ess/upgrade/${upgradeId}/logs?token=${token}`;

    const ws = new WebSocket(url);
    wsRef.current = ws;

    ws.onmessage = (e) => {
      try {
        const msg = JSON.parse(e.data);
        if (msg.type === "log") opts.onLog(msg.line);
        if (msg.type === "done") opts.onDone(msg.status);
      } catch {
        opts.onLog(e.data);
      }
    };

    ws.onerror = () => opts.onLog("[WebSocket Fehler]");
    ws.onclose = () => opts.onLog("[Verbindung getrennt]");

    return () => ws.close();
  }, [upgradeId]);
}
