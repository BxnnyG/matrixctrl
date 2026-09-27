// Helpers for rendering a settings UI from the ESS JSON Schema.

export interface JSONSchema {
  type?: string | string[];
  properties?: Record<string, JSONSchema>;
  items?: JSONSchema;
  enum?: unknown[];
  description?: string;
  default?: unknown;
  additionalProperties?: boolean | JSONSchema;
  required?: string[];
  anyOf?: JSONSchema[];
  oneOf?: JSONSchema[];
}

export type FieldKind =
  | "object"
  | "boolean"
  | "enum"
  | "string"
  | "number"
  | "integer"
  | "array"
  | "freeform"
  | "unknown";

/** Resolve a schema node to a single widget kind. */
export function fieldKind(node: JSONSchema | undefined): FieldKind {
  if (!node) return "unknown";
  if (node.enum && node.enum.length > 0) return "enum";
  // A union with no type of its own — Kubernetes quantities are `anyOf: [integer,
  // string]`, so every memory and CPU value was "unknown" and rendered as "nur via
  // YAML". A text field takes both "4Gi" and "2" (etappe 107).
  if (!node.type && (node.anyOf || node.oneOf)) {
    const branches = (node.anyOf ?? node.oneOf ?? []).map(fieldKind);
    if (branches.includes("string")) return "string";
    return branches.find((k) => k !== "unknown") ?? "unknown";
  }

  let t = node.type;
  if (Array.isArray(t)) t = t.find((x) => x !== "null") ?? t[0];

  switch (t) {
    case "boolean":
      return "boolean";
    case "string":
      return "string";
    case "number":
      return "number";
    case "integer":
      return "integer";
    case "array":
      return "array";
    case "object":
      // Object with declared properties → render recursively.
      if (node.properties && Object.keys(node.properties).length > 0) return "object";
      // Open-ended map (additionalProperties only) → not form-friendly.
      return "freeform";
    default:
      if (node.properties && Object.keys(node.properties).length > 0) return "object";
      return "unknown";
  }
}

/** Abbreviations that are written in capitals. `humanize("tlsEnabled")` gave "Tls
 *  Enabled" — a label that reads as a typo to exactly the people who know what TLS is. */
const ACRONYMS: Record<string, string> = {
  tls: "TLS", url: "URL", urls: "URLs", uri: "URI", dns: "DNS", cpu: "CPU", id: "ID", ids: "IDs",
  api: "API", ip: "IP", ips: "IPs", http: "HTTP", https: "HTTPS", jwt: "JWT", oidc: "OIDC",
  smtp: "SMTP", turn: "TURN", stun: "STUN", sfu: "SFU", rtc: "RTC", udp: "UDP", tcp: "TCP",
  mas: "MAS", sso: "SSO", saml: "SAML", ldap: "LDAP", ttl: "TTL", tcpip: "TCP/IP", ui: "UI",
  pvc: "PVC", dn: "DN", ca: "CA", sni: "SNI", hsts: "HSTS", cors: "CORS", json: "JSON", yaml: "YAML",
};

/** camelCase / kebab-case → "Title Case" for friendly labels. */
export function humanize(key: string): string {
  const spaced = key
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/[-_]/g, " ");
  const words = spaced.split(" ").map((w) => ACRONYMS[w.toLowerCase()] ?? w);
  const out = words.join(" ");
  return out.charAt(0).toUpperCase() + out.slice(1);
}

/** Read a dot-path from a nested object. */
export function getByPath(obj: unknown, path: string): unknown {
  if (!path) return obj;
  let cur: unknown = obj;
  for (const part of path.split(".")) {
    if (cur && typeof cur === "object" && part in (cur as Record<string, unknown>)) {
      cur = (cur as Record<string, unknown>)[part];
    } else {
      return undefined;
    }
  }
  return cur;
}

/** Top-level sections of the schema, in declared order. */
export function sections(schema: JSONSchema | undefined): string[] {
  if (!schema?.properties) return [];
  return Object.keys(schema.properties);
}

/** Count the leaf (editable) fields under a node — used for section badges. */
export function countLeaves(node: JSONSchema | undefined): number {
  if (!node) return 0;
  const kind = fieldKind(node);
  if (kind === "object" && node.properties) {
    return Object.values(node.properties).reduce((n, c) => n + countLeaves(c), 0);
  }
  return 1;
}

/** Keys a Kubernetes resource map always understands, offered even before a value is
 *  set: `resources.requests` and `resources.limits` are open maps in the schema, so
 *  without this there is nothing to type into until somebody writes YAML — which is how
 *  a Postgres asking for 4 Gi on an 8 Gi node stayed invisible in the form (etappe 107). */
const RESOURCE_KEYS = ["memory", "cpu"];

export interface Leaf { path: string; node: JSONSchema }

/** The entries of an open map that the form can render: the keys that have a value,
 *  plus the well-known ones for resource maps. The schema of each entry is the map's
 *  `additionalProperties`. */
export function mapEntries(node: JSONSchema | undefined, path: string, value: unknown): Leaf[] {
  if (!node || fieldKind(node) !== "freeform") return [];
  const entrySchema: JSONSchema =
    typeof node.additionalProperties === "object" ? node.additionalProperties : { type: "string" };
  if (fieldKind(entrySchema) === "object" || fieldKind(entrySchema) === "freeform") return [];
  const keys = new Set<string>();
  if (value && typeof value === "object" && !Array.isArray(value)) {
    for (const k of Object.keys(value as Record<string, unknown>)) keys.add(k);
  }
  if (/(^|\.)resources\.(requests|limits)$/.test(path)) RESOURCE_KEYS.forEach((k) => keys.add(k));
  return [...keys].sort().map((k) => ({ path: `${path}.${k}`, node: entrySchema }));
}

/** Every editable setting under a node, including the entries of open maps — so a
 *  search for "memory" finds Synapse and Postgres, not only Redis. */
export function collectLeaves(
  node: JSONSchema | undefined, path: string, values: unknown, acc: Leaf[] = [],
): Leaf[] {
  if (!node) return acc;
  if (fieldKind(node) === "object" && node.properties) {
    for (const [k, child] of Object.entries(node.properties)) collectLeaves(child, `${path}.${k}`, values, acc);
    return acc;
  }
  const entries = mapEntries(node, path, getByPath(values, path));
  if (entries.length > 0) acc.push(...entries);
  else if (path) acc.push({ path, node });
  return acc;
}
