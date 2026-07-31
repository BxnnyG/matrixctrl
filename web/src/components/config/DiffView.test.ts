import { describe, it, expect } from "vitest";
import { parseDiff } from "./DiffView";

// A real diff as internal/git.Diff() now emits it.
const SAMPLE = `--- a/synapse.yaml
+++ b/synapse.yaml
@@ -3,4 +3,5 @@
 synapse:
   replicas: 1
-  logLevel: INFO
+  logLevel: DEBUG
+  workers: 2
   ingress:
`;

describe("parseDiff", () => {
  it("parses a file with one hunk", () => {
    const files = parseDiff(SAMPLE);
    expect(files).toHaveLength(1);
    expect(files[0].displayName).toBe("synapse.yaml");
    expect(files[0].hunks).toHaveLength(1);
  });

  it("classifies added, removed and context lines", () => {
    const lines = parseDiff(SAMPLE)[0].hunks[0].lines;
    expect(lines.filter((l) => l.type === "add").map((l) => l.content))
      .toEqual(["  logLevel: DEBUG", "  workers: 2"]);
    expect(lines.filter((l) => l.type === "remove").map((l) => l.content))
      .toEqual(["  logLevel: INFO"]);
    expect(lines.filter((l) => l.type === "context").length).toBeGreaterThan(0);
  });

  it("numbers old and new lines from the hunk header", () => {
    const lines = parseDiff(SAMPLE)[0].hunks[0].lines;
    expect(lines[0]).toMatchObject({ type: "context", oldNum: 3, newNum: 3 });
    // A removed line advances only the old counter, an added line only the new.
    const removed = lines.find((l) => l.type === "remove")!;
    const added = lines.find((l) => l.type === "add")!;
    expect(removed.newNum).toBeUndefined();
    expect(added.oldNum).toBeUndefined();
  });

  it("handles several files in one diff", () => {
    const two = SAMPLE + `--- a/general.yaml
+++ b/general.yaml
@@ -1,1 +1,1 @@
-serverName: old.example.com
+serverName: new.example.com
`;
    expect(parseDiff(two).map((f) => f.displayName))
      .toEqual(["synapse.yaml", "general.yaml"]);
  });

  // The regression: the backend used to emit no @@ headers, so everything
  // between the file markers was dropped and the viewer rendered an empty diff.
  it("yields no lines when hunk headers are missing", () => {
    const headerless = "--- a/x.yaml\n+++ b/x.yaml\n-old\n+new\n";
    const files = parseDiff(headerless);
    expect(files[0].hunks).toHaveLength(0);
  });

  it("returns nothing for empty input", () => {
    expect(parseDiff("")).toEqual([]);
  });
});
