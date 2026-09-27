import { describe, expect, it } from "vitest";
import { collectLeaves, fieldKind, humanize, mapEntries, type JSONSchema } from "./schema";

// The shape of `resources` in the ESS schema: requests and limits are open maps whose
// values are Kubernetes quantities (`anyOf: [integer, string]`).
const quantity: JSONSchema = { anyOf: [{ type: "integer" }, { type: "string" }] };
const resources: JSONSchema = {
  type: "object",
  properties: {
    requests: { type: "object", additionalProperties: quantity },
    limits: { type: "object", additionalProperties: quantity },
  },
};
const schema: JSONSchema = {
  type: "object",
  properties: {
    synapse: { type: "object", properties: { resources, replicas: { type: "integer" } } },
    redis: { type: "object", properties: { maxMemory: { type: "string" } } },
  },
};

describe("humanize", () => {
  it("writes abbreviations in capitals", () => {
    expect(humanize("tlsEnabled")).toBe("TLS Enabled");
    expect(humanize("baseUrl")).toBe("Base URL");
    expect(humanize("oidcClientId")).toBe("OIDC Client ID");
  });
  it("leaves ordinary words alone", () => {
    expect(humanize("serverName")).toBe("Server Name");
    // "mask" contains "mas" but is not the word "mas".
    expect(humanize("subnetMask")).toBe("Subnet Mask");
  });
});

describe("fieldKind", () => {
  // Every memory and CPU value used to be "unknown" and so "nur via YAML".
  it("reads a Kubernetes quantity as a text field", () => {
    expect(fieldKind(quantity)).toBe("string");
  });
  it("still reports an open map as freeform", () => {
    expect(fieldKind(resources.properties!.requests)).toBe("freeform");
  });
});

describe("mapEntries", () => {
  it("offers memory and CPU for resource maps before any value is set", () => {
    const e = mapEntries(resources.properties!.requests, "synapse.resources.requests", undefined);
    expect(e.map((x) => x.path)).toEqual(["synapse.resources.requests.cpu", "synapse.resources.requests.memory"]);
    expect(fieldKind(e[0].node)).toBe("string");
  });
  it("includes keys that already have a value", () => {
    const e = mapEntries(resources.properties!.limits, "postgres.resources.limits", { memory: "4Gi", "ephemeral-storage": "1Gi" });
    expect(e.map((x) => x.path)).toContain("postgres.resources.limits.ephemeral-storage");
  });
  it("offers nothing for an unrelated empty map", () => {
    expect(mapEntries({ type: "object", additionalProperties: { type: "string" } }, "synapse.labels", undefined)).toEqual([]);
  });
});

describe("collectLeaves", () => {
  // The search that could not find the setting which took a server down.
  it("finds memory settings inside open maps", () => {
    const values = { synapse: { resources: { requests: { memory: "4Gi" } } } };
    const hits = collectLeaves(schema.properties!.synapse, "synapse", values)
      .map((l) => l.path)
      .filter((p) => p.includes("memory"))
      .sort();
    expect(hits).toEqual(["synapse.resources.limits.memory", "synapse.resources.requests.memory"]);
  });
  it("still returns plain fields", () => {
    const paths = collectLeaves(schema.properties!.redis, "redis", {}).map((l) => l.path);
    expect(paths).toEqual(["redis.maxMemory"]);
  });
});
