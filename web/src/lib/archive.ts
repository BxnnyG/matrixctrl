/** What an archive says about itself.
 *
 *  Shared, because two screens now read it: the backup page, which restores onto a
 *  server that already exists, and the setup page, which rebuilds one from it
 *  (etappe 82). The type lived on the backup page and would have been copied. */
export interface ArchiveManifest {
  created_at: string;
  app_version: string;
  ess?: { name?: string; namespace?: string; chart?: string; revision?: number };
  config_repo_files: number;
  tables?: { name: string; rows: number; regenerable: boolean }[];
  /** Read out of the archived configuration by the server. Absent when the archive
   *  carries no serverName — then the operator names the server themselves. */
  server_name?: string;
  restores?: string[];
  not_included?: string[];
}

/** "matrix-stack-26.8.0" → "26.8.0".
 *
 *  The archive records the chart as name + version, which is the right thing to store
 *  and the wrong thing to show. The two screens displayed it differently — one stripped
 *  the prefix, the other did not — which is what a second copy of a rule always
 *  produces eventually. */
export function essVersion(chart?: string): string {
  if (!chart) return "";
  return chart.replace(/^matrix-stack-/, "");
}
