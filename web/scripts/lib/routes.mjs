// The panel's routes, read from the router's generated table (etappe 121, DESIGN §4.130).
//
// verify-ui.mjs used to keep its own list and it drifted by eight routes within a few
// etappes (and once before, §4.48). The generated table is the only complete answer, so
// both browser runs read their paths from here instead of from a list.

import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const ROUTE_TREE = path.join(path.dirname(fileURLToPath(import.meta.url)), "../../src/routeTree.gen.ts");

/** Every full path the router knows, trailing slash removed, `$param` segments kept. */
export async function routerPaths() {
  const tree = await readFile(ROUTE_TREE, "utf8");
  const block = tree.match(/export interface FileRoutesByFullPath \{([\s\S]*?)\n\}/);
  if (!block) throw new Error("cannot read FileRoutesByFullPath from routeTree.gen.ts");
  return [...new Set([...block[1].matchAll(/^\s*'([^']+)':/gm)].map((m) => m[1].replace(/(.)\/$/, "$1")))];
}

/** Routes no browser run can open, with the reason. */
export const EXCLUDE = {
  "/auth/callback": "needs an authorisation code from the identity provider; there is no test IdP (etappe 121, out of scope)",
};

/** A file-system-safe name for screenshots. */
export const nameOf = (p) => (p === "/" ? "dashboard" : p.replace(/^\//, "").replace(/[^A-Za-z0-9]+/g, "-"));
