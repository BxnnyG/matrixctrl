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

/** camelCase / kebab-case → "Title Case" for friendly labels. */
export function humanize(key: string): string {
  const spaced = key
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/[-_]/g, " ");
  return spaced.charAt(0).toUpperCase() + spaced.slice(1);
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
