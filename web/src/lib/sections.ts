// Curation for the settings UI: how section files are grouped/ordered in the nav,
// and how keys are ordered within a section (most-intuitive first).
// Informed by the ESS matrix-stack chart structure (core components vs. infra).

export interface NavGroup {
  label: string;
  files: string[];
  defaultOpen: boolean;
}

// Section files grouped into intuitive buckets. Files not listed fall into "Weitere".
export const NAV_GROUPS: NavGroup[] = [
  { label: "Allgemein", defaultOpen: true, files: ["general.yaml"] },
  {
    label: "Komponenten",
    defaultOpen: true,
    files: [
      "synapse.yaml",
      "matrixAuthenticationService.yaml",
      "elementWeb.yaml",
      "matrixRTC.yaml",
      "elementAdmin.yaml",
      "wellKnownDelegation.yaml",
    ],
  },
  { label: "Daten & Cache", defaultOpen: true, files: ["postgres.yaml", "redis.yaml"] },
  {
    label: "Infrastruktur",
    defaultOpen: false,
    files: [
      "haproxy.yaml",
      "networking.yaml",
      "storage.yaml",
      "image.yaml",
      "initSecrets.yaml",
      "clusterDomain.yaml",
      "tolerations.yaml",
      "topologySpreadConstraints.yaml",
      "hookshot.yaml",
      "deploymentMarkers.yaml",
    ],
  },
];

// Lower rank sorts first. Applies to top-level keys within a file AND to
// fields/sub-groups within a section, so the things people reach for first
// (server name, enabled, host, …) lead and noisy infra trails.
const KEY_PRIORITY: Record<string, number> = {
  serverName: 0,
  enabled: 1,
  host: 2,
  hostname: 2,
  url: 3,
  ingress: 5,
  name: 6,
  replicas: 7,
  registration: 8,
  registrationSharedSecret: 9,
  config: 10,
  additional: 11,
  certManager: 20,
  postgres: 21,
  redis: 22,
  secrets: 25,
  extraEnv: 60,
  extraVolumes: 61,
  resources: 80,
  image: 82,
  labels: 84,
  annotations: 85,
  matrixTools: 86,
  serviceAccount: 88,
  podSecurityContext: 90,
  containersSecurityContext: 90,
  nodeSelector: 92,
  tolerations: 94,
  topologySpreadConstraints: 95,
  global: 98,
};

export function keyRank(key: string): number {
  return KEY_PRIORITY[key] ?? 50;
}

// Order a list of keys: by curated rank, then alphabetically.
export function orderKeys(keys: string[]): string[] {
  return [...keys].sort((a, b) => keyRank(a) - keyRank(b) || a.localeCompare(b));
}

// Build the grouped nav from the actual files present, dropping empty groups and
// collecting unknown files into "Weitere".
export function groupNav(files: string[]): NavGroup[] {
  const present = new Set(files);
  const used = new Set<string>();
  const groups: NavGroup[] = [];
  for (const g of NAV_GROUPS) {
    const fs = g.files.filter((f) => present.has(f));
    fs.forEach((f) => used.add(f));
    if (fs.length) groups.push({ ...g, files: fs });
  }
  const rest = files.filter((f) => !used.has(f)).sort();
  if (rest.length) groups.push({ label: "Weitere", defaultOpen: false, files: rest });
  return groups;
}
