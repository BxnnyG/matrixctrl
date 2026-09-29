// What "Übernehmen" would do, in words (etappe 108). Mirrors configVerdict in
// internal/api/handlers/config_preview.go.

export interface Restart { kind: string; name: string; new?: boolean }

export interface Finding {
  level: "ok" | "warn" | "blocked" | "unknown";
  workload?: string;
  kind?: string;
  message: string;
}

export interface ConfigVerdict {
  rendered: boolean;
  note?: string;
  restarts: Restart[];
  findings: Finding[];
  blocking: boolean;
  release_status?: string;
  stuck: boolean;
  /** hostAliases into the service network that no Service holds (etappe 109). */
  stale_aliases?: StaleAlias[];
  /** The Services could not be listed, so the alias check did not run. */
  aliases_unchecked?: boolean;
  /** What each workload reserves after the change, and the largest node (etappe 113). */
  requests?: { workload: string; cpu_millis: number; mem_mi: number }[];
  node?: { cpu_millis: number; mem_mi: number };
}

export interface StaleAlias {
  workload: string;
  ip: string;
  hostnames: string[];
  suggest?: string;
  message: string;
}

/** The release prefix is the same on every workload and says nothing. */
export function serviceName(name: string, release = "ess"): string {
  return name.startsWith(release + "-") ? name.slice(release.length + 1) : name;
}

/** One line for the bar: which services go down for a moment, and which are new. */
export function restartSummary(v: ConfigVerdict | undefined): string {
  if (!v) return "";
  if (!v.rendered) return "Auswirkung nicht prüfbar";
  // `?? []`: a missing list is "nothing", never a crash of the whole settings page.
  const restarts = v.restarts ?? [];
  const restarting = restarts.filter((r) => !r.new).map((r) => serviceName(r.name));
  const starting = restarts.filter((r) => r.new).map((r) => serviceName(r.name));
  const parts: string[] = [];
  if (restarting.length) parts.push(`startet neu: ${restarting.join(", ")}`);
  if (starting.length) parts.push(`startet erstmals: ${starting.join(", ")}`);
  return parts.length ? parts.join(" · ") : "kein Dienst startet neu";
}

/** Why "Übernehmen" will not go ahead, or null when it will. */
export function refusal(v: ConfigVerdict | undefined): string | null {
  if (!v) return null;
  if (v.stuck) {
    return `Das Release steht auf „${v.release_status}" und nimmt kein neues Übernehmen an, bis es zurückgesetzt ist.`;
  }
  if (v.blocking) return "Diese Konfiguration passt nicht auf den Cluster.";
  return null;
}
