import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { Card, Icon } from "@/components/mc";

interface Failure { name: string; kind: "timeout" | "servfail" | "notfound" | "other"; server?: string; error: string }
export interface DNSState { ok: boolean; checked: string; since?: string; names: string[]; failures?: Failure[]; outside?: "answers" | "silent" | "" }

/** The cluster cannot resolve names (etappe 119a).
 *
 *  On 2026-10-10 the host's resolver stopped answering and every service lost every
 *  external name at once; the only trace was a Go error on the login page. This says
 *  what is down, since when, which names, and how to make the cluster independent of
 *  the host's resolver. Renders nothing while resolution works. */
export function ClusterDNSAlert() {
  const { data } = useQuery({
    queryKey: ["dns-health"],
    queryFn: () => api.get<DNSState>("/api/v1/dns-health"),
    refetchInterval: 30_000,
  });
  if (!data || data.ok) return null;
  return <ClusterDNSAlertView state={data} />;
}

export function ClusterDNSAlertView({ state }: { state: DNSState }) {
  const outage = (state.failures ?? []).filter((f) => f.kind === "timeout" || f.kind === "servfail");
  const since = state.since ? new Date(state.since).toLocaleString("de-DE", { day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit" }) : "";
  const server = outage.find((f) => f.server)?.server;
  return (
    <Card style={{ display: "flex", flexDirection: "column", gap: 10, background: "color-mix(in oklch, var(--status-err) 9%, var(--surface))", borderColor: "color-mix(in oklch, var(--status-err) 28%, var(--border))" }}>
      <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
        <Icon name="alert" size={18} style={{ color: "var(--status-err)" }} />
        <strong style={{ fontSize: 14 }}>Der Cluster kann keine Namen auflösen{since ? ` — seit ${since}` : ""}</strong>
      </div>
      <p style={{ margin: 0, fontSize: 13, color: "var(--text)", lineHeight: 1.6 }}>
        {state.outside === "answers"
          ? <>Das Internet ist erreichbar — nur die Namensauflösung im Cluster hängt. CoreDNS{server ? ` (${server})` : ""} gibt an den DNS-Server weiter, der in <code>/etc/resolv.conf</code> des Hosts steht, und der antwortet nicht. Bei NetBird oder Tailscale ist das oft deren eigener Resolver.</>
          : state.outside === "silent"
            ? <>Weder der Cluster noch ein öffentlicher DNS-Server antwortet — der Server hat gerade keine Verbindung nach draußen.</>
            : <>Die Namensauflösung im Cluster antwortet nicht.</>}
        {" "}Betroffen ist alles, was einen Namen braucht: die Anmeldung über Matrix, Anmeldung bei externen Anbietern, die Föderation.
      </p>
      <div style={{ fontSize: 12.5, color: "var(--text-dim)", fontFamily: "var(--mono)", lineHeight: 1.6 }}>
        {outage.map((f) => <div key={f.name}>{f.name} — {f.kind === "timeout" ? "keine Antwort" : "Auflösung gescheitert (SERVFAIL)"}</div>)}
      </div>
      {state.outside === "answers" && (
        <details style={{ fontSize: 12.5, color: "var(--text-dim)" }}>
          <summary style={{ cursor: "pointer" }}>Den Cluster vom DNS des Hosts unabhängig machen (k3s)</summary>
          <pre style={{ margin: "8px 0 0", padding: 11, fontSize: 11.5, fontFamily: "var(--mono)", lineHeight: 1.6, background: "var(--bg)", border: "1px solid var(--border)", borderRadius: "var(--radius-sm)", whiteSpace: "pre-wrap", userSelect: "all" }}>{`printf 'nameserver 1.1.1.1\\nnameserver 9.9.9.9\\n' > /etc/rancher/k3s/resolv.conf
echo 'resolv-conf: /etc/rancher/k3s/resolv.conf' >> /etc/rancher/k3s/config.yaml
systemctl restart k3s
kubectl -n kube-system rollout restart deploy/coredns`}</pre>
          <div style={{ marginTop: 6, color: "var(--text-faint)" }}>
            Danach fragt CoreDNS öffentliche Resolver statt den des Hosts. Interne Namen deines VPNs löst der Cluster dann nicht mehr auf — die braucht ESS nicht.
          </div>
        </details>
      )}
    </Card>
  );
}
