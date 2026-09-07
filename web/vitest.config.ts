import { defineConfig } from "vitest/config";
import path from "path";

// Its own file, not a `test` block in vite.config.ts.
//
// vitest/config's defineConfig carries its own copy of vite's types, and mixing it with
// the plugins in the build config produces a type error about PluginOption that says
// nothing about anything real. Two files, no overlap, both typecheck.
//
// The environment is the point: vitest was here from the start and could only run pure
// logic, so every component shipped typechecked and otherwise unexercised (§4.94).
export default defineConfig({
  resolve: {
    alias: { "@": path.resolve(__dirname, "./src") },
  },
  test: {
    environment: "jsdom",
    globals: true,
    include: ["src/**/*.{test,spec}.{ts,tsx}"],
  },
});
