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
  /** Which parts the file holds beyond MatrixCtrl's own: "homeserver/", "mas/",
   *  "media/", "secrets/". */
  parts?: string[];
  /** Whether the homeserver part carries its sequences. Absent when there is no
   *  homeserver part at all — which is a different statement from "no counters", and
   *  the screen says each of them differently (etappe 106). */
  counters?: boolean;
  part_rows?: { name: string; tables: number; rows: number; sequences: number }[];
}

/** One step of a running restore, as the server reports it. */
export interface RestoreStep {
  at: string;
  step: string;
  detail: string;
}

export interface RestoreProgress {
  running?: boolean;
  started?: string;
  steps?: RestoreStep[];
  done?: boolean;
  failed?: string;
  summary?: {
    config_files: number;
    own_tables?: string[];
    databases?: { database: string; previous_name?: string; tables: number; total_rows: number; sequences: number }[];
    media_files?: number;
    keys?: { name: string; restored: string[]; kept: string[] };
  };
}

/** What each part is called on screen. The archive names them as directories; an
 *  operator does not think in prefixes. */
export const PART_LABELS: Record<string, string> = {
  "matrixctrl/": "Konfiguration und MatrixCtrl",
  "homeserver/": "Räume, Nachrichten, Verläufe",
  "mas/": "Konten",
  "media/": "Hochgeladene Dateien",
  "secrets/": "Schlüssel des Homeservers",
};

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
